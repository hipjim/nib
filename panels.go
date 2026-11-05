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

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
)

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
		switch {
		case f.query == "":
			msg = "type a few letters of a file name"
		case f.walking != nil:
			msg = "scanning…"
		}
		e.label(gtx, pad, bottom-lh-gtx.Dp(4), msg, colInfo)
		return
	}
	q := []rune(f.query)
	fold := !slices.ContainsFunc(q, unicode.IsUpper)
	if fold {
		for i, r := range q {
			q[i] = unicode.ToLower(r)
		}
	}
	open := e.openPaths()
	first := max(0, f.sel-rows+1)
	for i, r := range f.results[first : first+rows] {
		y := bottom - (i+1)*lh - gtx.Dp(4)
		if first+i == f.sel {
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
				return colInfo
			}
			return colText
		})
		if dirty, ok := open[r.path]; ok {
			c := colInfo
			if dirty {
				c = colAccent
			}
			e.label(gtx, pad+int(float32(utf8.RuneCountInString(r.path)+1)*e.charW), y, "●", c)
		}
	}
}

// paletteRows is how many commands fit above the prompt.
func (e *Editor) paletteRows() int { return max(1, (e.viewH*7/10)/max(1, e.lineH)) }

// drawPalette lists the matching commands above the prompt like the file
// finder, best at the bottom, each with its key at the right.
func (e *Editor) drawPalette(gtx layout.Context, width, bottom int) {
	p := &e.palette
	lh := e.lineH
	rows := max(1, min(len(p.results), e.paletteRows(), max(1, bottom/lh-1)))
	top := bottom - rows*lh - gtx.Dp(8)
	e.panel(gtx, image.Rect(-1, top, width+1, bottom+1))
	pad := gtx.Dp(12)
	if len(p.results) == 0 {
		e.label(gtx, pad, bottom-lh-gtx.Dp(4), "no such command", colInfo)
		return
	}
	q := []rune(e.input)
	fold := !slices.ContainsFunc(q, unicode.IsUpper)
	if fold {
		for i, r := range q {
			q[i] = unicode.ToLower(r)
		}
	}
	first := max(0, p.sel-rows+1)
	for i, idx := range p.results[first : first+rows] {
		c := p.all[idx]
		y := bottom - (i+1)*lh - gtx.Dp(4)
		if first+i == p.sel {
			fill(gtx, image.Rect(0, y, width, y+lh), colSel)
			fill(gtx, image.Rect(0, y, gtx.Dp(3), y+lh), colAccent)
		}
		pos, _ := fuzzyMatch(c.name, q, fold, nil)
		e.drawRuns(gtx, pad, y, c.name, func(b int) color.NRGBA {
			if slices.Contains(pos, b) {
				return colAccent
			}
			return colText
		})
		e.label(gtx, width-pad-int(float32(utf8.RuneCountInString(c.keys))*e.charW), y, c.keys, colInfo)
	}
}

func (e *Editor) openPaths() map[string]bool {
	m := map[string]bool{}
	for _, d := range e.docs {
		if d.dir == nil && d.path != "" {
			m[finderPath(e.finder.root, d.path)] = d.buf.Dirty()
		}
	}
	return m
}

