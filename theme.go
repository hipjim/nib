package main

import "image/color"

// A palette is one theme's colors. The editor keeps the current one in the
// col* variables, so a theme change takes effect on the next frame.
type palette struct {
	bg, curLine, text, dim, info, gutterHi, accent, sel, match, status, warn uint32
	guide                                                                    uint32
	str, num, kw, tag, tokWarn, dir                                          uint32
	thumb, thumbHover                                                        uint32
}

var darkTheme = palette{
	bg: 0x0b0d10, curLine: 0x12161c, text: 0xc8d0da, dim: 0x3a4250, info: 0x5f6b7c,
	gutterHi: 0x8b95a5, accent: 0x3ee6a8, sel: 0x17463a, match: 0x4a3f1c, status: 0x07080a, warn: 0xff6b6b,
	str: 0x7fd4a6, num: 0xd9b56c, kw: 0xf4f7fa, tag: 0x86a6cc, tokWarn: 0xe8b84e, dir: 0x7cb3e8,
	thumb: 0x343c48, thumbHover: 0x4a5566, guide: 0x1b2129,
}

var lightTheme = palette{
	bg: 0xfbfbfa, curLine: 0xf0f1f0, text: 0x2a3038, dim: 0xb9c0c9, info: 0x6f7a88,
	gutterHi: 0x4f5a68, accent: 0x12935f, sel: 0xd2ebdf, match: 0xf5e3a3, status: 0xf1f2f1, warn: 0xd1403f,
	str: 0x1d8657, num: 0x9b6a06, kw: 0x0d1117, tag: 0x30609f, tokWarn: 0xae7407, dir: 0x2a6db5,
	thumb: 0xcdd3db, thumbHover: 0xb0b8c3, guide: 0xe4e7ea,
}

var (
	colBg, colCurLine, colText, colDim, colInfo, colGutterHi color.NRGBA
	colAccent, colSel, colMatch, colStatus, colWarn          color.NRGBA
	colThumb, colThumbHover, colGuide                        color.NRGBA
	tokColors                                                [tokDir + 1]color.NRGBA // by token class
)

func init() { applyTheme(false) }

func (e *Editor) setLight(light bool) {
	e.light = light
	applyTheme(light)
}

func applyTheme(light bool) {
	p := darkTheme
	if light {
		p = lightTheme
	}
	colBg, colCurLine, colText, colDim, colInfo = rgb(p.bg), rgb(p.curLine), rgb(p.text), rgb(p.dim), rgb(p.info)
	colGutterHi, colAccent, colSel, colMatch = rgb(p.gutterHi), rgb(p.accent), rgb(p.sel), rgb(p.match)
	colStatus, colWarn, colThumb, colThumbHover = rgb(p.status), rgb(p.warn), rgb(p.thumb), rgb(p.thumbHover)
	colGuide = rgb(p.guide)
	tokColors = [...]color.NRGBA{
		tokPlain:   colText,
		tokComment: colInfo,
		tokString:  rgb(p.str),
		tokNumber:  rgb(p.num),
		tokKeyword: rgb(p.kw),
		tokTag:     rgb(p.tag),
		tokTime:    colInfo,
		tokError:   colWarn,
		tokWarn:    rgb(p.tokWarn),
		tokDir:     rgb(p.dir),
	}
}

func rgb(c uint32) color.NRGBA { return argb(0xff000000 | c) }
func argb(c uint32) color.NRGBA {
	return color.NRGBA{A: uint8(c >> 24), R: uint8(c >> 16), G: uint8(c >> 8), B: uint8(c)}
}
