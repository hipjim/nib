package main

import (
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// searchChunk is how many lines one worker searches at a time.
const searchChunk = 16 * blockLines

// lineRange is lines [a, b).
type lineRange struct{ a, b int }

// searchLines finds the first (or, backward, the last) match in the ranges,
// which are in search order. Chunks are searched on all cores; a chunk
// after one that already matched is skipped.
func (s *lineStore) searchLines(ranges []lineRange, backward bool, index func(string) int) (Pos, bool) {
	var chunks []lineRange
	for _, r := range ranges {
		if backward {
			for b := r.b; b > r.a; b -= searchChunk {
				chunks = append(chunks, lineRange{max(r.a, b-searchChunk), b})
			}
		} else {
			for a := r.a; a < r.b; a += searchChunk {
				chunks = append(chunks, lineRange{a, min(r.b, a+searchChunk)})
			}
		}
	}
	if len(chunks) == 0 {
		return Pos{}, false
	}
	found := make([]Pos, len(chunks))
	var best atomic.Int64
	best.Store(int64(len(chunks)))
	var next atomic.Int64
	var wg sync.WaitGroup
	for range min(runtime.NumCPU(), len(chunks)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			debug.SetPanicOnFault(true) // the mapped file may be truncated
			defer func() { recover() }()
			for {
				i := next.Add(1) - 1
				if i >= best.Load() {
					return
				}
				p, ok := s.searchRange(chunks[i], backward, index)
				if !ok {
					continue
				}
				found[i] = p
				for {
					b := best.Load()
					if i >= b || best.CompareAndSwap(b, i) {
						break
					}
				}
			}
		}()
	}
	wg.Wait()
	if b := best.Load(); b < int64(len(chunks)) {
		return found[b], true
	}
	return Pos{}, false
}

// countMatches counts the matches of a query on all cores, chunk by chunk
// in file order, and how many start before at. Workers stop once budget has
// passed; the result then covers the longest run of finished chunks from the
// start of the file, and complete is false.
func (s *lineStore) countMatches(index func(string) int, qlen int, at Pos, budget time.Duration) (total, before int, complete bool) {
	n := (s.count + searchChunk - 1) / searchChunk
	if n == 0 {
		return 0, 0, true
	}
	type counts struct {
		n, before int
		done      bool
	}
	res := make([]counts, n)
	start := time.Now()
	workers := min(runtime.NumCPU(), n)
	ok := parallelFor(workers, 1, func(_, _, w int) {
		for i := w; i < n; i += workers {
			if time.Since(start) >= budget {
				return
			}
			r := lineRange{i * searchChunk, min(s.count, (i+1)*searchChunk)}
			c, b := s.countRange(r, at, index, qlen)
			res[i] = counts{c, b, true}
		}
	})
	if !ok {
		return 0, 0, false
	}
	for _, c := range res {
		if !c.done {
			return total, before, false
		}
		total += c.n
		before += c.before
	}
	return total, before, true
}

// countRange counts the matches in lines [r.a, r.b) and how many start
// before at.
func (s *lineStore) countRange(r lineRange, at Pos, index func(string) int, qlen int) (n, before int) {
	bi := sort.Search(len(s.blocks), func(x int) bool { return s.first[x] > r.a }) - 1
	for ; bi < len(s.blocks) && s.first[bi] < r.b; bi++ {
		k := s.blocks[bi]
		j0 := max(r.a, s.first[bi]) - s.first[bi]
		j1 := min(r.b, s.first[bi]+k.n()) - s.first[bi]
		// Whole blocks before at count without locating each match.
		all := s.first[bi]+j1 <= at.Line
		none := s.first[bi]+j0 > at.Line
		each := func(line int, text string, off int) {
			for o := 0; o <= len(text); {
				m := index(text[o:])
				if m < 0 {
					return
				}
				n++
				if all || !none && s.posIn(k, bi, line, off+o+m).Less(at) {
					before++
				}
				o += m + qlen
			}
		}
		if k.idx != nil {
			start, end := k.base+int(k.idx[j0]), k.base+int(k.idx[j1])-1
			each(-1, s.data[start:end], start)
			continue
		}
		for j := j0; j < j1; j++ {
			each(s.first[bi]+j, k.lines[j], 0)
		}
	}
	return n, before
}

