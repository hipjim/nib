package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// A doc is an open file or folder listing with its own cursor, scroll
// position and undo history. Files stay open in the background.
type doc struct {
	buf      *Buffer
	path     string
	hl       *highlighter
	cursor   Pos
	anchor   Pos     // selection is [anchor, cursor]; empty when equal
	goal     int     // visual column kept across vertical moves; -1 if unset
	scrollY  float64 // pixels; float32 is too coarse past ~16M px
	scrollX  float32
	dir      *dirView // set for a folder listing
	stamp    stamp    // the file on disk when last read or written
	conflict bool     // changed on disk while edited here
	pending  *place   // cursor to restore once loading finishes
}

func newDoc(path string, b *Buffer) *doc {
	return &doc{buf: b, path: path, hl: newHighlighter(path), goal: -1}
}

// show makes d the current document.
func (e *Editor) show(d *doc) {
	// An untouched scratch buffer goes away once something else opens.
	if c := e.doc; c != nil && c != d && c.path == "" && !c.buf.Dirty() && c.buf.LineCount() == 1 && c.buf.Line(0) == "" {
		e.docs = slices.DeleteFunc(e.docs, func(x *doc) bool { return x == c })
	}
	e.docs = slices.DeleteFunc(e.docs, func(x *doc) bool { return x == d })
	e.docs = slices.Insert(e.docs, 0, d)
	e.doc = d
	e.followCursor = true
}

func (e *Editor) open(path string) {
	path = expandHome(path)
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if fi, err := os.Stat(path); err == nil && fi.IsDir() {
		e.root = path
		e.openDir(path, "")
		return
	}
	for _, d := range e.docs {
		if d.path == path && d.dir == nil {
			e.show(d)
			return
		}
	}
	b, err := Load(path)
	switch {
	case os.IsNotExist(err):
		b = NewBuffer("")
		e.flash("new file", false)
	case err != nil:
		e.flash(err.Error(), true)
		return
	default:
		e.flash(fmt.Sprintf("opened %d lines", b.LineCount()), false)
	}
	d := newDoc(path, b)
	d.stamp = statStamp(path)
	e.show(d)
}

// openDir shows the listing of a folder with the cursor on the entry named
// focus, or the first one. There is one listing; it moves between folders.
func (e *Editor) openDir(path, focus string) {
	var d *doc
	for _, x := range e.docs {
		if x.dir != nil {
			d = x
		}
	}
	if d == nil {
		d = &doc{goal: -1}
	}
	if e.fillDir(d, path, focus) {
		e.show(d)
	}
}

// fillDir puts the listing of path into d.
func (e *Editor) fillDir(d *doc, path, focus string) bool {
	b, v, l, err := listDir(path)
	if err != nil {
		e.flash(err.Error(), true)
		return false
	}
	d.buf, d.path, d.dir, d.stamp = b, path, v, statStamp(path)
	d.hl = &highlighter{lang: l, ck: []int{0}, guess: map[int]int{}}
	d.cursor = Pos{dirHeader, 0}
	if len(v.entries) > 1 && v.entries[0].name == ".." {
		d.cursor.Line++
	}
	for i, en := range v.entries {
		if en.name == focus {
			d.cursor.Line = dirHeader + i
		}
	}
	d.anchor, d.goal = d.cursor, -1
	d.scrollX, d.scrollY = 0, 0
	return true
}

// entry returns the listing entry on line, if any.
func (e *Editor) entry(line int) (dirEntry, bool) { return e.entryOf(e.doc, line) }

func (e *Editor) entryOf(d *doc, line int) (dirEntry, bool) {
	i := line - dirHeader
	if d.dir == nil || i < 0 || i >= len(d.dir.entries) {
		return dirEntry{}, false
	}
	return d.dir.entries[i], true
}

// activate opens the listing entry on line.
func (e *Editor) activate(line int) {
	en, ok := e.entry(line)
	switch {
	case !ok:
	case en.name == "..":
		e.up()
	case en.dir:
		e.openDir(en.path, "")
	default:
		e.open(en.path)
	}
}

// typePause is how long typing in a listing may pause before it starts
// over, as in Finder.
const typePause = time.Second

// typeTo adds s to what was typed in the listing and jumps to the entry
// it names.
func (e *Editor) typeTo(now time.Time, s string) {
	if now.Sub(e.typedAt) > typePause {
		e.typed = ""
	}
	e.typed += s
	e.typedAt = now
	e.jumpTyped()
}

// untype takes back the last letter typed in the listing. It reports false
// if nothing was being typed.
func (e *Editor) untype(now time.Time) bool {
	if e.typed == "" || now.Sub(e.typedAt) > typePause {
		e.typed = ""
		return false
	}
	_, n := utf8.DecodeLastRuneInString(e.typed)
	e.typed = e.typed[:len(e.typed)-n]
	e.typedAt = now
	if e.typed != "" {
		e.jumpTyped()
	}
	return true
}

func (e *Editor) jumpTyped() {
	if i := matchEntry(e.dir.entries, e.typed); i >= 0 {
		e.moveTo(Pos{dirHeader + i, 0}, false)
		e.flash("→ "+e.typed, false)
	} else {
		e.flash(e.typed+" · no match", true)
	}
}

// matchEntry is the first entry whose name starts with q, ignoring case,
// or else the first that contains it; -1 if none does.
func matchEntry(ents []dirEntry, q string) int {
	q = strings.ToLower(q)
	found := -1
	for i, en := range ents {
		if en.name == ".." {
			continue
		}
		name := strings.ToLower(en.name)
		if strings.HasPrefix(name, q) {
			return i
		}
		if found < 0 && strings.Contains(name, q) {
			found = i
		}
	}
	return found
}

// up shows the parent folder of the listing, or the folder of the file.
func (e *Editor) up() {
	switch {
	case e.dir != nil:
		if parent := filepath.Dir(e.path); parent != e.path {
			e.openDir(parent, filepath.Base(e.path))
		}
	case e.path != "":
		e.openDir(filepath.Dir(e.path), filepath.Base(e.path))
	default:
		e.openDir(e.root, "")
	}
}

// closeDoc closes the current document and shows the previous one.
func (e *Editor) closeDoc(discardOK bool) {
	if e.buf.Dirty() && !discardOK {
		e.flash("unsaved changes · press again to close", true)
		e.discardOK = true
		return
	}
	e.docs = e.docs[1:]
	if len(e.docs) == 0 {
		e.docs = []*doc{newDoc("", NewBuffer(""))}
	}
	e.doc = e.docs[0]
	e.followCursor = true
}

// openFinder shows the file finder for the root folder.
func (e *Editor) openFinder() {
	f := &e.finder
	f.start(e.root)
	f.recent = f.recent[:0]
	for _, d := range e.docs[1:] {
		if d.dir == nil && d.path != "" {
			if rel, err := filepath.Rel(e.root, d.path); err == nil {
				f.recent = append(f.recent, filepath.ToSlash(rel))
			}
		}
	}
	f.query, f.sel = "", 0
	f.rank()
	e.openPrompt(promptFiles, "")
}

// otherUnsaved counts background documents with unsaved changes.
func (e *Editor) otherUnsaved() int {
	n := 0
	for _, d := range e.docs[1:] {
		if d.buf.Dirty() {
			n++
		}
	}
	return n
}
