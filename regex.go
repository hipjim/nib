// Patterns: find with regular expressions, exact case or whole words. The
// default find is the literal searcher in search.go, which scans unedited
// blocks as one piece of the file. A regular expression cannot: ^, $ and
// \s would see across lines. So a pattern tests one line at a time, and a
// literal every match must contain picks the lines worth testing.
//
// A pattern over the whole file can take seconds, so it runs in slices of
// patternBudget from Layout. The store is only read between edits, on the
// main thread's terms, and nothing outlives a change to the buffer.

package main

import (
	"regexp"
	"regexp/syntax"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
)

const (
	patternBudget = 8 * time.Millisecond // of a frame, for each background step
	patternChunk  = blockLines           // lines between checks of the budget

	// A single line cannot be interrupted, so the longest one bounds a step.
	// Longer lines are skipped and the status bar says so.
	maxPatternLine = 16 << 20
)

// findOpts are the find modes, kept across searches.
type findOpts struct {
	regex, exact, word bool
}

// label is what the prompt shows for the modes that are on.
func (o findOpts) label() string {
	s := ""
	if o.regex {
		s += " .*"
	}
	if o.exact {
		s += " Aa"
	}
	if o.word {
		s += ` \b`
	}
	return s
}

type pattern struct {
	re      *regexp.Regexp
	regex   bool             // the replacement may use $1
	lit     func(string) int // finds a literal every match contains; may be nil
	skipped atomic.Bool      // a line over maxPatternLine was passed over
}

func compilePattern(q string, o findOpts) (*pattern, error) {
	src := q
	if !o.regex {
		src = regexp.QuoteMeta(q)
	}
	if o.word {
		src = `\b(?:` + src + `)\b`
	}
	if !o.exact && !hasUpper(q, o.regex) {
		src = "(?i)" + src
	}
	re, err := regexp.Compile(src)
	if err != nil {
		return nil, err
	}
	p := &pattern{re: re, regex: o.regex}
	if parsed, err := syntax.Parse(src, syntax.Perl); err == nil {
		if lit, fold := requiredLiteral(parsed); lit != "" {
			p.lit = newSearcher(lit, fold).index
		}
	}
	return p, nil
}

// hasUpper applies smart case to a query, ignoring what escapes mean in a
// regular expression: \S and \p{Greek} are not capital letters.
func hasUpper(q string, regex bool) bool {
	rs := []rune(q)
	for i := 0; i < len(rs); i++ {
		if regex && rs[i] == '\\' {
			i++
			if i+1 < len(rs) && (rs[i] == 'p' || rs[i] == 'P') && rs[i+1] == '{' {
				for i < len(rs) && rs[i] != '}' {
					i++
				}
			}
			continue
		}
		if i < len(rs) && unicode.IsUpper(rs[i]) {
			return true
		}
	}
	return false
}

// requiredLiteral returns the longest literal every match must contain, and
// whether it matches either ASCII case.
func requiredLiteral(re *syntax.Regexp) (string, bool) {
	switch re.Op {
	case syntax.OpLiteral:
		if re.Flags&syntax.FoldCase == 0 {
			return string(re.Rune), false
		}
		return foldableRun(re.Rune), true
	case syntax.OpCapture, syntax.OpPlus:
		return requiredLiteral(re.Sub[0])
	case syntax.OpRepeat:
		if re.Min > 0 {
			return requiredLiteral(re.Sub[0])
		}
	case syntax.OpConcat:
		best, bestFold := "", false
		for _, sub := range re.Sub {
			if s, fold := requiredLiteral(sub); len(s) > len(best) {
				best, bestFold = s, fold
			}
		}
		return best, bestFold
	}
	return "", false
}

