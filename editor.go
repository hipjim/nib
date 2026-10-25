package main

import (
	"fmt"
	"image"
	"image/color"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/clipboard"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/io/transfer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
)

var (
	colBg       = rgb(0x0b0d10)
	colCurLine  = rgb(0x12161c)
	colText     = rgb(0xc8d0da)
	colDim      = rgb(0x3a4250)
	colGutterHi = rgb(0x8b95a5)
	colAccent   = rgb(0x3ee6a8)
	colSel      = rgb(0x17463a)
	colMatch    = rgb(0x4a3f1c)
	colStatus   = rgb(0x07080a)
	colWarn     = rgb(0xff6b6b)
)

// Syntax colors, indexed by token class.
var tokColors = [...]color.NRGBA{
	tokPlain:   colText,
	tokComment: rgb(0x5f6b7c),
	tokString:  rgb(0x7fd4a6),
	tokNumber:  rgb(0xd9b56c),
	tokKeyword: rgb(0xf4f7fa),
	tokTag:     rgb(0x86a6cc),
	tokTime:    rgb(0x5f6b7c),
	tokError:   colWarn,
	tokWarn:    rgb(0xe8b84e),
}

func rgb(c uint32) color.NRGBA { return argb(0xff000000 | c) }
func argb(c uint32) color.NRGBA {
	return color.NRGBA{A: uint8(c >> 24), R: uint8(c >> 16), G: uint8(c >> 8), B: uint8(c)}
}

type promptKind int

const (
	promptNone promptKind = iota
	promptFind
	promptGoto
	promptOpen
	promptSaveAs
	promptFiles
)

var promptLabels = [...]string{"", "find", "goto line", "open", "save as", "file"}

const blinkPeriod = 530 * time.Millisecond

type Editor struct {
	*doc          // the current document
	docs   []*doc // open documents, most recent first
	root   string // folder the file finder searches
	finder finder
	plain  bool // syntax highlighting turned off
	info   bool // file properties panel shown

	followCursor bool
	bar          scrollbar
	watch        watcher

	quitArmed   bool // unsaved files: the next quit goes through
	Quit        func()
	sessionPath string
	sessionAt   time.Time
	sessionData []byte
	keep        []place // last session's documents that are not open

	shaper   *text.Shaper
	font     font.Font
	size     unit.Sp
	pxPerEm  int
	charW    float32
	lineH    int
	textOffY int

	viewW, viewH int // text area size from the last frame
	textX        int // left edge of the text area

	dragging   bool
	lastClick  time.Time
	clicks     int
	shiftTap   time.Time // when Shift was last tapped alone; zero if not
	shiftClean bool      // Shift is down and nothing else was pressed yet
	blinkStart time.Time
	focused    bool
	started    bool

	prompt     promptKind
	input      string
	findOrigin Pos
	findQuery  string
	notFound   bool

	msg        string
	msgWarn    bool
	discardOK  bool
	lineClip   string
	SetTitle   func(string)
	titleState string
}

func NewEditor(path string) *Editor {
	e := &Editor{
		shaper: text.NewShaper(text.NoSystemFonts(), text.WithCollection(fontCollection())),
		font:   font.Font{Typeface: typeface},
		size:   14,
	}
	e.root, _ = os.Getwd()
	if home, err := os.UserHomeDir(); err == nil && (e.root == "" || e.root == "/") {
		e.root = home // started from Finder or a launcher, not a shell
	}
	e.show(newDoc("", NewBuffer("")))
	if path != "" {
		e.open(path)
	}
	return e
}

