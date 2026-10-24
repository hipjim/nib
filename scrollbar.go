package main

import (
	"image"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
)

var (
	colThumb      = rgb(0x343c48)
	colThumbHover = rgb(0x4a5566)
)

// scrollbar is the thin bar at the right of the text. Clicking the track
// jumps there, which beats paging when a pixel is thousands of lines.
type scrollbar struct {
	hover, drag bool
	grab        int // where in the thumb the drag holds it
	thumbH      int // from the last frame
	trackH      int
}

func (e *Editor) maxScroll() float64 {
	return float64(max(0, (e.buf.LineCount()-1)*e.lineH))
}

// thumb returns the thumb's top and height in a track of height h.
func (e *Editor) thumb(h int) (top, size int, ok bool) {
	maxY := e.maxScroll()
	if maxY <= 0 || h <= 0 {
		return 0, 0, false
	}
	size = int(float64(h) * float64(h) / (maxY + float64(h)))
	size = min(h, max(size, e.lineH))
	top = int(e.scrollY / maxY * float64(h-size))
	return top, size, true
}

func (e *Editor) barEvent(ev pointer.Event) {
	b := &e.bar
	y := int(ev.Position.Y)
	switch ev.Kind {
	case pointer.Enter:
		b.hover = true
	case pointer.Leave:
		b.hover = false
	case pointer.Scroll:
		e.scrollY += float64(ev.Scroll.Y)
	case pointer.Press:
		top, size, ok := e.thumb(b.trackH)
		if !ok {
			return
		}
		b.drag, b.grab = true, y-top
		if y < top || y >= top+size {
			b.grab = size / 2 // jump so the thumb centers on the click
		}
		e.dragThumb(y)
	case pointer.Drag:
		if b.drag {
			e.dragThumb(y)
		}
	case pointer.Release, pointer.Cancel:
		b.drag = false
	}
}

func (e *Editor) dragThumb(y int) {
	b := &e.bar
	if free := b.trackH - b.thumbH; free > 0 {
		f := float64(y-b.grab) / float64(free)
		e.scrollY = max(0, min(f, 1)) * e.maxScroll()
	}
}

func (e *Editor) drawScrollbar(gtx layout.Context, r image.Rectangle) {
	area := clip.Rect(r).Push(gtx.Ops)
	event.Op(gtx.Ops, &e.bar)
	pointer.CursorDefault.Add(gtx.Ops)
	area.Pop()

	b := &e.bar
	b.trackH = r.Dy()
	top, size, ok := e.thumb(r.Dy())
	b.thumbH = size
	if !ok {
		return
	}
	w, c := gtx.Dp(4), colThumb
	if b.hover || b.drag {
		w, c = gtx.Dp(8), colThumbHover
	}
	x1 := r.Max.X - gtx.Dp(2)
	fill(gtx, image.Rect(x1-w, r.Min.Y+top, x1, r.Min.Y+top+size), c)

	// Where the cursor is in the whole file.
	n := max(1, e.buf.LineCount()-1)
	cy := r.Min.Y + int(float64(e.cursor.Line)/float64(n)*float64(r.Dy()-gtx.Dp(2)))
	fill(gtx, image.Rect(x1-gtx.Dp(8), cy, x1, cy+gtx.Dp(2)), colAccent)
}
