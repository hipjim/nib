package main

import (
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

func TestParallelFor(t *testing.T) {
	for _, n := range []int{0, 1, 7, 4096, 100_000} {
		var hits atomic.Int64
		seen := make([]atomic.Int32, n)
		if !parallelFor(n, 1, func(lo, hi, w int) {
			for i := lo; i < hi; i++ {
				seen[i].Add(1)
				hits.Add(1)
			}
		}) {
			t.Fatal("reported a fault")
		}
		for i := range seen {
			if seen[i].Load() != 1 {
				t.Fatalf("n=%d: item %d visited %d times", n, i, seen[i].Load())
			}
		}
		if hits.Load() != int64(n) {
			t.Fatalf("n=%d: %d visits", n, hits.Load())
		}
	}
	// A bug in a worker is not hidden.
	defer func() {
		if r := recover(); r != "boom" {
			t.Fatalf("recovered %v, want the worker's panic", r)
		}
	}()
	parallelFor(100_000, 1, func(lo, hi, w int) {
		if lo == 0 {
			panic("boom")
		}
	})
}

func TestReadAll(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []int{0, 1, 4095, 4096, 4097, 1 << 20} {
		want := strings.Repeat("x", n)
		name := filepath.Join(dir, "f")
		os.WriteFile(name, []byte(want), 0o644)
		f, _ := os.Open(name)
		// A wrong size, as when the file changes between stat and read.
		for _, size := range []int{n, 0, n / 2, 2*n + 1} {
			f.Seek(0, 0)
			got, err := readAll(f, size)
			if err != nil || string(got) != want {
				t.Fatalf("n=%d size=%d: got %d bytes, err %v", n, size, len(got), err)
			}
		}
		f.Close()
	}
}

// TestCharMaskSound checks that the mask never rejects a path the matcher
// would accept.
func TestCharMaskSound(t *testing.T) {
	rng := rand.New(rand.NewPCG(5, 6))
	const alphabet = "abcXYZ019/._-éÉ ()"
	random := func(n int) string {
		var sb strings.Builder
		for range n {
			sb.WriteByte(alphabet[rng.IntN(len(alphabet))])
		}
		return sb.String()
	}
	for range 200_000 {
		p, q := random(1+rng.IntN(12)), random(1+rng.IntN(3))
		rq := []rune(q)
		fold := !slices.ContainsFunc(rq, func(r rune) bool { return 'A' <= r && r <= 'Z' || r == 'É' })
		if fold {
			q = strings.ToLower(q)
			rq = []rune(q)
		}
		if _, ok := fuzzyMatch(p, rq, fold, nil); ok && charMask(p)&charMask(q) != charMask(q) {
			t.Fatalf("mask rejects %q for %q (fold %v)", p, q, fold)
		}
	}
}

// TestRankMatchesBruteForce ranks enough paths to use every core and checks
// the result against scoring every path.
func TestRankMatchesBruteForce(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 8))
	parts := []string{"src", "pkg", "editor", "lines", "test", "go", "md", "internal", "x", "Buffer", "a-b", "c_d"}
	f := finder{recent: []string{"src/editor.go", "nowhere/else.go"}}
	for range 60_000 {
		var p []string
		for range 1 + rng.IntN(4) {
			p = append(p, parts[rng.IntN(len(parts))])
		}
		f.paths = append(f.paths, strings.Join(p, "/")+"."+parts[rng.IntN(len(parts))])
	}
	for _, query := range []string{"e", "ed", "edgo", "Buf", "zzz", "a-b"} {
		f.last, f.query = "", query
		f.rank()
		q := []rune(query)
		fold := !slices.ContainsFunc(q, func(r rune) bool { return 'A' <= r && r <= 'Z' })
		if fold {
			q = []rune(strings.ToLower(query))
		}
		var want []result
		isRecent := map[string]int{}
		for i, p := range f.recent {
			isRecent[p] = len(f.recent) - i
		}
		for _, p := range slices.Concat(f.paths, []string{"nowhere/else.go"}) {
			if s, ok := fuzzyScore(p, q, fold, nil); ok {
				want = append(want, result{p, s + 20*isRecent[p]})
			}
		}
		slices.SortStableFunc(want, func(a, b result) int {
			if better(a, b) {
				return -1
			}
			if better(b, a) {
				return 1
			}
			return 0
		})
		if f.matches != len(want) {
			t.Fatalf("%q: %d matches, want %d", query, f.matches, len(want))
		}
		want = want[:min(len(want), maxResults)]
		if len(f.results) != len(want) {
			t.Fatalf("%q: %d results, want %d", query, len(f.results), len(want))
		}
		for i := range want {
			if f.results[i].score != want[i].score || len(f.results[i].path) != len(want[i].path) {
				t.Fatalf("%q: result %d is %v, want %v", query, i, f.results[i], want[i])
			}
		}
		// Typing one more letter narrows the candidates to the same answer.
		if query == "ed" {
			f.query = "edg"
			f.rank()
			narrowed := slices.Clone(f.results)
			f.last = ""
			f.rank()
			if !slices.Equal(narrowed, f.results) {
				t.Fatalf("narrowed ranking %v differs from the full one %v", narrowed, f.results)
			}
		}
	}
}
