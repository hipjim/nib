package main

import (
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// Token classes. Plain text has no span.
const (
	tokPlain uint8 = iota
	tokComment
	tokString
	tokNumber
	tokKeyword
	tokTag
	tokTime
	tokError
	tokWarn
)

// A span colors line[start:end].
type span struct {
	start, end int
	cls        uint8
}

// A lang describes just enough of a language to color comments, strings,
// numbers and keywords with a single pass over each line.
type lang struct {
	lineComments []string
	blocks       [][2]string // block comments, which may span lines
	multi        []string    // string delimiters that may span lines, longest first
	quotes       string      // single-line string quotes
	charQuote    bool        // ' is a short char literal, not a string (C, Go, Rust)
	rawMulti     bool        // multi-line strings have no escapes (Go backticks)
	keywords     map[string]bool
	foldKeywords bool // keywords are case-insensitive (SQL)
	dashIdent    bool // identifiers may contain '-' (CSS, shell)
	markup       bool // XML and HTML
	log          bool // log files: timestamps and levels

	opens [256]bool // first bytes of comment and string delimiters
	quote [256]bool
}

// opener handles a comment or multi-line string starting at i. It returns
// where it ends (i if there is none there), the class as a state, and
// whether it runs to the end of the line, leaving st for the next one.
func (l *lang) opener(line string, i int) (e, st int, done bool) {
	rest := line[i:]
	for b, bc := range l.blocks {
		if strings.HasPrefix(rest, bc[0]) {
			k := strings.Index(rest[len(bc[0]):], bc[1])
			if k < 0 {
				return len(line), stBlock + b, true
			}
			return i + len(bc[0]) + k + len(bc[1]), stBlock, false
		}
	}
	for _, lc := range l.lineComments {
		if strings.HasPrefix(rest, lc) {
			return len(line), 0, true
		}
	}
	for m, d := range l.multi {
		if strings.HasPrefix(rest, d) {
			e, ok := l.closeString(line, i+len(d), d)
			if !ok {
				return e, stMulti + m, true
			}
			return e, stMulti, false
		}
	}
	return i, 0, false
}

// tokOf is the class of an opener's state; 0 is a line comment.
func tokOf(st int) uint8 {
	if st >= stMulti {
		return tokString
	}
	return tokComment
}

// Line-start states. 0 is normal; the others are offset by an index into
// blocks or multi.
const (
	stTag   = 1
	stBlock = 2
	stMulti = 2 + 16
)

// maxHighlight is the longest line that gets colored.
const maxHighlight = 10 << 10

func words(s string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(s) {
		m[w] = true
	}
	return m
}

var (
	cBlock   = [][2]string{{"/*", "*/"}}
	cLike    = lang{lineComments: []string{"//"}, blocks: cBlock, quotes: `"`, charQuote: true}
	hashLike = lang{lineComments: []string{"#"}, quotes: `"'`}
)

func with(l lang, f func(*lang)) *lang { f(&l); return &l }

var langs = map[string]*lang{}

func init() {
	reg := func(l *lang, exts ...string) {
		for _, d := range l.lineComments {
			l.opens[d[0]] = true
		}
		for _, d := range l.blocks {
			l.opens[d[0][0]] = true
		}
		for _, d := range l.multi {
			l.opens[d[0]] = true
		}
		for _, q := range []byte(l.quotes) {
			l.quote[q] = true
		}
		for _, x := range exts {
			langs[x] = l
		}
	}
	reg(with(cLike, func(l *lang) {
		l.multi, l.rawMulti = []string{"`"}, true
		l.keywords = words(`break case chan const continue default defer else fallthrough for func go goto if
			import interface map package range return select struct switch type var
			true false nil iota any bool byte rune string int int8 int16 int32 int64 uint uint8 uint16
			uint32 uint64 uintptr float32 float64 complex64 complex128 error
			append cap clear close copy delete len make max min new panic print println recover`)
	}), ".go")
	reg(with(cLike, func(l *lang) {
		l.keywords = words(`auto break case char const continue default do double else enum extern float for
			goto if inline int long register restrict return short signed sizeof static struct switch
			typedef union unsigned void volatile while bool true false NULL nullptr
			class namespace template typename public private protected virtual override final
			new delete this throw try catch using operator friend explicit constexpr noexcept static_cast
			dynamic_cast reinterpret_cast const_cast decltype mutable int8_t int16_t int32_t int64_t
			uint8_t uint16_t uint32_t uint64_t size_t`)
	}), ".c", ".h", ".cc", ".cpp", ".cxx", ".hpp", ".hh", ".hxx", ".m", ".mm")
	reg(with(cLike, func(l *lang) {
		l.keywords = words(`as async await break const continue crate dyn else enum extern false fn for if impl
			in let loop match mod move mut pub ref return self Self static struct super trait true type
			unsafe use where while Some None Ok Err Box Vec String Option Result
			i8 i16 i32 i64 i128 isize u8 u16 u32 u64 u128 usize f32 f64 bool char str`)
	}), ".rs")
	reg(with(cLike, func(l *lang) {
		l.quotes, l.charQuote, l.multi = `"'`, false, []string{"`"}
		l.keywords = words(`async await break case catch class const continue debugger default delete do else
			export extends false finally for from function if import in instanceof let new null of return
			static super switch this throw true try typeof undefined var void while yield
			interface type enum implements private public protected readonly declare namespace abstract
			as keyof any unknown never number string boolean`)
	}), ".js", ".mjs", ".cjs", ".jsx", ".ts", ".tsx")
	reg(with(cLike, func(l *lang) {
		l.multi = []string{`"""`}
		l.keywords = words(`abstract assert boolean break byte case catch char class const continue default do
			double else enum extends final finally float for goto if implements import instanceof int
			interface long native new package private protected public return short static super switch
			synchronized this throw throws transient try void volatile while true false null var
			val fun object when is in override data sealed let guard func struct protocol extension
			import self Self nil async await namespace using get set string bool`)
	}), ".java", ".kt", ".kts", ".scala", ".swift", ".cs", ".dart", ".groovy")
	reg(with(hashLike, func(l *lang) {
		l.multi = []string{`"""`, `'''`}
		l.keywords = words(`False None True and as assert async await break class continue def del elif else
			except finally for from global if import in is lambda nonlocal not or pass raise return try
			while with yield self print len range`)
	}), ".py", ".pyi")
	reg(with(hashLike, func(l *lang) {
		l.dashIdent = true
		l.keywords = words(`if then else elif fi case esac for while until do done in function return local
			export readonly unset shift exit break continue source alias echo cd set eval exec trap`)
	}), ".sh", ".bash", ".zsh", ".fish", "makefile", "dockerfile", ".mk")
	reg(with(hashLike, func(l *lang) {
		l.keywords = words(`BEGIN END alias and begin break case class def defined do else elsif end ensure
			false for if in module next nil not or redo rescue retry return self super then true undef
			unless until when while yield require attr_reader attr_accessor puts`)
	}), ".rb")
	reg(with(hashLike, func(l *lang) {
		l.lineComments = []string{"#", ";"}
		l.keywords = words(`true false yes no on off null`)
	}), ".yaml", ".yml", ".toml", ".ini", ".conf", ".cfg", ".properties", ".env")
	reg(&lang{quotes: `"`, keywords: words(`true false null`)}, ".json", ".jsonl", ".ndjson")
	reg(&lang{blocks: cBlock, lineComments: []string{"//"}, quotes: `"'`, dashIdent: true,
		keywords: words(`important inherit initial unset none auto`)}, ".css", ".scss", ".less")
	reg(&lang{lineComments: []string{"--"}, blocks: cBlock, quotes: `'"`, foldKeywords: true,
		keywords: words(`select from where and or not insert into values update set delete create table
			drop alter index view join left right inner outer on as group by order having limit offset
			union all distinct null is in like between case when then else end primary key foreign
			references default exists begin commit rollback transaction int integer text varchar
			boolean date timestamp`)}, ".sql")
	reg(&lang{lineComments: []string{"--"}, blocks: [][2]string{{"--[[", "]]"}}, quotes: `"'`,
		keywords: words(`and break do else elseif end false for function goto if in local
			nil not or repeat return then true until while`)}, ".lua")
	reg(&lang{markup: true, blocks: [][2]string{{"<!--", "-->"}}},
		".xml", ".html", ".htm", ".xhtml", ".svg", ".plist", ".xsd", ".xsl", ".vue")
	reg(&lang{log: true}, ".log", ".out", ".err")
}

// langFor picks a language from the file name.
func langFor(path string) *lang {
	base := strings.ToLower(filepath.Base(path))
	if l := langs[filepath.Ext(base)]; l != nil {
		return l
	}
	if l := langs[base]; l != nil {
		return l
	}
	if strings.Contains(base, ".log") { // app.log.1, app.log.gz.txt
		return langs[".log"]
	}
	return nil
}

var identChars = func() (t [256]bool) {
	for c := range 256 {
		t[c] = c == '_' || c >= 0x80 || 'a' <= c|0x20 && c|0x20 <= 'z' || '0' <= c && c <= '9'
	}
	return
}()

func isIdent(c byte, dash bool) bool { return identChars[c] || dash && c == '-' }

func isDigit(c byte) bool { return '0' <= c && c <= '9' }

// scan colors one line that starts in state st. It appends spans to out and
// returns them with the state at the start of the next line.
func (l *lang) scan(line string, st int, out []span) ([]span, int) {
	if len(line) > maxHighlight {
		return out, st
	}
	switch {
	case l.log:
		return l.scanLog(line, out), 0
	case l.markup:
		return l.scanMarkup(line, st, out)
	}
	add := func(s, e int, c uint8) {
		if e > s {
			out = append(out, span{s, e, c})
		}
	}
	i := 0
	// Finish a construct carried over from the previous line.
	if st >= stMulti {
		d := l.multi[st-stMulti]
		e, ok := l.closeString(line, 0, d)
		add(0, e, tokString)
		if !ok {
			return out, st
		}
		i = e
	} else if st >= stBlock {
		end := l.blocks[st-stBlock][1]
		k := strings.Index(line, end)
		if k < 0 {
			add(0, len(line), tokComment)
			return out, st
		}
		i = k + len(end)
		add(0, i, tokComment)
	}
	for i < len(line) {
		c := line[i]
		if l.opens[c] {
			if e, st, done := l.opener(line, i); done {
				return append(out, span{i, e, tokOf(st)}), st
			} else if e > i {
				out = append(out, span{i, e, tokOf(st)})
				i = e
				continue
			}
		}
		switch {
		case c == '\'' && l.charQuote:
			// A char literal holds one character or an escape; anything else
			// ('a in Rust) is not one.
			if e := charEnd(line, i+1); e > 0 {
				add(i, e, tokString)
				i = e
				continue
			}
			i++
		case l.quote[c]:
			e, _ := l.closeString(line, i+1, line[i:i+1])
			add(i, e, tokString)
			i = e
		case isDigit(c) || c == '.' && i+1 < len(line) && isDigit(line[i+1]):
			e := i + 1
			for e < len(line) && (isIdent(line[e], false) || line[e] == '.') {
				e++
			}
			add(i, e, tokNumber)
			i = e
		case isIdent(c, false):
			e := i + 1
			for e < len(line) && isIdent(line[e], l.dashIdent) {
				e++
			}
			w := line[i:e]
			if l.foldKeywords {
				w = strings.ToLower(w)
			}
			if l.keywords[w] {
				add(i, e, tokKeyword)
			}
			i = e
		default:
			i++
		}
	}
	return out, 0
}

// charEnd returns the offset after a char literal whose body starts at i,
// or 0 if there is none.
func charEnd(line string, i int) int {
	if i < len(line) && line[i] == '\\' {
		if e, ok := (&lang{}).closeString(line, i, "'"); ok && e-i <= 12 {
			return e
		}
		return 0
	}
	_, n := utf8.DecodeRuneInString(line[i:])
	if n > 0 && i+n < len(line) && line[i+n] == '\'' {
		return i + n + 1
	}
	return 0
}

// closeString finds the end of a string whose body starts at i. It returns
// the offset after the closing delimiter, or len(line) and false.
func (l *lang) closeString(line string, i int, d string) (int, bool) {
	raw := l.rawMulti && d == "`"
	for i < len(line) {
		if line[i] == '\\' && !raw {
			i += 2
			continue
		}
		if strings.HasPrefix(line[i:], d) {
			return i + len(d), true
		}
		i++
	}
	return len(line), false
}

// scanMarkup colors tags, attribute values and comments in XML and HTML.
func (l *lang) scanMarkup(line string, st int, out []span) ([]span, int) {
	add := func(s, e int, c uint8) {
		if e > s {
			out = append(out, span{s, e, c})
		}
	}
	const open, end = "<!--", "-->"
	i := 0
	for i < len(line) {
		switch st {
		case stBlock:
			k := strings.Index(line[i:], end)
			if k < 0 {
				add(i, len(line), tokComment)
				return out, st
			}
			add(i, i+k+len(end), tokComment)
			i += k + len(end)
			st = 0
		case stTag:
			c := line[i]
			switch {
			case c == '>':
				add(i, i+1, tokTag)
				i++
				st = 0
			case (c == '/' || c == '?') && i+1 < len(line) && line[i+1] == '>':
				add(i, i+2, tokTag)
				i += 2
				st = 0
			case c == '"' || c == '\'':
				k := strings.IndexByte(line[i+1:], c)
				e := len(line)
				if k >= 0 {
					e = i + 1 + k + 1
				}
				add(i, e, tokString)
				i = e
			default:
				i++
			}
		default:
			k := strings.IndexByte(line[i:], '<')
			if k < 0 {
				return out, 0
			}
			i += k
			if strings.HasPrefix(line[i:], open) {
				st = stBlock
				add(i, i+len(open), tokComment)
				i += len(open)
				continue
			}
			e := i + 1
			if e < len(line) && (line[e] == '/' || line[e] == '!' || line[e] == '?') {
				e++
			}
			if e == len(line) || !isIdent(line[e], false) {
				i++ // a stray '<' in text
				continue
			}
			for e < len(line) && (isIdent(line[e], true) || line[e] == ':' || line[e] == '.') {
				e++
			}
			add(i, e, tokTag)
			i = e
			st = stTag
		}
	}
	return out, st
}

var months = words("jan feb mar apr may jun jul aug sep oct nov dec")

// scanLog dims a leading timestamp and colors log levels.
func (l *lang) scanLog(line string, out []span) []span {
	// Timestamp: digits and separators at the start, or "Oct 22 23:06:01".
	i := 0
	if len(line) > 3 && months[strings.ToLower(line[:3])] && line[3] == ' ' {
		i = 4
	}
	digits := 0
	for e := i; e < len(line) && e < 48; e++ {
		c := line[e]
		if isDigit(c) {
			digits++
		} else if !strings.ContainsRune("-:./,T Z+[]", rune(c)) {
			break
		}
		i = e + 1
	}
	if digits >= 6 {
		out = append(out, span{0, len(strings.TrimRight(line[:i], " ")), tokTime})
	} else {
		i = 0
	}
	for i < len(line) {
		c := line[i]
		if !isIdent(c, false) || isDigit(c) {
			i++
			continue
		}
		e := i + 1
		for e < len(line) && isIdent(line[e], false) {
			e++
		}
		if n := e - i; n >= 3 && n <= 8 {
			switch strings.ToLower(line[i:e]) {
			case "error", "err", "fatal", "panic", "critical", "crit", "severe", "failed", "failure", "exception":
				out = append(out, span{i, e, tokError})
			case "warn", "warning":
				out = append(out, span{i, e, tokWarn})
			}
		}
		i = e
	}
	return out
}

// ckLines is how often the highlighter saves the line-start state.
const ckLines = 1024

// highlighter colors the visible lines. It saves the state at every
// ckLines-th line so a multi-line comment or string far above the screen is
// still known; far from any saved state it assumes normal text a few
// thousand lines up, which is almost always right.
type highlighter struct {
	lang  *lang
	ck    []int       // exact state at line k*ckLines
	guess map[int]int // assumed state at line k*ckLines beyond ck
	spans []span
}

func newHighlighter(path string) *highlighter {
	return &highlighter{lang: langFor(path), ck: []int{0}, guess: map[int]int{}}
}

// edited drops saved states after line, which changed.
func (h *highlighter) edited(line int) {
	h.ck = h.ck[:min(len(h.ck), line/ckLines+1)]
	clear(h.guess)
}

// reach is how many blocks the exact states are extended in one call.
const reach = 8

func (h *highlighter) stateAt(b *Buffer, line int) int {
	k := line / ckLines
	if k-len(h.ck) < reach {
		for len(h.ck) <= k {
			n := len(h.ck)
			h.ck = append(h.ck, h.run(b, (n-1)*ckLines, n*ckLines, h.ck[n-1]))
		}
	} else if _, ok := h.guess[k]; !ok {
		h.guess[k] = h.run(b, (k-1)*ckLines, k*ckLines, 0)
	}
	st := h.guess[k]
	if k < len(h.ck) {
		st = h.ck[k]
	}
	return h.run(b, k*ckLines, line, st)
}

// run scans lines [from, to) starting in state st.
func (h *highlighter) run(b *Buffer, from, to, st int) int {
	for i := from; i < to && i < b.LineCount(); i++ {
		h.spans, st = h.lang.scan(b.Line(i), st, h.spans[:0])
	}
	return st
}
