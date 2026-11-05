package main

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"
)

// A file that mixes unedited blocks with edited ones, big enough to take many
// chunks, so the block paths and the line paths are both exercised.
func patternDoc(r *rand.Rand, lines int) (string, *Buffer) {
	words := []string{"ERROR", "error", "warn", "disk full", "id=42", "id=7", "foo_bar", "x", "", "skip this"}
	var sb strings.Builder
	for i := range lines {
		if i > 0 {
			sb.WriteByte('\n')
		}
		fmt.Fprintf(&sb, "%s %s", words[r.IntN(len(words))], words[r.IntN(len(words))])
	}
	return sb.String(), NewBuffer(sb.String())
}

func bufLines(b *Buffer) []string {
	out := make([]string, b.LineCount())
	for i := range out {
		out[i] = b.Line(i)
	}
	return out
}

func TestPatternAgreesWithPlainRegexp(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	_, b := patternDoc(r, 5*patternChunk)
	b.Replace(Pos{100, 0}, Pos{100, 0}, "ERROR edited id=99 ", Pos{})
	b.Replace(Pos{3000, 0}, Pos{3001, 0}, "", Pos{})

	cases := []struct {
		q string
		o findOpts
	}{
		{`id=\d+`, findOpts{regex: true}},
		{`^ERROR`, findOpts{regex: true}},
		{`full$`, findOpts{regex: true}},
		{`(?:ERROR|warn) x`, findOpts{regex: true}},
		{`nothing here at all`, findOpts{regex: true}},
		{`x*`, findOpts{regex: true}}, // matches empty everywhere
		{`error`, findOpts{exact: true}},
		{`ERROR`, findOpts{exact: true}},
		{`foo`, findOpts{word: true}},
		{`foo_bar`, findOpts{word: true}},
		{`skip`, findOpts{regex: true}}, // k and s also fold to non-ASCII
	}
	for _, c := range cases {
		p, err := compilePattern(c.q, c.o)
		if err != nil {
			t.Fatal(c.q, err)
		}
		var lines []int
		total := 0
		for i, l := range bufLines(b) {
			n := 0
			for _, loc := range p.re.FindAllStringIndex(l, -1) {
				if loc[1] > loc[0] {
					n++
				}
			}
			if n > 0 {
				lines = append(lines, i)
			}
			total += n
		}

		// eachLine may offer more lines than match, but never miss one.
		var seen []int
		b.lines.eachLine(lineRange{0, b.LineCount()}, p, func(line int, text string) bool {
			if _, _, ok := p.first(text, 0); ok {
				seen = append(seen, line)
			}
			return false
		})
		if fmt.Sprint(seen) != fmt.Sprint(lines) {
			t.Errorf("%q: eachLine found %d lines, want %d", c.q, len(seen), len(lines))
		}

		count := newPatternCount(p, b, c.q, c.o)
		for !count.done() {
			count.step(time.Millisecond)
		}
		if count.total != total {
			t.Errorf("%q: counted %d, want %d", c.q, count.total, total)
		}
		at := Pos{b.LineCount() / 2, 3}
		want := 0
		for i, l := range bufLines(b)[:at.Line+1] {
			limit := len(l)
			if i == at.Line {
				limit = at.Col
			}
			want += p.count(l, limit)
		}
		if got := count.before(at); got != want {
			t.Errorf("%q: before = %d, want %d", c.q, got, want)
		}
	}
}

func TestSmartCaseIgnoresEscapes(t *testing.T) {
	for _, c := range []struct {
		q     string
		regex bool
		want  bool
	}{
		{`\S+\D`, true, false},
		{`\p{Greek}x`, true, false},
		{`Foo`, true, true},
		{`\SFoo`, true, true},
		{`\S`, false, true}, // literal: the S is a capital letter
	} {
		if got := hasUpper(c.q, c.regex); got != c.want {
			t.Errorf("hasUpper(%q, %v) = %v", c.q, c.regex, got)
		}
	}
}

// Every literal picked must really be in every match, whatever the case.
func TestRequiredLiteral(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`foo\d+`, "foo"},
		{`\d+barbaz.*x`, "barbaz"},
		{`(?i)Hello`, "hello"},
		{`(?i)ask`, "a"},
		{`a|b`, ""},
		{`x*`, ""},
		{`(abc)+`, "abc"},
	} {
		p, err := compilePattern(c.src, findOpts{regex: true, exact: true})
		if err != nil {
			t.Fatal(err)
		}
		if (p.lit != nil) != (c.want != "") {
			t.Errorf("%q: literal %v, want %q", c.src, p.lit != nil, c.want)
		}
	}
	p, _ := compilePattern("ask", findOpts{regex: true})
	if !p.re.MatchString("aſK") {
		t.Fatal("the test's premise changed: (?i)s should match the long s")
	}
	hit := false
	b := NewBuffer("aſK")
	b.lines.eachLine(lineRange{0, 1}, p, func(int, string) bool { hit = true; return false })
	if !hit {
		t.Fatal("the literal filtered out a line that matches")
	}
}