// searchRange finds the first or last match in lines [r.a, r.b). It only
// reads the store, so it can run on several goroutines at once. Unedited
// blocks are searched as one piece of the file instead of line by line;
// the query has no newline, so a match never spans lines.
func (s *lineStore) searchRange(r lineRange, backward bool, index func(string) int) (Pos, bool) {
	bi := sort.Search(len(s.blocks), func(x int) bool { return s.first[x] > r.a }) - 1
	var last Pos
	found := false
	for ; bi < len(s.blocks) && s.first[bi] < r.b; bi++ {
		k := s.blocks[bi]
		j0 := max(r.a, s.first[bi]) - s.first[bi]
		j1 := min(r.b, s.first[bi]+k.n()) - s.first[bi]
		each := func(line int, text string, off int) bool {
			for o := 0; o <= len(text); {
				m := index(text[o:])
				if m < 0 {
					return false
				}
				last, found = s.posIn(k, bi, line, off+o+m), true
				if !backward {
					return true
				}
				o += m + 1
			}
			return false
		}
		if k.idx != nil {
			start, end := k.base+int(k.idx[j0]), k.base+int(k.idx[j1])-1
			if each(-1, s.data[start:end], start) {
				return last, true
			}
			continue
		}
		for j := j0; j < j1; j++ {
			if each(s.first[bi]+j, k.lines[j], 0) {
				return last, true
			}
		}
	}
	return last, found
}

// posIn turns a match into a position: line is known for edited blocks;
// for unedited ones off is a file offset and the line is looked up.
func (s *lineStore) posIn(k *block, bi, line, off int) Pos {
	if line >= 0 {
		return Pos{line, off}
	}
	rel := uint32(off - k.base)
	j := sort.Search(len(k.idx), func(x int) bool { return k.idx[x] > rel }) - 1
	return Pos{s.first[bi] + j, int(rel - k.idx[j])}
}

// byteRank orders bytes from rare to common in text and code, so a search
// can scan for the query's rarest byte.
var byteRank = func() (r [256]uint8) {
	for i := range r {
		r[i] = 100 // other bytes and non-ASCII
	}
	const common = " etaoinsrhldcumfpgwybv,.\n_()=;:\"'/-0123456789<>{}[]kxjqz"
	for i := 0; i < len(common); i++ {
		r[common[i]] = 255 - uint8(i)
	}
	for c := 'A'; c <= 'Z'; c++ {
		r[c] = r[c+'a'-'A'] / 2 // upper case is rarer than lower case
	}
	return
}()

// rarest returns the index in q of its rarest byte; fold counts both cases.
func rarest(q string, fold bool) int {
	best, bestRank := 0, 1<<30
	for i := 0; i < len(q); i++ {
		c := q[i]
		rank := int(byteRank[c])
		if fold && 'a' <= c && c <= 'z' {
			rank += int(byteRank[c-'a'+'A'])
		}
		if rank < bestRank {
			best, bestRank = i, rank
		}
	}
	return best
}

// commonRank is the rank from which a byte shows up every few bytes in
// text, so scanning for it stops too often.
const commonRank = 232

// A searcher finds one query in strings. It scans for the query's rarest
// byte with IndexByte and checks around each hit. If even that byte is
// common, it skips through the text by the last two bytes of each window
// (Horspool with 2-grams), which skips nearly the query's length at a time
// where single bytes would stop every few bytes. Its tables are built once,
// so a search over millions of short lines starts each one right away.
type searcher struct {
	q      string
	fold   bool // q is lower-case ASCII and matches either case
	k      int  // index of the rarest byte of q
	lo, up byte // that byte in both cases
	// With a skip table, d[pair] is m-1 minus the shift for a window
	// ending in pair, so a zeroed table shifts by m-1; m-1 itself marks the
	// query's own last pair, and last is the shift after checking it.
	d    *[65536]uint8
	last int
}

// horspoolMax is the longest query the skip table holds.
const horspoolMax = 256

