package main

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// pairs maps an opening bracket or quote to its closer.
var pairs = map[string]string{"(": ")", "[": "]", "{": "}", `"`: `"`, "'": "'", "`": "`"}

// typeText inserts typed text, closing brackets and quotes as they are
// opened: a typed opener before a space, a closer or the end of the line
// gets its closer after the cursor, a typed closer that is already there
// is skipped over, and an opener typed over a selection wraps it.
func (e *Editor) typeText(s string) {
	if e.locked() {
		return
	}
	closer, isOpen := pairs[s]
	a, b, hasSel := e.sel()
	switch {
	case hasSel && isOpen:
		t := e.buf.Text(a, b)
		p := e.buf.Replace(a, b, s+t+closer, e.cursor)
		e.anchor = Pos{a.Line, a.Col + len(s)}
		e.cursor = Pos{p.Line, p.Col - len(closer)}
		e.goal, e.followCursor = -1, true
		return
	case !hasSel && isCloser(s) && nextRuneAt(e.buf.Line(b.Line), b.Col) == s:
		e.moveTo(e.right(e.cursor), false)
		return
	case !hasSel && isOpen && e.shouldPair(s, a):
		p := e.buf.Replace(a, a, s+closer, e.cursor)
		e.moveTo(Pos{p.Line, p.Col - len(closer)}, false)
		return
	}
	e.insert(s)
}

func isCloser(s string) bool { return strings.Contains(`)]}"'`+"`", s) && s != "" }

func nextRuneAt(line string, col int) string {
	if col >= len(line) {
		return ""
	}
	_, n := utf8.DecodeRuneInString(line[col:])
	return line[col : col+n]
}

// shouldPair says whether an opener typed at p should get its closer: only
// before a space, a closer or the end of the line, and for quotes not right
// after a word or another quote, so don't and it's stay single.
func (e *Editor) shouldPair(s string, p Pos) bool {
	line := e.buf.Line(p.Line)
	next := nextRuneAt(line, p.Col)
	if next != "" && !strings.ContainsAny(next, " \t)]};,") {
		return false
	}
	if pairs[s] != s {
		return true
	}
	if p.Col == 0 {
		return true
	}
	prev, _ := utf8.DecodeLastRuneInString(line[:p.Col])
	return !(prev == '_' || prev == '\\' || unicode.IsLetter(prev) || unicode.IsDigit(prev) || strings.ContainsRune(`"'`+"`", prev))
}

// deletePair removes both halves of an empty pair when Backspace is pressed
// between them, and reports whether it did.
func (e *Editor) deletePair() bool {
	c := e.cursor
	line := e.buf.Line(c.Line)
	if c.Col == 0 || c.Col >= len(line) {
		return false
	}
	open, next := line[c.Col-1:c.Col], line[c.Col:c.Col+1]
	if pairs[open] != next {
		return false
	}
	e.buf.Replace(Pos{c.Line, c.Col - 1}, Pos{c.Line, c.Col + 1}, "", e.cursor)
	e.moveTo(Pos{c.Line, c.Col - 1}, false)
	return true
}