func searchToEnd(e *Editor) {
	for e.search != nil {
		e.stepSearch()
	}
}

func newPatternEditor(text string, o findOpts, q string) *Editor {
	e := newTestEditor(text, Pos{})
	e.opts = o
	e.findQuery = q
	return e
}

func TestFindPattern(t *testing.T) {
	e := newPatternEditor("one\nid=42 and id=7\nlast", findOpts{regex: true}, `id=\d+`)
	e.find(Pos{}, false)
	searchToEnd(e)
	if e.anchor != (Pos{1, 0}) || e.cursor != (Pos{1, 5}) {
		t.Fatalf("first: %v %v", e.anchor, e.cursor)
	}
	e.findNext(false)
	searchToEnd(e)
	if e.anchor != (Pos{1, 10}) || e.cursor != (Pos{1, 14}) {
		t.Fatalf("next: %v %v", e.anchor, e.cursor)
	}
	e.findNext(false) // wraps around
	searchToEnd(e)
	if e.anchor != (Pos{1, 0}) {
		t.Fatalf("wrap: %v", e.anchor)
	}
	e.findNext(true) // and backward, around the top
	searchToEnd(e)
	if e.anchor != (Pos{1, 10}) {
		t.Fatalf("back: %v", e.anchor)
	}

	e.findQuery = "no such thing"
	e.find(Pos{}, false)
	searchToEnd(e)
	if !e.notFound {
		t.Fatal("found nothing but did not say so")
	}

	// ^ must not match at the column the search started from.
	e = newPatternEditor("xfoo\nfoo", findOpts{regex: true}, `^foo`)
	e.find(Pos{0, 1}, false)
	searchToEnd(e)
	if e.anchor != (Pos{1, 0}) {
		t.Fatalf("^ matched mid-line: %v", e.anchor)
	}

	// A half-typed expression is a state to show, not a crash.
	e.findQuery = "(oops"
	e.find(Pos{}, false)
	if !e.notFound || !strings.HasPrefix(func() string { e.prompt = promptFind; return e.matchCountText() }(), "bad pattern") {
		t.Fatal("a bad pattern was not reported")
	}
}

// A background find stops the moment what it searched is out of date.
func TestPatternSearchCancels(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	_, b := patternDoc(r, 50*patternChunk)
	e := &Editor{doc: newDoc("", b)}
	e.opts, e.findQuery = findOpts{regex: true}, "nothing matches this"
	start := func() {
		e.search = &patternSearch{p: mustPattern(t, e.findQuery, e.opts), buf: e.buf, version: e.buf.version,
			cursor: e.cursor, anchor: e.anchor, chunks: e.buf.lines.chunksOf([]lineRange{{0, b.LineCount()}}, false)}
	}
	start()
	e.moveTo(Pos{5, 0}, false)
	e.stepSearch()
	if e.search != nil {
		t.Fatal("kept searching after the cursor moved")
	}
	start()
	e.insert("x")
	e.stepSearch()
	if e.search != nil {
		t.Fatal("kept searching after an edit")
	}
}