// drawHint greets an empty scratch buffer: the logo, the name and the
// ways to open something, centered. The logo and then the name go when
// the window is too short for them.
func (e *Editor) drawHint(gtx layout.Context, width, height int) {
	hints := [][2]string{
		{shortcutName + "O", "browse the folder"},
		{shortcutName + "P", "find a file in the folder"},
		{shortcutName + shiftName + "F", "search the folder"},
		{shortcutName + shiftName + "/", "all the keys"},
	}
	keyW, descW := 0, 0
	for _, h := range hints {
		keyW = max(keyW, utf8.RuneCountInString(h[0]))
		descW = max(descW, utf8.RuneCountInString(h[1]))
	}
	lh := e.lineH
	logoH, nameH := 5*lh, 2*lh
	h := len(hints)*lh + nameH + lh/2 + logoH + lh
	// Never over the first line, where the cursor is, nor the last.
	if h > height-4*lh {
		h -= logoH + lh
		logoH = 0
	}
	if h > height-4*lh {
		h -= nameH + lh/2
		nameH = 0
	}
	cx := e.textX + (width-e.textX)/2
	y := max(2*lh, (height-h)/2)
	if logoH > 0 {
		scale := float32(logoH) / float32(logoBox.Dy())
		x := float32(cx) - float32(logoBox.Min.X+logoBox.Max.X)/2*scale
		drawLogo(gtx.Ops, f32.Pt(x, float32(y)-float32(logoBox.Min.Y)*scale), scale, colAccent)
		y += logoH + lh
	}
	if nameH > 0 {
		e.title(gtx, cx, y, "nib", colText, e.size*2)
		y += nameH + lh/2
	}
	cols := keyW + 2 + descW
	x := max(e.textX, cx-int(float32(cols)*e.charW)/2)
	for i, h := range hints {
		ly := y + i*lh
		e.label(gtx, x+int(float32(keyW-utf8.RuneCountInString(h[0]))*e.charW), ly, h[0], colInfo)
		e.label(gtx, x+int(float32(keyW+2)*e.charW), ly, h[1], colInfo)
	}
}

// title draws s in bold at the given size, centered on cx.
func (e *Editor) title(gtx layout.Context, cx, y int, s string, c color.NRGBA, size unit.Sp) {
	f := e.font
	f.Weight = font.Bold
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(1<<20, 1<<20)}
	m := op.Record(gtx.Ops)
	paint.ColorOp{Color: c}.Add(gtx.Ops)
	mat := m.Stop()
	m = op.Record(gtx.Ops)
	dims := widget.Label{MaxLines: 1}.Layout(g, e.shaper, f, size, s, mat)
	text := m.Stop()
	defer op.Offset(image.Pt(cx-dims.Size.X/2, y)).Push(gtx.Ops).Pop()
	text.Add(gtx.Ops)
}

func keyRows() [][2]string {
	rows := [][2]string{
		{"⌘S / ⌘⇧S", "save / save as"},
		{"⌘O / ⌘⇧O", "browse the folder / open a path (Tab completes)"},
		{"⌘P or ⇧⇧", "find a file in the folder"},
		{"⌘⇧P", "run any command by name"},
		{"⌘⇧F", "search every file in the folder"},
		{"⌘E", "show the file's folder"},
		{"⌘I / ⌘⇧I", "file properties / show spaces and tabs"},
		{"⌘T", "open a terminal in the file's folder"},
		{"⌘W / ⌘⇧W", "close the file / all other saved files"},
		{"Ctrl+Tab / Ctrl+⇧Tab", "next / previous tab"},
		{"⌘1 … ⌘9", "go to that tab"},
		{"⌘Q", "quit"},
		{"⌘F, then Enter / ⇧Enter / F3", "find next / previous"},
		{"⌘R, then Enter / ⌘Enter / ↓", "replace this match / all of them / skip"},
		{"⌘G", "go to line[:col]"},
		{"⌘Z / ⌘⇧Z / ⌘Y", "undo / redo"},
		{"⌘C / ⌘X / ⌘V", "copy / cut / paste (no selection: the line)"},
		{"⌘D", "select the word, then its next occurrence"},
		{"⌘L", "select the line (repeat to extend)"},
		{"⌘⇧D / ⌘⇧K", "duplicate / delete the line"},
		{"Alt↑ / Alt↓", "move the line up / down"},
		{"Tab / ⇧Tab", "indent / dedent"},
		{"⌘= / ⌘- / ⌘0", "zoom in / out / reset"},
		{"⌘⇧H", "syntax highlighting on / off"},
		{"⌘⇧L", "light / dark theme"},
		{"⌘/", "comment the lines out, or back in"},
		{"⌘⇧/ or F1", "this list"},
	}
	for i := range rows {
		rows[i][0] = strings.NewReplacer("⌘", shortcutName, "⇧", shiftName).Replace(rows[i][0])
	}
	return rows
}

func (e *Editor) toggleKeys() { e.keys, e.keysY = !e.keys, 0 }

