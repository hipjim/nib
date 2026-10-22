package main

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const tabWidth = 4

// visCol converts a byte column into a visual (on-screen) column,
// expanding tabs.
func visCol(line string, col int) int {
	v := 0
	for i, r := range line {
		if i >= col {
			break
		}
		v += runeWidth(r, v)
	}
	return v
}

func runeWidth(r rune, v int) int {
	if r == '\t' {
		return tabWidth - v%tabWidth
	}
	return 1
}

// colAt returns the byte column nearest to the visual position x.
func colAt(line string, x float32) int {
	v := 0
	for i, r := range line {
		w := runeWidth(r, v)
		if x < float32(v)+float32(w)/2 {
			return i
		}
		v += w
	}
	return len(line)
}

// displaySlice returns the part of line between visual columns v0 and v1
// with tabs expanded to spaces.
func displaySlice(line string, v0, v1 int) string {
	var sb strings.Builder
	v := 0
	for _, r := range line {
		if v >= v1 {
			break
		}
		w := runeWidth(r, v)
		if v+w > v0 {
			if r == '\t' || v < v0 {
				sb.WriteString(strings.Repeat(" ", min(v+w, v1)-max(v, v0)))
			} else {
				sb.WriteRune(r)
			}
		}
		v += w
	}
	return sb.String()
}

func class(r rune) int {
	switch {
	case unicode.IsSpace(r):
		return 0
	case r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
		return 1
	}
	return 2
}

// wordEnd returns the column after the word that starts at or after col.
func wordEnd(line string, col int) int {
	for col < len(line) {
		r, n := utf8.DecodeRuneInString(line[col:])
		if class(r) != 0 {
			break
		}
		col += n
	}
	if col == len(line) {
		return col
	}
	r, _ := utf8.DecodeRuneInString(line[col:])
	c := class(r)
	for col < len(line) {
		r, n := utf8.DecodeRuneInString(line[col:])
		if class(r) != c {
			break
		}
		col += n
	}
	return col
}

// wordStart returns the column of the start of the word before col.
func wordStart(line string, col int) int {
	for col > 0 {
		r, n := utf8.DecodeLastRuneInString(line[:col])
		if class(r) != 0 {
			break
		}
		col -= n
	}
	if col == 0 {
		return 0
	}
	r, _ := utf8.DecodeLastRuneInString(line[:col])
	c := class(r)
	for col > 0 {
		r, n := utf8.DecodeLastRuneInString(line[:col])
		if class(r) != c {
			break
		}
		col -= n
	}
	return col
}

// wordAt returns the bounds of the word touching col.
func wordAt(line string, col int) (int, int) {
	isWord := func(r rune) bool { return class(r) == 1 }
	s, e := col, col
	for s > 0 {
		r, n := utf8.DecodeLastRuneInString(line[:s])
		if !isWord(r) {
			break
		}
		s -= n
	}
	for e < len(line) {
		r, n := utf8.DecodeRuneInString(line[e:])
		if !isWord(r) {
			break
		}
		e += n
	}
	return s, e
}

func leadingSpace(s string) string {
	return s[:len(s)-len(strings.TrimLeft(s, " \t"))]
}
