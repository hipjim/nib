// The line filter: every line of the open file with a match, listed like the
// folder search. It goes through the file a slice of time per frame, so a
// 22-million-line log stays responsive while the list fills. It keeps at most
// maxHits lines, so what it holds does not grow with the file.

package main

import (
	"errors"
	"regexp/syntax"
	"sync/atomic"
)

type lineFilter struct {
	q        string
	p        *pattern
	err      error
	buf      *Buffer
	version  int
	results  [][]hit // by chunk, until merged into hits in file order
	merged   int     // chunks already in hits
	next     atomic.Int64
	hits     []hit
	sel, top int
}

func (f *lineFilter) start(b *Buffer, q string, o findOpts) {
	f.q, f.buf, f.version = q, b, b.version
	f.p, f.err, f.hits, f.results = nil, nil, nil, nil
	f.merged, f.sel, f.top = 0, 0, 0
	f.next.Store(0)
	if q == "" {
		return
	}
	if f.p, f.err = compilePattern(q, o); f.err != nil {
		return
	}
	f.results = make([][]hit, (b.LineCount()+patternChunk-1)/patternChunk)
}

func (f *lineFilter) capped() bool { return len(f.hits) >= maxHits }

func (f *lineFilter) busy() bool {
	return f.p != nil && f.merged < len(f.results) && !f.capped()
}

// step goes through the file for one slice of time.
func (f *lineFilter) step() {
	if f.p == nil {
		return
	}
	s := &f.buf.lines
	// Lines may still be arriving from the indexer, in whole blocks.
	if n := (s.count + patternChunk - 1) / patternChunk; n > len(f.results) {
		f.results = append(f.results, make([][]hit, n-len(f.results))...)
	}
	if !f.busy() {
		return
	}
	n := len(f.results)
	ok := stepChunks(&f.next, n, patternBudget, func(int) bool { return false }, func(i int) {
		var hs []hit
		r := lineRange{i * patternChunk, min(s.count, (i+1)*patternChunk)}
		s.eachLine(r, f.p, func(line int, text string) bool {
			if a, b, ok := f.p.first(text, 0); ok {
				hs = append(hs, newHit("", line, text, a, b-a))
			}
			return false
		})
		f.results[i] = hs
	})
	if !ok {
		f.next.Store(int64(n))
	}
	done := min(int(f.next.Load()), n)
	f.next.Store(int64(done))
	for ; f.merged < done; f.merged++ {
		room := max(0, maxHits-len(f.hits))
		f.hits = append(f.hits, f.results[f.merged][:min(room, len(f.results[f.merged]))]...)
		f.results[f.merged] = nil
	}
}

func (e *Editor) openFilter() {
	q := e.filter.q
	if a, b, ok := e.sel(); ok && a.Line == b.Line {
		q = e.asQuery(e.buf.Text(a, b))
	}
	e.openPrompt(promptFilter, q)
	e.setInput(q)
}

// pickFilter goes to the line picked, with its match selected, so F3 goes on
// from there.
func (e *Editor) pickFilter() {
	f := &e.filter
	if f.sel >= len(f.hits) {
		return
	}
	h := f.hits[f.sel]
	e.prompt = promptNone
	e.findQuery = f.q
	e.anchor = e.buf.Clamp(Pos{h.line, h.at})
	e.moveTo(Pos{h.line, h.at + h.n}, true)
}

func (e *Editor) filterStatus() string {
	f := &e.filter
	switch {
	case f.err != nil:
		return patternError(f.err)
	case f.capped():
		return "first " + plural(int64(len(f.hits)), "line")
	case f.busy():
		return "searching…  " + plural(int64(len(f.hits)), "line")
	case f.q == "":
		return ""
	}
	s := plural(int64(len(f.hits)), "line")
	if f.p != nil && f.p.skipped.Load() {
		s = longLinesNote + s
	}
	return s
}

const longLinesNote = "lines over 16 MB skipped · "

// patternError is the status bar's short form of a compile error, leaving out
// the expression, which is in the prompt and may be long.
func patternError(err error) string {
	var se *syntax.Error
	if errors.As(err, &se) {
		return "bad pattern: " + string(se.Code)
	}
	return "bad pattern"
}
