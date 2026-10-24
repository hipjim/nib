package main

import (
	"os"
	"strings"
	"testing"
)

// colored renders the spans of line as "text:class" pieces.
func colored(line string, spans []span) string {
	var parts []string
	for _, s := range spans {
		parts = append(parts, line[s.start:s.end]+":"+string("pcsnktTEW"[s.cls]))
	}
	return strings.Join(parts, " ")
}

func TestScan(t *testing.T) {
	tests := []struct {
		ext   string
		lines []string
		want  []string
	}{
		{".go", []string{`x := "a\"b" + 'c' // done`}, []string{`"a\"b":s 'c':s // done:c`}},
		{".go", []string{"for i := 0x1f; /* a", "b */ s := `raw", "\\` + 1.5"},
			[]string{"for:k 0x1f:n /* a:c", "b */:c `raw:s", "\\`:s 1.5:n"}},
		{".rs", []string{`fn f<'a>(x: &'a str) -> char { '\n' }`}, []string{`fn:k str:k char:k '\n':s`}},
		{".py", []string{`def f(): """doc`, `more""" # hi`}, []string{`def:k """doc:s`, `more""":s # hi:c`}},
		{".sql", []string{`SELECT a FROM t -- x`}, []string{`SELECT:k FROM:k -- x:c`}},
		{".xml", []string{`<a href="x"`, `c="d">t</a> <!-- e`, `f -->`},
			[]string{`<a:t "x":s`, `"d":s >:t </a:t >:t <!--:c  e:c`, `f -->:c`}},
		{".log", []string{`2026-10-24T09:12:04Z ERROR failed: Warning`},
			[]string{`2026-10-24T09:12:04Z:T ERROR:E failed:E Warning:W`}},
	}
	for _, tt := range tests {
		l := langFor("f" + tt.ext)
		st := 0
		for i, line := range tt.lines {
			var spans []span
			spans, st = l.scan(line, st, nil)
			if got := colored(line, spans); got != tt.want[i] {
				t.Errorf("%s line %d %q:\n got %s\nwant %s", tt.ext, i, line, got, tt.want[i])
			}
		}
	}
}

func TestLangFor(t *testing.T) {
	for path, ok := range map[string]bool{"a.GO": true, "Makefile": true, "app.log.1": true, "notes.txt": false, "x": false} {
		if (langFor(path) != nil) != ok {
			t.Errorf("langFor(%q) = %v", path, langFor(path))
		}
	}
}

// A block comment opened far above the screen is known, and edits fix it.
func TestHighlighterState(t *testing.T) {
	lines := make([]string, 50000)
	for i := range lines {
		lines[i] = "x := 1"
	}
	lines[10] = "/* open"
	lines[3000] = "close */"
	b := NewBuffer(strings.Join(lines, "\n"))
	h := newHighlighter("a.go")
	for _, tc := range []struct{ line, want int }{{5, 0}, {2000, stBlock}, {2999, stBlock}, {3001, 0}} {
		if got := h.stateAt(b, tc.line); got != tc.want {
			t.Errorf("state at %d = %d, want %d", tc.line, got, tc.want)
		}
	}
	// Remove the closing line: everything below is now inside the comment.
	b.Replace(Pos{3000, 0}, Pos{3000, 8}, "", Pos{})
	if l, ok := b.TakeEdited(); ok {
		h.edited(l)
	}
	if got := h.stateAt(b, 4000); got != stBlock {
		t.Errorf("after edit: state at 4000 = %d, want %d", got, stBlock)
	}
	// Far from any exact state the highlighter guesses normal text.
	if got := h.stateAt(b, 40000); got != 0 {
		t.Errorf("far guess = %d, want 0", got)
	}
}

// BenchmarkScan measures highlighting a file (QUOD_FILE, default sqlite3.c).
func BenchmarkScan(b *testing.B) {
	path := orDefault(os.Getenv("QUOD_FILE"), "samples/sqlite3.c")
	data, err := os.ReadFile(path)
	if err != nil {
		b.Skip(err)
	}
	lines := strings.Split(string(data), "\n")
	l := langFor(path)
	b.SetBytes(int64(len(data)))
	var spans []span
	for b.Loop() {
		st := 0
		for _, line := range lines {
			spans, st = l.scan(line, st, spans[:0])
		}
	}
}
