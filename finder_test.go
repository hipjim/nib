package main

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

func TestFuzzyRank(t *testing.T) {
	f := finder{paths: []string{
		"docs/editor-notes.md",
		"internal/edit/tor.go",
		"editor.go",
		"vendor/x/editor_test.go",
		"lines.go",
	}}
	f.query = "edit"
	f.rank()
	if f.matches != 4 || f.results[0].path != "editor.go" {
		t.Fatalf("got %v (%d matches)", f.results, f.matches)
	}
	// Letters spread over the path still match; word starts win.
	f.query = "lgo"
	f.rank()
	if f.results[0].path != "lines.go" {
		t.Fatalf("lgo: got %v", f.results)
	}
	// Upper case makes the query case-sensitive.
	f.query = "Edit"
	f.rank()
	if f.matches != 0 {
		t.Fatalf("Edit: got %v", f.results)
	}
	// With no query, open files come first.
	f.query, f.recent = "", []string{"lines.go"}
	f.rank()
	if f.results[0].path != "lines.go" || len(f.results) != 5 {
		t.Fatalf("recent: got %v", f.results)
	}
}

func TestFuzzyMatchTight(t *testing.T) {
	pos, ok := fuzzyMatch("a/b/abc/abcd", []rune("abc"), true, nil)
	if !ok || !slices.Equal(pos, []int{4, 5, 6}) {
		t.Fatalf("got %v %v", pos, ok)
	}
}

func TestWalkIgnores(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"a.go", "src/b.go", "build/out.bin", "node_modules/m/i.js", ".git/HEAD", "x.log", "src/keep.txt"} {
		os.MkdirAll(filepath.Join(root, filepath.Dir(p)), 0o755)
		os.WriteFile(filepath.Join(root, p), nil, 0o644)
	}
	os.WriteFile(filepath.Join(root, ".gitignore"), []byte("build/\n*.log\n/src/keep.txt\n"), 0o644)
	var f finder
	f.start(root)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, walking := f.poll(); !walking || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	slices.Sort(f.paths)
	want := []string{".gitignore", "a.go", "src/b.go"}
	if !slices.Equal(f.paths, want) {
		t.Fatalf("got %v, want %v", f.paths, want)
	}
}

// Files stay open in the background with their cursor and edits, and the
// listing moves between folders.
func TestDocsAndListing(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "sub"), 0o755)
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello\nworld"), 0o644)
	os.WriteFile(filepath.Join(root, "sub", "b.txt"), []byte("bee"), 0o644)

	e := NewEditor(root)
	if e.dir == nil || len(e.docs) != 1 {
		t.Fatalf("expected only the listing, got %d docs", len(e.docs))
	}
	// Folders come first: "..", "sub", "a.txt"; the cursor starts on "sub".
	if en, _ := e.entry(e.cursor.Line); en.name != "sub" {
		t.Fatalf("cursor on %q", en.name)
	}
	e.activate(e.cursor.Line)
	if en, _ := e.entry(e.cursor.Line); filepath.Base(e.path) != "sub" || en.name != "b.txt" {
		t.Fatalf("in %s on %q", e.path, en.name)
	}
	e.up()
	if en, _ := e.entry(e.cursor.Line); en.name != "sub" {
		t.Fatalf("after up: cursor on %q", en.name)
	}
	e.activate(e.cursor.Line + 1) // a.txt
	if filepath.Base(e.path) != "a.txt" || e.dir != nil {
		t.Fatalf("opened %s", e.path)
	}
	e.cursor, e.anchor = Pos{1, 2}, Pos{1, 2}
	e.insert("X")
	e.open(filepath.Join(root, "sub", "b.txt"))
	if e.otherUnsaved() != 1 {
		t.Fatalf("unsaved in background: %d", e.otherUnsaved())
	}
	e.open(filepath.Join(root, "a.txt"))
	if content(e) != "hello\nwoXrld" || e.cursor != (Pos{1, 3}) {
		t.Fatalf("a.txt came back as %q at %v", content(e), e.cursor)
	}
	e.closeDoc(false) // refuses: unsaved
	if filepath.Base(e.path) != "a.txt" {
		t.Fatal("closed a document with unsaved changes")
	}
	e.closeDoc(true)
	if filepath.Base(e.path) != "b.txt" || len(e.docs) != 2 {
		t.Fatalf("after close: %s, %d docs", e.path, len(e.docs))
	}
	if e.insert("y"); content(e) != "ybee" {
		t.Fatal("cannot edit b.txt")
	}
}

func TestFileProps(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.go")
	os.WriteFile(p, []byte("package x\n"), 0o640)
	got := map[string]string{}
	for _, pr := range fileProps(p, 2, false) {
		got[pr.key] = pr.val
	}
	if got["kind"] != "go file" || got["size"] != "10 B (10 bytes)" || got["lines"] != "2" || got["modified"] == "-" {
		t.Fatalf("got %v", got)
	}
	if runtime.GOOS != "windows" && got["mode"] != "-rw-r----- (0640)" {
		t.Fatalf("mode %q", got["mode"]) // Windows has no Unix permissions
	}
	if humanSize(1536) != "1.5 KB" || humanSize(3<<30) != "3.0 GB" || groupDigits(1234567) != "1,234,567" {
		t.Fatal("formatting")
	}
}

// BenchmarkRank walks NIB_ROOT (default: this folder) and ranks a query.
func BenchmarkRank(b *testing.B) {
	var f finder
	start := time.Now()
	f.start(orDefault(os.Getenv("NIB_ROOT"), "."))
	for {
		if _, walking := f.poll(); !walking {
			break
		}
		time.Sleep(time.Millisecond)
	}
	b.Logf("walked %d files in %v", len(f.paths), time.Since(start))
	q := orDefault(os.Getenv("NIB_QUERY"), "edgo")
	b.Run("full", func(b *testing.B) {
		for b.Loop() {
			f.last, f.query = "", q
			f.rank()
		}
	})
	b.Run("typing", func(b *testing.B) {
		for b.Loop() {
			f.last, f.query = "", q[:1]
			f.rank()
			for i := 2; i <= len(q); i++ {
				f.query = q[:i]
				f.rank()
			}
		}
	})
}

// BenchmarkWalk lists every file under NIB_ROOT (default: this folder).
func BenchmarkWalk(b *testing.B) {
	root := orDefault(os.Getenv("NIB_ROOT"), ".")
	for b.Loop() {
		ch := make(chan []string, 64)
		go walk(root, ch, new(atomic.Bool))
		n := 0
		for batch := range ch {
			n += len(batch)
		}
		b.ReportMetric(float64(n), "files")
	}
}
