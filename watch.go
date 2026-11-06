package main

import (
	"maps"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// A stamp is what a file looked like on disk.
type stamp struct {
	mod  int64
	size int64
	ok   bool
}

func statStamp(path string) stamp {
	fi, err := os.Stat(path)
	if err != nil {
		return stamp{}
	}
	return stamp{fi.ModTime().UnixNano(), fi.Size(), true}
}

// watcher checks the open files once a second and wakes the editor only
// when one changed, so an idle window stays idle.
type watcher struct {
	mu    sync.Mutex
	files map[string]stamp
	fast  bool // a file is being tailed, so look more often
	hit   atomic.Bool
}

// sync publishes the files to watch.
func (w *watcher) sync(docs []*doc) {
	files := map[string]stamp{}
	fast := false
	for _, d := range docs {
		if d.path != "" {
			files[d.path] = d.stamp
		}
		fast = fast || d.tail != nil
	}
	w.mu.Lock()
	w.fast = fast
	if !maps.Equal(files, w.files) {
		w.files = files
	}
	w.mu.Unlock()
}

func (w *watcher) run(wake func()) {
	for {
		w.mu.Lock()
		files, wait := w.files, time.Second
		if w.fast {
			wait = tailPoll
		}
		w.mu.Unlock()
		time.Sleep(wait)
		for p, s := range files {
			if statStamp(p) != s {
				w.hit.Store(true)
				wake()
				break
			}
		}
	}
}

// checkDisk reloads documents that changed on disk. Edited ones are left
// alone with a warning; listings are refreshed.
func (e *Editor) checkDisk() {
	for _, d := range e.docs {
		if d.path == "" {
			continue
		}
		st := statStamp(d.path)
		if st == d.stamp {
			continue
		}
		d.stamp = st
		cur := d == e.doc
		switch {
		case d.dir != nil:
			focus := ""
			if en, ok := e.entryOf(d, d.cursor.Line); ok {
				focus = en.name
			}
			e.fillDir(d, d.path, focus)
		case !st.ok:
			if d.tail != nil {
				d.tail.gone = true // a rotation; it comes back
			} else if cur {
				e.flash("file was deleted on disk", true)
			}
		case d.buf.Dirty():
			d.conflict = true
			if cur {
				e.flash("file changed on disk · ⌘S twice overwrites it", true)
			}
		case d.tail != nil:
			d.tail.size, d.tail.check, d.tail.gone = st.size, true, false
		default:
			e.reload(d)
			if cur {
				e.flash("reloaded: file changed on disk", false)
			}
		}
	}
}

// reload reads d again, keeping the cursor, or following the end of the
// file if the cursor was on the last line (like tail -f).
func (e *Editor) reload(d *doc) {
	b, err := Load(d.path)
	if err != nil {
		return
	}
	atEnd := d.cursor.Line >= d.buf.LineCount()-1
	d.pending = &place{Line: d.cursor.Line, Col: d.cursor.Col, Scroll: d.scrollY, End: atEnd}
	d.buf, d.hl, d.conflict = b, newHighlighter(d.path), false
	d.base = statStamp(d.path)
	d.anchor = d.cursor
}
