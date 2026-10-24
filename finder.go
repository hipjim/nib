package main

import (
	"bufio"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"
)

// Folders the finder never walks into.
var skipDirs = map[string]bool{
	".git": true, ".hg": true, ".svn": true, "node_modules": true, "__pycache__": true,
	".venv": true, ".idea": true, ".cache": true, ".DS_Store": true,
}

const (
	maxFiles    = 500_000 // the walk stops after this many files
	maxResults  = 12
	walkBatch   = 2048
	rewalkAfter = 30 * time.Second
)

// finder lists every file under a folder in the background and ranks them
// against a query.
type finder struct {
	root    string
	paths   []string // slash-separated, relative to root
	walking chan []string
	stop    *atomic.Bool
	started time.Time

	query   string
	recent  []string // open files, most recent first, relative to root
	last    string   // the previous query, if all paths were ranked for it
	cand    []int32  // indices of the paths that matched last
	results []result
	matches int
	sel     int
}

type result struct {
	path  string
	score int
}

// start walks root unless a recent walk of it is still good.
func (f *finder) start(root string) {
	if f.root == root && time.Since(f.started) < rewalkAfter {
		return
	}
	if f.stop != nil {
		f.stop.Store(true)
	}
	stop := new(atomic.Bool)
	ch := make(chan []string, 8)
	*f = finder{root: root, walking: ch, stop: stop, started: time.Now()}
	go walk(root, ch, stop)
}

func walk(root string, ch chan<- []string, stop *atomic.Bool) {
	defer close(ch)
	ignore := readIgnore(filepath.Join(root, ".gitignore"))
	batch := make([]string, 0, walkBatch)
	n := 0
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if stop.Load() || n >= maxFiles {
			return filepath.SkipAll
		}
		if err != nil || p == root {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if skipDirs[d.Name()] || ignore.match(rel, d.IsDir()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		batch = append(batch, rel)
		n++
		if len(batch) == walkBatch {
			ch <- batch
			batch = make([]string, 0, walkBatch)
		}
		return nil
	})
	ch <- batch
}

// poll takes in walked paths. It reports whether new ones arrived and
// whether the walk is still going.
func (f *finder) poll() (changed, walking bool) {
	for f.walking != nil {
		select {
		case b, ok := <-f.walking:
			if !ok {
				f.walking = nil
				return true, false
			}
			f.paths = append(f.paths, b...)
			changed = true
		default:
			return changed, true
		}
	}
	return changed, false
}

// rank fills results with the best matches for the query. With no query it
// lists the open files, then the rest.
func (f *finder) rank() {
	f.results, f.matches = f.results[:0], 0
	isRecent := map[string]int{}
	for i, p := range f.recent {
		isRecent[p] = len(f.recent) - i
	}
	if f.query == "" {
		for _, p := range f.recent {
			f.results = append(f.results, result{p, 0})
		}
		for _, p := range f.paths {
			if len(f.results) == maxResults {
				break
			}
			if isRecent[p] == 0 {
				f.results = append(f.results, result{p, 0})
			}
		}
		f.results = f.results[:min(len(f.results), maxResults)]
		f.matches = len(f.paths)
		f.sel = min(f.sel, max(0, len(f.results)-1))
		return
	}
	q := []rune(f.query)
	fold := !slices.ContainsFunc(q, unicode.IsUpper)
	if fold {
		for i, r := range q {
			q[i] = unicode.ToLower(r)
		}
	}
	pos := make([]int, len(q))
	try := func(p string, bonus int) {
		s, ok := fuzzyScore(p, q, fold, pos)
		if !ok {
			return
		}
		f.matches++
		r := result{p, s + bonus}
		if len(f.results) == maxResults && !better(r, f.results[maxResults-1]) {
			return
		}
		i, _ := slices.BinarySearchFunc(f.results, r, func(a, b result) int {
			if better(a, b) {
				return -1
			}
			return 1
		})
		f.results = slices.Insert(f.results, i, r)
		f.results = f.results[:min(len(f.results), maxResults)]
	}
	// A longer query only matches paths the shorter one matched.
	walked := map[string]bool{}
	var cand []int32
	visit := func(i int32) {
		before := f.matches
		p := f.paths[i]
		if b := isRecent[p]; b > 0 {
			walked[p] = true
			try(p, 20*b)
		} else {
			try(p, 0)
		}
		if f.matches > before {
			cand = append(cand, i)
		}
	}
	if f.last != "" && strings.HasPrefix(f.query, f.last) {
		for _, i := range f.cand {
			visit(i)
		}
	} else {
		for i := range f.paths {
			visit(int32(i))
		}
	}
	f.last, f.cand = f.query, cand
	if f.walking != nil {
		f.last = "" // more paths are coming; rank them all next time
	}
	for _, p := range f.recent {
		if !walked[p] {
			try(p, 20*isRecent[p])
		}
	}
	f.sel = min(f.sel, max(0, len(f.results)-1))
}

