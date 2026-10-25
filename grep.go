package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
	"unsafe"
)

const (
	maxHits     = 10_000 // the search stops after this many lines
	maxFileHits = 1_000
	hitBefore   = 40  // bytes of a long line kept before the match
	hitAfter    = 300 // and after it
	grepBuf     = 1 << 20
)

// A hit is a line with a match.
type hit struct {
	path string // slash-separated, relative to root
	line int
	col  int    // byte offset of the match in text
	at   int    // and in the line
	text string // the line, or the part of a long one around the match
	cut  bool   // text starts inside the line
}

// grep searches every file under a folder for a query on all cores, in
// the background, one hit per matching line.
type grep struct {
	root, query string
	hits        []hit
	files       int // files with hits
	searched    *atomic.Int64
	results     chan []hit // closed when the search is done
	stop        *atomic.Bool
	sel, top    int
	list        *fileList // the files under root, shared by the searches
}

// fileList is the files under a folder, listed once in the background and
// read by searches while it grows.
type fileList struct {
	mu    sync.Mutex
	grown sync.Cond
	paths []string
	done  bool
	at    time.Time
	stop  atomic.Bool
}

func listFiles(root string) *fileList {
	l := &fileList{at: time.Now()}
	l.grown.L = &l.mu
	ch := make(chan []string, 8)
	go walk(root, ch, &l.stop)
	go func() {
		for b := range ch {
			l.mu.Lock()
			l.paths = append(l.paths, b...)
			l.grown.Broadcast()
			l.mu.Unlock()
		}
		l.mu.Lock()
		l.done = true
		l.grown.Broadcast()
		l.mu.Unlock()
	}()
	return l
}

// get returns the i-th file, waiting for the walk to reach it.
func (l *fileList) get(i int) (string, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i >= len(l.paths) && !l.done {
		l.grown.Wait()
	}
	if i < len(l.paths) {
		return l.paths[i], true
	}
	return "", false
}

// start searches root for q, stopping the search before it.
func (g *grep) start(root, q string) {
	if g.stop != nil {
		g.stop.Store(true)
		if old := g.results; old != nil {
			go func() {
				for range old {
				}
			}()
		}
	}
	if l := g.list; l == nil || root != g.root || time.Since(l.at) > rewalkAfter {
		if l != nil {
			l.stop.Store(true)
		}
		g.list = listFiles(root)
	}
	g.root, g.query = root, q
	g.hits, g.files, g.sel, g.top = nil, 0, 0, 0
	g.results, g.stop, g.searched = nil, nil, new(atomic.Int64)
	if q == "" || strings.Contains(q, "\n") {
		return
	}
	g.stop = new(atomic.Bool)
	g.results = make(chan []hit, 64)
	go search(root, q, g.list, g.results, g.stop, g.searched)
}

// poll takes in new hits, keeping them in file and line order and the
// selection on the same hit. It reports whether hits arrived and whether
// the search is still going.
func (g *grep) poll() (changed, running bool) {
	var cur hit
	if g.sel < len(g.hits) {
		cur = g.hits[g.sel]
	}
loop:
	for g.results != nil {
		select {
		case hs, ok := <-g.results:
			if !ok {
				g.results = nil
				changed = true
				break loop
			}
			g.hits = append(g.hits, hs...)
			g.files++
			changed = true
		default:
			break loop
		}
	}
	if changed {
		slices.SortStableFunc(g.hits, func(a, b hit) int {
			if c := strings.Compare(a.path, b.path); c != 0 {
				return c
			}
			return a.line - b.line
		})
		g.hits = g.hits[:min(len(g.hits), maxHits)]
		if i := slices.IndexFunc(g.hits, func(h hit) bool { return h.path == cur.path && h.line == cur.line }); i >= 0 {
			g.sel = i
		}
	}
	return changed, g.results != nil
}