func (e *Editor) save(path string) {
	if e.locked() {
		return
	}
	path = expandHome(path)
	if err := e.buf.Save(path); err != nil {
		e.flash(err.Error(), true)
		return
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if langFor(path) != e.hl.lang {
		e.hl = newHighlighter(path)
	}
	e.path, e.stamp, e.conflict = path, statStamp(path), false
	e.flash(fmt.Sprintf("wrote %d lines", e.buf.LineCount()), false)
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}

func (e *Editor) flash(s string, warn bool) { e.msg, e.msgWarn = s, warn }

// locked reports whether edits must wait for the file to finish loading.
func (e *Editor) locked() bool {
	if e.dir != nil {
		e.flash("folder listings are read-only", true)
		return true
	}
	if e.buf.Loading() {
		e.flash("still loading, editing is paused", true)
		return true
	}
	return false
}

// ---- selection and movement ----

func (e *Editor) sel() (Pos, Pos, bool) {
	s, t := order(e.anchor, e.cursor)
	return s, t, s != t
}

func (e *Editor) moveTo(p Pos, extend bool) {
	e.cursor = e.buf.Clamp(p)
	if !extend {
		e.anchor = e.cursor
	}
	e.goal = -1
	e.followCursor = true
}

func (e *Editor) left(p Pos) Pos {
	if p.Col > 0 {
		_, n := utf8.DecodeLastRuneInString(e.buf.Line(p.Line)[:p.Col])
		return Pos{p.Line, p.Col - n}
	}
	if p.Line > 0 {
		return Pos{p.Line - 1, len(e.buf.Line(p.Line - 1))}
	}
	return p
}

func (e *Editor) right(p Pos) Pos {
	line := e.buf.Line(p.Line)
	if p.Col < len(line) {
		_, n := utf8.DecodeRuneInString(line[p.Col:])
		return Pos{p.Line, p.Col + n}
	}
	if p.Line < e.buf.LineCount()-1 {
		return Pos{p.Line + 1, 0}
	}
	return p
}

func (e *Editor) wordLeft(p Pos) Pos {
	if p.Col == 0 {
		return e.left(p)
	}
	return Pos{p.Line, wordStart(e.buf.Line(p.Line), p.Col)}
}

func (e *Editor) wordRight(p Pos) Pos {
	line := e.buf.Line(p.Line)
	if p.Col == len(line) {
		return e.right(p)
	}
	return Pos{p.Line, wordEnd(line, p.Col)}
}

// vertical moves the cursor by n lines, keeping the goal column.
func (e *Editor) vertical(n int, extend bool) {
	if e.goal < 0 {
		e.goal = visCol(e.buf.Line(e.cursor.Line), e.cursor.Col)
	}
	goal := e.goal
	l := e.cursor.Line + n
	var p Pos
	switch {
	case l < 0:
		p = Pos{}
	case l >= e.buf.LineCount():
		p = e.buf.End()
	default:
		p = Pos{l, colAt(e.buf.Line(l), float32(goal))}
	}
	e.moveTo(p, extend)
	e.goal = goal
}

func (e *Editor) home(extend bool) {
	line := e.buf.Line(e.cursor.Line)
	ind := len(leadingSpace(line))
	col := ind
	if e.cursor.Col == ind {
		col = 0
	}
	e.moveTo(Pos{e.cursor.Line, col}, extend)
}

// ---- editing ----

func (e *Editor) insert(s string) {
	if e.locked() {
		return
	}
	a, b, _ := e.sel()
	p := e.buf.Replace(a, b, s, e.cursor)
	e.moveTo(p, false)
}

func (e *Editor) deleteTo(p Pos) {
	if e.locked() {
		return
	}
	if _, _, ok := e.sel(); ok {
		e.insert("")
		return
	}
	a, b := order(e.cursor, p)
	e.buf.Replace(a, b, "", e.cursor)
	e.moveTo(a, false)
}

func (e *Editor) indentUnit() string {
	if e.buf.UseTabs {
		return "\t"
	}
	return strings.Repeat(" ", tabWidth)
}

func (e *Editor) backspace(mods key.Modifiers) {
	c := e.cursor
	line := e.buf.Line(c.Line)
	switch {
	case mods.Contain(key.ModCommand):
		e.deleteTo(Pos{c.Line, 0})
	case mods.Contain(key.ModShortcutAlt):
		e.deleteTo(e.wordLeft(c))
	case !e.buf.UseTabs && c.Col > 0 && strings.TrimLeft(line[:c.Col], " ") == "":
		// Soft tab: delete back to the previous tab stop.
		e.deleteTo(Pos{c.Line, (c.Col - 1) / tabWidth * tabWidth})
	default:
		e.deleteTo(e.left(c))
	}
}

func (e *Editor) newline() {
	a, _, _ := e.sel()
	line := e.buf.Line(a.Line)
	indent := leadingSpace(line[:a.Col])
	before, after := "", ""
	if a.Col > 0 {
		before = line[a.Col-1 : a.Col]
	}
	_, b, _ := e.sel()
	if bl := e.buf.Line(b.Line); b.Col < len(bl) {
		after = bl[b.Col : b.Col+1]
	}
	if strings.Contains("{[(", before) && before != "" {
		inner := indent + e.indentUnit()
		if pairs := map[string]string{"{": "}", "[": "]", "(": ")"}; after == pairs[before] {
			e.insert("\n" + inner + "\n" + indent)
			e.moveTo(Pos{a.Line + 1, len(inner)}, false)
			return
		}
		indent = inner
	}
	e.insert("\n" + indent)
}

// lineSpan returns the first and last line touched by the selection.
func (e *Editor) lineSpan() (int, int) {
	a, b, ok := e.sel()
	if ok && b.Col == 0 && b.Line > a.Line {
		b.Line--
	}
	return a.Line, b.Line
}

// replaceLines swaps lines [first, last] for repl as a single undo step.
func (e *Editor) replaceLines(first, last int, repl []string) {
	start, end := Pos{first, 0}, Pos{last, len(e.buf.Line(last))}
	e.buf.Replace(start, end, strings.Join(repl, "\n"), e.cursor)
}

func (e *Editor) indent(dir int) {
	if e.locked() {
		return
	}
	first, last := e.lineSpan()
	unit := e.indentUnit()
	var out []string
	delta := map[int]int{}
	for i := first; i <= last; i++ {
		l := e.buf.Line(i)
		if dir > 0 {
			if l != "" {
				l = unit + l
				delta[i] = len(unit)
			}
		} else {
			n := 0
			if strings.HasPrefix(l, "\t") {
				n = 1
			} else {
				for n < tabWidth && n < len(l) && l[n] == ' ' {
					n++
				}
			}
			l = l[n:]
			delta[i] = -n
		}
		out = append(out, l)
	}
	e.replaceLines(first, last, out)
	shift := func(p Pos) Pos { return e.buf.Clamp(Pos{p.Line, max(0, p.Col+delta[p.Line])}) }
	e.anchor, e.cursor = shift(e.anchor), shift(e.cursor)
	e.followCursor = true
}

func (e *Editor) deleteLines() {
	if e.locked() {
		return
	}
	first, last := e.lineSpan()
	switch {
	case last < e.buf.LineCount()-1:
		e.buf.Replace(Pos{first, 0}, Pos{last + 1, 0}, "", e.cursor)
	case first > 0:
		e.buf.Replace(Pos{first - 1, len(e.buf.Line(first - 1))}, e.buf.End(), "", e.cursor)
		first--
	default:
		e.buf.Replace(Pos{}, e.buf.End(), "", e.cursor)
	}
	e.moveTo(Pos{first, 0}, false)
}

func (e *Editor) duplicateLines() {
	if e.locked() {
		return
	}
	first, last := e.lineSpan()
	block := e.buf.Text(Pos{first, 0}, Pos{last, len(e.buf.Line(last))})
	e.buf.Replace(Pos{last, len(e.buf.Line(last))}, Pos{last, len(e.buf.Line(last))}, "\n"+block, e.cursor)
	n := last - first + 1
	e.anchor.Line += n
	e.cursor.Line += n
	e.followCursor = true
}

// moveLines moves the selected lines up (dir<0) or down (dir>0).
func (e *Editor) moveLines(dir int) {
	if e.locked() {
		return
	}
	first, last := e.lineSpan()
	if (dir < 0 && first == 0) || (dir > 0 && last == e.buf.LineCount()-1) {
		return
	}
	var lines []string
	for i := first; i <= last; i++ {
		lines = append(lines, e.buf.Line(i))
	}
	if dir < 0 {
		e.replaceLines(first-1, last, append(lines, e.buf.Line(first-1)))
	} else {
		e.replaceLines(first, last+1, append([]string{e.buf.Line(last + 1)}, lines...))
	}
	e.anchor.Line += dir
	e.cursor.Line += dir
	e.followCursor = true
}

func (e *Editor) undo(redo bool) {
	if e.locked() {
		return
	}
	f := e.buf.Undo
	if redo {
		f = e.buf.Redo
	}
	if p, ok := f(); ok {
		e.moveTo(p, false)
	}
}

// selectNext selects the word under the cursor, or the next occurrence of
// the current selection.
func (e *Editor) selectNext() {
	a, b, ok := e.sel()
	if !ok || a.Line != b.Line {
		s, t := wordAt(e.buf.Line(e.cursor.Line), e.cursor.Col)
		e.anchor, e.cursor = Pos{e.cursor.Line, s}, Pos{e.cursor.Line, t}
		return
	}
	q := e.buf.Text(a, b)
	if p, ok := e.buf.Find(q, b, false); ok {
		e.anchor = p
		e.moveTo(endOf(p, q), true)
	}
}

func (e *Editor) copy(gtx layout.Context, cut bool) {
	a, b, ok := e.sel()
	var s string
	if ok {
		s = e.buf.Text(a, b)
		e.lineClip = ""
	} else {
		s = e.buf.Line(e.cursor.Line) + "\n"
		e.lineClip = s
	}
	gtx.Execute(clipboard.WriteCmd{Type: "application/text", Data: io.NopCloser(strings.NewReader(s))})
	if cut {
		if ok {
			e.insert("")
		} else {
			e.deleteLines()
		}
	}
}

func (e *Editor) paste(s string) {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if e.prompt != promptNone {
		e.setInput(e.input + strings.ReplaceAll(s, "\n", " "))
		return
	}
	if e.locked() {
		return
	}
	if _, _, ok := e.sel(); !ok && s == e.lineClip {
		// A whole line copied without a selection pastes above the cursor line.
		p := Pos{e.cursor.Line, 0}
		e.buf.Replace(p, p, s, e.cursor)
		e.moveTo(Pos{e.cursor.Line + strings.Count(s, "\n"), e.cursor.Col}, false)
		return
	}
	e.insert(s)
}

// ---- prompt (bottom command line) ----

func (e *Editor) openPrompt(k promptKind, initial string) {
	e.prompt, e.input, e.notFound = k, initial, false
	e.findOrigin = e.cursor
	if k == promptFind {
		e.findOrigin, _, _ = e.sel()
	}
}

func (e *Editor) setInput(s string) {
	e.input = s
	switch e.prompt {
	case promptFind:
		e.findQuery = s
		e.find(e.findOrigin, false)
	case promptFiles:
		e.finder.query, e.finder.sel = s, 0
		e.finder.rank()
	}
}

func (e *Editor) find(from Pos, backward bool) {
	q := e.findQuery
	if q == "" {
		e.notFound = false
		return
	}
	p, ok := e.buf.Find(q, from, backward)
	e.notFound = !ok
	if ok {
		e.anchor = p
		e.moveTo(endOf(p, q), true)
	}
}

func (e *Editor) findNext(backward bool) {
	a, b, _ := e.sel()
	if backward {
		e.find(a, true)
	} else {
		e.find(b, false)
	}
}

func (e *Editor) submitPrompt() {
	k, in := e.prompt, strings.TrimSpace(e.input)
	e.prompt = promptNone
	switch k {
	case promptFind:
		e.prompt = promptFind // keep finding with Enter
		e.findNext(false)
	case promptGoto:
		parts := strings.SplitN(in, ":", 2)
		l, err := strconv.Atoi(parts[0])
		if err != nil {
			e.flash("not a line number", true)
			return
		}
		col := 0
		if len(parts) == 2 {
			c, _ := strconv.Atoi(parts[1])
			col = colAt(e.buf.Line(e.buf.Clamp(Pos{l - 1, 0}).Line), float32(c-1))
		}
		e.moveTo(Pos{l - 1, col}, false)
	case promptOpen:
		if in != "" {
			e.open(in)
		}
	case promptSaveAs:
		if in != "" {
			e.save(in)
		}
	case promptFiles:
		if f := &e.finder; f.sel < len(f.results) {
			e.open(filepath.Join(f.root, filepath.FromSlash(f.results[f.sel].path)))
		}
	}
}

// dirPrefix is a folder path for the open prompt, ending in a separator.
func dirPrefix(wd string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(wd, home) {
		wd = "~" + wd[len(home):]
	}
	return wd + string(filepath.Separator)
}

// ---- input ----

const anyMods = key.ModShift | key.ModCtrl | key.ModAlt | key.ModCommand | key.ModSuper

var navKeys = []key.Name{
	key.NameLeftArrow, key.NameRightArrow, key.NameUpArrow, key.NameDownArrow,
	key.NameHome, key.NameEnd, key.NamePageUp, key.NamePageDown,
	key.NameDeleteBackward, key.NameDeleteForward, key.NameReturn, key.NameEnter,
	key.NameTab, key.NameEscape, key.NameF3,
}

var shortcutKeys = []key.Name{"A", "C", "X", "V", "Z", "Y", "S", "O", "F", "G", "D", "K", "L", "H", "P", "I", "W", "E", "Q", "=", "+", "-", "0"}

func (e *Editor) filters() []event.Filter {
	fs := []event.Filter{
		key.FocusFilter{Target: e},
		transfer.TargetFilter{Target: e, Type: "application/text"},
	}
	for _, n := range navKeys {
		fs = append(fs, key.Filter{Focus: e, Name: n, Optional: anyMods})
	}
	fs = append(fs, key.Filter{Focus: e, Name: key.NameShift, Optional: anyMods})
	for _, n := range shortcutKeys {
		fs = append(fs, key.Filter{Focus: e, Name: n, Required: key.ModShortcut, Optional: key.ModShift})
	}
	return fs
}

func (e *Editor) events(gtx layout.Context) {
	for {
		ev, ok := gtx.Event(e.filters()...)
		if !ok {
			break
		}
		e.blinkStart = gtx.Now
		switch ev := ev.(type) {
		case key.FocusEvent:
			if ev.Focus && !e.focused {
				e.checkDisk()
			}
			e.focused = ev.Focus
		case key.Event:
			if ev.Name == key.NameShift {
				e.shiftKey(gtx, ev)
				continue
			}
			if ev.State == key.Press {
				e.shiftClean, e.shiftTap = false, time.Time{}
				e.msg = ""
				discard := e.discardOK
				e.discardOK = false
				if ev.Name != "Q" {
					e.quitArmed = false
				}
				e.key(gtx, ev, discard)
			}
		case key.EditEvent:
			e.shiftClean, e.shiftTap = false, time.Time{}
			e.msg = ""
			if e.prompt != promptNone {
				e.setInput(e.input + ev.Text)
			} else {
				e.insert(ev.Text)
			}
		case transfer.DataEvent:
			if data, err := io.ReadAll(ev.Open()); err == nil {
				e.paste(string(data))
			}
		}
	}

	for {
		ev, ok := gtx.Event(pointer.Filter{
			Target:  &e.bar,
			Kinds:   pointer.Press | pointer.Drag | pointer.Release | pointer.Enter | pointer.Leave | pointer.Scroll,
			ScrollY: pointer.ScrollRange{Min: math.MinInt32, Max: math.MaxInt32},
		})
		if !ok {
			break
		}
		if pe, ok := ev.(pointer.Event); ok {
			e.barEvent(pe)
		}
	}

	for {
		ev, ok := gtx.Event(pointer.Filter{
			Target:  e,
			Kinds:   pointer.Press | pointer.Drag | pointer.Release | pointer.Scroll,
			ScrollX: pointer.ScrollRange{Min: math.MinInt32, Max: math.MaxInt32},
			ScrollY: pointer.ScrollRange{Min: math.MinInt32, Max: math.MaxInt32},
		})
		if !ok {
			break
		}
		if pe, ok := ev.(pointer.Event); ok {
			e.pointer(gtx, pe)
		}
	}
}

func (e *Editor) pointer(gtx layout.Context, ev pointer.Event) {
	switch ev.Kind {
	case pointer.Scroll:
		e.scrollY += float64(ev.Scroll.Y)
		e.scrollX += ev.Scroll.X
	case pointer.Press:
		if !ev.Buttons.Contain(pointer.ButtonPrimary) {
			return
		}
		gtx.Execute(key.FocusCmd{Tag: e})
		e.blinkStart = gtx.Now
		e.shiftClean, e.shiftTap = false, time.Time{} // Shift-click is not a tap
		p := e.posAt(ev.Position)
		if gtx.Now.Sub(e.lastClick) < 400*time.Millisecond && p.Line == e.cursor.Line {
			e.clicks++
		} else {
			e.clicks = 1
		}
		e.lastClick = gtx.Now
		switch {
		case e.clicks == 2 && e.dir != nil:
			e.activate(p.Line)
		case e.clicks == 2:
			s, t := wordAt(e.buf.Line(p.Line), p.Col)
			e.anchor, e.cursor = Pos{p.Line, s}, Pos{p.Line, t}
		case e.clicks >= 3:
			e.anchor = Pos{p.Line, 0}
			e.cursor = e.buf.Clamp(Pos{p.Line + 1, 0})
			if p.Line == e.buf.LineCount()-1 {
				e.cursor = e.buf.End()
			}
		default:
			e.moveTo(p, ev.Modifiers.Contain(key.ModShift))
			e.dragging = true
		}
	case pointer.Drag:
		if e.dragging {
			e.moveTo(e.posAt(ev.Position), true)
		}
	case pointer.Release:
		e.dragging = false
	}
}

func (e *Editor) posAt(pt f32.Point) Pos {
	l := int(math.Floor((float64(pt.Y) + e.scrollY) / float64(e.lineH)))
	l = max(0, min(l, e.buf.LineCount()-1))
	x := (pt.X - float32(e.textX) + e.scrollX) / e.charW
	return Pos{l, colAt(e.buf.Line(l), x)}
}

// shiftKey opens the file finder when Shift is tapped twice in a row with
// no other key, like Shift Shift in JetBrains IDEs, and closes it again.
func (e *Editor) shiftKey(gtx layout.Context, k key.Event) {
	if k.State == key.Press {
		e.shiftClean = true
		return
	}
	if !e.shiftClean {
		return
	}
	e.shiftClean = false
	if gtx.Now.Sub(e.shiftTap) > 400*time.Millisecond {
		e.shiftTap = gtx.Now
		return
	}
	e.shiftTap = time.Time{}
	e.msg = ""
	if e.prompt == promptFiles {
		e.prompt = promptNone
	} else {
		e.openFinder()
	}
}

func (e *Editor) key(gtx layout.Context, k key.Event, discardOK bool) {
	m := k.Modifiers
	shift := m.Contain(key.ModShift)

	if e.prompt != promptNone && e.promptKey(k) {
		return
	}

	if m.Contain(key.ModShortcut) && len(k.Name) == 1 {
		switch k.Name {
		case "A":
			e.anchor = Pos{}
			e.cursor = e.buf.End()
		case "C", "X":
			e.copy(gtx, k.Name == "X")
		case "V":
			gtx.Execute(clipboard.ReadCmd{Tag: e})
		case "Z":
			e.undo(shift)
		case "Y":
			e.undo(true)
		case "S":
			if e.path == "" || shift {
				e.openPrompt(promptSaveAs, orDefault(e.path, dirPrefix(e.root)))
			} else if e.conflict && !discardOK {
				e.flash("file changed on disk · ⌘S again to overwrite it", true)
				e.discardOK = true
			} else {
				e.save(e.path)
			}
		case "O":
			e.openPrompt(promptOpen, dirPrefix(e.root))
		case "P":
			e.openFinder()
		case "E":
			e.up()
		case "I":
			e.info = !e.info
		case "W":
			e.closeDoc(discardOK)
		case "Q":
			if e.Quit != nil {
				e.Quit()
			}
		case "F":
			q := e.findQuery
			if a, b, ok := e.sel(); ok && a.Line == b.Line {
				q = e.buf.Text(a, b)
			}
			e.openPrompt(promptFind, q)
			e.findQuery = q
		case "G":
			e.openPrompt(promptGoto, "")
		case "D":
			if shift {
				e.duplicateLines()
			} else {
				e.selectNext()
			}
		case "H":
			if shift {
				e.plain = !e.plain
				e.flash("highlighting "+map[bool]string{false: "on", true: "off"}[e.plain], false)
			}
		case "K":
			if shift {
				e.deleteLines()
			}
		case "L":
			a, _ := e.lineSpan()
			if _, _, ok := e.sel(); !ok {
				e.anchor = Pos{a, 0}
			}
			e.cursor = e.buf.Clamp(Pos{e.cursor.Line + 1, 0})
			if e.cursor.Line == e.buf.LineCount()-1 && e.cursor.Col == 0 && e.anchor.Line == e.cursor.Line {
				e.cursor = e.buf.End()
			}
		case "=", "+":
			e.size = min(e.size+1, 48)
		case "-":
			e.size = max(e.size-1, 8)
		case "0":
			e.size = 14
		}
		e.followCursor = true
		return
	}

	if e.dir != nil && m == 0 {
		switch k.Name {
		case key.NameReturn, key.NameEnter, key.NameRightArrow:
			e.activate(e.cursor.Line)
			return
		case key.NameLeftArrow, key.NameDeleteBackward:
			e.up()
			return
		}
	}

	switch k.Name {
	case key.NameLeftArrow, key.NameRightArrow:
		a, b, ok := e.sel()
		left := k.Name == key.NameLeftArrow
		switch {
		case m.Contain(key.ModCommand):
			if left {
				e.home(shift)
			} else {
				e.moveTo(Pos{e.cursor.Line, len(e.buf.Line(e.cursor.Line))}, shift)
			}
		case m.Contain(key.ModShortcutAlt):
			if left {
				e.moveTo(e.wordLeft(e.cursor), shift)
			} else {
				e.moveTo(e.wordRight(e.cursor), shift)
			}
		case ok && !shift:
			if left {
				e.moveTo(a, false)
			} else {
				e.moveTo(b, false)
			}
		case left:
			e.moveTo(e.left(e.cursor), shift)
		default:
			e.moveTo(e.right(e.cursor), shift)
		}
	case key.NameUpArrow, key.NameDownArrow:
		dir := 1
		if k.Name == key.NameUpArrow {
			dir = -1
		}
		switch {
		case m.Contain(key.ModAlt) && !m.Contain(key.ModCtrl):
			e.moveLines(dir)
		case m.Contain(key.ModCommand):
			if dir < 0 {
				e.moveTo(Pos{}, shift)
			} else {
				e.moveTo(e.buf.End(), shift)
			}
		default:
			e.vertical(dir, shift)
		}
	case key.NamePageUp, key.NamePageDown:
		rows := max(1, e.viewH/e.lineH-1)
		if k.Name == key.NamePageUp {
			rows = -rows
		}
		e.scrollY += float64(rows * e.lineH)
		e.vertical(rows, shift)
	case key.NameHome:
		if m.Contain(key.ModShortcut) {
			e.moveTo(Pos{}, shift)
		} else {
			e.home(shift)
		}
	case key.NameEnd:
		if m.Contain(key.ModShortcut) {
			e.moveTo(e.buf.End(), shift)
		} else {
			e.moveTo(Pos{e.cursor.Line, len(e.buf.Line(e.cursor.Line))}, shift)
		}
	case key.NameDeleteBackward:
		e.backspace(m)
	case key.NameDeleteForward:
		if m.Contain(key.ModShortcutAlt) {
			e.deleteTo(e.wordRight(e.cursor))
		} else {
			e.deleteTo(e.right(e.cursor))
		}
	case key.NameReturn, key.NameEnter:
		e.newline()
	case key.NameTab:
		a, b, _ := e.sel()
		switch {
		case shift:
			e.indent(-1)
		case a.Line != b.Line:
			e.indent(1)
		case e.buf.UseTabs:
			e.insert("\t")
		default:
			v := visCol(e.buf.Line(a.Line), a.Col)
			e.insert(strings.Repeat(" ", tabWidth-v%tabWidth))
		}
	case key.NameEscape:
		e.anchor = e.cursor
		e.info = false
	case key.NameF3:
		e.findNext(shift)
	}
}

// promptKey handles a key while the prompt is open. It returns false to let
// the editor handle the key instead.
func (e *Editor) promptKey(k key.Event) bool {
	switch k.Name {
	case key.NameEscape:
		e.prompt = promptNone
	case key.NameReturn, key.NameEnter:
		if e.prompt == promptFind && k.Modifiers.Contain(key.ModShift) {
			e.findNext(true)
		} else {
			e.submitPrompt()
		}
	case key.NameDeleteBackward:
		if k.Modifiers.Contain(key.ModShortcutAlt) || k.Modifiers.Contain(key.ModCommand) {
			e.setInput(e.input[:wordStart(e.input, len(e.input))])
		} else if e.input != "" {
			_, n := utf8.DecodeLastRuneInString(e.input)
			e.setInput(e.input[:len(e.input)-n])
		}
	case key.NameUpArrow, key.NameDownArrow, key.NameF3:
		if f := &e.finder; e.prompt == promptFiles {
			// The best match is at the bottom, next to the prompt.
			if k.Name == key.NameUpArrow {
				f.sel = min(f.sel+1, max(0, len(f.results)-1))
			} else {
				f.sel = max(f.sel-1, 0)
			}
			return true
		}
		if e.prompt != promptFind {
			return true
		}
		e.findNext(k.Name == key.NameUpArrow || k.Modifiers.Contain(key.ModShift))
	case "V":
		return false // the editor requests the clipboard; paste() fills the prompt
	case "F":
		if e.prompt == promptFind {
			e.findNext(false)
			return true
		}
		e.prompt = promptNone
		return false
	case "A", "C", "X", "Z", "Y", "S", "O", "G", "D", "K", "L", "H", "P", "I", "W", "E", "Q", "=", "+", "-", "0":
		// Any other shortcut closes the prompt and runs normally.
		e.prompt = promptNone
		return false
	case key.NameTab:
		if e.prompt == promptOpen || e.prompt == promptSaveAs {
			e.input = completePath(e.input)
		}
	default:
		return true
	}
	return true
}

func orDefault(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// completePath extends a path to the longest unambiguous match.
func completePath(in string) string {
	full := expandHome(in)
	dir, base := filepath.Split(full)
	if dir == "" {
		dir = "."
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return in
	}
	var match []string
	for _, ent := range ents {
		if strings.HasPrefix(ent.Name(), base) {
			n := ent.Name()
			if ent.IsDir() {
				n += string(filepath.Separator)
			}
			match = append(match, n)
		}
	}
	if len(match) == 0 {
		return in
	}
	pre := match[0]
	for _, m := range match[1:] {
		for !strings.HasPrefix(m, pre) {
			pre = pre[:len(pre)-1]
		}
	}
	return in + pre[len(base):]
}

// ---- layout ----

func (e *Editor) measure(gtx layout.Context) {
	px := gtx.Sp(e.size)
	if px == e.pxPerEm {
		return
	}
	e.pxPerEm = px
	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(1<<20, 1<<20)}
	const probe = "0123456789abcdefghijklmnopqrstuvwxyz"
	dims := widget.Label{MaxLines: 1}.Layout(g, e.shaper, e.font, e.size, probe, op.CallOp{})
	m.Stop()
	e.charW = float32(dims.Size.X) / float32(len(probe))
	e.lineH = int(math.Round(float64(px) * 1.55))
	e.textOffY = (e.lineH - dims.Size.Y) / 2
}

// WarmUp lays out a first screen offscreen, so the font is loaded and the
// visible text shaped while the window is still being created.
func (e *Editor) WarmUp() {
	var ops op.Ops
	e.Layout(layout.Context{
		Ops:         &ops,
		Now:         time.Now(),
		Metric:      unit.Metric{PxPerDp: 2, PxPerSp: 2}, // a guess; most screens are 2x
		Constraints: layout.Exact(image.Pt(2000, 1400)),
	})
	e.started, e.titleState = false, "" // the real first frame still does these
}

func (e *Editor) Layout(gtx layout.Context) (dims layout.Dimensions) {
	defer func() {
		// A memory-mapped file that is truncated on disk faults on access.
		if r := recover(); r != nil {
			if err, ok := r.(error); !ok || !strings.Contains(err.Error(), "fault") {
				panic(r)
			}
			e.buf = NewBuffer("")
			e.cursor, e.anchor = Pos{}, Pos{}
			e.flash("file changed on disk while open; reopen it", true)
			gtx.Execute(op.InvalidateCmd{})
		}
	}()
	if quitAsked.Swap(false) {
		e.askQuit()
	}
	if e.watch.hit.Swap(false) {
		e.checkDisk()
	}
	if e.buf.Poll() {
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(30 * time.Millisecond)})
	}
	if e.prompt == promptFiles {
		changed, walking := e.finder.poll()
		if changed {
			e.finder.rank()
		}
		if walking {
			gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(50 * time.Millisecond)})
		}
	}
	e.measure(gtx)
	if !e.started {
		e.started = true
		gtx.Execute(key.FocusCmd{Tag: e})
	}
	e.events(gtx)
	e.applyPending()
	e.cursor, e.anchor = e.buf.Clamp(e.cursor), e.buf.Clamp(e.anchor)
	e.updateTitle()

	size := gtx.Constraints.Max
	statusH := e.lineH + gtx.Dp(6)
	textH := max(0, size.Y-statusH)
	digits := max(3, len(strconv.Itoa(e.buf.LineCount())))
	gutterW := int(float32(digits+3) * e.charW)
	e.textX = gutterW + gtx.Dp(8)
	e.viewW, e.viewH = size.X-e.textX, textH

	e.scrollIntoView()

	fill(gtx, image.Rect(0, 0, size.X, size.Y), colBg)

	// Text area with input handling.
	area := clip.Rect(image.Rect(0, 0, size.X, textH)).Push(gtx.Ops)
	event.Op(gtx.Ops, e)
	pointer.CursorText.Add(gtx.Ops)
	key.InputHintOp{Tag: e, Hint: key.HintText}.Add(gtx.Ops)
	e.drawText(gtx, size.X, textH, gutterW)
	area.Pop()
	e.drawScrollbar(gtx, image.Rect(size.X-gtx.Dp(12), 0, size.X, textH))

	if e.prompt == promptFiles {
		e.drawFinder(gtx, size.X, textH)
	} else if e.info {
		e.drawInfo(gtx, size.X, textH)
	}

	e.drawStatus(gtx, image.Rect(0, textH, size.X, size.Y))

	quitOK.Store(e.unsaved() == 0 || e.quitArmed)
	e.watch.sync(e.docs)
	if e.saveSession(false) {
		gtx.Execute(op.InvalidateCmd{At: e.sessionAt.Add(time.Second)})
	}
	return layout.Dimensions{Size: size}
}

