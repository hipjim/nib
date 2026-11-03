package main

import (
	"bufio"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
)

// TestGif records the README demo: the editor is driven through a short
// storyboard offscreen and every frame goes into an animated GIF. Set
// NIB_GIF=docs/demo.gif to record it. The demo runs in ~/nib-demo, a copy
// of this folder's files next to a generated 1 GB server log, which is
// removed afterwards unless NIB_KEEP is set.
func TestGif(t *testing.T) {
	out := os.Getenv("NIB_GIF")
	if out == "" {
		t.Skip("set NIB_GIF to record the demo")
	}
	out, _ = filepath.Abs(out)
	const w, h = 1200, 760
	win, err := headless.NewWindow(w, h)
	if err != nil {
		t.Skip("no GPU:", err)
	}
	defer win.Release()

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	demo := filepath.Join(home, "nib-demo")
	if _, err := os.Stat(demo); err == nil && os.Getenv("NIB_KEEP") == "" {
		t.Fatalf("%s exists; remove it or set NIB_KEEP to reuse it", demo)
	}
	if os.Getenv("NIB_KEEP") == "" {
		defer os.RemoveAll(demo)
	}
	src := filepath.Join(demo, "nib")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(".")
	for _, en := range entries {
		n := en.Name()
		if en.IsDir() || !(strings.HasSuffix(n, ".go") || n == "go.mod" || n == "go.sum" || n == "README.md" || n == "LICENSE") {
			continue
		}
		data, err := os.ReadFile(n)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(src, n), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	logPath := filepath.Join(demo, "server.log")
	if _, err := os.Stat(logPath); err != nil {
		if err := genLog(logPath, 1<<30); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(src)

	e := NewEditor("editor.go")
	e.focused = true
	e.cursor, e.anchor = Pos{90, 1}, Pos{90, 1}
	e.followCursor = true
	var ops op.Ops
	clock := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	e.blinkStart = clock
	gtx := layout.Context{
		Ops:         &ops,
		Now:         clock,
		Metric:      unit.Metric{PxPerDp: 1.5, PxPerSp: 1.5},
		Constraints: layout.Exact(image.Pt(w, h)),
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	rec := &recorder{}
	rnd := rand.New(rand.NewPCG(7, 11))

	shot := func() {
		ops.Reset()
		gtx.Now = clock
		e.Layout(gtx)
		if err := win.Frame(&ops); err != nil {
			t.Fatal(err)
		}
		if err := win.Screenshot(img); err != nil {
			t.Fatal(err)
		}
		rec.add(img, clock)
		if dir := os.Getenv("NIB_FRAMES"); dir != "" {
			os.MkdirAll(dir, 0o755)
			f, _ := os.Create(filepath.Join(dir, fmt.Sprintf("%03d.png", rec.shots)))
			png.Encode(f, img)
			f.Close()
		}
		rec.shots++
	}
	wait := func(d time.Duration) {
		end := clock.Add(d)
		for {
			k := clock.Sub(e.blinkStart)/blinkPeriod + 1
			next := e.blinkStart.Add(k * blinkPeriod)
			if !next.Before(end) {
				break
			}
			clock = next
			shot()
		}
		clock = end
	}
	act := func(f func()) {
		e.msg = ""
		f()
		e.blinkStart = clock
		shot()
	}
	typing := func(s string) {
		for _, r := range s {
			act(func() { e.setInput(e.input + string(r)) })
			wait(time.Duration(90+rnd.IntN(60)) * time.Millisecond)
		}
	}
	mark := func(s string) { t.Logf("%s at shot %d", s, rec.shots) }
	settle := func(running func() bool) {
		for running() {
			time.Sleep(time.Millisecond)
		}
		shot()
	}

	shot()
	wait(1400 * time.Millisecond)

	act(e.openFinder)
	settle(func() bool { _, walking := e.finder.poll(); e.finder.rank(); return walking })
	wait(700 * time.Millisecond)
	typing("hlgo")
	wait(800 * time.Millisecond)
	act(e.submitPrompt)
	wait(1200 * time.Millisecond)

	act(func() { e.openPrompt(promptFind, "") })
	wait(300 * time.Millisecond)
	typing("comment")
	wait(900 * time.Millisecond)
	for range 3 {
		act(e.submitPrompt)
		wait(450 * time.Millisecond)
	}
	wait(400 * time.Millisecond)
	act(func() { e.prompt = promptNone; e.anchor = e.cursor })
	wait(600 * time.Millisecond)

	act(e.openGrep)
	wait(300 * time.Millisecond)
	typing("parallelFor")
	settle(func() bool { _, running := e.grep.poll(); return running })
	wait(1600 * time.Millisecond)
	act(e.submitPrompt)
	wait(1300 * time.Millisecond)

	mark("open the log")
	act(func() { e.open(logPath) })
	for e.buf.Loading() {
		t0 := time.Now()
		time.Sleep(40 * time.Millisecond)
		clock = clock.Add(time.Since(t0))
		shot()
	}
	mark("loaded")
	wait(1600 * time.Millisecond)
	act(func() { e.cursor = e.buf.End(); e.anchor = e.cursor; e.followCursor = true })
	wait(1200 * time.Millisecond)
	mark("find in the log")
	act(func() { e.findQuery = ""; e.openPrompt(promptFind, "") })
	wait(300 * time.Millisecond)
	typing("ERROR")
	wait(1500 * time.Millisecond)
	act(func() { e.prompt = promptNone; e.anchor = e.cursor; e.findQuery = "" })
	wait(400 * time.Millisecond)
	act(e.openReplace)
	wait(300 * time.Millisecond)
	typing("WARN")
	wait(500 * time.Millisecond)
	act(e.submitPrompt)
	if e.prompt != promptWith {
		t.Fatalf("after the replace query the prompt is %v", e.prompt)
	}
	wait(300 * time.Millisecond)
	typing("WARNING")
	wait(600 * time.Millisecond)
	mark("replace all")
	act(e.replaceAll)
	wait(2000 * time.Millisecond)

	mark("switcher")
	act(e.openFinder)
	wait(1200 * time.Millisecond)
	for range 2 {
		act(func() { e.finder.sel++ })
		wait(350 * time.Millisecond)
	}
	wait(500 * time.Millisecond)
	act(e.submitPrompt)
	wait(1500 * time.Millisecond)
	rec.end(clock)

	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := gif.EncodeAll(f, rec.gif()); err != nil {
		t.Fatal(err)
	}
	fi, _ := f.Stat()
	area := 0
	for _, fr := range rec.frames {
		area += fr.r.Dx() * fr.r.Dy()
	}
	t.Logf("%d frames, %.1f s, %.1f MB, %.1f full frames of pixels", len(rec.frames), rec.seconds(), float64(fi.Size())/1e6, float64(area)/float64(w*h))
}

// TestGenLog writes a synthetic log of NIB_GEN_MB megabytes (default 64)
// to NIB_GEN, for the benchmarks to open as NIB_FILE where no real big
// file is at hand, as in CI.
func TestGenLog(t *testing.T) {
	out := os.Getenv("NIB_GEN")
	if out == "" {
		t.Skip("set NIB_GEN to write a log")
	}
	mb := 64
	if s := os.Getenv("NIB_GEN_MB"); s != "" {
		var err error
		if mb, err = strconv.Atoi(s); err != nil {
			t.Fatal(err)
		}
	}
	if err := genLog(out, int64(mb)<<20); err != nil {
		t.Fatal(err)
	}
}

func genLog(path string, size int64) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	bw := bufio.NewWriterSize(f, 1<<20)
	rnd := rand.New(rand.NewPCG(1, 2))
	routes := []string{"/api/users", "/api/orders", "/api/cart", "/api/search", "/healthz", "/api/sessions", "/static/app.js", "/api/payments"}
	hosts := []string{"web-01", "web-02", "web-03", "api-01", "api-02", "worker-01"}
	ts := time.Date(2026, 10, 31, 22, 14, 3, 0, time.UTC)
	var n int64
	for n < size {
		ts = ts.Add(time.Duration(rnd.IntN(9)) * time.Millisecond)
		host := hosts[rnd.IntN(len(hosts))]
		route := routes[rnd.IntN(len(routes))]
		var line string
		switch r := rnd.IntN(1000); {
		case r < 4:
			line = fmt.Sprintf("%s ERROR %s %s %d upstream timeout after %dms\n", ts.Format("2006-01-02T15:04:05.000Z"), host, route, 500+rnd.IntN(4), 2000+rnd.IntN(3000))
		case r < 50:
			line = fmt.Sprintf("%s WARN  %s %s slow query %dms\n", ts.Format("2006-01-02T15:04:05.000Z"), host, route, 300+rnd.IntN(900))
		case r < 140:
			line = fmt.Sprintf("%s DEBUG %s cache hit %s/%d\n", ts.Format("2006-01-02T15:04:05.000Z"), host, route, rnd.IntN(90000))
		default:
			line = fmt.Sprintf("%s INFO  %s GET %s/%d 200 %dms\n", ts.Format("2006-01-02T15:04:05.000Z"), host, route, rnd.IntN(90000), 1+rnd.IntN(40))
		}
		bw.WriteString(line)
		n += int64(len(line))
	}
	return bw.Flush()
}

// A recorder keeps the changed part of each frame and the time it was
// shown, and writes them out as a GIF with one palette built from the
// colors that appear most.
type recorder struct {
	w, h   int
	prev   *image.RGBA
	frames []rawFrame
	hist   map[uint32]int
	last   time.Time
	shots  int
}

type rawFrame struct {
	r   image.Rectangle
	pix []uint32 // 0xRRGGBB, row-major within r
	at  time.Time
}

func (rc *recorder) add(img *image.RGBA, at time.Time) {
	if rc.prev == nil {
		rc.w, rc.h = img.Rect.Dx(), img.Rect.Dy()
		rc.hist = map[uint32]int{}
	}
	r := image.Rect(0, 0, rc.w, rc.h)
	if rc.prev != nil {
		r = diffRect(rc.prev, img)
		if r.Empty() {
			return
		}
	}
	fr := rawFrame{r: r, pix: make([]uint32, 0, r.Dx()*r.Dy()), at: at}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		row := img.Pix[y*img.Stride+r.Min.X*4 : y*img.Stride+r.Max.X*4]
		for x := 0; x < len(row); x += 4 {
			c := uint32(row[x])<<16 | uint32(row[x+1])<<8 | uint32(row[x+2])
			fr.pix = append(fr.pix, c)
			rc.hist[c]++
		}
	}
	rc.frames = append(rc.frames, fr)
	if rc.prev == nil {
		rc.prev = image.NewRGBA(img.Rect)
	}
	copy(rc.prev.Pix, img.Pix)
}

func (rc *recorder) end(at time.Time) { rc.last = at }

func (rc *recorder) seconds() float64 {
	return rc.last.Sub(rc.frames[0].at).Seconds()
}

func diffRect(a, b *image.RGBA) image.Rectangle {
	w, h := a.Rect.Dx(), a.Rect.Dy()
	r := image.Rectangle{Min: image.Pt(w, h)} // image.Rect would flip it
	for y := 0; y < h; y++ {
		ra, rb := a.Pix[y*a.Stride:y*a.Stride+w*4], b.Pix[y*b.Stride:y*b.Stride+w*4]
		x0, x1 := -1, -1
		for x := 0; x < w*4; x += 4 {
			if ra[x] != rb[x] || ra[x+1] != rb[x+1] || ra[x+2] != rb[x+2] {
				if x0 < 0 {
					x0 = x / 4
				}
				x1 = x/4 + 1
			}
		}
		if x0 >= 0 {
			r.Min.X, r.Max.X = min(r.Min.X, x0), max(r.Max.X, x1)
			r.Min.Y, r.Max.Y = min(r.Min.Y, y), max(r.Max.Y, y+1)
		}
	}
	if r.Min.X >= r.Max.X {
		return image.Rectangle{}
	}
	return r
}

// A palette of 256 colors by median cut over the distinct colors seen, so
// that rare ones like the red of an error keep a slot of their own.
func medianCut(hist map[uint32]int) color.Palette {
	type cc struct {
		c uint32
		n int
	}
	chans := [3]func(uint32) int{
		func(c uint32) int { return int(c >> 16) },
		func(c uint32) int { return int(c >> 8 & 255) },
		func(c uint32) int { return int(c & 255) },
	}
	all := make([]cc, 0, len(hist))
	for c, n := range hist {
		all = append(all, cc{c, n})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].c < all[j].c })
	boxes := [][]cc{all}
	for len(boxes) < 256 {
		bi, bch, best := -1, 0, 0
		for i, b := range boxes {
			if len(b) < 2 {
				continue
			}
			for ch, f := range chans {
				lo, hi := 255, 0
				for _, x := range b {
					lo, hi = min(lo, f(x.c)), max(hi, f(x.c))
				}
				if hi-lo > best {
					bi, bch, best = i, ch, hi-lo
				}
			}
		}
		if bi < 0 {
			break
		}
		b, f := boxes[bi], chans[bch]
		sort.SliceStable(b, func(i, j int) bool { return f(b[i].c) < f(b[j].c) })
		mid := len(b) / 2
		boxes[bi], boxes = b[:mid:mid], append(boxes, b[mid:])
	}
	pal := make(color.Palette, 0, len(boxes))
	for _, b := range boxes {
		var r, g, bl, n int
		for _, x := range b {
			r += int(x.c>>16) * x.n
			g += int(x.c>>8&255) * x.n
			bl += int(x.c&255) * x.n
			n += x.n
		}
		pal = append(pal, color.NRGBA{uint8(r / n), uint8(g / n), uint8(bl / n), 255})
	}
	return pal
}

