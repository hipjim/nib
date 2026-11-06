// Tail mode: follow a file that grows, as tail -f does. Only the new bytes are
// read, so a line added to a 2 GB log costs what it costs in a small one, where
// a reload would index the whole file again. The new lines live in the buffer
// like edited ones, until they add up to enough that indexing the file once
// gives their memory back.
//
// Following is not a setting of its own but a fact about the view: the cursor
// is on the last line, with nothing selected, and that line is on screen. So
// scrolling up or clicking elsewhere pauses it, and coming back resumes it.

package main

import (
	"os"
	"strings"
	"time"
)

const (
	tailPoll   = 250 * time.Millisecond // how often a tailed file is looked at
	tailChunk  = 1 << 20                // read at most this much per frame, so a burst never drops one
	tailSigLen = 64                     // bytes before the end that must still match for the file to be the same
	tailBehind = 32 << 20               // further behind than this, indexing the file beats reading it in chunks
	tailHeld   = 64 << 20               // this much read as lines goes back to being a mapped file
	tailFade   = 1200 * time.Millisecond
	tailMerge  = 100 * time.Millisecond // arrivals closer than this fade as one, which bounds tailState.arrivals
)

// arrival is a run of new lines from line up to the next arrival, fading out.
type arrival struct {
	line int
	at   time.Time
}

type tailState struct {
	off      int64  // bytes of the file in the buffer
	size     int64  // bytes on disk when last looked
	sig      string // the last tailSigLen bytes before off
	held     int64  // bytes read since the file was last indexed
	check    bool   // the file changed, so sig must be compared even if it did not grow
	gone     bool   // the file is missing, as during a log rotation
	fresh    int    // lines that came in while paused
	next     *Buffer
	replaced bool // next is a different file, not the same one indexed again
	arrivals []arrival
}

// sync starts over from what the buffer holds now.
func (t *tailState) sync(d *doc) {
	data := d.buf.lines.data
	t.off, t.size, t.held = int64(len(data)), int64(len(data)), 0
	t.sig = strings.Clone(data[max(0, len(data)-tailSigLen):])
	t.check, t.gone, t.fresh, t.arrivals = false, false, 0, nil
	if st := statStamp(d.path); st.ok {
		t.size = st.size
	}
}

// appendText adds text at the end of the document: the first line of it goes
// onto the last line, which may have been written only in part.
func (s *lineStore) appendText(text string) (added int) {
	bi := len(s.blocks) - 1
	k := s.blocks[bi]
	s.materialize(k)
	n := len(k.lines) - 1
	parts := strings.Split(k.lines[n]+text, "\n")
	if s.crlf {
		for i, l := range parts[:len(parts)-1] {
			parts[i] = strings.TrimSuffix(l, "\r")
		}
	}
	added = len(parts) - 1
	k.lines = k.lines[:n]
	room := min(blockLines-n, len(parts))
	k.lines, parts = append(k.lines, parts[:room]...), parts[room:]
	for len(parts) > 0 {
		c := min(blockLines, len(parts))
		s.blocks = append(s.blocks, &block{lines: parts[:c:c]})
		parts = parts[c:]
	}
	s.recount(bi)
	return added
}

// Append adds what the file gained on disk. It is not an edit: the buffer is
// as saved before and after, and there is nothing to undo.
func (b *Buffer) Append(text string) (added int) {
	last := b.LineCount() - 1
	col := len(b.Line(last))
	if len(b.lines.data) == 0 && last == 0 && col == 0 && strings.Contains(text, "\r\n") {
		b.lines.crlf, b.CRLF = true, true // nothing could tell when the file was empty
	}
	added = b.lines.appendText(text)
	b.version++
	b.appends++
	b.edited = min(b.edited, last)
	b.cols.edited(last, col)
	return added
}

// following reports whether d's view is at the end of the file.
func (e *Editor) following(d *doc) bool {
	if d.cursor != d.anchor || d.cursor.Line < d.buf.LineCount()-1 {
		return false
	}
	if e.lineH == 0 || e.viewH == 0 {
		return true
	}
	y := float64(d.cursor.Line * e.lineH)
	return y+float64(e.lineH) > d.scrollY && y < d.scrollY+float64(e.viewH)
}

// tailEnd puts d's view at the end of the file, the last line on the bottom
// row of the window rather than a few rows up, as an edit would leave it.
func (e *Editor) tailEnd(d *doc) {
	d.cursor = d.buf.End()
	d.anchor, d.goal = d.cursor, -1
	if e.lineH == 0 || e.viewH == 0 {
		if d == e.doc {
			e.followCursor = true
		}
		return
	}
	d.scrollY = max(0, float64(d.buf.LineCount()*e.lineH-e.viewH))
}

func (e *Editor) toggleTail() {
	if e.tail != nil {
		e.tail = nil
		e.flash("tail off", false)
		return
	}
	switch {
	case e.path == "" || e.dir != nil:
		e.flash("tail follows a file on disk", true)
	case e.buf.Dirty():
		e.flash("save this file before tailing it", true)
	default:
		e.startTail()
		e.flash("tail on · scroll up to pause, End to come back", false)
	}
}

