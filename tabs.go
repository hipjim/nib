package main

import (
	"fmt"
	"image"
	"math"
	"path/filepath"
	"slices"
	"unicode/utf8"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
)

// The tab bar lists the open documents above the text, in the order they
// were opened, once there is more than one. Clicking a tab shows it; its ×
// or a middle click closes it. An unsaved file shows ● in place of the ×
// until the pointer is over the tab.

// tabMinChars is how much of a name a tab keeps before the bar scrolls
// instead. Every tab also has one cell for the × and a space before it.
const tabMinChars = 8

func (e *Editor) tabs() []*doc {
	t := slices.Clone(e.docs)
	slices.SortFunc(t, func(a, b *doc) int { return a.opened - b.opened })
	return t
}

// tabNames names each tab: the file name, with its folder when another
// open file has the same name.
func tabNames(tabs []*doc) []string {
	names := make([]string, len(tabs))
	count := map[string]int{}
	for i, d := range tabs {
		names[i] = tabBase(d)
		count[names[i]]++
	}
	for i, d := range tabs {
		if count[names[i]] > 1 && d.dir == nil && d.path != "" {
			names[i] = filepath.Base(filepath.Dir(d.path)) + string(filepath.Separator) + names[i]
		}
	}
	return names
}

func tabBase(d *doc) string {
	switch {
	case d.dir != nil:
		return filepath.Base(d.path) + string(filepath.Separator)
	case d.path == "":
		return "scratch"
	}
	return filepath.Base(d.path)
}

func (e *Editor) tabHeight(gtx layout.Context) int {
	if len(e.docs) < 2 {
		return 0
	}
	return e.lineH + gtx.Dp(4)
}

// tabLayout places the tabs in a bar of the given width: their left edges
// and widths before scrolling, and the scroll that keeps the current one in
// view. Tabs shrink evenly down to tabMinChars, then the bar scrolls.
func (e *Editor) tabLayout(tabs []*doc, names []string, width, pad int) (x0, w []int, scroll int) {
	x0, w = make([]int, len(tabs)), make([]int, len(tabs))
	total := 0
	for i := range tabs {
		n := utf8.RuneCountInString(names[i]) + 2
		w[i] = int(float32(n)*e.charW) + 2*pad
		total += w[i]
	}
	if total > width {
		minW := int(float32(tabMinChars+2)*e.charW) + 2*pad
		scale := float32(width) / float32(total)
		total = 0
		for i := range w {
			w[i] = max(minW, int(float32(w[i])*scale))
			total += w[i]
		}
	}
	for i := range w {
		if i > 0 {
			x0[i] = x0[i-1] + w[i-1]
		}
	}
	scroll = max(0, min(e.tabScroll, total-width))
	if cur := slices.Index(tabs, e.doc); cur >= 0 {
		scroll = max(scroll, x0[cur]+w[cur]-width)
		scroll = min(scroll, x0[cur])
	}
	e.tabScroll = scroll
	return x0, w, scroll
}

func (e *Editor) drawTabs(gtx layout.Context, width, h int) {
	tabs := e.tabs()
	names := tabNames(tabs)
	pad := gtx.Dp(10)
	x0, w, scroll := e.tabLayout(tabs, names, width, pad)
	fill(gtx, image.Rect(0, 0, width, h), colStatus)
	fill(gtx, image.Rect(0, h-1, width, h), colCurLine)
	y := (h - e.lineH) / 2
	cw := int(math.Round(float64(e.charW)))
	for i, d := range tabs {
		r := image.Rect(x0[i]-scroll, 0, x0[i]+w[i]-scroll, h)
		gx := r.Max.X - pad - cw // the × cell
		d.closeR = image.Rect(gx-gtx.Dp(4), gtx.Dp(3), gx+cw+gtx.Dp(4), h-gtx.Dp(3))
		if r.Max.X <= 0 || r.Min.X >= width {
			continue
		}
		area := clip.Rect(r).Push(gtx.Ops)
		event.Op(gtx.Ops, d)
		pointer.CursorPointer.Add(gtx.Ops)
		c := colInfo
		switch {
		case d == e.doc:
			fill(gtx, r, colBg)
			fill(gtx, image.Rect(r.Min.X, h-gtx.Dp(2), r.Max.X, h), colAccent)
			c = colText
		case d == e.tabHover:
			fill(gtx, r, colCurLine)
		}
		room := int(math.Round(float64(float32(w[i]-2*pad)/e.charW))) - 2
		e.label(gtx, r.Min.X+pad, y, truncLeft(names[i], room), c)
		glyph, gc := "×", colInfo
		switch {
		case d == e.tabClose:
			gc = colText
			rr := clip.UniformRRect(d.closeR, gtx.Dp(3)).Push(gtx.Ops)
			fill(gtx, d.closeR, colSel)
			rr.Pop()
		case d.buf.Dirty() && d != e.tabHover:
			glyph = "●"
			if d == e.doc {
				gc = colAccent
			}
		}
		e.label(gtx, gx, y, glyph, gc)
		fill(gtx, image.Rect(r.Max.X-1, gtx.Dp(6), r.Max.X, h-gtx.Dp(6)), colCurLine)
		area.Pop()
	}
}

