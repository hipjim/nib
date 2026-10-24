package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

// A doc is an open file or folder listing with its own cursor, scroll
// position and undo history. Files stay open in the background.
type doc struct {
	buf              *Buffer
	path             string
	hl               *highlighter
	cursor           Pos
	anchor           Pos // selection is [anchor, cursor]; empty when equal
	goal             int // visual column kept across vertical moves; -1 if unset
	scrollY, scrollX float32
	dir              *dirView // set for a folder listing
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
	e.show(newDoc(path, b))
}

// openDir shows the listing of a folder with the cursor on the entry named
// focus, or the first one. There is one listing; it moves between folders.
func (e *Editor) openDir(path, focus string) {
	b, v, l, err := listDir(path)
	if err != nil {
		e.flash(err.Error(), true)
		return
	}
	var d *doc
	for _, x := range e.docs {
		if x.dir != nil {
			d = x
		}
	}
	if d == nil {
		d = &doc{goal: -1}
	}
	d.buf, d.path, d.dir = b, path, v
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
	e.show(d)
}

// entry returns the listing entry on line, if any.
func (e *Editor) entry(line int) (dirEntry, bool) {
	i := line - dirHeader
	if e.dir == nil || i < 0 || i >= len(e.dir.entries) {
		return dirEntry{}, false
	}
	return e.dir.entries[i], true
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