func newSearcher(q string, fold bool) *searcher {
	k := rarest(q, fold)
	sr := &searcher{q: q, fold: fold, k: k, lo: q[k], up: q[k]}
	if fold && 'a' <= sr.lo && sr.lo <= 'z' {
		sr.up = sr.lo - 'a' + 'A'
	}
	m := len(q)
	if m < 4 || m > horspoolMax || byteRank[q[k]] < commonRank {
		return sr
	}
	sr.d = new([65536]uint8)
	sr.last = m - 1
	cases := func(c byte) []byte {
		if fold && 'a' <= c && c <= 'z' {
			return []byte{c, c - 'a' + 'A'}
		}
		return []byte{c}
	}
	for j := 0; j+1 < m; j++ {
		if j+1 == m-1 {
			// The shift after a window that ends like the query: to its
			// previous occurrence of that pair, if any.
			sr.last = m - 1 - int(sr.d[pairAt(q, j)])
		}
		for _, a := range cases(q[j]) {
			for _, b := range cases(q[j+1]) {
				sr.d[uint16(a)|uint16(b)<<8] = uint8(j + 1)
			}
		}
	}
	return sr
}

func (sr *searcher) equal(cand string) bool {
	if sr.fold {
		return equalFoldASCII(cand, sr.q)
	}
	return cand == sr.q
}

func (sr *searcher) index(s string) int {
	q, m := sr.q, len(sr.q)
	if len(s) < m {
		return -1
	}
	if sr.d != nil {
		return sr.horspool2(s)
	}
	if m > horspoolMax && byteRank[q[sr.k]] >= commonRank {
		return horspool(s, q, sr.fold)
	}
	k, lo, up := sr.k, sr.lo, sr.up
	nextLo, nextUp := -1, -1 // positions of the rare byte's two cases
	for i := k; i+m-k <= len(s); {
		if nextLo < i {
			if nextLo = strings.IndexByte(s[i:], lo); nextLo < 0 {
				nextLo = len(s)
			} else {
				nextLo += i
			}
		}
		if nextUp < i {
			if lo == up {
				nextUp = nextLo
			} else if nextUp = strings.IndexByte(s[i:], up); nextUp < 0 {
				nextUp = len(s)
			} else {
				nextUp += i
			}
		}
		i = min(nextLo, nextUp)
		start := i - k
		if start+m > len(s) {
			return -1
		}
		if sr.equal(s[start : start+m]) {
			return start
		}
		i++
	}
	return -1
}

// pairAt returns the bytes s[i] and s[i+1] as one word, which is how the
// skip table is indexed. The compiler makes it a single load.
func pairAt(s string, i int) uint16 {
	return uint16(s[i]) | uint16(s[i+1])<<8
}

// streams is how many parts of the text horspool2 scans at once.
const streams = 4

// horspool2 finds the query by looking at the last two bytes of each
// window of the text and skipping ahead by up to len(q)-1. Each step
// waits for the previous one's loads, so a long text is cut into streams
// parts scanned in the same loop, which the processor overlaps.
func (sr *searcher) horspool2(s string) int {
	m := len(sr.q)
	if len(s) < streams*4096 {
		return sr.scan(s, 0, len(s))
	}
	var from, to [streams]int // the windows of part k start in [from, to-m]
	part := len(s) / streams
	for k := range streams {
		from[k], to[k] = k*part, min(len(s), (k+1)*part+m-1)
	}
	d := sr.d
	i := from
	for i[0]+m <= to[0] && i[1]+m <= to[1] && i[2]+m <= to[2] && i[3]+m <= to[3] {
		var x [streams]int
		x[0] = int(d[pairAt(s, i[0]+m-2)])
		x[1] = int(d[pairAt(s, i[1]+m-2)])
		x[2] = int(d[pairAt(s, i[2]+m-2)])
		x[3] = int(d[pairAt(s, i[3]+m-2)])
		if x[0] == m-1 || x[1] == m-1 || x[2] == m-1 || x[3] == m-1 {
			for k := range streams {
				if x[k] != m-1 {
					i[k] += m - 1 - x[k]
					continue
				}
				if sr.equal(s[i[k] : i[k]+m]) {
					// An earlier part may still hold an earlier match.
					for j := range k {
						if r := sr.scan(s, i[j], to[j]); r >= 0 {
							return r
						}
					}
					return i[k]
				}
				i[k] += sr.last
			}
			continue
		}
		i[0] += m - 1 - x[0]
		i[1] += m - 1 - x[1]
		i[2] += m - 1 - x[2]
		i[3] += m - 1 - x[3]
	}
	// One part ran out; finish the rest in order.
	for k := range streams {
		if j := sr.scan(s, i[k], to[k]); j >= 0 {
			return j
		}
	}
	return -1
}