// foldableRun is the longest run of the runes that an ASCII case fold finds
// exactly. Non-ASCII letters have other case forms, and so do k and s: they
// also match the Kelvin sign and the long s.
func foldableRun(rs []rune) string {
	best, cur := "", []rune{}
	for _, r := range append(rs[:len(rs):len(rs)], -1) {
		l := unicode.ToLower(r)
		if r >= 0 && r < 0x80 && l != 'k' && l != 's' {
			cur = append(cur, l)
			continue
		}
		if len(cur) > len(best) {
			best = string(cur)
		}
		cur = cur[:0]
	}
	return best
}

// first finds the first non-empty match in line at or after col.
func (p *pattern) first(line string, col int) (a, b int, ok bool) {
	if col == 0 {
		loc := p.re.FindStringIndex(line)
		if loc == nil {
			return 0, 0, false
		}
		if loc[1] > loc[0] {
			return loc[0], loc[1], true
		}
	}
	// Not a search of line[col:], which would see a false start of line.
	for _, loc := range p.re.FindAllStringIndex(line, -1) {
		if loc[0] >= col && loc[1] > loc[0] {
			return loc[0], loc[1], true
		}
	}
	return 0, 0, false
}

// last finds the last non-empty match in line ending at or before col.
func (p *pattern) last(line string, col int) (a, b int, ok bool) {
	for _, loc := range p.re.FindAllStringIndex(line, -1) {
		if loc[1] <= col && loc[1] > loc[0] {
			a, b, ok = loc[0], loc[1], true
		}
	}
	return a, b, ok
}

// count is the number of non-empty matches in line starting before limit.
func (p *pattern) count(line string, limit int) int {
	n := 0
	for _, loc := range p.re.FindAllStringIndex(line, -1) {
		if loc[1] > loc[0] && loc[0] < limit {
			n++
		}
	}
	return n
}

// isMatch reports whether line[a:b] is exactly one of the matches in line.
func (p *pattern) isMatch(line string, a, b int) bool {
	for _, loc := range p.re.FindAllStringIndex(line, -1) {
		if loc[0] == a && loc[1] == b {
			return b > a
		}
	}
	return false
}

// expand is what the match at line[loc[0]:loc[1]] is replaced by.
func (p *pattern) expand(dst []byte, with, line string, loc []int) []byte {
	if !p.regex {
		return append(dst, with...)
	}
	return p.re.ExpandString(dst, with, line, loc)
}

// replaced is line with every non-empty match replaced, and how many.
func (p *pattern) replaced(line, with string) (string, int) {
	var out []byte
	at, n := 0, 0
	for _, loc := range p.re.FindAllStringSubmatchIndex(line, -1) {
		if loc[1] == loc[0] {
			continue
		}
		out = append(out, line[at:loc[0]]...)
		out = p.expand(out, with, line, loc)
		at = loc[1]
		n++
	}
	if n == 0 {
		return line, 0
	}
	return string(append(out, line[at:]...)), n
}

// replaceOne is what the match at line[a:b] becomes.
func (p *pattern) replaceOne(line string, a, b int, with string) string {
	for _, loc := range p.re.FindAllStringSubmatchIndex(line, -1) {
		if loc[0] == a && loc[1] == b {
			return string(p.expand(nil, with, line, loc))
		}
	}
	return with
}

// eachLine calls fn with the lines in r that may match, in order, until it
// returns true. Unedited blocks jump between hits of the literal instead of
// walking every line. It only reads the store, so it can run on several
// goroutines at once.
func (s *lineStore) eachLine(r lineRange, p *pattern, fn func(line int, text string) bool) {
	test := func(line int, text string) bool {
		if len(text) > maxPatternLine {
			p.skipped.Store(true)
			return false
		}
		return fn(line, text)
	}
	bi := sort.Search(len(s.blocks), func(x int) bool { return s.first[x] > r.a }) - 1
	for ; bi >= 0 && bi < len(s.blocks) && s.first[bi] < r.b; bi++ {
		k := s.blocks[bi]
		j0 := max(r.a, s.first[bi]) - s.first[bi]
		j1 := min(r.b, s.first[bi]+k.n()) - s.first[bi]
		if k.idx == nil || p.lit == nil {
			for j := j0; j < j1; j++ {
				text := s.lineIn(bi, j)
				if p.lit != nil && p.lit(text) < 0 {
					continue
				}
				if test(s.first[bi]+j, text) {
					return
				}
			}
			continue
		}
		end := k.base + int(k.idx[j1]) - 1
		for o := k.base + int(k.idx[j0]); o < end; {
			m := p.lit(s.data[o:end])
			if m < 0 {
				break
			}
			j := s.posIn(k, bi, -1, o+m).Line - s.first[bi]
			if test(s.first[bi]+j, s.raw(k, j)) {
				return
			}
			o = k.base + int(k.idx[j+1])
		}
	}
}