func (e *Editor) scrollIntoView() {
	lh := float64(e.lineH)
	maxY := e.maxScroll()
	if e.followCursor {
		e.followCursor = false
		cy := float64(e.cursor.Line) * lh
		margin := min(lh*3, float64(e.viewH)/3)
		if cy < e.scrollY+margin {
			e.scrollY = cy - margin
		}
		if cy+lh > e.scrollY+float64(e.viewH)-margin {
			e.scrollY = cy + lh - float64(e.viewH) + margin
		}
		cx := float32(visCol(e.buf.Line(e.cursor.Line), e.cursor.Col)) * e.charW
		mx := e.charW * 8
		if cx < e.scrollX+mx {
			e.scrollX = cx - mx
		}
		if cx > e.scrollX+float32(e.viewW)-mx {
			e.scrollX = cx - float32(e.viewW) + mx
		}
	}
	e.scrollY = max(0, min(e.scrollY, maxY))
	e.scrollX = max(0, e.scrollX)
}

func (e *Editor) drawText(gtx layout.Context, width, height, gutterW int) {
	lh := e.lineH
	sy := int(e.scrollY)
	first := sy / lh
	last := min(e.buf.LineCount()-1, (sy+height)/lh)
	selA, selB, hasSel := e.sel()

	// Current line highlight.
	if !hasSel {
		y := e.cursor.Line*lh - sy
		fill(gtx, image.Rect(0, y, width, y+lh), colCurLine)
	}

	xOf := func(v int) int { return e.textX + int(math.Round(float64(float32(v)*e.charW-e.scrollX))) }

	// Gutter: line numbers.
	for i := first; i <= last; i++ {
		y := i*lh - sy
		n := strconv.Itoa(i + 1)
		c := colDim
		if i == e.cursor.Line {
			c = colGutterHi
		}
		x := gutterW - int(float32(len(n)+1)*e.charW)
		e.label(gtx, x, y, n, c)
	}

	body := clip.Rect(image.Rect(gutterW, 0, width, height)).Push(gtx.Ops)
	defer body.Pop()

	v0 := int(e.scrollX / e.charW)
	v1 := v0 + int(float32(e.viewW)/e.charW) + 2

	q := ""
	if e.prompt == promptFind && !strings.Contains(e.findQuery, "\n") {
		q = e.findQuery
	}
	var index func(string) int
	if q != "" {
		index = matcher(q)
	}

	if l, ok := e.buf.TakeEdited(); ok {
		e.hl.edited(l)
	}
	lang := e.hl.lang
	if e.plain {
		lang = nil
	}
	st := 0
	if lang != nil {
		st = e.hl.stateAt(e.buf, first)
	}

	for i := first; i <= last; i++ {
		y := i*lh - sy
		line := e.buf.Line(i)

		if q != "" {
			for off := 0; ; {
				j := index(line[off:])
				if j < 0 {
					break
				}
				off += j
				x0, x1 := xOf(visCol(line, off)), xOf(visCol(line, off+len(q)))
				fill(gtx, image.Rect(x0, y, x1, y+lh), colMatch)
				off += len(q)
			}
		}

		if hasSel && i >= selA.Line && i <= selB.Line {
			c0, c1 := 0, visCol(line, len(line))+1
			if i == selA.Line {
				c0 = visCol(line, selA.Col)
			}
			if i == selB.Line {
				c1 = visCol(line, selB.Col)
			}
			fill(gtx, image.Rect(xOf(c0), y, xOf(c1), y+lh), colSel)
		}

		if lang != nil {
			e.hl.spans, st = lang.scan(line, st, e.hl.spans[:0])
			e.drawSpans(gtx, xOf, y, line, v0, v1, e.hl.spans)
		} else if s := displaySlice(line, v0, v1); s != "" {
			e.label(gtx, xOf(v0), y, s, colText)
		}
	}

	// Cursor.
	if e.focused && e.prompt == promptNone {
		since := gtx.Now.Sub(e.blinkStart)
		if since < 10*time.Second {
			phase := since / blinkPeriod
			gtx.Execute(op.InvalidateCmd{At: e.blinkStart.Add((phase + 1) * blinkPeriod)})
		}
		if since >= 10*time.Second || (since/blinkPeriod)%2 == 0 {
			x := xOf(visCol(e.buf.Line(e.cursor.Line), e.cursor.Col))
			y := e.cursor.Line*lh - sy
			fill(gtx, image.Rect(x, y+gtx.Dp(2), x+gtx.Dp(2), y+lh-gtx.Dp(2)), colAccent)
		}
	}
}

