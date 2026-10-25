package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// A place is where a document was left.
type place struct {
	Path   string  `json:"path"`
	Line   int     `json:"line"`
	Col    int     `json:"col"`
	Scroll float64 `json:"scroll"`
	End    bool    `json:"-"` // follow the end of the file
	Select int     `json:"-"` // select this many bytes from Col
}

type session struct {
	Root string  `json:"root"`
	Docs []place `json:"docs"` // most recent first
}

// maxSession is how many documents a session remembers.
const maxSession = 20

func sessionFile() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	p := filepath.Join(dir, "nib", "session.json")
	if _, err := os.Stat(p); os.IsNotExist(err) {
		os.Rename(filepath.Join(dir, "quod"), filepath.Join(dir, "nib")) // the old name
	}
	return p
}

// saveSession writes the open documents and where they were left, at most
// once a second unless forced. Unsaved edits are not kept. It reports
// whether a change is waiting to be written.
func (e *Editor) saveSession(force bool) bool {
	if e.sessionPath == "" {
		return false
	}
	s := session{Root: e.root}
	open := map[string]bool{}
	for _, d := range e.docs {
		if d.path != "" && len(s.Docs) < maxSession {
			s.Docs = append(s.Docs, place{Path: d.path, Line: d.cursor.Line, Col: d.cursor.Col, Scroll: d.scrollY})
			open[d.path] = true
		}
	}
	for _, p := range e.keep {
		if !open[p.Path] && len(s.Docs) < maxSession {
			s.Docs = append(s.Docs, p)
		}
	}
	data, _ := json.MarshalIndent(s, "", "  ")
	if bytes.Equal(data, e.sessionData) {
		return false
	}
	if !force && time.Since(e.sessionAt) < time.Second {
		return true
	}
	e.sessionAt = time.Now()
	e.sessionData = data
	os.MkdirAll(filepath.Dir(e.sessionPath), 0o755)
	tmp := e.sessionPath + ".tmp"
	if os.WriteFile(tmp, data, 0o644) == nil {
		os.Rename(tmp, e.sessionPath)
	}
	return false
}

func readSession(path string) (session, bool) {
	var s session
	data, err := os.ReadFile(path)
	return s, err == nil && json.Unmarshal(data, &s) == nil
}

// keepSession carries the last session's documents into the next one
// without opening them, when nib was started on a file.
func (e *Editor) keepSession() {
	if s, ok := readSession(e.sessionPath); ok {
		e.keep = s.Docs
	}
}

// restoreSession reopens the documents of the last session.
func (e *Editor) restoreSession() {
	s, ok := readSession(e.sessionPath)
	if !ok {
		return
	}
	if fi, err := os.Stat(s.Root); err == nil && fi.IsDir() {
		e.root = s.Root
	}
	for _, p := range slices.Backward(s.Docs) {
		if _, err := os.Stat(p.Path); err != nil {
			continue
		}
		e.open(p.Path)
		if e.path == filepath.Clean(p.Path) {
			e.pending = &p
			e.applyPending()
		}
	}
	e.msg = ""
}

// applyPending puts the cursor back once the document has loaded.
func (e *Editor) applyPending() {
	p := e.pending
	if p == nil || e.buf.Loading() {
		return
	}
	e.pending = nil
	if p.End {
		e.cursor = e.buf.End()
		e.cursor.Col = 0
		e.followCursor = true
	} else if p.Select > 0 {
		e.anchor = e.buf.Clamp(Pos{p.Line, p.Col})
		e.cursor = e.buf.Clamp(Pos{p.Line, p.Col + p.Select})
		e.followCursor = true
		return
	} else {
		e.cursor = e.buf.Clamp(Pos{p.Line, p.Col})
		e.scrollY = p.Scroll
		e.followCursor = false
	}
	e.anchor = e.cursor
}
