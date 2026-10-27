package main

import (
	"cmp"
	"encoding/binary"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
	"unsafe"
)

const tabWidth = 4

// visCol converts a byte column into a visual (on-screen) column,
// expanding tabs.
func visCol(line string, col int) int {
	return markAt(line, colMark{}, col).v
}

func runeWidth(r rune, v int) int {
	if r == '\t' {
		return tabWidth - v%tabWidth
	}
	return 1
}

// colAt returns the byte column nearest to the visual position x.
func colAt(line string, x float32) int {
	return colAtFrom(line, colMark{}, x)
}

// displaySlice returns the part of line between visual columns v0 and v1
// with tabs expanded to spaces.
func displaySlice(line string, v0, v1 int) string {
	return displayFrom(line, colMark{}, v0, v1)
}

// A colMark is a rune start in a line and its visual column.
type colMark struct{ b, v int }

// nextRune decodes the rune at s[i] the way ranging over s does.
func nextRune(s string, i int) (rune, int) {
	if c := s[i]; c < utf8.RuneSelf {
		return rune(c), 1
	}
	return utf8.DecodeRuneInString(s[i:])
}

// markAt scans s from m to the first rune start at or after byte col.
func markAt(s string, m colMark, col int) colMark {
	i, v := m.b, m.v
	end := min(col, len(s))
	for i < end {
		if i+8 <= end && plain8(s, i) {
			i += 8
			v += 8
			continue
		}
		r, n := nextRune(s, i)
		v += runeWidth(r, v)
		i += n
	}
	return colMark{i, v}
}

// plain8 reports whether the 8 bytes at s[i] are ASCII without a tab, so
// each is one column wide.
func plain8(s string, i int) bool {
	const ones, high, tabs = 0x0101010101010101, 0x8080808080808080, 0x0909090909090909
	w := binary.LittleEndian.Uint64(unsafe.Slice(unsafe.StringData(s[i:]), 8))
	t := w ^ tabs // a zero byte where w has a tab
	return w&high == 0 && (t-ones)&^t&high == 0
}

// colAtFrom is colAt for the part of s from m on, m being at or before x.
func colAtFrom(s string, m colMark, x float32) int {
	i, v := m.b, m.v
	for i < len(s) {
		r, n := nextRune(s, i)
		w := runeWidth(r, v)
		if x < float32(v)+float32(w)/2 {
			return i
		}
		v += w
		i += n
	}
	return len(s)
}

// displayFrom is displaySlice for the part of s from m on, m being at or
// before v0.
func displayFrom(s string, m colMark, v0, v1 int) string {
	var sb strings.Builder
	for i, v := m.b, m.v; i < len(s) && v < v1; {
		r, n := nextRune(s, i)
		w := runeWidth(r, v)
		if v+w > v0 {
			if r == '\t' || v < v0 {
				sb.WriteString(strings.Repeat(" ", min(v+w, v1)-max(v, v0)))
			} else {
				sb.WriteRune(r)
			}
		}
		v += w
		i += n
	}
	return sb.String()
}

const (
	longLine = 4 << 10 // lines this long look columns up in a colIndex
	colStep  = 4 << 10 // bytes between marks
	colLines = 4       // long lines remembered
)

// colIndex keeps a mark every colStep bytes of the last few long lines
// used, so a column at the end of an 18 MB line is found as fast as one at
// its start. Edits drop the marks after them.
type colIndex struct {
	lines []colLine // most recently used first
}

type colLine struct {
	line  int
	marks []colMark // marks[0] is the line start
}

// near returns the last mark at or before byte col, or, with col < 0, at
// or before visual column x, adding marks as far as needed.
func (c *colIndex) near(line int, s string, col int, x float32) colMark {
	l := c.get(line, len(s))
	before := func(m colMark) bool {
		if col >= 0 {
			return m.b <= col
		}
		return float32(m.v) <= x
	}
	for last := l.marks[len(l.marks)-1]; last.b < len(s) && before(last); last = l.marks[len(l.marks)-1] {
		l.marks = append(l.marks, markAt(s, last, last.b+colStep))
	}
	k, _ := slices.BinarySearchFunc(l.marks, true, func(m colMark, _ bool) int {
		if before(m) {
			return -1
		}
		return 1
	})
	return l.marks[max(0, k-1)]
}

// get returns the marks of line, which is n bytes long, moving them to the
// front.
func (c *colIndex) get(line, n int) *colLine {
	i := slices.IndexFunc(c.lines, func(l colLine) bool { return l.line == line })
	if i < 0 {
		if len(c.lines) < colLines {
			c.lines = append(c.lines, colLine{})
		}
		i = len(c.lines) - 1
		c.lines[i] = colLine{line: line, marks: []colMark{{}}}
	}
	l := c.lines[i]
	copy(c.lines[1:i+1], c.lines[:i])
	if l.marks[len(l.marks)-1].b > n {
		l.marks = l.marks[:1] // a change it was not told about
	}
	c.lines[0] = l
	return &c.lines[0]
}

// edited drops the marks after byte col of line, and those of later lines.
func (c *colIndex) edited(line, col int) {
	keep := c.lines[:0]
	for _, l := range c.lines {
		if l.line == line {
			k, _ := slices.BinarySearchFunc(l.marks, col+1, func(m colMark, b int) int { return cmp.Compare(m.b, b) })
			l.marks = l.marks[:max(1, k)]
		}
		if l.line <= line {
			keep = append(keep, l)
		}
	}
	c.lines = keep
}

func class(r rune) int {
	switch {
	case unicode.IsSpace(r):
		return 0
	case r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
		return 1
	}
	return 2
}

// wordEnd returns the column after the word that starts at or after col.
func wordEnd(line string, col int) int {
	for col < len(line) {
		r, n := utf8.DecodeRuneInString(line[col:])
		if class(r) != 0 {
			break
		}
		col += n
	}
	if col == len(line) {
		return col
	}
	r, _ := utf8.DecodeRuneInString(line[col:])
	c := class(r)
	for col < len(line) {
		r, n := utf8.DecodeRuneInString(line[col:])
		if class(r) != c {
			break
		}
		col += n
	}
	return col
}

// wordStart returns the column of the start of the word before col.
func wordStart(line string, col int) int {
	for col > 0 {
		r, n := utf8.DecodeLastRuneInString(line[:col])
		if class(r) != 0 {
			break
		}
		col -= n
	}
	if col == 0 {
		return 0
	}
	r, _ := utf8.DecodeLastRuneInString(line[:col])
	c := class(r)
	for col > 0 {
		r, n := utf8.DecodeLastRuneInString(line[:col])
		if class(r) != c {
			break
		}
		col -= n
	}
	return col
}

// wordAt returns the bounds of the word touching col.
func wordAt(line string, col int) (int, int) {
	isWord := func(r rune) bool { return class(r) == 1 }
	s, e := col, col
	for s > 0 {
		r, n := utf8.DecodeLastRuneInString(line[:s])
		if !isWord(r) {
			break
		}
		s -= n
	}
	for e < len(line) {
		r, n := utf8.DecodeRuneInString(line[e:])
		if !isWord(r) {
			break
		}
		e += n
	}
	return s, e
}

func leadingSpace(s string) string {
	return s[:len(s)-len(strings.TrimLeft(s, " \t"))]
}