// scan finds the query in the windows of s that start at or after i and
// end at or before to.
func (sr *searcher) scan(s string, i, to int) int {
	m, d := len(sr.q), sr.d
	for i+m <= to {
		x := int(d[pairAt(s, i+m-2)])
		if x == m-1 {
			if sr.equal(s[i : i+m]) {
				return i
			}
			i += sr.last
			continue
		}
		i += m - 1 - x
	}
	return -1
}

// horspool finds q in s by checking the last byte of each window and
// skipping ahead by up to len(q) when it cannot end a match. It serves
// queries too long for a searcher's table.
func horspool(s, q string, fold bool) int {
	m := len(q)
	var skip [256]int
	for i := range skip {
		skip[i] = m
	}
	for i := 0; i < m-1; i++ {
		skip[q[i]] = m - 1 - i
		if fold && 'a' <= q[i] && q[i] <= 'z' {
			skip[q[i]-'a'+'A'] = m - 1 - i
		}
	}
	last := q[m-1]
	for i := 0; i+m <= len(s); {
		c := s[i+m-1]
		lc := c
		if fold && 'A' <= c && c <= 'Z' {
			lc = c + 'a' - 'A'
		}
		if lc == last {
			if cand := s[i : i+m]; fold && equalFoldASCII(cand, q) || !fold && cand == q {
				return i
			}
		}
		i += skip[c]
	}
	return -1
}

// matchingLines returns, in order, every line with a match, searching
// chunks on all cores.
func (s *lineStore) matchingLines(index func(string) int) []int {
	var chunks []lineRange
	for a := 0; a < s.count; a += searchChunk {
		chunks = append(chunks, lineRange{a, min(s.count, a+searchChunk)})
	}
	found := make([][]int, len(chunks))
	var next atomic.Int64
	var wg sync.WaitGroup
	for range min(runtime.NumCPU(), len(chunks)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			debug.SetPanicOnFault(true) // the mapped file may be truncated
			defer func() { recover() }()
			for {
				i := int(next.Add(1) - 1)
				if i >= len(chunks) {
					return
				}
				found[i] = s.linesIn(chunks[i], index)
			}
		}()
	}
	wg.Wait()
	var lines []int
	for _, f := range found {
		lines = append(lines, f...)
	}
	return lines
}

// linesIn returns the lines in [r.a, r.b) with a match. Like searchRange,
// it searches unedited blocks as one piece of the file.
func (s *lineStore) linesIn(r lineRange, index func(string) int) []int {
	var lines []int
	bi := sort.Search(len(s.blocks), func(x int) bool { return s.first[x] > r.a }) - 1
	for ; bi < len(s.blocks) && s.first[bi] < r.b; bi++ {
		k := s.blocks[bi]
		j0 := max(r.a, s.first[bi]) - s.first[bi]
		j1 := min(r.b, s.first[bi]+k.n()) - s.first[bi]
		if k.idx == nil {
			for j := j0; j < j1; j++ {
				if index(k.lines[j]) >= 0 {
					lines = append(lines, s.first[bi]+j)
				}
			}
			continue
		}
		start, end := k.base+int(k.idx[j0]), k.base+int(k.idx[j1])-1
		for o := start; o < end; {
			m := index(s.data[o:end])
			if m < 0 {
				break
			}
			p := s.posIn(k, bi, -1, o+m)
			lines = append(lines, p.Line)
			o = k.base + int(k.idx[p.Line-s.first[bi]+1]) // the next line
		}
	}
	return lines
}