// stepChunks runs fn on chunks from *next on all cores until the budget is
// spent or stop says so. Every chunk below the final *next is finished when
// it returns. It returns false if the mapped file faulted.
func stepChunks(next *atomic.Int64, n int, budget time.Duration, stop func(i int) bool, fn func(i int)) bool {
	start := time.Now()
	return parallelFor(runtime.NumCPU(), 1, func(_, _, _ int) {
		for time.Since(start) < budget {
			i := int(next.Add(1) - 1)
			if i >= n || stop(i) {
				return
			}
			fn(i)
		}
	})
}

// patternSearch is a find still going through the file in the background:
// the first match in search order, wrapping around.
type patternSearch struct {
	p        *pattern
	buf      *Buffer
	version  int
	cursor   Pos // cancelled if the cursor moves meanwhile
	anchor   Pos
	backward bool
	chunks   []lineRange
	next     atomic.Int64
}

func (s *lineStore) chunksOf(ranges []lineRange, backward bool) []lineRange {
	var chunks []lineRange
	for _, r := range ranges {
		if backward {
			for b := r.b; b > r.a; b -= patternChunk {
				chunks = append(chunks, lineRange{max(r.a, b-patternChunk), b})
			}
		} else {
			for a := r.a; a < r.b; a += patternChunk {
				chunks = append(chunks, lineRange{a, min(r.b, a+patternChunk)})
			}
		}
	}
	return chunks
}

// step searches for one slice of time. It returns the match if found, and
// done once there is nothing left to do.
func (ps *patternSearch) step(budget time.Duration) (a, b Pos, found, done bool) {
	s := &ps.buf.lines
	type result struct{ a, b Pos }
	n := len(ps.chunks)
	var best atomic.Int64
	best.Store(int64(n))
	var res sync.Map // chunk to result; few are ever stored
	ok := stepChunks(&ps.next, n, budget, func(i int) bool { return int64(i) >= best.Load() }, func(i int) {
		var r result
		hit := false
		s.eachLine(ps.chunks[i], ps.p, func(line int, text string) bool {
			if ps.backward {
				if x, y, ok := ps.p.last(text, len(text)); ok {
					r, hit = result{Pos{line, x}, Pos{line, y}}, true
				}
				return false
			}
			if x, y, ok := ps.p.first(text, 0); ok {
				r, hit = result{Pos{line, x}, Pos{line, y}}, true
				return true
			}
			return false
		})
		if !hit {
			return
		}
		res.Store(i, r)
		for {
			c := best.Load()
			if int64(i) >= c || best.CompareAndSwap(c, int64(i)) {
				return
			}
		}
	})
	if !ok {
		return a, b, false, true
	}
	if i := int(best.Load()); i < n {
		v, _ := res.Load(i)
		return v.(result).a, v.(result).b, true, true
	}
	return a, b, false, ps.next.Load() >= int64(n)
}

// patternCount counts a pattern's matches a slice of time per frame, keeping
// one count per chunk, so moving to the next match need not count again.
type patternCount struct {
	p       *pattern
	buf     *Buffer
	version int
	q       string
	opts    findOpts
	counts  []int32
	next    atomic.Int64
	total   int
}

func newPatternCount(p *pattern, b *Buffer, q string, o findOpts) *patternCount {
	n := (b.LineCount() + patternChunk - 1) / patternChunk
	return &patternCount{p: p, buf: b, version: b.version, q: q, opts: o, counts: make([]int32, n)}
}