// drawSpans draws the part of line between visual columns v0 and v1, one
// label per run of the same color. Spaces join the run before them.
func (e *Editor) drawSpans(gtx layout.Context, xOf func(int) int, y int, line string, v0, v1 int, spans []span) {
	var sb strings.Builder
	runV, runCls := 0, tokPlain
	flush := func() {
		if sb.Len() > 0 {
			e.labelFont(gtx, xOf(runV), y, sb.String(), tokColors[runCls], runCls == tokKeyword)
			sb.Reset()
		}
	}
	si, v := 0, 0
	for i, r := range line {
		if v >= v1 {
			break
		}
		w := runeWidth(r, v)
		if v+w > v0 {
			if r == ' ' || r == '\t' {
				if sb.Len() > 0 {
					sb.WriteString(strings.Repeat(" ", min(v+w, v1)-max(v, v0)))
				}
			} else {
				for si < len(spans) && spans[si].end <= i {
					si++
				}
				c := tokPlain
				if si < len(spans) && spans[si].start <= i {
					c = spans[si].cls
				}
				if c != runCls {
					flush()
				}
				if sb.Len() == 0 {
					runV, runCls = v, c
				}
				sb.WriteRune(r)
			}
		}
		v += w
	}
	flush()
}

func (e *Editor) drawStatus(gtx layout.Context, r image.Rectangle) {
	fill(gtx, r, colStatus)
	fill(gtx, image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+1), colCurLine)
	y := r.Min.Y + (r.Dy()-e.lineH)/2
	pad := gtx.Dp(12)
	cw := e.charW

	// Right side: position info.
	ind := fmt.Sprintf("spaces:%d", tabWidth)
	if e.buf.UseTabs {
		ind = "tabs"
	}
	eol := "LF"
	if e.buf.CRLF {
		eol = "CRLF"
	}
	right := fmt.Sprintf("%d:%d  %s  %s  %d lines", e.cursor.Line+1,
		visCol(e.buf.Line(e.cursor.Line), e.cursor.Col)+1, ind, eol, e.buf.LineCount())
	if e.dir != nil {
		right = fmt.Sprintf("%d items", e.dir.count)
	}
	if e.buf.Loading() {
		right = fmt.Sprintf("loading %d%%  ", e.buf.LoadPct()) + right
	}
	if f := &e.finder; e.prompt == promptFiles {
		right = fmt.Sprintf("%s/%s files", groupDigits(int64(f.matches)), groupDigits(int64(len(f.paths))))
		if f.walking != nil {
			right = "scanning…  " + right
		}
	}
	if a, b, ok := e.sel(); ok && e.prompt != promptFiles {
		if a.Line == b.Line {
			right = fmt.Sprintf("(%d sel)  ", utf8.RuneCountInString(e.buf.Text(a, b))) + right
		} else {
			right = fmt.Sprintf("(%d lines sel)  ", b.Line-a.Line+1) + right
		}
	}
	rx := r.Max.X - pad - int(float32(utf8.RuneCountInString(right))*cw)
	e.label(gtx, rx, y, right, colDim)

	x := r.Min.X + pad
	if e.prompt != promptNone {
		lbl := promptLabels[e.prompt] + " ›"
		c := colAccent
		if e.notFound {
			c = colWarn
		}
		e.label(gtx, x, y, lbl, c)
		x += int(float32(utf8.RuneCountInString(lbl)+1) * cw)
		e.label(gtx, x, y, e.input, colText)
		cx := x + int(float32(utf8.RuneCountInString(e.input))*cw)
		fill(gtx, image.Rect(cx, y+gtx.Dp(2), cx+int(cw), y+e.lineH-gtx.Dp(2)), colAccent)
		return
	}

	const appName = "quod"
	e.label(gtx, x, y, appName, colAccent)
	x += int(float32(len(appName)+1) * cw)
	name := "[scratch]"
	if e.path != "" {
		name = shortPath(e.path)
	}
	if e.dir != nil {
		name = dirName(e.path)
	}
	if e.buf.Dirty() {
		name += " ●"
	}
	if n := e.otherUnsaved(); n > 0 {
		name += fmt.Sprintf("  +%d unsaved", n)
	}
	// Keep the left side clear of the position info on the right.
	room := int(float32(rx-x)/cw) - 3
	name = truncLeft(name, room)
	e.label(gtx, x, y, name, colText)
	x += int(float32(utf8.RuneCountInString(name)+3) * cw)
	if e.msg != "" && utf8.RuneCountInString(e.msg) <= room-utf8.RuneCountInString(name)-3 {
		c := colDim
		if e.msgWarn {
			c = colWarn
		}
		e.label(gtx, x, y, e.msg, c)
	}
}

