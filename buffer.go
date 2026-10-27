package main

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
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
	lines         []lineEdit // set instead of the above for a replace-all
}

// lineEdit is one line changed by a replace-all.
type lineEdit struct {
	line     int
	old, new string
}

// Buffer holds the document as lines without terminators.
type Buffer struct {
	lines   lineStore
	undo    []edit
	redo    []edit
	version int // bumped on every change
	nextID  int
	saved   int // state() at last load/save
	edited  int // first line changed since the last TakeEdited
	cols    colIndex
	CRLF    bool
	UseTabs bool
}

func NewBuffer(text string) *Buffer {
	b := &Buffer{edited: math.MaxInt}
	b.lines.init(text)
	b.CRLF = b.lines.crlf
	b.UseTabs = b.detectTabs()
	return b
}

func Load(path string) (*Buffer, error) {
	data, err := readFile(path)
	if err != nil {
		return nil, err
	}
	return NewBuffer(data), nil
}

// Poll takes in lines indexed in the background and reports whether the
// file is still loading. Edits must wait until loading is done.
func (b *Buffer) Poll() bool    { return b.lines.poll() }
func (b *Buffer) Loading() bool { return b.lines.loading != nil }
func (b *Buffer) LoadPct() int  { return b.lines.progressPct() }

func (b *Buffer) detectTabs() bool {
	tabs, spaces := 0, 0
	for i := range min(b.LineCount(), 5000) {
		if l := b.Line(i); strings.HasPrefix(l, "\t") {
			tabs++
		} else if strings.HasPrefix(l, "  ") {
			spaces++
		}
	}
	return tabs > spaces
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
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".nib*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := b.lines.writeTo(tmp, sep); err != nil {
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

// TakeEdited returns the first line changed since the last call, if any.
func (b *Buffer) TakeEdited() (int, bool) {
	l := b.edited
	b.edited = math.MaxInt
	return l, l != math.MaxInt
}

func (b *Buffer) Dirty() bool       { return b.state() != b.saved }
func (b *Buffer) LineCount() int    { return b.lines.count }
func (b *Buffer) Line(i int) string { return b.lines.line(i) }

// VisCol returns the visual column of byte col in line i.
func (b *Buffer) VisCol(i, col int) int {
	s := b.Line(i)
	if len(s) < longLine {
		return visCol(s, col)
	}
	return markAt(s, b.cols.near(i, s, col, 0), col).v
}

// ColAt returns the byte column in line i nearest to visual position x.
func (b *Buffer) ColAt(i int, x float32) int {
	s := b.Line(i)
	if len(s) < longLine {
		return colAt(s, x)
	}
	return colAtFrom(s, b.cols.near(i, s, -1, x), x)
}

// Visible returns line i between visual columns v0 and v1, tabs expanded.
func (b *Buffer) Visible(i, v0, v1 int) string {
	s := b.Line(i)
	if len(s) < longLine {
		return displaySlice(s, v0, v1)
	}
	return displayFrom(s, b.cols.near(i, s, -1, float32(v0)), v0, v1)
}

func (b *Buffer) End() Pos {
	n := b.LineCount() - 1
	return Pos{n, len(b.Line(n))}
}

// Clamp returns the nearest valid position to p.
func (b *Buffer) Clamp(p Pos) Pos {
	if p.Line < 0 {
		return Pos{}
	}
	if p.Line >= b.LineCount() {
		return b.End()
	}
	p.Col = max(0, min(p.Col, len(b.Line(p.Line))))
	return p
}

func (b *Buffer) Text(start, end Pos) string {
	start, end = order(start, end)
	if start.Line == end.Line {
		return b.Line(start.Line)[start.Col:end.Col]
	}
	n := len(b.Line(start.Line)) - start.Col + end.Col
	for i := start.Line + 1; i <= end.Line; i++ {
		n += len(b.Line(i)) + 1
	}
	var sb strings.Builder
	sb.Grow(n)
	sb.WriteString(b.Line(start.Line)[start.Col:])
	for i := start.Line + 1; i < end.Line; i++ {
		sb.WriteByte('\n')
		sb.WriteString(b.Line(i))
	}
	sb.WriteByte('\n')
	sb.WriteString(b.Line(end.Line)[:end.Col])
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
	prefix := b.Line(start.Line)[:start.Col]
	suffix := b.Line(end.Line)[end.Col:]
	nl := strings.Split(text, "\n")
	nl[0] = prefix + nl[0]
	nl[len(nl)-1] += suffix
	b.lines.replace(start.Line, end.Line, nl)
	b.version++
	b.edited = min(b.edited, start.Line)
	b.cols.edited(start.Line, start.Col)
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
	if e.lines != nil {
		b.setLines(e.lines, false)
	} else {
		b.replace(e.start, endOf(e.start, e.new), e.old)
	}
	b.redo = append(b.redo, e)
	return e.before, true
}

func (b *Buffer) Redo() (Pos, bool) {
	if len(b.redo) == 0 {
		return Pos{}, false
	}
	e := b.redo[len(b.redo)-1]
	b.redo = b.redo[:len(b.redo)-1]
	if e.lines != nil {
		b.setLines(e.lines, true)
	} else {
		b.replace(e.start, endOf(e.start, e.old), e.new)
	}
	e.at = time.Time{} // never merge a redone edit
	b.undo = append(b.undo, e)
	return e.after, true
}

// ReplaceAll replaces every match of q with r as one undo step and returns
// how many it replaced. Neither may contain a newline, so line numbers stay
// the same and only the lines with a match are touched.
func (b *Buffer) ReplaceAll(q, r string, cursor Pos) int {
	if q == "" || strings.Contains(q, "\n") || strings.Contains(r, "\n") {
		return 0
	}
	index := matcher(q)
	lines := b.lines.matchingLines(index)
	if len(lines) == 0 {
		return 0
	}
	changes := make([]lineEdit, len(lines))
	n := 0
	var sb strings.Builder
	for i, l := range lines {
		old := b.Line(l)
		sb.Reset()
		for s := old; ; {
			j := index(s)
			if j < 0 {
				sb.WriteString(s)
				break
			}
			sb.WriteString(s[:j])
			sb.WriteString(r)
			s = s[j+len(q):]
			n++
		}
		changes[i] = lineEdit{l, old, sb.String()}
	}
	b.setLines(changes, true)
	b.redo = b.redo[:0]
	b.nextID++
	// A zero time keeps the next typing from merging into this step.
	b.undo = append(b.undo, edit{lines: changes, before: cursor, after: cursor, id: b.nextID})
	return n
}

// setLines puts the new (or, undoing, the old) text of each changed line.
func (b *Buffer) setLines(changes []lineEdit, redo bool) {
	for _, c := range changes {
		s := c.old
		if redo {
			s = c.new
		}
		b.lines.replace(c.line, c.line, []string{s})
		b.cols.edited(c.line, 0)
	}
	b.version++
	b.edited = min(b.edited, changes[0].line)
}

// Find searches for q starting at from, wrapping around. Lower-case queries
// match case-insensitively (smart case).
func (b *Buffer) Find(q string, from Pos, backward bool) (Pos, bool) {
	if q == "" || strings.Contains(q, "\n") {
		return Pos{}, false
	}
	index := matcher(q)
	n := b.LineCount()
	from = b.Clamp(from)
	l, s := from.Line, b.Line(from.Line)
	if !backward {
		if j := index(s[from.Col:]); j >= 0 {
			return Pos{l, from.Col + j}, true
		}
		// The rest of the file, then from the top back to here.
		return b.lines.searchLines([]lineRange{{l + 1, n}, {0, l + 1}}, false, index)
	}
	last := -1
	for off := 0; ; {
		j := index(s[off:from.Col])
		if j < 0 {
			break
		}
		last = off + j
		off = last + 1
	}
	if last >= 0 {
		return Pos{l, last}, true
	}
	return b.lines.searchLines([]lineRange{{0, l}, {l, n}}, true, index)
}

// matcher returns a function that finds q in a line. A lower-case query
// matches case-insensitively without allocating for ASCII queries.
func matcher(q string) func(string) int {
	exact := func(s string) int { return indexRare(s, q, false) }
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
	return func(s string) int { return indexRare(s, q, true) }
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
