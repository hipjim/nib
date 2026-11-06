package main

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
)

func appendFile(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Duration(tailClock) * time.Second)
	tailClock++
	os.Chtimes(path, later, later)
}

var tailClock = 2

// tailSettle looks at the disk and takes in everything, as frames would.
func tailSettle(e *Editor) {
	e.checkDisk()
	for e.stepTails() {
	}
}

func tailEditor(t *testing.T, s string) (*Editor, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "app.log")
	writeFile(t, p, s)
	e := NewEditor(p)
	e.toggleTail()
	if e.tail == nil {
		t.Fatalf("tail did not start: %q", e.msg)
	}
	return e, p
}

func TestAppendText(t *testing.T) {
	for _, tc := range []struct {
		name, start string
		adds        []string
	}{
		{"trailing newline", "a\nb\n", []string{"c\n", "d\n"}},
		{"partial last line", "a\nb", []string{"c", "d\ne", "f\n"}},
		{"empty file", "", []string{"x\n", "y"}},
		{"many blocks", "a\n", []string{strings.Repeat("line\n", 5000), strings.Repeat("more\n", 3000)}},
		{"full block first", strings.Repeat("z\n", blockLines-1), []string{"a\nb\nc\n"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBuffer(tc.start)
			want := tc.start
			for _, a := range tc.adds {
				b.Append(a)
				want += a
			}
			if b.Dirty() {
				t.Fatal("arriving lines made the buffer dirty")
			}
			got := b.Text(Pos{}, b.End())
			if got != want {
				t.Fatalf("text differs: %d bytes, want %d", len(got), len(want))
			}
			if n := strings.Count(want, "\n") + 1; b.LineCount() != n {
				t.Fatalf("%d lines, want %d", b.LineCount(), n)
			}
			for _, k := range b.lines.blocks {
				if k.n() > 2*blockLines {
					t.Fatalf("block of %d lines", k.n())
				}
			}
			p := filepath.Join(t.TempDir(), "out")
			if err := b.Save(p); err != nil {
				t.Fatal(err)
			}
			if data, _ := os.ReadFile(p); string(data) != want {
				t.Fatalf("saved %d bytes, want %d", len(data), len(want))
			}
		})
	}
}

func TestAppendCRLF(t *testing.T) {
	b := NewBuffer("a\r\nb\r")
	b.Append("\nc\r\n")
	if b.Line(1) != "b" || b.Line(2) != "c" || b.LineCount() != 4 {
		t.Fatalf("lines %q %q, %d", b.Line(1), b.Line(2), b.LineCount())
	}
	b = NewBuffer("")
	b.Append("x\r\ny\r\n")
	if !b.CRLF || b.Line(0) != "x" {
		t.Fatalf("empty file: crlf %v, line %q", b.CRLF, b.Line(0))
	}
}

func TestTailFollowsAndPauses(t *testing.T) {
	e, p := tailEditor(t, "one\ntwo\n")
	e.lineH, e.viewH = 10, 40
	buf := e.buf

	appendFile(t, p, "three\nfour\n")
	tailSettle(e)
	if e.buf != buf {
		t.Fatal("growing reloaded the file")
	}
	if content(e) != "one\ntwo\nthree\nfour\n" || e.cursor != (Pos{4, 0}) {
		t.Fatalf("got %q, cursor %v", content(e), e.cursor)
	}
	if e.scrollY != 10 {
		t.Fatalf("last line not on the bottom row: scroll %v", e.scrollY)
	}
	if e.buf.Dirty() {
		t.Fatal("dirty")
	}

	// A half-written line is finished by the next write.
	appendFile(t, p, "fi")
	tailSettle(e)
	appendFile(t, p, "ve\n")
	tailSettle(e)
	if e.buf.Line(4) != "five" {
		t.Fatalf("line %q", e.buf.Line(4))
	}

	// Scrolling up pauses, and the view stays put while lines are counted.
	e.scrollY = 0
	if e.following(e.doc) {
		t.Fatal("still following after scrolling up")
	}
	appendFile(t, p, "six\nseven\n")
	tailSettle(e)
	if e.scrollY != 0 || e.tail.fresh != 2 {
		t.Fatalf("scroll %v, fresh %d", e.scrollY, e.tail.fresh)
	}
	if s, on, _ := e.tailStatus(); on || !strings.Contains(s, "+2 ") {
		t.Fatalf("status %q", s)
	}

	// The cursor moves with the end only while following.
	e.scrollY = 1 << 20
	e.tailEnd(e.doc)
	if s, on, _ := e.tailStatus(); !on || e.tail.fresh != 0 {
		t.Fatalf("not resumed: %q", s)
	}

	// A selection is not taken away.
	e.cursor, e.anchor = Pos{0, 0}, Pos{0, 3}
	appendFile(t, p, "eight\n")
	tailSettle(e)
	if e.anchor != (Pos{0, 3}) || e.cursor != (Pos{}) {
		t.Fatalf("selection moved: %v %v", e.anchor, e.cursor)
	}
}