func better(a, b result) bool {
	if a.score != b.score {
		return a.score > b.score
	}
	return len(a.path) < len(b.path)
}

// fuzzyScore matches q against p as a subsequence and scores it: matches at
// word starts, in a row and in the file name count more; gaps count less.
// It looks for the tightest match ending at the first possible place, like
// fzf's first algorithm.
func fuzzyScore(p string, q []rune, fold bool, pos []int) (int, bool) {
	pos, ok := fuzzyMatch(p, q, fold, pos)
	if !ok {
		return 0, false
	}
	base := strings.LastIndexByte(p, '/') + 1
	score := 0
	prev := -2
	for _, i := range pos {
		score += 16
		switch {
		case i == 0 || p[i-1] == '/':
			score += 12
		case strings.IndexByte("_-. ", p[i-1]) >= 0:
			score += 10
		case unicode.IsLower(rune(p[i-1])) && unicode.IsUpper(rune(p[i])):
			score += 8
		}
		if i == prev+1 {
			score += 8
		} else if prev >= 0 {
			score -= min(3+(i-prev-2), 12) // gap
		}
		if i >= base {
			score += 6
		}
		prev = i
	}
	return score - len(p)/16, true
}

// fuzzyMatch returns the byte positions in p of the tightest match of q,
// reusing pos.
func fuzzyMatch(p string, q []rune, fold bool, pos []int) ([]int, bool) {
	if len(q) == 0 {
		return nil, true
	}
	pos = slices.Grow(pos[:0], len(q))[:len(q)]
	if !slices.ContainsFunc(q, func(r rune) bool { return r >= utf8.RuneSelf }) {
		return matchASCII(p, q, fold, pos)
	}
	lower := func(r rune) rune {
		if fold {
			return unicode.ToLower(r)
		}
		return r
	}
	// Forward: find where the first full match ends.
	j, end := 0, -1
	for i, r := range p {
		if lower(r) == q[j] {
			j++
			if j == len(q) {
				end = i
				break
			}
		}
	}
	if end < 0 {
		return nil, false
	}
	// Backward from there: the latest start gives the tightest match.
	j = len(q) - 1
	for i := end; i >= 0 && j >= 0; i-- {
		if p[i] >= 0x80 && p[i] < 0xC0 {
			continue // inside a multi-byte rune
		}
		r := []rune(p[i:min(len(p), i+4)])[0]
		if lower(r) == q[j] {
			pos[j] = i
			j--
		}
	}
	return pos, true
}

// matchASCII is fuzzyMatch for an ASCII query, comparing bytes: the bytes
// of multi-byte runes in p never equal an ASCII byte.
func matchASCII(p string, q []rune, fold bool, pos []int) ([]int, bool) {
	lower := func(c byte) byte {
		if fold && 'A' <= c && c <= 'Z' {
			return c + 'a' - 'A'
		}
		return c
	}
	// Forward: jump to each query byte (either case) with IndexByte.
	end := -1
	for j := range q {
		c := byte(q[j])
		k := strings.IndexByte(p[end+1:], c)
		if fold && 'a' <= c && c <= 'z' {
			lim := len(p) - end - 1
			if k >= 0 {
				lim = k
			}
			if u := strings.IndexByte(p[end+1:end+1+lim], c-'a'+'A'); u >= 0 {
				k = u
			}
		}
		if k < 0 {
			return nil, false
		}
		end += k + 1
	}
	j := len(q) - 1
	for i := end; j >= 0; i-- {
		if lower(p[i]) == byte(q[j]) {
			pos[j] = i
			j--
		}
	}
	return pos, true
}

// gitignore holds the simple patterns of a .gitignore: names and globs,
// optionally anchored with '/' or limited to folders with a trailing '/'.
type gitignore []ignoreRule

type ignoreRule struct {
	pattern  string
	anchored bool // contains a '/': matched against the whole path
	dirOnly  bool
}

func readIgnore(name string) gitignore {
	f, err := os.Open(name)
	if err != nil {
		return nil
	}
	defer f.Close()
	var g gitignore
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l == "" || l[0] == '#' || l[0] == '!' {
			continue
		}
		r := ignoreRule{}
		if strings.HasSuffix(l, "/") {
			r.dirOnly, l = true, strings.TrimSuffix(l, "/")
		}
		r.anchored = strings.Contains(l, "/")
		r.pattern = strings.TrimPrefix(l, "/")
		g = append(g, r)
	}
	return g
}

func (g gitignore) match(rel string, dir bool) bool {
	for _, r := range g {
		if r.dirOnly && !dir {
			continue
		}
		target := path.Base(rel)
		if r.anchored {
			target = rel
		}
		if ok, _ := path.Match(r.pattern, target); ok {
			return true
		}
	}
	return false
}