func (e *Editor) tabEvents(gtx layout.Context) {
	// Clicks change e.docs, so walk a copy.
	for _, d := range e.tabs() {
		for {
			ev, ok := gtx.Event(pointer.Filter{
				Target:  d,
				Kinds:   pointer.Press | pointer.Move | pointer.Enter | pointer.Leave | pointer.Scroll,
				ScrollX: pointer.ScrollRange{Min: -1 << 20, Max: 1 << 20},
				ScrollY: pointer.ScrollRange{Min: -1 << 20, Max: 1 << 20},
			})
			if !ok {
				break
			}
			pe, ok := ev.(pointer.Event)
			if !ok {
				continue
			}
			at := image.Pt(int(pe.Position.X), int(pe.Position.Y))
			switch pe.Kind {
			case pointer.Enter, pointer.Move:
				e.tabHover = d
				e.tabHoverClose(d, at.In(d.closeR))
			case pointer.Leave:
				if e.tabHover == d {
					e.tabHover = nil
				}
				e.tabHoverClose(d, false)
			case pointer.Scroll:
				e.tabScroll += int(pe.Scroll.X + pe.Scroll.Y)
			case pointer.Press:
				switch {
				case pe.Buttons.Contain(pointer.ButtonTertiary):
					e.closeTab(d)
				case pe.Buttons.Contain(pointer.ButtonPrimary):
					e.tabPress(d, at)
				}
			}
		}
	}
}

// tabHoverClose records whether the pointer is over d's ×.
func (e *Editor) tabHoverClose(d *doc, over bool) {
	switch {
	case over:
		e.tabClose = d
	case e.tabClose == d:
		e.tabClose = nil
	}
}

// tabPress handles a left click on a tab, at a point in the bar's
// coordinates: on the × it closes the file, elsewhere it shows it.
func (e *Editor) tabPress(d *doc, at image.Point) {
	if at.In(d.closeR) {
		e.closeTab(d)
		return
	}
	e.prompt = promptNone
	e.show(d)
}

func (e *Editor) showTab(n int) {
	if tabs := e.tabs(); n >= 0 && n < len(tabs) {
		e.show(tabs[n])
	}
}

func (e *Editor) cycleTab(back bool) {
	tabs := e.tabs()
	i := slices.Index(tabs, e.doc)
	if i < 0 || len(tabs) < 2 {
		return
	}
	if back {
		i += len(tabs) - 1
	} else {
		i++
	}
	e.show(tabs[i%len(tabs)])
}

// closeTab closes a document. One with unsaved changes is shown instead,
// so ⌘W can close it with a warning.
func (e *Editor) closeTab(d *doc) {
	if d == e.doc {
		// A second click after the warning closes it, like a second ⌘W.
		ok := e.discardOK
		e.discardOK = false
		e.closeDoc(ok)
		return
	}
	if d.buf.Dirty() {
		e.show(d)
		e.flash("unsaved changes · "+shortcutName+"W twice to close", true)
		return
	}
	e.docs = slices.DeleteFunc(e.docs, func(x *doc) bool { return x == d })
	if e.tabHover == d {
		e.tabHover = nil
	}
	if e.tabClose == d {
		e.tabClose = nil
	}
}

func (e *Editor) closeOthers() {
	n := len(e.docs)
	e.docs = slices.DeleteFunc(e.docs, func(x *doc) bool { return x != e.doc && !x.buf.Dirty() })
	closed := n - len(e.docs)
	if !slices.Contains(e.docs, e.tabHover) {
		e.tabHover = nil
	}
	if !slices.Contains(e.docs, e.tabClose) {
		e.tabClose = nil
	}
	msg := fmt.Sprintf("closed %d files", closed)
	switch closed {
	case 0:
		msg = "nothing else to close"
	case 1:
		msg = "closed 1 file"
	}
	if kept := len(e.docs) - 1; kept > 0 {
		msg += fmt.Sprintf(" · kept %d unsaved", kept)
	}
	e.flash(msg, false)
}
