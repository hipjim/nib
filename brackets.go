package main

import (
	"image"

	"gioui.org/layout"
)

// bracketReach is how many lines around the screen a partner bracket is
// looked for, and bracketBytes how much text at most.
const (
	bracketReach = 2000
	bracketBytes = 256 << 10
)

var bracketPartner = map[byte]byte{'(': ')', '[': ']', '{': '}', ')': '(', ']': '[', '}': '{'}

// bracketPair returns the bracket the cursor touches and its partner. The
// bracket before the cursor wins, as after typing one. The partner is only
// looked for near the screen, so a frame never scans a whole big file.
func (e *Editor) bracketPair(first, last int) (Pos, Pos, bool) {
	l, c := e.cursor.Line, e.cursor.Col
	line := e.buf.Line(l)
	if len(line) >= longLine {
		return Pos{}, Pos{}, false
	}
	at := -1
	if c > 0 && bracketPartner[line[c-1]] != 0 {
		at = c - 1
	} else if c < len(line) && bracketPartner[line[c]] != 0 {
		at = c
	}
	if at < 0 {
		return Pos{}, Pos{}, false
	}
	open, want := line[at], bracketPartner[line[at]]
	forward := open == '(' || open == '[' || open == '{'
	depth, budget := 0, bracketBytes
	lo, hi := max(0, first-bracketReach), min(e.buf.LineCount()-1, last+bracketReach)
	for i := l; i >= lo && i <= hi && budget > 0; {
		s := e.buf.Line(i)
		if len(s) >= longLine {
			return Pos{}, Pos{}, false
		}
		budget -= len(s)
		if forward {
			for j := at; j < len(s); j++ {
				if depth = depthAfter(s[j], open, want, depth); depth == 0 {
					return Pos{l, at}, Pos{i, j}, true
				}
			}
			i, at = i+1, 0
		} else {
			for j := at; j >= 0; j-- {
				if depth = depthAfter(s[j], open, want, depth); depth == 0 {
					return Pos{l, at}, Pos{i, j}, true
				}
			}
			i--
			if i >= lo {
				at = len(e.buf.Line(i)) - 1
			}
		}
	}
	return Pos{}, Pos{}, false
}

func depthAfter(b, open, want byte, depth int) int {
	switch b {
	case open:
		return depth + 1
	case want:
		return depth - 1
	}
	return depth
}

// outlineCell draws a thin frame around the character at p, if on screen.
func (e *Editor) outlineCell(gtx layout.Context, xOf func(int) int, p Pos, sy int) {
	y := p.Line*e.lineH - sy
	if y+e.lineH < 0 || y > e.viewH {
		return
	}
	x0, x1 := xOf(e.buf.VisCol(p.Line, p.Col)), xOf(e.buf.VisCol(p.Line, p.Col+1))
	r := image.Rect(x0, y, x1, y+e.lineH)
	w := gtx.Dp(1)
	fill(gtx, image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+w), colGutterHi)
	fill(gtx, image.Rect(r.Min.X, r.Max.Y-w, r.Max.X, r.Max.Y), colGutterHi)
	fill(gtx, image.Rect(r.Min.X, r.Min.Y, r.Min.X+w, r.Max.Y), colGutterHi)
	fill(gtx, image.Rect(r.Max.X-w, r.Min.Y, r.Max.X, r.Max.Y), colGutterHi)
}