func (c *patternCount) done() bool { return int(c.next.Load()) >= len(c.counts) }

func (c *patternCount) step(budget time.Duration) {
	s := &c.buf.lines
	// Lines may still be arriving from the indexer, in whole blocks.
	if n := (s.count + patternChunk - 1) / patternChunk; n > len(c.counts) {
		c.counts = append(c.counts, make([]int32, n-len(c.counts))...)
	}
	if c.done() {
		return
	}
	from := int(c.next.Load())
	ok := stepChunks(&c.next, len(c.counts), budget, func(int) bool { return false }, func(i int) {
		r := lineRange{i * patternChunk, min(s.count, (i+1)*patternChunk)}
		n := 0
		s.eachLine(r, c.p, func(_ int, text string) bool {
			n += c.p.count(text, len(text))
			return false
		})
		c.counts[i] = int32(n)
	})
	if !ok {
		c.next.Store(int64(len(c.counts)))
		return
	}
	to := min(int(c.next.Load()), len(c.counts))
	c.next.Store(int64(to))
	for _, n := range c.counts[from:to] {
		c.total += int(n)
	}
}

// before is how many matches start before at, of those counted so far.
func (c *patternCount) before(at Pos) int {
	n := 0
	k := at.Line / patternChunk
	done := min(int(c.next.Load()), len(c.counts))
	for _, x := range c.counts[:min(k, done)] {
		n += int(x)
	}
	if k < done {
		s := &c.buf.lines
		r := lineRange{k * patternChunk, min(s.count, at.Line+1)}
		s.eachLine(r, c.p, func(line int, text string) bool {
			if line < at.Line {
				n += c.p.count(text, len(text))
			} else {
				n += c.p.count(text, at.Col)
			}
			return false
		})
	}
	return n
}

// ReplaceAllPattern replaces every match of p as one undo step and returns
// how many it replaced. A replacement cannot add lines, so like ReplaceAll
// it only touches the lines with a match.
func (b *Buffer) ReplaceAllPattern(p *pattern, with string, cursor Pos) int {
	if strings.Contains(with, "\n") {
		return 0
	}
	s := &b.lines
	n := (s.count + searchChunk - 1) / searchChunk
	found := make([][]lineEdit, n)
	counts := make([]int, n)
	var next atomic.Int64
	ok := parallelFor(runtime.NumCPU(), 1, func(_, _, _ int) {
		for {
			i := int(next.Add(1) - 1)
			if i >= n {
				return
			}
			r := lineRange{i * searchChunk, min(s.count, (i+1)*searchChunk)}
			s.eachLine(r, p, func(line int, text string) bool {
				if repl, c := p.replaced(text, with); c > 0 {
					found[i] = append(found[i], lineEdit{line: line, old: text, new: repl})
					counts[i] += c
				}
				return false
			})
		}
	})
	if !ok {
		return 0 // the file vanished under us; the next draw reports it
	}
	var changes []lineEdit
	total := 0
	for i, f := range found {
		changes = append(changes, f...)
		total += counts[i]
	}
	if len(changes) > 0 {
		b.commitLines(changes, cursor)
	}
	return total
}

// ---- the editor's side ----

// pattern is the compiled query when a find mode is on; with none on it is
// nil and find uses the literal searcher.
func (e *Editor) pattern() (*pattern, error) {
	if e.opts == (findOpts{}) {
		return nil, nil
	}
	if e.patQ != e.findQuery || e.patOpts != e.opts || (e.pat == nil && e.patErr == nil) {
		e.patQ, e.patOpts = e.findQuery, e.opts
		e.pat, e.patErr = compilePattern(e.findQuery, e.opts)
	}
	return e.pat, e.patErr
}

// asQuery turns selected text into a query that finds that text.
func (e *Editor) asQuery(s string) string {
	if e.opts.regex {
		return regexp.QuoteMeta(s)
	}
	return s
}

