package main

import (
	"fmt"
	"image"
	"image/color"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"gioui.org/layout"
)

// panel draws a box with a border above the status bar.
func (e *Editor) panel(gtx layout.Context, r image.Rectangle) {
	fill(gtx, r, colCurLine)
	fill(gtx, r.Inset(1), colStatus)
}

// drawFinder lists the best matches above the prompt, the best at the bottom.
func (e *Editor) drawFinder(gtx layout.Context, width, bottom int) {
	f := &e.finder
	lh := e.lineH
	rows := max(1, len(f.results))
	rows = min(rows, max(1, bottom/lh-1))
	top := bottom - rows*lh - gtx.Dp(8)
	e.panel(gtx, image.Rect(-1, top, width+1, bottom+1))
	pad := gtx.Dp(12)
	if len(f.results) == 0 {
		msg := "no matches"
		if f.walking != nil {
			msg = "scanning…"
		}
		e.label(gtx, pad, bottom-lh-gtx.Dp(4), msg, tokColors[tokComment])
		return
	}
	q := []rune(f.query)
	fold := !slices.ContainsFunc(q, unicode.IsUpper)
	if fold {
		for i, r := range q {
			q[i] = unicode.ToLower(r)
		}
	}
	for i, r := range f.results[:rows] {
		y := bottom - (i+1)*lh - gtx.Dp(4)
		if i == f.sel {
			fill(gtx, image.Rect(0, y, width, y+lh), colSel)
			fill(gtx, image.Rect(0, y, gtx.Dp(3), y+lh), colAccent)
		}
		pos, _ := fuzzyMatch(r.path, q, fold, nil)
		base := strings.LastIndexByte(r.path, '/') + 1
		e.drawRuns(gtx, pad, y, r.path, func(b int) color.NRGBA {
			switch {
			case slices.Contains(pos, b):
				return colAccent
			case b < base:
				return tokColors[tokComment]
			}
			return colText
		})
	}
}

// drawRuns draws s one rune per column, colored by byte offset, with one
// label per run of the same color.
func (e *Editor) drawRuns(gtx layout.Context, x, y int, s string, colorAt func(int) color.NRGBA) {
	col, start, startCol := 0, 0, 0
	var cur color.NRGBA
	for i := range s {
		c := colorAt(i)
		if i > 0 && c != cur {
			e.label(gtx, x+int(float32(startCol)*e.charW), y, s[start:i], cur)
			start, startCol = i, col
		}
		cur = c
		col++
	}
	if start < len(s) {
		e.label(gtx, x+int(float32(startCol)*e.charW), y, s[start:], cur)
	}
}

// drawInfo shows the properties of the file, or of the listing entry under
// the cursor, in a box at the bottom right.
func (e *Editor) drawInfo(gtx layout.Context, width, bottom int) {
	var title string
	var props []prop
	switch en, ok := e.entry(e.cursor.Line); {
	case e.dir != nil && ok:
		title = en.name
		if en.name == ".." {
			title = filepath.Base(en.path)
		}
		props = fileProps(en.path, -1, false)
		props = append(props, prop{"path", shortPath(en.path)})
	case e.dir != nil:
		title = filepath.Base(e.path)
		props = fileProps(e.path, -1, false)
	case e.path != "":
		title = filepath.Base(e.path)
		props = fileProps(e.path, e.buf.LineCount(), e.buf.Dirty())
		props = append(props, prop{"path", shortPath(e.path)})
	default:
		title = "scratch"
		props = []prop{{"lines", groupDigits(int64(e.buf.LineCount()))}, {"", "not saved to disk yet"}}
	}

	const keyW = 10
	cols := utf8.RuneCountInString(title)
	for _, p := range props {
		cols = max(cols, keyW+utf8.RuneCountInString(p.val))
	}
	maxCols := int(float32(width-gtx.Dp(48)) / e.charW)
	cols = min(cols, maxCols)
	pad := gtx.Dp(12)
	lh := e.lineH
	w := int(float32(cols)*e.charW) + 2*pad
	h := (len(props)+1)*lh + 2*gtx.Dp(6) + gtx.Dp(4)
	r := image.Rect(width-w-gtx.Dp(12), bottom-h-gtx.Dp(8), width-gtx.Dp(12), bottom-gtx.Dp(8))
	e.panel(gtx, r)

	x, y := r.Min.X+pad, r.Min.Y+gtx.Dp(6)
	e.labelFont(gtx, x, y, truncLeft(title, cols), colAccent, true)
	y += lh + gtx.Dp(4)
	for _, p := range props {
		vx, room := x+int(keyW*e.charW), cols-keyW
		if p.key == "" {
			vx, room = x, cols
		} else {
			e.label(gtx, x, y, p.key, tokColors[tokComment])
		}
		c := colText
		if p.key == "" {
			c = tokColors[tokWarn]
		}
		e.label(gtx, vx, y, truncLeft(p.val, room), c)
		y += lh
	}
}

