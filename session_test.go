package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestQuitGuard(t *testing.T) {
	e := newTestEditor("a", Pos{})
	e.docs = []*doc{e.doc}
	if !e.MayClose() {
		t.Fatal("refused to close with nothing unsaved")
	}
	e.insert("x")
	if e.MayClose() || !strings.Contains(e.msg, "1 file has unsaved changes") {
		t.Fatalf("closed with unsaved changes (msg %q)", e.msg)
	}
	if !e.MayClose() {
		t.Fatal("second close was refused")
	}

	quitOK.Store(false)
	if mayQuit() || !quitAsked.Swap(false) {
		t.Fatal("app quit without asking the editor")
	}
	quitOK.Store(true)
	if !mayQuit() {
		t.Fatal("app may not quit")
	}
}

func writeFile(t *testing.T, path, s string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSessionRestore(t *testing.T) {
	dir := t.TempDir()
	a, b, c := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt"), filepath.Join(dir, "c.txt")
	writeFile(t, a, "1\n2\n3\n4")
	writeFile(t, b, "hello\nworld")
	writeFile(t, c, "sea")
	sess := filepath.Join(dir, "conf", "session.json")

	e := NewEditor(a)
	e.sessionPath = sess
	e.cursor = Pos{3, 1}
	e.open(b)
	e.cursor = Pos{1, 2}
	e.saveSession(true)

	// No arguments: both files come back, b on top, with their cursors.
	e = NewEditor("")
	e.sessionPath = sess
	e.restoreSession()
	if len(e.docs) != 2 || e.path != b || e.cursor != (Pos{1, 2}) {
		t.Fatalf("restored %d docs, %s at %v", len(e.docs), e.path, e.cursor)
	}
	if e.docs[1].path != a || e.docs[1].cursor != (Pos{3, 1}) {
		t.Fatalf("second doc %s at %v", e.docs[1].path, e.docs[1].cursor)
	}

	// Started on a file: the old session is kept behind it, not lost.
	e = NewEditor(c)
	e.sessionPath = sess
	e.keepSession()
	e.saveSession(true)
	s, _ := readSession(sess)
	var paths []string
	for _, p := range s.Docs {
		paths = append(paths, p.Path)
	}
	if !slices.Equal(paths, []string{c, b, a}) {
		t.Fatalf("session has %v", paths)
	}

	// A deleted file is skipped.
	os.Remove(b)
	e = NewEditor("")
	e.sessionPath = sess
	e.restoreSession()
	if len(e.docs) != 2 || e.path != c {
		t.Fatalf("after delete: %d docs, top %s", len(e.docs), e.path)
	}
}

// touch makes sure the next write gets a new modification time.
func touch(t *testing.T, path, s string) {
	t.Helper()
	writeFile(t, path, s)
	later := time.Now().Add(2 * time.Second)
	os.Chtimes(path, later, later)
}

func TestReloadOnChange(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "app.log")
	writeFile(t, p, "one\ntwo")
	e := NewEditor(p)
	e.cursor, e.anchor = Pos{0, 2}, Pos{0, 2}

	touch(t, p, "one!\ntwo\nthree")
	e.checkDisk()
	e.applyPending()
	if content(e) != "one!\ntwo\nthree" || e.cursor != (Pos{0, 2}) {
		t.Fatalf("reload: %q at %v", content(e), e.cursor)
	}

	// On the last line, the cursor follows new lines like tail -f.
	e.cursor, e.anchor = Pos{2, 0}, Pos{2, 0}
	touch(t, p, "one!\ntwo\nthree\nfour\nfive")
	e.checkDisk()
	e.applyPending()
	if e.cursor.Line != 4 {
		t.Fatalf("tail: cursor on line %d", e.cursor.Line)
	}

	// Edited here: keep the edits and warn instead.
	e.insert("X")
	touch(t, p, "replaced")
	e.checkDisk()
	if !e.conflict || !strings.HasPrefix(content(e), "one!") || !strings.Contains(e.msg, "changed on disk") {
		t.Fatalf("conflict: %v %q %q", e.conflict, content(e), e.msg)
	}
	e.save(p)
	if e.conflict || e.stamp != statStamp(p) {
		t.Fatal("save did not clear the conflict")
	}

	// A listing shows new files.
	e.open(dir)
	writeFile(t, filepath.Join(dir, "new.txt"), "")
	later := time.Now().Add(4 * time.Second)
	os.Chtimes(dir, later, later)
	e.checkDisk()
	if !strings.Contains(content(e), "new.txt") {
		t.Fatalf("listing not refreshed:\n%s", content(e))
	}
}
