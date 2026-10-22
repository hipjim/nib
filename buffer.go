package main

import (
	"bufio"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unsafe"
)

// Pos is a position in the buffer. Col is a byte offset into the line.
type Pos struct{ Line, Col int }

func (p Pos) Less(q Pos) bool {
	return p.Line < q.Line || (p.Line == q.Line && p.Col < q.Col)
}

func order(a, b Pos) (Pos, Pos) {
	if b.Less(a) {
		return b, a
	}
	return a, b
}

// edit is one undoable change: the text old at start was replaced by new.
type edit struct {
	start         Pos
	old, new      string
	before, after Pos // cursor before and after the edit
	at            time.Time
	id            int
}

// Buffer holds the document as a slice of lines without terminators.
type Buffer struct {
	lines   []string
	undo    []edit
	redo    []edit
	version int // bumped on every change
	nextID  int
	saved   int // state() at last load/save
	CRLF    bool
	UseTabs bool
}

func NewBuffer(text string) *Buffer {
	b := &Buffer{}
	b.SetText(text)
	return b
}

func (b *Buffer) SetText(text string) {
	b.CRLF = strings.Contains(text, "\r\n")
	if b.CRLF {
		text = strings.ReplaceAll(text, "\r\n", "\n")
	}
	b.lines = strings.Split(text, "\n")
	b.undo, b.redo = nil, nil
	b.saved = 0
	b.UseTabs = detectTabs(b.lines)
}

func detectTabs(lines []string) bool {
	tabs, spaces := 0, 0
	for i, l := range lines {
		if i > 5000 {
			break
		}
		if strings.HasPrefix(l, "\t") {
			tabs++
		} else if strings.HasPrefix(l, "  ") {
			spaces++
		}
	}
	return tabs > spaces
}

func Load(path string) (*Buffer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return NewBuffer(""), nil
	}
	// data is never written again, so share it instead of copying it.
	return NewBuffer(unsafe.String(&data[0], len(data))), nil
}

