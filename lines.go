package main

import (
	"bufio"
	"io"
	"math"
	"os"
	"runtime/debug"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
	"unsafe"
)

const (
	blockLines = 1024     // lines per block when indexing
	mmapMin    = 32 << 20 // files at least this big are memory-mapped
	asyncMin   = 8 << 20  // files at least this big are indexed in the background
	batchLines = 256 * blockLines
)

// A block is a run of lines. Fresh from disk it only stores line start
// offsets into the file contents; it turns into real strings once edited.
type block struct {
	base  int      // offset of the block in the file contents
	idx   []uint32 // line starts relative to base, plus the start of the next line
	lines []string // materialized lines, used once idx is nil
}

func (k *block) n() int {
	if k.idx != nil {
		return len(k.idx) - 1
	}
	return len(k.lines)
}

// lineStore keeps the document as blocks of lines, so edits only touch one
// small block and unedited lines cost 4 bytes each.
type lineStore struct {
	data   string // file contents (possibly memory-mapped); never modified
	crlf   bool
	blocks []*block
	first  []int // first[i] is the line number of the first line in blocks[i]
	count  int
	hint   int // block of the last lookup, for fast sequential access

	loading  chan []*block // pending blocks from the background indexer
	progress atomic.Int64  // bytes indexed so far
}

func (s *lineStore) init(data string) {
	*s = lineStore{data: data, crlf: strings.Contains(data[:min(len(data), 64<<10)], "\r\n")}
	if len(data) < asyncMin {
		indexLines(data, &s.progress, func(bs []*block) { s.appendBlocks(bs) })
		return
	}
	ch := make(chan []*block, 4)
	go func() {
		debug.SetPanicOnFault(true) // the mapped file may be truncated under us
		defer func() {
			recover()
			close(ch)
		}()
		indexLines(data, &s.progress, func(bs []*block) { ch <- bs })
	}()
	s.appendBlocks(<-ch) // show the first screen right away
	s.loading = ch
}

// maxSpan is the most file bytes one block may cover, since offsets are
// 32-bit. It is a variable so tests can make it small.
var maxSpan = math.MaxUint32

// indexLines finds line starts in data and emits them in batches of blocks.
func indexLines(data string, progress *atomic.Int64, emit func([]*block)) {
	newBlock := func(base int) *block { return &block{base: base, idx: make([]uint32, 1, blockLines+1)} }
	cur := newBlock(0)
	var batch []*block
	sent := 0
	for pos := 0; ; {
		k := strings.IndexByte(data[pos:], '\n')
		if k < 0 {
			break
		}
		pos += k + 1
		if pos+1-cur.base > maxSpan && len(cur.idx) > 1 {
			// End the block where the current line starts.
			batch = append(batch, cur)
			cur = newBlock(cur.base + int(cur.idx[len(cur.idx)-1]))
		}
		cur.idx = append(cur.idx, uint32(pos-cur.base))
		if len(cur.idx) == blockLines+1 {
			batch = append(batch, cur)
			cur = newBlock(pos)
			// A small first batch gets the first screen up quickly.
			if len(batch)*blockLines >= batchLines || (sent == 0 && len(batch) >= 4) {
				sent++
				progress.Store(int64(pos))
				emit(batch)
				batch = nil
			}
		}
	}
	// The sentinel puts the end of the last line at len(data).
	if len(data)+1-cur.base > maxSpan && len(cur.idx) > 1 {
		batch = append(batch, cur)
		cur = newBlock(cur.base + int(cur.idx[len(cur.idx)-1]))
	}
	cur.idx = append(cur.idx, uint32(len(data)+1-cur.base))
	batch = append(batch, cur)
	progress.Store(int64(len(data)))
	emit(batch)
}

// poll takes in blocks indexed in the background. It reports whether
// loading is still in progress.
func (s *lineStore) poll() bool {
	for s.loading != nil {
		select {
		case bs, ok := <-s.loading:
			if !ok {
				s.loading = nil
				return false
			}
			s.appendBlocks(bs)
		default:
			return true
		}
	}
	return false
}

func (s *lineStore) progressPct() int {
	return int(s.progress.Load() * 100 / int64(max(1, len(s.data))))
}

func (s *lineStore) appendBlocks(bs []*block) {
	from := len(s.blocks)
	s.blocks = append(s.blocks, bs...)
	s.recount(from)
}

