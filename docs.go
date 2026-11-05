package main

import (
	"fmt"
	"image"
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
	dir      *dirView        // set for a folder listing
	stamp    stamp           // the file on disk when last read or written
	conflict bool            // changed on disk while edited here
	pending  *place          // cursor to restore once loading finishes
	opened   int             // order of opening, for the tab bar
	used     bool            // shown by the user this session, not just restored
	idle     int             // sessions in a row restored but not used
	loadAt   time.Time       // when a file still being indexed was opened
	closeR   image.Rectangle // the tab's ×, where it was last drawn
}

func openedMsg(lines int, d time.Duration) string {
	return "opened " + plural(int64(lines), "line") + tookText(d)
}

func tookText(d time.Duration) string {
	switch {
	case d < 10*time.Millisecond:
		return ""
	case d < time.Second:
		return fmt.Sprintf(" in %d ms", d.Milliseconds())
	}
	return fmt.Sprintf(" in %.1f s", d.Seconds())
}

func newDoc(path string, b *Buffer) *doc {
	return &doc{buf: b, path: path, hl: newHighlighter(path), goal: -1}
}

func (e *Editor) show(d *doc) {
	// An untouched scratch buffer goes away once something else opens.
	if c := e.doc; c != nil && c != d && c.path == "" && !c.buf.Dirty() && c.buf.LineCount() == 1 && c.buf.Line(0) == "" {
		e.docs = slices.DeleteFunc(e.docs, func(x *doc) bool { return x == c })
	}
	e.docs = slices.DeleteFunc(e.docs, func(x *doc) bool { return x == d })
	e.docs = slices.Insert(e.docs, 0, d)
	e.doc = d
	e.followCursor = true
	if d.opened == 0 {
		e.nextOpen++
		d.opened = e.nextOpen
	}
	if !e.restoring {
		d.used = true
	}
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
	t0 := time.Now()
	b, err := Load(path)
	d := newDoc(path, b)
	switch {
	case os.IsNotExist(err):
		d.buf = NewBuffer("")
		e.flash("new file", false)
	case err != nil:
		e.flash(err.Error(), true)
		return
	case b.Loading():
		d.loadAt = t0 // reported once the lines are indexed
	default:
		e.flash(openedMsg(b.LineCount(), time.Since(t0)), false)
	}
	d.stamp = statStamp(path)
	e.show(d)
}

// There is one listing; it moves between folders.
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

func (e *Editor) entry(line int) (dirEntry, bool) { return e.entryOf(e.doc, line) }

func (e *Editor) entryOf(d *doc, line int) (dirEntry, bool) {
	i := line - dirHeader
	if d.dir == nil || i < 0 || i >= len(d.dir.entries) {
		return dirEntry{}, false
	}
	return d.dir.entries[i], true
}

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

func (e *Editor) typeTo(now time.Time, s string) {
	if now.Sub(e.typedAt) > typePause {
		e.typed = ""
	}
	e.typed += s
	e.typedAt = now
	e.jumpTyped()
}

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
	e.doc.used = true
	e.followCursor = true
}

func (e *Editor) openFinder() {
	f := &e.finder
	f.start(e.root)
	f.recent = f.recent[:0]
	for _, d := range e.docs[1:] {
		if d.dir == nil && d.path != "" {
			f.recent = append(f.recent, finderPath(e.root, d.path))
		}
	}
	f.query, f.sel = "", 0
	f.rank()
	e.openPrompt(promptFiles, "")
}

func (e *Editor) otherUnsaved() int {
	n := 0
	for _, d := range e.docs[1:] {
		if d.buf.Dirty() {
			n++
		}
	}
	return n
}