// grepRows is how many search hits the panel shows at once.
func (e *Editor) grepRows() int { return max(1, e.viewH*6/10/max(1, e.lineH)) }

// drawGrep lists the folder search hits above the prompt, in file and line
// order, keeping the selected one in view.
func (e *Editor) drawGrep(gtx layout.Context, width, bottom int) {
	g := &e.grep
	lh := e.lineH
	rows := min(max(1, len(g.hits)), e.grepRows())
	top := bottom - rows*lh - gtx.Dp(8)
	e.panel(gtx, image.Rect(-1, top, width+1, bottom+1))
	pad := gtx.Dp(12)
	if len(g.hits) == 0 {
		msg := "no matches"
		switch {
		case g.query == "":
			msg = "type to search the files in " + shortPath(e.root)
		case g.results != nil:
			msg = "searching…"
		}
		e.label(gtx, pad, top+gtx.Dp(4), msg, tokColors[tokComment])
		return
	}
	g.top = min(g.top, g.sel)
	if g.sel >= g.top+rows {
		g.top = g.sel - rows + 1
	}
	g.top = max(0, min(g.top, len(g.hits)-rows))
	cols := int(float32(width-2*pad) / e.charW)
	// The file and line take up to a third of the width, aligned.
	pw := 0
	for _, h := range g.hits[g.top : g.top+rows] {
		pw = max(pw, utf8.RuneCountInString(h.path)+1+len(strconv.Itoa(h.line+1)))
	}
	pw = min(pw, cols/3)
	room := cols - pw - 2
	q := len(g.query)
	for i, h := range g.hits[g.top : g.top+rows] {
		y := top + gtx.Dp(4) + i*lh
		if g.top+i == g.sel {
			fill(gtx, image.Rect(0, y, width, y+lh), colSel)
			fill(gtx, image.Rect(0, y, gtx.Dp(3), y+lh), colAccent)
		}
		e.label(gtx, pad, y, truncLeft(fmt.Sprintf("%s:%d", h.path, h.line+1), pw), tokColors[tokComment])

		text, col, cut := h.text, h.col, h.cut
		lead := min(col, len(text)-len(strings.TrimLeft(text, " \t")))
		text, col = text[lead:], col-lead
		if col+q > room-1 && col > 10 {
			// Bring the match into view.
			s := col - 10
			for s < col && !utf8.RuneStart(text[s]) {
				s++
			}
			text, col, cut = text[s:], col-s, true
		}
		if cut {
			text, col = "…"+text, col+len("…")
		}
		text = strings.ReplaceAll(text, "\t", " ")
		if n := 0; utf8.RuneCountInString(text) > room {
			for j := range text {
				if n == room {
					text = text[:j]
					break
				}
				n++
			}
		}
		e.drawRuns(gtx, pad+int(float32(pw+2)*e.charW), y, text, func(b int) color.NRGBA {
			if b >= col && b < col+q {
				return colAccent
			}
			return colText
		})
	}
}