func search(root, q string, files *fileList, res chan<- []hit, stop *atomic.Bool, searched *atomic.Int64) {
	defer close(res)
	index := matcher(q)
	byLine := !isASCII(q) && strings.ToLower(q) == q // folding may change offsets
	work := make(chan string, 256)
	var total atomic.Int64
	var wg sync.WaitGroup
	for range runtime.NumCPU() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, grepBuf)
			for rel := range work {
				if stop.Load() || total.Load() >= maxHits {
					continue
				}
				hs := grepFile(filepath.Join(root, filepath.FromSlash(rel)), rel, index, len(q), byLine, &buf, stop)
				searched.Add(1)
				if len(hs) > 0 {
					total.Add(int64(len(hs)))
					res <- hs
				}
			}
		}()
	}
	for i := 0; !stop.Load(); i++ {
		p, ok := files.get(i)
		if !ok {
			break
		}
		work <- p
	}
	close(work)
	wg.Wait()
}

// grepFile finds the lines of a file with a match. It reads the file a
// buffer at a time, cut after the last whole line, so files of any size
// take little memory. Files with a NUL byte near the start are binary and
// skipped.
func grepFile(name, rel string, index func(string) int, qlen int, byLine bool, buf *[]byte, stop *atomic.Bool) []hit {
	f, err := os.Open(name)
	if err != nil {
		return nil
	}
	defer f.Close()
	var hits []hit
	b := *buf
	carry, line := 0, 0
	for first := true; !stop.Load() && len(hits) < maxFileHits; first = false {
		n, err := io.ReadFull(f, b[carry:])
		data := b[:carry+n]
		if first && bytes.IndexByte(data[:min(len(data), 8000)], 0) >= 0 {
			return nil
		}
		eof := err != nil
		cut := len(data)
		if !eof {
			cut = bytes.LastIndexByte(data, '\n') + 1
			if cut == 0 && len(b) < 64<<20 {
				// One line longer than the buffer: grow it and read on.
				nb := make([]byte, 2*len(b))
				carry = copy(nb, data)
				b, *buf = nb, nb
				continue
			}
			if cut == 0 {
				cut = len(data) // split a huge line
			}
		}
		if cut > 0 {
			s := unsafe.String(&data[0], cut)
			hits = scanChunk(s, rel, index, qlen, byLine, &line, hits)
		}
		if eof {
			break
		}
		carry = copy(b, data[cut:])
	}
	return hits
}

// scanChunk adds the hits in s, whole lines starting at line *line, and
// moves *line past them.
func scanChunk(s, rel string, index func(string) int, qlen int, byLine bool, line *int, hits []hit) []hit {
	if byLine {
		for len(s) > 0 {
			l, rest, more := strings.Cut(s, "\n")
			if j := index(l); j >= 0 && len(hits) < maxFileHits {
				hits = append(hits, newHit(rel, *line, l, j, qlen))
			}
			if more {
				*line++
			}
			s = rest
		}
		return hits
	}
	counted := 0
	for o := 0; o < len(s) && len(hits) < maxFileHits; {
		m := index(s[o:])
		if m < 0 {
			break
		}
		p := o + m
		*line += strings.Count(s[counted:p], "\n")
		counted = p
		ls := strings.LastIndexByte(s[:p], '\n') + 1
		le := strings.IndexByte(s[p:], '\n')
		if le < 0 {
			le = len(s)
		} else {
			le += p
		}
		hits = append(hits, newHit(rel, *line, s[ls:le], p-ls, qlen))
		o = le + 1
	}
	*line += strings.Count(s[counted:], "\n")
	return hits
}

// newHit copies the line, or the part of a long one around the match, out
// of the read buffer.
func newHit(rel string, line int, text string, col, qlen int) hit {
	text = strings.TrimSuffix(text, "\r")
	start, end := 0, len(text)
	if col > 2*hitBefore {
		start = col - hitBefore
		for start < col && !utf8.RuneStart(text[start]) {
			start++
		}
	}
	if end > col+qlen+hitAfter {
		end = col + qlen + hitAfter
		for end > col+qlen && !utf8.RuneStart(text[end]) {
			end--
		}
	}
	return hit{path: rel, line: line, col: col - start, at: col, text: strings.Clone(text[start:end]), cut: start > 0}
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}
