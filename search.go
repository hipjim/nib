package main

import (
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
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

// indexRare finds q in s. It scans for q's rarest byte with IndexByte and
// checks around each hit; if even that byte is common, it uses Horspool's
// skip search instead. With fold, q is lower-case ASCII and matches either
// case.
func indexRare(s, q string, fold bool) int {
	k := rarest(q, fold)
	if len(q) >= 4 && byteRank[q[k]] >= commonRank {
		return horspool(s, q, fold)
	}
	lo, up := q[k], q[k]
	if fold && 'a' <= lo && lo <= 'z' {
		up = lo - 'a' + 'A'
	}
	nextLo, nextUp := -1, -1 // positions of the rare byte's two cases
	for i := k; i+len(q)-k <= len(s); {
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
		if start+len(q) > len(s) {
			return -1
		}
		cand := s[start : start+len(q)]
		if fold && equalFoldASCII(cand, q) || !fold && cand == q {
			return start
		}
		i++
	}
	return -1
}

// horspool finds q in s by checking the last byte of each window and
// skipping ahead by up to len(q) when it cannot end a match.
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