func TestTailRotation(t *testing.T) {
	e, p := tailEditor(t, "old one\nold two\n")
	tailSettle(e)

	// Moved away: wait for the new one without complaining.
	os.Rename(p, p+".1")
	tailSettle(e)
	if !e.tail.gone || content(e) != "old one\nold two\n" {
		t.Fatalf("gone %v, %q", e.tail.gone, content(e))
	}

	// A new, longer file is caught by its contents, not its size.
	long := strings.Repeat("new line\n", 20)
	touch(t, p, long)
	tailSettle(e)
	if content(e) != long || e.tail.gone || e.cursor.Line != e.buf.LineCount()-1 {
		t.Fatalf("after rotation %q, cursor %v", content(e), e.cursor)
	}

	// Truncated in place, then appended to.
	touch(t, p, "")
	tailSettle(e)
	if content(e) != "" {
		t.Fatalf("after truncate %q", content(e))
	}
	appendFile(t, p, "again\n")
	tailSettle(e)
	if content(e) != "again\n" {
		t.Fatalf("after regrowth %q", content(e))
	}
}

func TestTailSameSizeRewrite(t *testing.T) {
	e, p := tailEditor(t, "abcdef\n")
	touch(t, p, "uvwxyz\n")
	tailSettle(e)
	if content(e) != "uvwxyz\n" {
		t.Fatalf("got %q", content(e))
	}
}

func TestTailWithEdits(t *testing.T) {
	e, p := tailEditor(t, "one\n")
	e.moveTo(Pos{0, 3}, false)
	e.insert("!")
	appendFile(t, p, "two\n")
	tailSettle(e)
	if !e.conflict || !e.buf.Dirty() || content(e) != "one!\n" {
		t.Fatalf("edits not kept: %v %q", e.conflict, content(e))
	}
	if _, _, warn := e.tailStatus(); !warn {
		t.Fatal("no warning about the edits")
	}

	// Saved, the file is the edited text: what follows is read from there.
	e.save(p)
	appendFile(t, p, "three\n")
	tailSettle(e)
	if content(e) != "one!\nthree\n" || e.buf.Dirty() {
		t.Fatalf("after save %q", content(e))
	}

	e2 := NewEditor("")
	e2.toggleTail()
	if e2.tail != nil {
		t.Fatal("tailing a scratch buffer")
	}
}

// A burst is read in chunks, so a frame never reads more than one.
func TestTailBurst(t *testing.T) {
	e, p := tailEditor(t, "start\n")
	var sb strings.Builder
	for i := 0; sb.Len() < 3*tailChunk; i++ {
		fmt.Fprintf(&sb, "line %d\n", i)
	}
	appendFile(t, p, sb.String())
	e.checkDisk()
	steps := 0
	for e.stepTails() {
		steps++
	}
	if steps < 3 {
		t.Fatalf("taken in %d steps", steps)
	}
	if content(e) != "start\n"+sb.String() {
		t.Fatal("text differs after the burst")
	}
}

// Too far behind, or holding too much, it indexes the file again, and the view
// keeps following without the buffer on screen changing until that is done.
func TestTailReindex(t *testing.T) {
	e, p := tailEditor(t, "start\n")
	e.tail.held = tailHeld + 1
	appendFile(t, p, "more\n")
	tailSettle(e)
	if content(e) != "start\nmore\n" || e.tail.held != 0 || e.tail.next != nil {
		t.Fatalf("got %q, held %d", content(e), e.tail.held)
	}
	if e.cursor.Line != e.buf.LineCount()-1 {
		t.Fatalf("cursor %v", e.cursor)
	}
	if len(e.buf.lines.data) != len("start\nmore\n") {
		t.Fatalf("not indexed from the file: %d bytes", len(e.buf.lines.data))
	}
}