// recount updates line numbers from block i onwards.
func (s *lineStore) recount(i int) {
	s.first = slices.Grow(s.first[:min(i, len(s.first))], len(s.blocks)-i)[:len(s.blocks)]
	for ; i < len(s.blocks); i++ {
		if i == 0 {
			s.first[0] = 0
		} else {
			s.first[i] = s.first[i-1] + s.blocks[i-1].n()
		}
	}
	last := len(s.blocks) - 1
	s.count = s.first[last] + s.blocks[last].n()
}

func (s *lineStore) locate(line int) (int, int) {
	if h := s.hint; h < len(s.blocks) && line >= s.first[h] && line < s.first[h]+s.blocks[h].n() {
		return h, line - s.first[h]
	}
	b := sort.Search(len(s.blocks), func(x int) bool { return s.first[x] > line }) - 1
	s.hint = b
	return b, line - s.first[b]
}

func (s *lineStore) raw(k *block, j int) string {
	l := s.data[k.base+int(k.idx[j]) : k.base+int(k.idx[j+1])-1]
	if s.crlf && strings.HasSuffix(l, "\r") {
		l = l[:len(l)-1]
	}
	return l
}

func (s *lineStore) line(i int) string {
	b, j := s.locate(i)
	if k := s.blocks[b]; k.idx != nil {
		return s.raw(k, j)
	}
	return s.blocks[b].lines[j]
}

func (s *lineStore) materialize(k *block) {
	if k.idx == nil {
		return
	}
	lines := make([]string, k.n())
	for j := range lines {
		lines[j] = s.raw(k, j)
	}
	k.idx, k.lines = nil, lines
}

// replace swaps lines first..last (inclusive) for repl.
func (s *lineStore) replace(first, last int, repl []string) {
	bi, j := s.locate(first)
	bj, jj := s.locate(last)
	s.materialize(s.blocks[bi])
	s.materialize(s.blocks[bj])
	if bi == bj && len(repl) == last-first+1 {
		copy(s.blocks[bi].lines[j:], repl) // common case: edit within lines
		return
	}
	head := s.blocks[bi].lines[:j]
	tail := s.blocks[bj].lines[jj+1:]
	merged := make([]string, 0, len(head)+len(repl)+len(tail))
	merged = append(append(append(merged, head...), repl...), tail...)
	var nb []*block
	for len(merged) > 2*blockLines {
		nb = append(nb, &block{lines: merged[:blockLines:blockLines]})
		merged = merged[blockLines:]
	}
	nb = append(nb, &block{lines: merged})
	s.blocks = slices.Replace(s.blocks, bi, bj+1, nb...)
	s.recount(bi)
	s.hint = bi
}

// readFile returns the contents of path, memory-mapping large files.
func readFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "", err
	}
	if fi.Size() >= mmapMin {
		if data, err := mapFile(f, int(fi.Size())); err == nil {
			return data, nil
		}
	}
	data, err := io.ReadAll(f)
	if err != nil || len(data) == 0 {
		return "", err
	}
	// data is never written again, so share it instead of copying it.
	return unsafe.String(&data[0], len(data)), nil
}

// writeTo writes the document with sep between lines. Unedited runs of
// blocks are written straight from the file contents, newlines included,
// in one call each instead of line by line.
func (s *lineStore) writeTo(f io.Writer, sep string) error {
	w := bufio.NewWriterSize(f, 1<<20)
	runStart, runEnd := -1, -1 // pending run of unedited file bytes
	flushRun := func() error {
		if runStart < 0 {
			return nil
		}
		if err := w.Flush(); err != nil {
			return err
		}
		_, err := io.WriteString(f, s.data[runStart:runEnd])
		runStart = -1
		return err
	}
	for bi, k := range s.blocks {
		lastBlock := bi == len(s.blocks)-1
		if k.idx != nil {
			// The block's lines with the newline after each, as in the file.
			start, end := k.base+int(k.idx[0]), k.base+int(k.idx[len(k.idx)-1])
			switch {
			case end > len(s.data):
				end = len(s.data) // the file's last line has no newline
			case lastBlock:
				// Lines after this block were deleted: drop its newline.
				end--
				if s.crlf && end > start && s.data[end-1] == '\r' {
					end--
				}
			}
			if start != runEnd {
				if err := flushRun(); err != nil {
					return err
				}
				runStart = start
			}
			runEnd = end
			continue
		}
		if err := flushRun(); err != nil {
			return err
		}
		for j, l := range k.lines {
			w.WriteString(l)
			if !lastBlock || j < len(k.lines)-1 {
				w.WriteString(sep)
			}
		}
	}
	if err := flushRun(); err != nil {
		return err
	}
	return w.Flush()
}
