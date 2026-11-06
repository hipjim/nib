// The command palette: every action by name, found from a few letters of
// it, so nothing needs a key remembered. It is the same prompt the file
// finder uses, ranked by the same fuzzy match.

package main

import (
	"slices"
	"strings"
	"unicode"

	"gioui.org/io/clipboard"
	"gioui.org/io/key"
	"gioui.org/layout"
)

type command struct {
	name string
	keys string // the shortcut, in ⌘⇧ notation; shown beside the name
	run  func(e *Editor, gtx layout.Context)
}

// commandList is the state of the command prompt: the commands that match the
// query, best first, and the one picked.
type commandList struct {
	all     []command
	results []int // indices into all
	sel     int
}

// commands lists everything the palette can run. It is built when the
// palette opens, so names can say which way a toggle will go.
func (e *Editor) commands() []command {
	onOff := func(on bool) string { return map[bool]string{true: "off", false: "on"}[on] }
	cmds := []command{
		{"save", "⌘S", func(e *Editor, gtx layout.Context) { e.key(gtx, keyEvent("S"), false) }},
		{"save as", "⌘⇧S", func(e *Editor, _ layout.Context) { e.openPrompt(promptSaveAs, orDefault(e.path, dirPrefix(e.root))) }},
		{"browse the folder", "⌘O", func(e *Editor, _ layout.Context) { e.up() }},
		{"open a path", "⌘⇧O", func(e *Editor, _ layout.Context) { e.openPrompt(promptOpen, dirPrefix(e.root)) }},
		{"find a file in the folder", "⌘P", func(e *Editor, _ layout.Context) { e.openFinder() }},
		{"search every file in the folder", "⌘⇧F", func(e *Editor, _ layout.Context) { e.openGrep() }},
		{"show the file's folder", "⌘E", func(e *Editor, _ layout.Context) { e.up() }},
		{"file properties", "⌘I", func(e *Editor, _ layout.Context) { e.info = !e.info }},
		{"show spaces and tabs", "⌘⇧I", func(e *Editor, _ layout.Context) { e.showWS = !e.showWS }},
		{"open a terminal in the file's folder", "⌘T", func(e *Editor, _ layout.Context) { e.openTerminal() }},
		{"close the file", "⌘W", func(e *Editor, gtx layout.Context) { e.key(gtx, keyEvent("W"), false) }},
		{"close all other saved files", "⌘⇧W", func(e *Editor, _ layout.Context) { e.closeOthers() }},
		{"next tab", "Ctrl+Tab", func(e *Editor, _ layout.Context) { e.cycleTab(false) }},
		{"previous tab", "Ctrl+⇧Tab", func(e *Editor, _ layout.Context) { e.cycleTab(true) }},
		{"quit", "⌘Q", func(e *Editor, gtx layout.Context) { e.key(gtx, keyEvent("Q"), false) }},
		{"find", "⌘F", func(e *Editor, gtx layout.Context) { e.key(gtx, keyEvent("F"), false) }},
		{"replace", "⌘R", func(e *Editor, _ layout.Context) { e.openReplace() }},
		{"filter the lines of this file", "⌘⇧G", func(e *Editor, _ layout.Context) { e.openFilter() }},
		{"tail the file " + onOff(e.tail != nil) + ", following what is added", "⌘⇧T", func(e *Editor, _ layout.Context) { e.toggleTail() }},
		{"regular expressions " + onOff(e.opts.regex), "⌘⇧R", func(e *Editor, _ layout.Context) { e.opts.regex = !e.opts.regex }},
		{"exact case in find " + onOff(e.opts.exact), "⌘⇧C, finding", func(e *Editor, _ layout.Context) { e.opts.exact = !e.opts.exact }},
		{"whole words in find " + onOff(e.opts.word), "⌘⇧W, finding", func(e *Editor, _ layout.Context) { e.opts.word = !e.opts.word }},
		{"go to line", "⌘G", func(e *Editor, _ layout.Context) { e.openPrompt(promptGoto, "") }},
		{"undo", "⌘Z", func(e *Editor, _ layout.Context) { e.undo(false) }},
		{"redo", "⌘⇧Z", func(e *Editor, _ layout.Context) { e.undo(true) }},
		{"copy", "⌘C", func(e *Editor, gtx layout.Context) { e.copy(gtx, false) }},
		{"cut", "⌘X", func(e *Editor, gtx layout.Context) { e.copy(gtx, true) }},
		{"paste", "⌘V", func(e *Editor, gtx layout.Context) { gtx.Execute(clipboard.ReadCmd{Tag: e}) }},
		{"select the word, then its next occurrence", "⌘D", func(e *Editor, _ layout.Context) { e.selectNext() }},
		{"select the line", "⌘L", func(e *Editor, gtx layout.Context) { e.key(gtx, keyEvent("L"), false) }},
		{"duplicate the line", "⌘⇧D", func(e *Editor, _ layout.Context) { e.duplicateLines() }},
		{"delete the line", "⌘⇧K", func(e *Editor, _ layout.Context) { e.deleteLines() }},
		{"move the line up", "Alt↑", func(e *Editor, _ layout.Context) { e.moveLines(-1) }},
		{"move the line down", "Alt↓", func(e *Editor, _ layout.Context) { e.moveLines(1) }},
		{"indent", "Tab", func(e *Editor, _ layout.Context) { e.indent(1) }},
		{"dedent", "⇧Tab", func(e *Editor, _ layout.Context) { e.indent(-1) }},
		{"comment the lines out, or back in", "⌘/", func(e *Editor, _ layout.Context) { e.toggleComment() }},
		{"zoom in", "⌘=", func(e *Editor, _ layout.Context) { e.size = min(e.size+1, 48) }},
		{"zoom out", "⌘-", func(e *Editor, _ layout.Context) { e.size = max(e.size-1, 8) }},
		{"reset the zoom", "⌘0", func(e *Editor, _ layout.Context) { e.size = 14 }},
		{"syntax highlighting " + onOff(!e.plain), "⌘⇧H", func(e *Editor, gtx layout.Context) { e.key(gtx, keyEvent("H", true), false) }},
		{map[bool]string{true: "dark", false: "light"}[e.light] + " theme", "⌘⇧L", func(e *Editor, gtx layout.Context) { e.key(gtx, keyEvent("L", true), false) }},
		{"all the keys", "⌘⇧/", func(e *Editor, _ layout.Context) { e.toggleKeys() }},
	}
	if e.showWS {
		cmds[8].name = "hide spaces and tabs"
	}
	for i := range cmds {
		cmds[i].keys = strings.NewReplacer("⌘", shortcutName, "⇧", shiftName).Replace(cmds[i].keys)
	}
	return cmds
}