func (e *Editor) startTail() {
	if e.path == "" || e.dir != nil {
		return
	}
	e.tail = &tailState{}
	e.tail.sync(e.doc)
	if e.buf.Loading() {
		e.pending = &place{End: true}
	} else {
		e.tailEnd(e.doc)
	}
}

// reindex loads the file again, leaving the buffer on screen until the new one
// is complete, so the view does not jump while a big file is indexed.
func (t *tailState) reindex(d *doc, replaced bool) {
	t.check = false
	if b, err := Load(d.path); err == nil {
		t.next, t.replaced = b, replaced
	}
}

func (e *Editor) swapTail(d *doc) {
	t := d.tail
	follow := e.following(d)
	replaced := t.replaced
	d.buf, d.hl, d.conflict = t.next, newHighlighter(d.path), false
	t.next = nil
	t.sync(d)
	d.stamp = statStamp(d.path)
	d.base = d.stamp
	if follow {
		e.tailEnd(d)
	} else {
		d.cursor, d.anchor = d.buf.Clamp(d.cursor), d.buf.Clamp(d.anchor)
	}
	if replaced && d == e.doc {
		e.flash("the file was replaced; reading the new one", false)
	}
}

// stepTail takes in what one tailed file gained, up to tailChunk, and reports
// whether there is more to do right away.
func (e *Editor) stepTail(d *doc) bool {
	t := d.tail
	if t.next != nil {
		if t.next.Poll() {
			return true
		}
		e.swapTail(d)
		return t.off < t.size
	}
	if d.buf.Loading() || d.buf.Dirty() || !t.check && t.off >= t.size {
		return false
	}
	if len(d.buf.undo) > 0 || len(d.buf.redo) > 0 {
		t.reindex(d, false) // edits were saved, so the file no longer matches the bytes read
		return t.next != nil
	}
	if t.size-t.off > tailBehind || t.held > tailHeld {
		t.reindex(d, false)
		return t.next != nil
	}
	f, err := os.Open(d.path)
	if err != nil {
		t.check = false
		return false
	}
	defer f.Close()
	buf := make([]byte, int64(len(t.sig))+max(0, min(tailChunk, t.size-t.off)))
	m, _ := f.ReadAt(buf, t.off-int64(len(t.sig)))
	if m < len(t.sig) || string(buf[:len(t.sig)]) != t.sig || t.size < t.off {
		t.reindex(d, true) // rotated, truncated or rewritten
		return t.next != nil
	}
	t.check = false
	text := string(buf[len(t.sig):m])
	if text == "" {
		t.size = t.off
		d.base = d.stamp
		return false
	}
	follow := e.following(d)
	from := d.buf.LineCount() - 1
	added := d.buf.Append(text)
	t.off += int64(len(text))
	t.held += int64(len(text))
	sig := t.sig + text[max(0, len(text)-tailSigLen):]
	t.sig = strings.Clone(sig[max(0, len(sig)-tailSigLen):])
	now := time.Now()
	if n := len(t.arrivals); n == 0 || now.Sub(t.arrivals[n-1].at) >= tailMerge {
		t.arrivals = append(t.arrivals, arrival{from, now})
	}
	if follow {
		e.tailEnd(d)
	} else {
		t.fresh += added
	}
	if t.off >= t.size {
		d.base = d.stamp
	}
	return t.off < t.size
}

// stepTails runs over the open files and reports whether any has more to do.
func (e *Editor) stepTails() (busy bool) {
	for _, d := range e.docs {
		if d.tail != nil && e.stepTail(d) {
			busy = true
		}
	}
	return busy
}

// tailStatus is what the status bar says about the mode.
func (e *Editor) tailStatus() (string, bool, bool) {
	t := e.tail
	switch {
	case e.buf.Dirty():
		return "tail paused · unsaved edits", false, true
	case t.gone:
		return "tail · waiting for the file", false, false
	case e.following(e.doc):
		t.fresh = 0
		return "● tail", true, false
	case t.fresh > 0:
		return "tail paused +" + groupDigits(int64(t.fresh)) + " · End", false, false
	}
	return "tail paused · End", false, false
}

// drawArrivals tints the lines that just came in, fading, so the eye can tell
// what is new in a moving log. It reports whether a tint is still fading.
func (e *Editor) drawArrivals(now time.Time, tint func(from, to int, strength float32)) bool {
	t := e.tail
	if t == nil {
		return false
	}
	live := t.arrivals[:0]
	for _, a := range t.arrivals {
		if now.Sub(a.at) < tailFade {
			live = append(live, a)
		}
	}
	t.arrivals = live
	for i, a := range live {
		to := e.buf.LineCount()
		if i+1 < len(live) {
			to = live[i+1].line
		}
		tint(a.line, to, 1-float32(now.Sub(a.at))/float32(tailFade))
	}
	return len(live) > 0
}