func mustPattern(t *testing.T, q string, o findOpts) *pattern {
	t.Helper()
	p, err := compilePattern(q, o)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReplacePattern(t *testing.T) {
	text := "id=42 and id=7\nnothing\nid=1"
	e := newPatternEditor(text, findOpts{regex: true}, `id=(\d+)`)
	e.replaceBy = "num:$1"
	e.replaceAll()
	if got := content(e); got != "num:42 and num:7\nnothing\nnum:1" {
		t.Fatalf("all: %q", got)
	}
	e.undo(false)
	if got := content(e); got != text {
		t.Fatalf("undo: %q", got)
	}

	e.prompt = promptWith
	e.find(Pos{}, false)
	e.replaceNext()
	searchToEnd(e)
	if got := content(e); got != "num:42 and id=7\nnothing\nid=1" {
		t.Fatalf("one: %q", got)
	}
	if e.anchor != (Pos{0, 11}) {
		t.Fatalf("after one, selected from %v", e.anchor)
	}

	// Without regular expressions on, $1 is what it says.
	e = newPatternEditor("foo food", findOpts{word: true}, "foo")
	e.replaceBy = "$1"
	e.replaceAll()
	if got := content(e); got != "$1 food" {
		t.Fatalf("whole word: %q", got)
	}
}

func TestFilter(t *testing.T) {
	e := newTestEditor("alpha\nbeta\n  alphabet\ngamma", Pos{})
	e.openPrompt(promptFilter, "")
	e.setInput("alpha")
	for e.filter.busy() {
		e.filter.step()
	}
	f := &e.filter
	if len(f.hits) != 2 || f.hits[0].line != 0 || f.hits[1].line != 2 {
		t.Fatalf("hits %+v", f.hits)
	}
	f.sel = 1
	e.key(layout.Context{}, key.Event{Name: key.NameReturn}, false)
	if e.prompt != promptNone || e.anchor != (Pos{2, 2}) || e.cursor != (Pos{2, 7}) {
		t.Fatalf("picked: prompt %v, %v %v", e.prompt, e.anchor, e.cursor)
	}

	// It holds at most maxHits lines, however many match.
	b := NewBuffer(strings.Repeat("hit\n", maxHits+5000))
	f = &lineFilter{}
	f.start(b, "hit", findOpts{})
	for f.busy() {
		f.step()
	}
	if len(f.hits) != maxHits || !f.capped() {
		t.Fatalf("kept %d hits", len(f.hits))
	}

	f = &lineFilter{}
	f.start(b, "(", findOpts{regex: true})
	if f.err == nil || f.busy() {
		t.Fatal("a bad pattern did not stop the filter")
	}
}

func TestGrepPattern(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("no\r\nid=12 x\nid=3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var g grep
	g.opts = findOpts{regex: true}
	g.start(dir, `id=\d+$`)
	for {
		if _, running := g.poll(); !running {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if len(g.hits) != 1 || g.hits[0].line != 2 || g.hits[0].n != 4 {
		t.Fatalf("hits %+v", g.hits)
	}
	g.start(dir, `(`)
	if g.err == nil || g.results != nil {
		t.Fatal("a bad pattern was searched for")
	}

	// Line numbers agree with a plain walk, literal or not.
	r := rand.New(rand.NewPCG(5, 6))
	text, _ := patternDoc(r, 3000)
	for _, src := range []string{`id=\d+`, `^warn`, `x*y?`, `foo_bar$`} {
		p := mustPattern(t, src, findOpts{regex: true})
		var want []int
		for i, l := range strings.Split(text, "\n") {
			if _, _, ok := p.first(l, 0); ok {
				want = append(want, i)
			}
		}
		line := 0
		var got []int
		for _, h := range scanPattern(text, "f", p, &line, nil) {
			got = append(got, h.line)
		}
		if len(want) > maxFileHits {
			want = want[:maxFileHits]
		}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("%q: %d lines, want %d", src, len(got), len(want))
		}
	}
}

// The query made from selected text finds that text.
func TestAsQuery(t *testing.T) {
	e := newTestEditor("a.b(c) axb", Pos{})
	e.opts.regex = true
	e.findQuery = e.asQuery("a.b(c)")
	e.find(Pos{0, 1}, false)
	searchToEnd(e)
	if e.anchor != (Pos{0, 0}) || e.cursor != (Pos{0, 6}) {
		t.Fatalf("%v %v", e.anchor, e.cursor)
	}
}

func BenchmarkFindRegexBig(b *testing.B) {
	path := os.Getenv("NIB_FILE")
	if path == "" {
		b.Skip("set NIB_FILE")
	}
	buf, err := Load(path)
	if err != nil {
		b.Fatal(err)
	}
	for buf.Poll() {
		time.Sleep(time.Millisecond)
	}
	for _, src := range []string{`nothing[0-9]+here`, `[0-9]{5}[!?]{3}`} {
		b.Run(src, func(b *testing.B) {
			p, _ := compilePattern(src, findOpts{regex: true})
			for b.Loop() {
				ps := &patternSearch{p: p, buf: buf, chunks: buf.lines.chunksOf([]lineRange{{0, buf.LineCount()}}, false)}
				for {
					if _, _, _, done := ps.step(time.Hour); done {
						break
					}
				}
			}
			b.SetBytes(int64(len(buf.lines.data)))
		})
	}
}

// Lines still arriving from the indexer get counted and filtered too.
func TestPatternSeesLinesStillLoading(t *testing.T) {
	var sb strings.Builder
	for range asyncMin / 8 {
		sb.WriteString("hit one\n")
	}
	b := NewBuffer(sb.String())
	c := newPatternCount(mustPattern(t, "hit", findOpts{exact: true}), b, "hit", findOpts{exact: true})
	f := &lineFilter{}
	f.start(b, "hit", findOpts{})
	for b.Poll() {
		c.step(time.Millisecond)
		f.step()
		time.Sleep(time.Millisecond)
	}
	for !c.done() {
		c.step(time.Second)
	}
	c.step(time.Second)
	for !c.done() {
		c.step(time.Second)
	}
	if c.total != b.LineCount()-1 {
		t.Fatalf("counted %d of %d", c.total, b.LineCount()-1)
	}
	for f.busy() {
		f.step()
	}
	f.step()
	for f.busy() {
		f.step()
	}
	if len(f.hits) != min(maxHits, b.LineCount()-1) {
		t.Fatalf("filtered %d", len(f.hits))
	}
}
