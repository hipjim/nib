package main

import "strings"

// commentEdit is where a line changed and by how many bytes, so the cursor
// and anchor can follow.
type commentEdit struct{ at, delta int }

// toggleComments comments every line out, or, when all the non-blank lines
// are comments already, uncomments them. Languages with only block
// comments (CSS, HTML) get one block per line.
func toggleComments(lines []string, l *lang) ([]string, []commentEdit) {
	out := make([]string, len(lines))
	edits := make([]commentEdit, len(lines))
	var open, close string
	if len(l.lineComments) > 0 {
		open = l.lineComments[0]
	} else {
		open, close = l.blocks[0][0], l.blocks[0][1]
	}
	isComment := func(s string) bool {
		s = strings.TrimSpace(s)
		return strings.HasPrefix(s, open) && (close == "" || strings.HasSuffix(s, close))
	}
	blank := func(s string) bool { return strings.TrimSpace(s) == "" }

	indent, all, any := -1, true, false
	for _, s := range lines {
		if blank(s) {
			continue
		}
		any = true
		if n := len(leadingSpace(s)); indent < 0 || n < indent {
			indent = n
		}
		all = all && isComment(s)
	}
	for i, s := range lines {
		switch {
		case !any:
			out[i] = open + " " + close
			edits[i] = commentEdit{0, len(open) + 1}
		case blank(s):
			out[i] = s
		case all:
			ws := leadingSpace(s)
			rest := strings.TrimPrefix(s[len(ws):], open)
			if close != "" {
				rest = strings.TrimSuffix(strings.TrimRight(rest, " \t"), close)
				rest = strings.TrimSuffix(rest, " ")
			}
			rest = strings.TrimPrefix(rest, " ")
			out[i] = ws + rest
			edits[i] = commentEdit{len(ws), len(rest) - (len(s) - len(ws))}
		default:
			tail := ""
			if close != "" {
				tail = " " + close
			}
			out[i] = s[:indent] + open + " " + s[indent:] + tail
			edits[i] = commentEdit{indent, len(open) + 1}
		}
	}
	return out, edits
}

// toggleComment comments the selected lines out, or back in, with ⌘/.
func (e *Editor) toggleComment() {
	if e.locked() {
		return
	}
	l := e.hl.lang
	if l == nil || (len(l.lineComments) == 0 && len(l.blocks) == 0) {
		e.flash("no comment syntax for this file", true)
		return
	}
	first, last := e.lineSpan()
	lines := make([]string, 0, last-first+1)
	for i := first; i <= last; i++ {
		lines = append(lines, e.buf.Line(i))
	}
	out, edits := toggleComments(lines, l)
	e.replaceLines(first, last, out)
	shift := func(p Pos) Pos {
		if p.Line < first || p.Line > last {
			return p
		}
		ed := edits[p.Line-first]
		if p.Col >= ed.at {
			p.Col = max(ed.at, p.Col+ed.delta)
		}
		return e.buf.Clamp(p)
	}
	e.anchor, e.cursor = shift(e.anchor), shift(e.cursor)
	e.followCursor = true
}
