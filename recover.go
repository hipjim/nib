// Recovery: unsaved work survives a crash, a power cut or a killed process.
// While a document has unsaved changes a file in the recovery folder holds
// them, and the next start with no arguments brings them back.
//
// A small document is written whole. A big one must not be copied for one
// edit, so it is written as its edits since the last save, which replay onto
// the file on disk as long as that is still the same file. Replaying them
// through the undo stack means undo keeps working afterwards.
//
// The files are written off the main thread at most every recoverEvery, or
// less often if writing takes long, as after replacing millions of lines. Only
// what changed is written, so idle frames cost one comparison per document.

package main

import (
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

const (
	recoverEvery = 2 * time.Second

	// Below these a document is cheap enough to write whole on every change,
	// and then its unsaved text survives the file changing on disk.
	recoverFullBytes = 1 << 20
	recoverFullLines = 1 << 15
)

type recoverEdit struct {
	Line, Col int
	Old, New  string
	Lines     []recoverLine // set instead for a replace-all
}

type recoverLine struct {
	Line int
	New  string
}

// recoverFile is gob, not JSON: a text file may hold bytes that are not UTF-8.
type recoverFile struct {
	Path     string
	BaseMod  int64
	BaseSize int64
	Whole    bool
	Text     string
	Edits    []recoverEdit
	Line     int
	Col      int
	Scroll   float64
}

type recoverer struct {
	files map[string]bool // what this editor has in the folder
	at    time.Time
	every atomic.Int64 // how long to wait between writes, in nanoseconds
	busy  atomic.Bool
	wg    sync.WaitGroup
}

func (e *Editor) recoverDir() string {
	if e.sessionPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(e.sessionPath), "recovery")
}

// sinceSave is the edits made after the last save, oldest first. It is false
// if the saved state is no longer on the undo stack, which happens after
// undoing past a save and typing something else.
func (b *Buffer) sinceSave() ([]edit, bool) {
	if b.saved == 0 {
		return slices.Clone(b.undo), true
	}
	i := slices.IndexFunc(b.undo, func(u edit) bool { return u.id == b.saved })
	if i < 0 {
		return nil, false
	}
	return slices.Clone(b.undo[i+1:]), true
}

// replay applies edits saved by recovery as new undo steps.
func (b *Buffer) replay(edits []recoverEdit) {
	for _, r := range edits {
		if r.Lines == nil {
			start := Pos{r.Line, r.Col}
			b.Replace(start, b.Clamp(endOf(start, r.Old)), r.New, start)
			continue
		}
		changes := make([]lineEdit, len(r.Lines))
		for i, l := range r.Lines {
			changes[i] = lineEdit{line: l.Line, old: b.Line(l.Line), new: l.New}
		}
		b.commitLines(changes, Pos{})
	}
}

// snapshot is what must be written for d, or false if it cannot be expressed.
// edits are converted by the writer, since a replace-all may hold millions of
// lines; the strings and line lists are never written in place, so it is safe.
func snapshot(d *doc) (r recoverFile, edits []edit, ok bool) {
	b := d.buf
	r = recoverFile{Path: d.path, BaseMod: d.base.mod, BaseSize: d.base.size,
		Line: d.cursor.Line, Col: d.cursor.Col, Scroll: d.scrollY}
	if len(b.lines.data) < recoverFullBytes && b.LineCount() < recoverFullLines {
		r.Whole = true
		r.Text = b.Text(Pos{}, b.End())
		return r, nil, true
	}
	edits, ok = b.sinceSave()
	return r, edits, ok
}

func (r *recoverFile) addEdits(edits []edit) {
	for _, u := range edits {
		x := recoverEdit{Line: u.start.Line, Col: u.start.Col, Old: u.old, New: u.new}
		if u.lines != nil {
			x.Lines = make([]recoverLine, len(u.lines))
			for i, l := range u.lines {
				x.Lines[i] = recoverLine{l.line, l.new}
			}
		}
		r.Edits = append(r.Edits, x)
	}
}

func writeRecover(dir, name string, r recoverFile) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, name+".tmp*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := gob.NewEncoder(tmp).Encode(r); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, name))
}

func recoverName(d *doc) string {
	if d.path == "" {
		return fmt.Sprintf("scratch-%d", time.Now().UnixNano())
	}
	h := sha256.Sum256([]byte(d.path))
	return hex.EncodeToString(h[:12])
}

