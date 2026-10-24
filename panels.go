package main

import (
	"image"
	"image/color"
	"path/filepath"
	"slices"
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