func truncLeft(s string, n int) string {
	r := []rune(s)
	if n <= 1 || len(r) <= n {
		return s
	}
	return "…" + string(r[len(r)-n+1:])
}

func shortPath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home) {
		p = "~" + p[len(home):]
	}
	return p
}

func (e *Editor) updateTitle() {
	t := "quod"
	if e.path != "" {
		t = filepath.Base(e.path) + " — quod"
	}
	if e.buf.Dirty() {
		t = "● " + t
	}
	if t != e.titleState && e.SetTitle != nil {
		e.titleState = t
		e.SetTitle(t)
	}
}

func (e *Editor) label(gtx layout.Context, x, y int, s string, c color.NRGBA) {
	e.labelFont(gtx, x, y, s, c, false)
}

func (e *Editor) labelFont(gtx layout.Context, x, y int, s string, c color.NRGBA, bold bool) {
	f := e.font
	if bold {
		f.Weight = font.Bold
	}
	defer op.Offset(image.Pt(x, y+e.textOffY)).Push(gtx.Ops).Pop()
	m := op.Record(gtx.Ops)
	paint.ColorOp{Color: c}.Add(gtx.Ops)
	mat := m.Stop()
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(1<<20, e.lineH)}
	widget.Label{MaxLines: 1}.Layout(g, e.shaper, f, e.size, s, mat)
}

func fill(gtx layout.Context, r image.Rectangle, c color.NRGBA) {
	paint.FillShape(gtx.Ops, c, clip.Rect(r).Op())
}
