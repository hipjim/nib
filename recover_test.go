package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func recoverEditor(t *testing.T, path string) (*Editor, string) {
	t.Helper()
	sess := filepath.Join(t.TempDir(), "session.json")
	e := NewEditor(path)
	e.sessionPath = sess
	return e, sess
}

// restart is what the next start does after this process vanished.
func restart(sess string) *Editor {
	e := NewEditor("")
	e.sessionPath = sess
	e.restoreSession()
	return e
}

func recoverCount(t *testing.T, sess string) int {
	t.Helper()
	ents, err := os.ReadDir(filepath.Join(filepath.Dir(sess), "recovery"))
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return len(ents)
}

func TestRecoverSmallFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.txt")
	writeFile(t, p, "one\ntwo")
	e, sess := recoverEditor(t, p)
	e.moveTo(Pos{1, 3}, false)
	e.insert("!")
	e.saveRecovery(true)

	e = restart(sess)
	if e.path != p || content(e) != "one\ntwo!" || !e.buf.Dirty() || e.cursor != (Pos{1, 4}) {
		t.Fatalf("restored %q at %v, dirty %v, path %q", content(e), e.cursor, e.buf.Dirty(), e.path)
	}
	if !strings.Contains(e.msg, "restored") {
		t.Fatalf("msg %q", e.msg)
	}
	e.undo(false)
	if content(e) != "one\ntwo" {
		t.Fatalf("undo: %q", content(e))
	}

	// Saving makes the recovery file unnecessary.
	e.undo(true)
	e.save(p)
	e.saveRecovery(true)
	if n := recoverCount(t, sess); n != 0 {
		t.Fatalf("%d files left after saving", n)
	}
	if e = restart(sess); e.path == p && e.buf.Dirty() {
		t.Fatal("restored a saved file as unsaved")
	}
}

// A small file changed on disk keeps the unsaved text, and says so on saving.
func TestRecoverSmallFileChangedOnDisk(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.txt")
	writeFile(t, p, "one")
	e, sess := recoverEditor(t, p)
	e.insert("x")
	e.saveRecovery(true)
	touch(t, p, "someone else")

	e = restart(sess)
	if content(e) != "xone" || !e.conflict {
		t.Fatalf("restored %q, conflict %v", content(e), e.conflict)
	}
}

func TestRecoverScratch(t *testing.T) {
	e, sess := recoverEditor(t, "")
	e.insert("notes\nmore")
	e.saveRecovery(true)
	if e = restart(sess); content(e) != "notes\nmore" || !e.buf.Dirty() {
		t.Fatalf("restored %q", content(e))
	}
	name := e.recName
	e.insert("!")
	e.saveRecovery(true)
	if n := recoverCount(t, sess); n != 1 || e.recName != name {
		t.Fatalf("%d files, %q then %q", n, name, e.recName)
	}
	if e = restart(sess); content(e) != "!notes\nmore" && content(e) != "notes\nmore!" {
		t.Fatalf("second restore %q", content(e))
	}
}

func bigFile(t *testing.T) string {
	t.Helper()
	var sb strings.Builder
	for i := 0; sb.Len() < 2*recoverFullBytes; i++ {
		fmt.Fprintf(&sb, "line %d of the file\n", i)
	}
	p := filepath.Join(t.TempDir(), "big.txt")
	writeFile(t, p, sb.String())
	return p
}

// A big file is written as edits, never whole, and replays on the file.
func TestRecoverBigFile(t *testing.T) {
	p := bigFile(t)
	e, sess := recoverEditor(t, p)
	e.moveTo(Pos{5, 0}, false)
	e.insert("typed ")
	e.buf.ReplaceAll("of the", "in the", e.cursor)
	e.moveTo(Pos{7, 0}, false)
	e.insert("more\nlines\n")
	want := content(e)
	e.saveRecovery(true)

	dir := filepath.Join(filepath.Dir(sess), "recovery")
	ents, _ := os.ReadDir(dir)
	r, ok := readRecover(filepath.Join(dir, ents[0].Name()))
	if !ok || r.Whole || r.Text != "" {
		t.Fatalf("a big file was written whole: %v %v", ok, r.Whole)
	}

	e = restart(sess)
	if content(e) != want || !e.buf.Dirty() {
		t.Fatal("restored text differs")
	}
	data, _ := os.ReadFile(p)
	for range 3 {
		e.undo(false)
	}
	if content(e) != string(data) {
		t.Fatal("undoing the restored edits does not give the file back")
	}
}