// saveRecovery brings the recovery folder up to date with the open documents.
// force waits for the writing to finish; otherwise it happens in the
// background, and the result says a change is waiting for its turn.
func (e *Editor) saveRecovery(force bool) bool {
	dir := e.recoverDir()
	if dir == "" {
		return false
	}
	rc := &e.recover
	if force {
		rc.wg.Wait()
	} else if rc.busy.Load() {
		// A write under way could put back a file removed now.
		return e.recoverPending()
	}
	for name := range rc.files {
		if !slices.ContainsFunc(e.docs, func(d *doc) bool { return d.recName == name && d.buf.Dirty() }) {
			os.Remove(filepath.Join(dir, name))
			delete(rc.files, name)
			for _, d := range e.docs {
				if d.recName == name {
					d.recName = ""
				}
			}
		}
	}
	if !e.recoverPending() {
		return false
	}
	if !force && time.Since(rc.at) < max(recoverEvery, time.Duration(rc.every.Load())) {
		return true
	}
	rc.at = time.Now()
	type job struct {
		name  string
		r     recoverFile
		edits []edit
	}
	var jobs []job
	for _, d := range e.docs {
		if !d.buf.Dirty() || d.dir != nil || d.recVer == d.buf.version && d.recName != "" {
			continue
		}
		d.recVer = d.buf.version
		r, edits, ok := snapshot(d)
		if !ok {
			continue
		}
		if d.recName == "" {
			d.recName = recoverName(d)
		}
		if rc.files == nil {
			rc.files = map[string]bool{}
		}
		rc.files[d.recName] = true
		jobs = append(jobs, job{d.recName, r, edits})
	}
	rc.busy.Store(true)
	rc.wg.Add(1)
	write := func() {
		defer rc.wg.Done()
		defer rc.busy.Store(false)
		start := time.Now()
		for _, j := range jobs {
			j.r.addEdits(j.edits)
			writeRecover(dir, j.name, j.r)
		}
		rc.every.Store(int64(10 * time.Since(start))) // at most a tenth of the time
	}
	if force {
		write()
	} else {
		go write()
	}
	return false
}

func (e *Editor) recoverPending() bool {
	return slices.ContainsFunc(e.docs, func(d *doc) bool {
		return d.dir == nil && d.buf.Dirty() && d.recVer != d.buf.version
	})
}

// clearRecovery removes this editor's files on an orderly exit, when anything
// unsaved was offered a chance and refused. A crash never gets here.
func (e *Editor) clearRecovery() {
	e.recover.wg.Wait()
	for name := range e.recover.files {
		os.Remove(filepath.Join(e.recoverDir(), name))
	}
	e.recover.files = nil
}

func readRecover(path string) (recoverFile, bool) {
	var r recoverFile
	f, err := os.Open(path)
	if err != nil {
		return r, false
	}
	defer f.Close()
	return r, gob.NewDecoder(f).Decode(&r) == nil
}

// restoreRecovery brings back what the recovery folder holds. A file left
// behind by a crash is the only copy of that work, so one that cannot be
// applied stays in the folder instead of being deleted.
func (e *Editor) restoreRecovery() {
	dir := e.recoverDir()
	if dir == "" {
		return
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	top := e.doc
	restored, lost := 0, 0
	for _, ent := range ents {
		r, ok := readRecover(filepath.Join(dir, ent.Name()))
		if !ok {
			continue
		}
		if !r.Whole && statStamp(r.Path) != (stamp{r.BaseMod, r.BaseSize, true}) {
			lost++
			continue
		}
		if r.Path == "" {
			e.show(newDoc("", NewBuffer("")))
		} else {
			e.open(r.Path)
			if e.path != r.Path || e.dir != nil {
				lost++
				continue
			}
		}
		for e.buf.Poll() {
			time.Sleep(time.Millisecond)
		}
		e.doc.loadAt = time.Time{}
		e.doc.pending = nil
		if r.Whole {
			if r.Text != e.buf.Text(Pos{}, e.buf.End()) {
				e.buf.Replace(Pos{}, e.buf.End(), r.Text, Pos{})
			}
			st := statStamp(r.Path)
			e.doc.conflict = r.Path != "" && st.ok && st != (stamp{r.BaseMod, r.BaseSize, true})
		} else {
			e.buf.replay(r.Edits)
		}
		e.cursor = e.buf.Clamp(Pos{r.Line, r.Col})
		e.anchor = e.cursor
		e.scrollY = r.Scroll
		e.doc.recName, e.doc.recVer = ent.Name(), e.buf.version
		if e.recover.files == nil {
			e.recover.files = map[string]bool{}
		}
		e.recover.files[ent.Name()] = true
		if e.buf.Dirty() {
			restored++
		}
	}
	if slices.Contains(e.docs, top) && e.doc != top {
		e.show(top)
	}
	switch {
	case lost > 0:
		e.flash(fmt.Sprintf("unsaved changes in %s could not be restored · kept in %s", plural(int64(lost), "file"), shortPath(dir)), true)
	case restored > 0:
		e.flash("restored unsaved changes in "+plural(int64(restored), "file"), false)
	}
}
