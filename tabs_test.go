package main

import (
	"os"
	"path/filepath"
	"testing"
)

func testDocs(t *testing.T, names ...string) (*Editor, string) {
	t.Helper()
	dir := t.TempDir()
	e := &Editor{doc: newDoc("", NewBuffer(""))}
	e.docs = []*doc{e.doc}
	e.root = dir
	for _, n := range names {
		p := filepath.Join(dir, n)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(n+"\n"), 0o644)
		e.open(p)
	}
	return e, dir
}

func names(e *Editor) []string { return tabNames(e.tabs()) }

func TestTabsOrderAndNames(t *testing.T) {
	e, _ := testDocs(t, "a.go", "b.go", "sub/a.go")
	got := names(e)
	want := []string{"a.go", "b.go", "sub" + string(filepath.Separator) + "a.go"}
	if len(got) != 3 || got[1] != want[1] || got[2] != want[2] || got[0] == got[2] {
		t.Fatalf("names: %q", got)
	}
	e.showTab(0)
	if n := names(e); n[0] != got[0] || n[2] != got[2] || e.doc != e.tabs()[0] {
		t.Fatalf("after showTab(0): %q, current %q", n, filepath.Base(e.path))
	}
	e.cycleTab(true)
	if filepath.Base(e.path) != "a.go" || e.doc != e.tabs()[2] {
		t.Fatalf("cycle back from the first: %q", e.path)
	}
	e.cycleTab(false)
	if e.doc != e.tabs()[0] {
		t.Fatal("cycle forward from the last")
	}
	e.showTab(7)
	if e.doc != e.tabs()[0] {
		t.Fatal("showTab out of range")
	}
}

func TestCloseTabs(t *testing.T) {
	e, _ := testDocs(t, "a.go", "b.go", "c.go")
	tabs := e.tabs()
	e.closeTab(tabs[0])
	if len(e.docs) != 2 || e.doc != tabs[2] {
		t.Fatalf("after closing a.go: %d docs, current %q", len(e.docs), e.path)
	}
	tabs[1].buf.Replace(Pos{}, Pos{}, "x", Pos{})
	e.closeTab(tabs[1])
	if len(e.docs) != 2 || e.doc != tabs[1] || e.msg == "" {
		t.Fatalf("closing an unsaved tab: %d docs, current %q, msg %q", len(e.docs), e.path, e.msg)
	}
	e.show(tabs[2])
	e.closeOthers()
	if len(e.docs) != 2 || e.doc != tabs[2] {
		t.Fatalf("closeOthers kept %d docs", len(e.docs))
	}
	e.closeTab(e.doc)
	if len(e.docs) != 1 || e.doc != tabs[1] {
		t.Fatalf("closing the current tab: %d docs, current %q", len(e.docs), e.path)
	}
	e.closeTab(e.doc)
	if len(e.docs) != 1 || e.doc != tabs[1] || !e.discardOK {
		t.Fatalf("first click on an unsaved current tab: %d docs, msg %q", len(e.docs), e.msg)
	}
	e.closeTab(e.doc)
	if len(e.docs) != 1 || e.doc == tabs[1] || e.path != "" {
		t.Fatalf("second click did not close: current %q", e.path)
	}
}

func TestTabLayoutShrinksThenScrolls(t *testing.T) {
	e, _ := testDocs(t, "one.go", "two.go", "three.go", "four.go")
	e.charW, e.lineH = 8, 20
	tabs := e.tabs()
	nm := names(e)
	x0, w, scroll := e.tabLayout(tabs, nm, 10000, 10)
	for i := range w {
		if w[i] != len(nm[i])*8+20 || (i > 0 && x0[i] != x0[i-1]+w[i-1]) {
			t.Fatalf("natural widths: x0 %v w %v", x0, w)
		}
	}
	if scroll != 0 {
		t.Fatal("scrolled with room to spare")
	}
	_, w, _ = e.tabLayout(tabs, nm, 100, 10)
	for _, x := range w {
		if x != tabMinChars*8+20 {
			t.Fatalf("shrunk widths %v", w)
		}
	}
	x0, w, scroll = e.tabLayout(tabs, nm, 100, 10)
	last := len(w) - 1
	if x0[last]+w[last]-scroll != 100 {
		t.Fatalf("current tab not at the right edge: x0 %d w %d scroll %d", x0[last], w[last], scroll)
	}
	e.showTab(0)
	tabs = e.tabs()
	if _, _, scroll = e.tabLayout(tabs, nm, 100, 10); scroll != 0 {
		t.Fatalf("first tab current, scroll %d", scroll)
	}
}

func TestFinderPaths(t *testing.T) {
	abs := func(p string) string {
		a, err := filepath.Abs(p)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	root := abs("/r/proj")
	home, _ := os.UserHomeDir()
	other := abs("/r/other.go")
	cases := []struct{ path, want string }{
		{filepath.Join(root, "a", "b.go"), "a/b.go"},
		{filepath.Join(root, "b.go"), "b.go"},
		{filepath.Join(home, "Downloads", "x.pdf"), "~/Downloads/x.pdf"},
		{other, filepath.ToSlash(other)},
	}
	for _, c := range cases {
		got := finderPath(root, c.path)
		if got != c.want {
			t.Errorf("finderPath(%q) = %q, want %q", c.path, got, c.want)
		}
		if back := fullPath(root, got); back != c.path {
			t.Errorf("fullPath(%q) = %q, want %q", got, back, c.path)
		}
	}
	if got, want := fullPath(root, "~notes"), filepath.Join(root, "~notes"); got != want {
		t.Errorf("fullPath(~notes) = %q, want %q", got, want)
	}
	sep := string(filepath.Separator)
	for p, want := range map[string]string{root: "proj" + sep, sep: sep, ".": "."} {
		if got := folderName(p); got != want {
			t.Errorf("folderName(%q) = %q, want %q", p, got, want)
		}
	}
}

func TestSessionDropsIdleFiles(t *testing.T) {
	e, dir := testDocs(t, "a.go", "b.go")
	e.sessionPath = filepath.Join(dir, "session.json")
	e.saveSession(true)
	for i := range maxIdle {
		f := NewEditor("")
		f.sessionPath = e.sessionPath
		f.restoreSession()
		if len(f.docs) != 2 {
			t.Fatalf("restore %d: %d docs", i, len(f.docs))
		}
		f.sessionData = nil
		f.saveSession(true)
		s, _ := readSession(f.sessionPath)
		want := 2
		if i == maxIdle-1 {
			want = 1
		}
		if len(s.Docs) != want {
			t.Fatalf("save %d: %d docs in the session, want %d", i, len(s.Docs), want)
		}
	}
	g := NewEditor("")
	g.sessionPath = e.sessionPath
	g.restoreSession()
	if len(g.docs) != 1 || filepath.Base(g.path) != "b.go" {
		t.Fatalf("after dropping: %d docs, current %q", len(g.docs), g.path)
	}
}
