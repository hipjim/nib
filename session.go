package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"time"
)

type place struct {
	Path   string  `json:"path"`
	Line   int     `json:"line"`
	Col    int     `json:"col"`
	Scroll float64 `json:"scroll"`
	Idle   int     `json:"idle,omitempty"` // sessions restored but not used
	Tail   bool    `json:"tail,omitempty"`
	End    bool    `json:"-"` // follow the end of the file
	Select int     `json:"-"` // select this many bytes from Col
}

type session struct {
	Root  string  `json:"root"`
	Light bool    `json:"light,omitempty"`
	Docs  []place `json:"docs"` // most recent first
}

// maxSession is how many documents a session remembers, and maxIdle how
// many sessions in a row a restored file may go unused before it is dropped,
// so files opened once by accident do not come back forever.
const (
	maxSession = 20
	maxIdle    = 2
)

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
// once a second unless forced. Unsaved edits are not kept, and neither are
// folder listings: they are a way to a file, not work to come back to, and
// a session of only listings would hide the start screen. It reports
// whether a change is waiting to be written.
func (e *Editor) saveSession(force bool) bool {
	if e.sessionPath == "" {
		return false
	}
	s := session{Root: e.root, Light: e.light}
	open := map[string]bool{}
	for _, d := range e.docs {
		idle := d.idle + 1
		if d.used || d.buf.Dirty() {
			idle = 0
		}
		if d.path != "" && d.dir == nil && idle < maxIdle && len(s.Docs) < maxSession {
			s.Docs = append(s.Docs, place{Path: d.path, Line: d.cursor.Line, Col: d.cursor.Col, Scroll: d.scrollY, Idle: idle, Tail: d.tail != nil})
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
		e.setLight(s.Light)
	}
}

func (e *Editor) restoreSession() {
	defer e.restoreRecovery()
	s, ok := readSession(e.sessionPath)
	if !ok {
		return
	}
	if fi, err := os.Stat(s.Root); err == nil && fi.IsDir() {
		e.root = s.Root
	}
	e.setLight(s.Light)
	e.restoring = true
	defer func() { e.restoring = false }()
	for _, p := range slices.Backward(s.Docs) {
		if fi, err := os.Stat(p.Path); err != nil || fi.IsDir() {
			continue // gone, or a folder listing from an older session
		}
		e.open(p.Path)
		if e.path == filepath.Clean(p.Path) {
			e.idle = p.Idle
			e.pending = &p
			e.applyPending()
			if p.Tail {
				e.startTail()
			}
		}
	}
	e.doc.used = true // the one on screen stays
	e.msg = ""
}

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