// After a save, the edits replay onto the saved file, not the one first read.
func TestRecoverBigFileAfterSave(t *testing.T) {
	p := bigFile(t)
	e, sess := recoverEditor(t, p)
	e.insert("saved ")
	e.save(p)
	e.insert("unsaved ")
	want := content(e)
	e.saveRecovery(true)
	if e = restart(sess); content(e) != want {
		t.Fatalf("got %q", content(e)[:30])
	}
}

// A big file changed on disk cannot take the edits. They must not be applied
// to the wrong text, and must not be thrown away either.
func TestRecoverBigFileChangedOnDisk(t *testing.T) {
	p := bigFile(t)
	e, sess := recoverEditor(t, p)
	e.insert("x")
	e.saveRecovery(true)
	touch(t, p, "replaced")

	e = restart(sess)
	if e.path == p && content(e) != "replaced" {
		t.Fatalf("applied edits to another file: %q", content(e))
	}
	if !strings.Contains(e.msg, "could not be restored") {
		t.Fatalf("msg %q", e.msg)
	}
	if n := recoverCount(t, sess); n != 1 {
		t.Fatalf("%d recovery files, want the one kept", n)
	}
}

func TestRecoverNotUTF8(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bin.txt")
	writeFile(t, p, "a\xff\xfeb")
	e, sess := recoverEditor(t, p)
	e.insert("x")
	e.saveRecovery(true)
	if e = restart(sess); content(e) != "xa\xff\xfeb" {
		t.Fatalf("got %q", content(e))
	}
}

// An orderly exit, having refused to save, leaves nothing to restore; closing
// a document with its changes discarded does too.
func TestRecoverCleared(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.txt")
	writeFile(t, p, "one")
	e, sess := recoverEditor(t, p)
	e.insert("x")
	e.saveRecovery(true)
	e.clearRecovery()
	if n := recoverCount(t, sess); n != 0 {
		t.Fatalf("%d files after exit", n)
	}

	e, sess = recoverEditor(t, p)
	e.insert("x")
	e.saveRecovery(true)
	e.closeDoc(true)
	e.saveRecovery(true)
	if n := recoverCount(t, sess); n != 0 {
		t.Fatalf("%d files after closing", n)
	}
}

// Nothing is written while nothing changes.
func TestRecoverIdle(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.txt")
	writeFile(t, p, "one")
	e, sess := recoverEditor(t, p)
	e.saveRecovery(true)
	if n := recoverCount(t, sess); n != 0 {
		t.Fatalf("wrote %d files for a clean document", n)
	}
	e.insert("x")
	e.saveRecovery(true)
	if e.recoverPending() {
		t.Fatal("still pending after writing")
	}
	dir := filepath.Join(filepath.Dir(sess), "recovery")
	before, _ := os.ReadDir(dir)
	info, _ := before[0].Info()
	e.saveRecovery(false)
	after, _ := before[0].Info()
	if info.ModTime() != after.ModTime() {
		t.Fatal("rewrote an unchanged document")
	}
}

// A file reloaded from disk is what later edits apply to.
func TestRecoverAfterReload(t *testing.T) {
	p := bigFile(t)
	e, sess := recoverEditor(t, p)
	touch(t, p, strings.Repeat("new contents\n", recoverFullBytes/8))
	e.checkDisk()
	e.applyPending()
	e.moveTo(Pos{}, false)
	e.insert("x")
	want := content(e)
	e.saveRecovery(true)
	if e = restart(sess); content(e) != want {
		t.Fatalf("restored %q, msg %q", content(e)[:20], e.msg)
	}
}

// Started on one file after a crash, the unsaved work in another still shows.
func TestRecoverWithFileArgument(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	writeFile(t, a, "a")
	writeFile(t, b, "b")
	e, sess := recoverEditor(t, a)
	e.insert("x")
	e.saveRecovery(true)

	e = NewEditor(b)
	e.sessionPath = sess
	e.keepSession()
	e.restoreRecovery()
	if e.path != b || len(e.docs) != 2 || !e.docs[1].buf.Dirty() || e.docs[1].path != a {
		t.Fatalf("top %q, %d docs", e.path, len(e.docs))
	}
}
