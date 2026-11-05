package main

import (
	"fmt"
	"time"
)

// countBudget bounds how long a frame spends counting the matches of the
// find query. Past it the count is shown as a lower bound.
const countBudget = 20 * time.Millisecond

// matchCount caches the match count of the find query for the status bar.
type matchCount struct {
	buf      *Buffer
	q        string
	version  int
	at       Pos
	total    int
	before   int // matches starting before at
	complete bool
}

// matchCountText is the status bar's "3 of 128" while finding.
func (e *Editor) matchCountText() string {
	if e.prompt != promptFind && e.prompt != promptReplace && e.prompt != promptWith || e.findQuery == "" {
		return ""
	}
	a, b, ok := e.sel()
	if !ok {
		a = e.cursor
	}
	if p, err := e.pattern(); err != nil {
		return patternError(err)
	} else if p != nil {
		return e.patternCountText(p, a, b, ok)
	}
	c := &e.count
	if c.buf != e.buf || c.q != e.findQuery || c.version != e.buf.version || c.at != a {
		c.buf, c.q, c.version, c.at = e.buf, e.findQuery, e.buf.version, a
		c.total, c.before, c.complete = e.buf.CountMatches(e.findQuery, a, countBudget)
	}
	n := groupDigits(int64(c.total))
	if !c.complete {
		n += "+"
	}
	switch {
	case c.total == 0 && c.complete:
		return "no matches"
	case ok && e.isMatch(a, b):
		return fmt.Sprintf("%s of %s", groupDigits(int64(c.before+1)), n)
	case c.total == 1:
		return "1 match"
	}
	return n + " matches"
}