// Save writes the buffer atomically via a temp file and rename.
func (b *Buffer) Save(path string) error {
	sep := "\n"
	if b.CRLF {
		sep = "\r\n"
	}
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".quod*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	w := bufio.NewWriterSize(tmp, 1<<20)
	for i, l := range b.lines {
		if i > 0 {
			w.WriteString(sep)
		}
		w.WriteString(l)
	}
	if err := w.Flush(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	b.saved = b.state()
	return nil
}

// state identifies the current undo position, so undoing back to the
// saved state clears the dirty flag.
func (b *Buffer) state() int {
	if len(b.undo) == 0 {
		return 0
	}
	return b.undo[len(b.undo)-1].id
}

func (b *Buffer) Dirty() bool       { return b.state() != b.saved }
func (b *Buffer) LineCount() int    { return len(b.lines) }
func (b *Buffer) Line(i int) string { return b.lines[i] }

func (b *Buffer) End() Pos {
	n := len(b.lines) - 1
	return Pos{n, len(b.lines[n])}
}

// Clamp returns the nearest valid position to p.
func (b *Buffer) Clamp(p Pos) Pos {
	if p.Line < 0 {
		return Pos{}
	}
	if p.Line >= len(b.lines) {
		return b.End()
	}
	p.Col = max(0, min(p.Col, len(b.lines[p.Line])))
	return p
}

func (b *Buffer) Text(start, end Pos) string {
	start, end = order(start, end)
	if start.Line == end.Line {
		return b.lines[start.Line][start.Col:end.Col]
	}
	n := len(b.lines[start.Line]) - start.Col + end.Col
	for i := start.Line + 1; i <= end.Line; i++ {
		n += len(b.lines[i]) + 1
	}
	var sb strings.Builder
	sb.Grow(n)
	sb.WriteString(b.lines[start.Line][start.Col:])
	for i := start.Line + 1; i < end.Line; i++ {
		sb.WriteByte('\n')
		sb.WriteString(b.lines[i])
	}
	sb.WriteByte('\n')
	sb.WriteString(b.lines[end.Line][:end.Col])
	return sb.String()
}

// endOf returns the position after inserting s at start.
func endOf(start Pos, s string) Pos {
	n := strings.Count(s, "\n")
	if n == 0 {
		return Pos{start.Line, start.Col + len(s)}
	}
	return Pos{start.Line + n, len(s) - strings.LastIndexByte(s, '\n') - 1}
}

func (b *Buffer) replace(start, end Pos, text string) Pos {
	prefix := b.lines[start.Line][:start.Col]
	suffix := b.lines[end.Line][end.Col:]
	if !strings.Contains(text, "\n") {
		b.lines[start.Line] = prefix + text + suffix
		if end.Line > start.Line {
			b.lines = slices.Delete(b.lines, start.Line+1, end.Line+1)
		}
	} else {
		nl := strings.Split(text, "\n")
		nl[0] = prefix + nl[0]
		nl[len(nl)-1] += suffix
		b.lines = slices.Replace(b.lines, start.Line, end.Line+1, nl...)
	}
	b.version++
	return endOf(start, text)
}

// Replace swaps the text between start and end for text, records it for
// undo, and returns the position just after the inserted text.
func (b *Buffer) Replace(start, end Pos, text string, cursor Pos) Pos {
	start, end = order(start, end)
	old := b.Text(start, end)
	if old == "" && text == "" {
		return start
	}
	after := b.replace(start, end, text)
	b.record(edit{start: start, old: old, new: text, before: cursor, after: after, at: time.Now()})
	return after
}

// record pushes e, merging runs of typing or deleting into one undo step.
func (b *Buffer) record(e edit) {
	b.redo = b.redo[:0]
	b.nextID++
	e.id = b.nextID
	if n := len(b.undo); n > 0 {
		p := &b.undo[n-1]
		recent := e.at.Sub(p.at) < time.Second
		typing := p.old == "" && e.old == "" && e.start == endOf(p.start, p.new) &&
			!strings.Contains(e.new, "\n") && len(e.new) <= 4
		backspacing := p.new == "" && e.new == "" && endOf(e.start, e.old) == p.start &&
			!strings.Contains(e.old, "\n")
		if recent && typing {
			p.new += e.new
			p.after, p.at, p.id = e.after, e.at, e.id
			return
		}
		if recent && backspacing {
			p.start = e.start
			p.old = e.old + p.old
			p.after, p.at, p.id = e.after, e.at, e.id
			return
		}
	}
	b.undo = append(b.undo, e)
}

// Undo reverts the last edit and returns the cursor to restore.
func (b *Buffer) Undo() (Pos, bool) {
	if len(b.undo) == 0 {
		return Pos{}, false
	}
	e := b.undo[len(b.undo)-1]
	b.undo = b.undo[:len(b.undo)-1]
	b.replace(e.start, endOf(e.start, e.new), e.old)
	b.redo = append(b.redo, e)
	return e.before, true
}

func (b *Buffer) Redo() (Pos, bool) {
	if len(b.redo) == 0 {
		return Pos{}, false
	}
	e := b.redo[len(b.redo)-1]
	b.redo = b.redo[:len(b.redo)-1]
	b.replace(e.start, endOf(e.start, e.old), e.new)
	e.at = time.Time{} // never merge a redone edit
	b.undo = append(b.undo, e)
	return e.after, true
}

// Find searches for q starting at from, wrapping around. Lower-case queries
// match case-insensitively (smart case).
func (b *Buffer) Find(q string, from Pos, backward bool) (Pos, bool) {
	if q == "" || strings.Contains(q, "\n") {
		return Pos{}, false
	}
	index := matcher(q)
	n := len(b.lines)
	for i := 0; i <= n; i++ {
		li := (from.Line + i) % n
		if backward {
			li = ((from.Line-i)%n + n) % n
		}
		s := b.lines[li]
		if backward {
			lim := len(s)
			if i == 0 {
				lim = from.Col
			}
			last := -1
			for off := 0; ; {
				j := index(s[off:lim])
				if j < 0 {
					break
				}
				last = off + j
				off = last + 1
			}
			if last >= 0 {
				return Pos{li, last}, true
			}
		} else {
			off := 0
			if i == 0 {
				off = from.Col
			}
			if j := index(s[off:]); j >= 0 {
				return Pos{li, off + j}, true
			}
		}
	}
	return Pos{}, false
}

// matcher returns a function that finds q in a line. A lower-case query
// matches case-insensitively without allocating for ASCII queries.
func matcher(q string) func(string) int {
	exact := func(s string) int { return strings.Index(s, q) }
	if strings.ToLower(q) != q || strings.ToUpper(q) == q {
		return exact // has upper case, or nothing to fold
	}
	for i := 0; i < len(q); i++ {
		if q[i] >= 0x80 {
			// Non-ASCII query: fold the line, if that keeps byte offsets.
			return func(s string) int {
				if l := strings.ToLower(s); len(l) == len(s) {
					return strings.Index(l, q)
				}
				return strings.Index(s, q)
			}
		}
	}
	return func(s string) int { return indexFoldASCII(s, q) }
}

// indexFoldASCII finds the lower-case ASCII string q in s, ignoring case.
func indexFoldASCII(s, q string) int {
	lo, up := q[0], q[0]
	if 'a' <= lo && lo <= 'z' {
		up = lo - 'a' + 'A'
	}
	nextLo, nextUp := -1, -1 // cached positions of the first byte's two cases
	for i := 0; i+len(q) <= len(s); {
		if nextLo < i {
			if nextLo = strings.IndexByte(s[i:], lo); nextLo < 0 {
				nextLo = len(s)
			} else {
				nextLo += i
			}
		}
		if nextUp < i {
			if nextUp = strings.IndexByte(s[i:], up); nextUp < 0 {
				nextUp = len(s)
			} else {
				nextUp += i
			}
		}
		i = min(nextLo, nextUp)
		if i+len(q) > len(s) {
			return -1
		}
		if equalFoldASCII(s[i:i+len(q)], q) {
			return i
		}
		i++
	}
	return -1
}

func equalFoldASCII(s, lower string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != lower[i] {
			return false
		}
	}
	return true
}