// keysKey scrolls the key reference while it covers the text, so the
// arrows do not move a cursor nobody can see. It returns false for any
// other key, which the editor handles.
func (e *Editor) keysKey(name key.Name) bool {
	lh := float64(e.lineH)
	switch name {
	case key.NameUpArrow:
		e.keysY -= lh
	case key.NameDownArrow:
		e.keysY += lh
	case key.NamePageUp:
		e.keysY -= float64(e.viewH) / 2
	case key.NamePageDown:
		e.keysY += float64(e.viewH) / 2
	case key.NameHome:
		e.keysY = 0
	case key.NameEnd:
		e.keysY = 1 << 30 // drawKeys clamps it to the last row
	default:
		return false
	}
	return true
}

// drawKeys lists the keys in a panel over the text. When the window is
// too short for all of them the list scrolls, with a thumb at its edge.
func (e *Editor) drawKeys(gtx layout.Context, width, bottom int) {
	rows := keyRows()
	lh := e.lineH
	keyW, cols := 0, 0
	for _, r := range rows {
		keyW = max(keyW, utf8.RuneCountInString(r[0]))
	}
	for _, r := range rows {
		cols = max(cols, keyW+3+utf8.RuneCountInString(r[1]))
	}
	pad := gtx.Dp(16)
	maxCols := int(float32(width-2*pad-gtx.Dp(24)) / e.charW)
	cols = min(cols, max(keyW+3, maxCols))
	n := min(len(rows), max(1, (bottom-gtx.Dp(24))/lh-2))
	w := int(float32(cols)*e.charW) + 2*pad
	h := (n+1)*lh + gtx.Dp(12) + gtx.Dp(8)
	r := image.Rect((width-w)/2, (bottom-h)/2, (width+w)/2, (bottom+h)/2)
	e.panel(gtx, r)

	maxY := float64((len(rows) - n) * lh)
	e.keysY = max(0, min(e.keysY, maxY))
	top := int(e.keysY) / lh

	x, y := r.Min.X+pad, r.Min.Y+gtx.Dp(8)
	e.labelFont(gtx, x, y, "keys", colAccent, true)
	esc := "Esc closes"
	if n < len(rows) {
		esc = "↑↓ scroll · " + esc
	}
	e.label(gtx, r.Max.X-pad-int(float32(utf8.RuneCountInString(esc))*e.charW), y, esc, colInfo)
	y += lh + gtx.Dp(4)
	for _, row := range rows[top : top+n] {
		e.label(gtx, x, y, row[0], colText)
		e.label(gtx, x+int(float32(keyW+3)*e.charW), y, truncRight(row[1], cols-keyW-3), colInfo)
		y += lh
	}
	if n < len(rows) {
		trackY, trackH := r.Min.Y+lh+gtx.Dp(12), n*lh
		size := max(lh, trackH*n/len(rows))
		ty := trackY + int(float64(trackH-size)*e.keysY/maxY)
		fill(gtx, image.Rect(r.Max.X-gtx.Dp(6), ty, r.Max.X-gtx.Dp(2), ty+size), colThumb)
	}
}

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
			e.label(gtx, x, y, p.key, colInfo)
		}
		c := colText
		if p.key == "" {
			c = tokColors[tokWarn]
		}
		e.label(gtx, vx, y, truncLeft(p.val, room), c)
		y += lh
	}
}

func (e *Editor) grepRows() int { return max(1, e.viewH*6/10/max(1, e.lineH)) }

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
		e.label(gtx, pad, top+gtx.Dp(4), msg, colInfo)
		return
	}
	g.top = min(g.top, g.sel)
	if g.sel >= g.top+rows {
		g.top = g.sel - rows + 1
	}
	g.top = max(0, min(g.top, len(g.hits)-rows))
	cols := int(float32(width-2*pad) / e.charW)
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
		e.label(gtx, pad, y, truncLeft(fmt.Sprintf("%s:%d", h.path, h.line+1), pw), colInfo)

		text, col, cut := h.text, h.col, h.cut
		lead := min(col, len(text)-len(strings.TrimLeft(text, " \t")))
		text, col = text[lead:], col-lead
		if col+q > room-1 && col > 10 {
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