func (rc *recorder) gif() *gif.GIF {
	pal := medianCut(rc.hist)
	index := map[uint32]uint8{}
	nearest := func(c uint32) uint8 {
		if i, ok := index[c]; ok {
			return i
		}
		r, g, b := int(c>>16), int(c>>8&255), int(c&255)
		best, bd := 0, 1<<30
		for i, p := range pal {
			q := p.(color.NRGBA)
			d := (r-int(q.R))*(r-int(q.R)) + (g-int(q.G))*(g-int(q.G)) + (b-int(q.B))*(b-int(q.B))
			if d < bd {
				best, bd = i, d
			}
		}
		index[c] = uint8(best)
		return uint8(best)
	}

	g := &gif.GIF{LoopCount: 0, Config: image.Config{ColorModel: pal, Width: rc.w, Height: rc.h}}
	for i, fr := range rc.frames {
		p := image.NewPaletted(fr.r, pal)
		for j, c := range fr.pix {
			p.Pix[j] = nearest(c)
		}
		next := rc.last
		if i+1 < len(rc.frames) {
			next = rc.frames[i+1].at
		}
		g.Image = append(g.Image, p)
		g.Delay = append(g.Delay, max(2, int(next.Sub(fr.at)/(10*time.Millisecond))))
		g.Disposal = append(g.Disposal, gif.DisposalNone)
	}
	return g
}