// keyEvent is a shortcut key press, for commands that share the key
// handler's logic rather than repeating it.
func keyEvent(name string, shift ...bool) key.Event {
	k := key.Event{Name: key.Name(name), Modifiers: key.ModShortcut}
	if len(shift) > 0 && shift[0] {
		k.Modifiers |= key.ModShift
	}
	return k
}

func (e *Editor) openPalette() {
	if e.prompt == promptPalette {
		e.prompt = promptNone
		return
	}
	e.palette.all = e.commands()
	e.palette.sel = 0
	e.openPrompt(promptPalette, "")
	e.palette.rank("")
}

// rank keeps the commands matching q, best first; with no query, all of
// them in their listed order.
func (p *commandList) rank(query string) {
	p.results = p.results[:0]
	q := []rune(query)
	fold := !slices.ContainsFunc(q, unicode.IsUpper)
	if fold {
		for i, r := range q {
			q[i] = unicode.ToLower(r)
		}
	}
	type hit struct{ i, score int }
	var hits []hit
	pos := make([]int, len(q))
	for i, c := range p.all {
		if s, ok := fuzzyScore(c.name, q, fold, pos); ok {
			hits = append(hits, hit{i, s})
		}
	}
	if len(q) > 0 {
		slices.SortStableFunc(hits, func(a, b hit) int { return b.score - a.score })
	}
	for _, h := range hits {
		p.results = append(p.results, h.i)
	}
	p.sel = min(p.sel, max(0, len(p.results)-1))
}

// runCommand closes the palette and runs the command picked.
func (e *Editor) runCommand(gtx layout.Context) {
	p := &e.palette
	e.prompt = promptNone
	if p.sel < len(p.results) {
		p.all[p.results[p.sel]].run(e, gtx)
	}
}
