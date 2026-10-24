package main

import (
	"fmt"
	"sync/atomic"
)

// Quitting is decided on whichever thread the platform asks on, so the
// editor publishes its answer each frame.
var (
	quitOK    atomic.Bool // nothing is unsaved, or the user confirmed
	quitAsked atomic.Bool // a quit was refused; the editor should say why
	wake      atomic.Pointer[func()]
)

// mayQuit reports whether the app may quit now. If not, the editor shows
// why on the next frame.
func mayQuit() bool {
	if quitOK.Load() {
		return true
	}
	quitAsked.Store(true)
	if w := wake.Load(); w != nil {
		(*w)()
	}
	return false
}

func (e *Editor) unsaved() int {
	n := 0
	for _, d := range e.docs {
		if d.buf.Dirty() {
			n++
		}
	}
	return n
}

// MayClose reports whether the window may close. With unsaved files it
// refuses once and asks for confirmation.
func (e *Editor) MayClose() bool {
	if e.unsaved() == 0 || e.quitArmed {
		e.saveSession(true)
		return true
	}
	e.askQuit()
	return false
}

func (e *Editor) askQuit() {
	n := e.unsaved()
	s := "s have"
	if n == 1 {
		s = " has"
	}
	e.flash(fmt.Sprintf("%d file%s unsaved changes · quit again to discard", n, s), true)
	e.quitArmed = true
}
