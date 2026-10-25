package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// naiveGrep is the line-by-line search grepFile must agree with.
func naiveGrep(text, q string) []hit {
	index := matcher(q)
	var hits []hit
	for i, l := range strings.Split(text, "\n") {
		if j := index(l); j >= 0 {
			hits = append(hits, newHit("f", i, l, j, len(q)))
		}
	}
	return hits
}

func TestGrepFileMatchesNaive(t *testing.T) {
	var sb strings.Builder
	for i := range 20000 {
		switch {
		case i%997 == 0:
			sb.WriteString(strings.Repeat("long ", 3000) + "Needle at the end")
		case i%13 == 0:
			sb.WriteString("a needle and another needle")
		case i%7 == 0:
			sb.WriteString("\tNEEDLE ünïcode ñeedle")
		default:
			sb.WriteString("nothing here")
		}
		if i%2 == 0 {
			sb.WriteByte('\r')
		}
		sb.WriteByte('\n')
	}
	sb.WriteString("last needle without a newline")
	text := sb.String()
	name := filepath.Join(t.TempDir(), "f")
	os.WriteFile(name, []byte(text), 0o644)
	for _, q := range []string{"needle", "Needle", "ñeedle", "ünï", "zzz"} {
		want := naiveGrep(strings.ReplaceAll(text, "\r", ""), q)
		buf := make([]byte, 4096) // small, so lines cross reads and the buffer grows
		got := grepFile(name, "f", matcher(q), len(q), !isASCII(q) && strings.ToLower(q) == q, &buf, new(atomic.Bool))
		if len(got) != min(len(want), maxFileHits) {
			t.Fatalf("%q: %d hits, want %d", q, len(got), len(want))
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("%q: hit %d = %+v, want %+v", q, i, got[i], want[i])
			}
		}
	}
}

func TestGrepFolder(t *testing.T) {
	dir := t.TempDir()
	write := func(name, s string) {
		p := filepath.Join(dir, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(s), 0o644)
	}
	write("b.txt", "one\ntwo needle\n")
	write("a/c.go", "needle\nx\nneedle again\n")
	write("bin.dat", "needle\x00binary")
	write("node_modules/m.js", "needle")
	write(".gitignore", "*.log\n")
	write("skip.log", "needle")

	var g grep
	run := func(q string) {
		g.start(dir, q)
		for {
			if _, running := g.poll(); !running {
				break
			}
		}
	}
	run("needle")
	var got []string
	for _, h := range g.hits {
		got = append(got, h.path+":"+string(rune('1'+h.line)))
	}
	if want := "a/c.go:1 a/c.go:3 b.txt:2"; strings.Join(got, " ") != want {
		t.Fatalf("hits %v, want %s", got, want)
	}
	if g.files != 2 {
		t.Fatalf("files %d, want 2", g.files)
	}
	// The second query reuses the file list.
	list := g.list
	run("two")
	if g.list != list {
		t.Fatal("file list not kept")
	}
	if len(g.hits) != 1 || g.hits[0].path != "b.txt" || g.hits[0].at != 0 {
		t.Fatalf("hits %+v", g.hits)
	}
}
