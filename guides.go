package main

import "strings"

// guideLookahead is how far past the screen a blank line looks for the
// next non-blank one.
const guideLookahead = 200

// guides returns, for each line in [first, last], the visual column its
// indent ends at, which is where its indent guides stop. A blank line gets
// the deeper of its non-blank neighbors, so guides run through blocks.
func (e *Editor) guides(first, last int) []int {
	if e.dir != nil {
		return make([]int, last-first+1)
	}
	n := e.buf.LineCount()
	indent := func(i int) (int, bool) {
		l := e.buf.Line(i)
		if strings.TrimSpace(l) == "" {
			return 0, false
		}
		return e.buf.VisCol(i, len(leadingSpace(l))), true
	}
	out := make([]int, last-first+1)
	prev := 0
	for i := first - 1; i >= max(0, first-guideLookahead); i-- {
		if w, ok := indent(i); ok {
			prev = w
			break
		}
	}
	next, nextAt := 0, -1 // indent of the next non-blank line, once looked up
	for i := first; i <= last; i++ {
		if w, ok := indent(i); ok {
			out[i-first], prev = w, w
			continue
		}
		if nextAt < i {
			next, nextAt = 0, n
			for j := i + 1; j < min(n, i+guideLookahead); j++ {
				if w, ok := indent(j); ok {
					next, nextAt = w, j
					break
				}
			}
		}
		out[i-first] = max(prev, next)
	}
	return out
}