func TestTailFilter(t *testing.T) {
	e, p := tailEditor(t, "error one\nfine\n")
	f := &e.filter
	f.start(e.buf, "error", e.opts)
	f.tail = true
	drain := func() {
		tailSettle(e)
		if f.version != e.buf.version {
			f.grew()
		}
		for f.step(); f.busy(); f.step() {
		}
	}
	drain()
	if len(f.hits) != 1 {
		t.Fatalf("%d hits", len(f.hits))
	}
	appendFile(t, p, "error two\nfine\nerr")
	drain()
	if len(f.hits) != 2 || f.sel != 1 {
		t.Fatalf("%d hits, sel %d", len(f.hits), f.sel)
	}

	// The line written in part is searched again when it is finished, once.
	appendFile(t, p, "or three\n")
	drain()
	if len(f.hits) != 3 || f.hits[2].line != 4 || f.sel != 2 {
		t.Fatalf("%d hits, sel %d: %+v", len(f.hits), f.sel, f.hits)
	}

	// A pick moved up stays on its line.
	f.sel = 0
	appendFile(t, p, "error four\n")
	drain()
	if f.sel != 0 || len(f.hits) != 4 {
		t.Fatalf("%d hits, sel %d", len(f.hits), f.sel)
	}

	// The list keeps the latest, across chunks.
	f.sel = len(f.hits) - 1
	appendFile(t, p, strings.Repeat("error many\n", maxHits+patternChunk))
	drain()
	if len(f.hits) != maxHits || f.sel != maxHits-1 || f.capped() {
		t.Fatalf("%d hits, sel %d", len(f.hits), f.sel)
	}
	if last := f.hits[maxHits-1].line; last != e.buf.LineCount()-2 {
		t.Fatalf("last hit on line %d of %d", last, e.buf.LineCount())
	}
}

func TestTailSession(t *testing.T) {
	e, p := tailEditor(t, "one\n")
	sess := filepath.Join(t.TempDir(), "session.json")
	e.sessionPath = sess
	e.saveSession(true)
	e = NewEditor("")
	e.sessionPath = sess
	e.restoreSession()
	if e.path != p || e.tail == nil {
		t.Fatalf("restored %q, tail %v", e.path, e.tail)
	}
}

// The whole path a real window takes: the watcher notices the file, wakes the
// editor, and a frame shows the new line.
func TestTailLive(t *testing.T) {
	e, p := tailEditor(t, "one\n")
	woke := make(chan struct{}, 1)
	e.watch.sync(e.docs)
	go e.watch.run(func() {
		select {
		case woke <- struct{}{}:
		default:
		}
	})
	var ops op.Ops
	gtx := layout.Context{
		Ops:         &ops,
		Now:         time.Now(),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(image.Pt(800, 200)),
	}
	frame := func() {
		ops.Reset()
		gtx.Now = time.Now()
		e.Layout(gtx)
	}
	frame()
	start := time.Now()
	for i := range 3 {
		appendFile(t, p, fmt.Sprintf("line %d\n", i))
		select {
		case <-woke:
			frame()
		case <-time.After(2 * tailPoll):
			t.Fatal("the watcher did not wake the editor")
		}
		if e.buf.LineCount() < i+3 {
			frame()
		}
		if e.buf.LineCount() != i+3 || e.cursor.Line != i+2 {
			t.Fatalf("after %d writes: %d lines, cursor %v", i+1, e.buf.LineCount(), e.cursor)
		}
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("three writes took %v", d)
	}
	if y := float64(e.cursor.Line * e.lineH); y+float64(e.lineH) > e.scrollY+float64(e.viewH) {
		t.Fatalf("last line below the window: y %v, scroll %v, view %d", y, e.scrollY, e.viewH)
	}
	s, on, _ := e.tailStatus()
	if !on {
		t.Fatalf("status %q", s)
	}
}

// BenchmarkTailStepBig is one line reaching the screen in a file the size of
// NIB_FILE, which BenchmarkLoadBig shows would otherwise be indexed again.
func BenchmarkTailStepBig(b *testing.B) {
	src := os.Getenv("NIB_FILE")
	if src == "" {
		b.Skip("set NIB_FILE")
	}
	data, err := os.ReadFile(src)
	if err != nil {
		b.Fatal(err)
	}
	p := filepath.Join(b.TempDir(), "big.log")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		b.Fatal(err)
	}
	data = nil
	e := NewEditor(p)
	for e.buf.Poll() {
		time.Sleep(time.Millisecond)
	}
	e.startTail()
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		b.Fatal(err)
	}
	defer f.Close()
	line := []byte("2026-11-05 12:00:01 INFO one more line in the log\n")
	for b.Loop() {
		b.StopTimer()
		f.Write(line)
		b.StartTimer()
		e.tail.size += int64(len(line))
		e.tail.check = true
		e.stepTail(e.doc)
	}
}