func (e *Editor) findPattern(from Pos, backward bool) {
	e.search = nil
	p, err := e.pattern()
	if err != nil {
		e.notFound = true
		return
	}
	e.notFound = false
	from = e.buf.Clamp(from)
	l, line := from.Line, e.buf.Line(from.Line)
	if len(line) <= maxPatternLine {
		a, b, ok := p.first(line, from.Col)
		if backward {
			a, b, ok = p.last(line, from.Col)
		}
		if ok {
			e.anchor = Pos{l, a}
			e.moveTo(Pos{l, b}, true)
			return
		}
	}
	n := e.buf.LineCount()
	ranges := []lineRange{{l + 1, n}, {0, l + 1}}
	if backward {
		ranges = []lineRange{{0, l}, {l, n}}
	}
	e.search = &patternSearch{p: p, buf: e.buf, version: e.buf.version, cursor: e.cursor, anchor: e.anchor,
		backward: backward, chunks: e.buf.lines.chunksOf(ranges, backward)}
	e.stepSearch() // a small file is done within the key press
}

// stepSearch moves a find in the background on by one slice of time.
func (e *Editor) stepSearch() {
	ps := e.search
	if ps == nil {
		return
	}
	if ps.buf != e.buf || ps.version != e.buf.version || ps.cursor != e.cursor || ps.anchor != e.anchor {
		e.search = nil
		return
	}
	a, b, found, done := ps.step(patternBudget)
	switch {
	case found:
		e.search = nil
		e.anchor = a
		e.moveTo(b, true)
	case done:
		e.search = nil
		e.notFound = true
	}
}

// patternCountText is the status bar's "3 of 128" for a pattern. The count
// goes on over several frames, shown as a lower bound until it is done.
func (e *Editor) patternCountText(p *pattern, a, b Pos, sel bool) string {
	c := e.pcount
	if c == nil || c.buf != e.buf || c.version != e.buf.version || c.q != e.findQuery || c.opts != e.opts {
		c = newPatternCount(p, e.buf, e.findQuery, e.opts)
		e.pcount = c
		e.pcAt, e.pcNext = Pos{-1, 0}, -1
	}
	c.step(patternBudget)
	if next := int(c.next.Load()); e.pcAt != a || e.pcNext != next || e.pcEnd != b {
		e.pcAt, e.pcEnd, e.pcNext = a, b, next
		e.pcBefore = c.before(a)
		e.pcIs = sel && e.isMatch(a, b)
	}
	n := groupDigits(int64(c.total))
	if !c.done() {
		n += "+"
	}
	note := ""
	if p.skipped.Load() {
		note = longLinesNote
	}
	switch {
	case c.total == 0 && c.done():
		return note + "no matches"
	case e.pcIs:
		return note + groupDigits(int64(e.pcBefore+1)) + " of " + n
	case c.total == 1 && c.done():
		return note + "1 match"
	}
	return note + n + " matches"
}

// toggleFind flips a find mode and searches again with it.
func (e *Editor) toggleFind(flip func(*findOpts)) {
	flip(&e.opts)
	switch e.prompt {
	case promptFind, promptReplace:
		e.notFound = false
		e.find(e.findOrigin, false)
	case promptGrep:
		e.grep.opts = e.opts
		e.grep.start(e.root, e.input)
	case promptFilter:
		e.filter.start(e.buf, e.input, e.opts)
	}
}

func toggleRegex(o *findOpts) { o.regex = !o.regex }
func toggleExact(o *findOpts) { o.exact = !o.exact }
func toggleWord(o *findOpts)  { o.word = !o.word }

// findBusy reports whether a find, a count or the filter wants another frame.
func (e *Editor) findBusy() bool {
	switch e.prompt {
	case promptFilter:
		return e.filter.busy()
	case promptFind, promptReplace, promptWith:
		if e.pcount != nil && e.opts != (findOpts{}) && !e.pcount.done() {
			return true
		}
	}
	return e.search != nil
}
