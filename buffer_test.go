package main

import (
	"os"
	"path/filepath"
	"testing"
)

func bufText(b *Buffer) string { return b.Text(Pos{}, b.End()) }

func TestReplace(t *testing.T) {
	b := NewBuffer("hello\nworld")
	end := b.Replace(Pos{0, 5}, Pos{0, 5}, ", there\nbig", Pos{})
	if got := bufText(b); got != "hello, there\nbig\nworld" {
		t.Fatalf("got %q", got)
	}
	if end != (Pos{1, 3}) {
		t.Fatalf("end %v", end)
	}
	b.Replace(Pos{0, 5}, Pos{2, 0}, " ", Pos{})
	if got := bufText(b); got != "hello world" {
		t.Fatalf("got %q", got)
	}
}

func TestUndoRedo(t *testing.T) {
	b := NewBuffer("abc")
	p := Pos{0, 3}
	for _, c := range "def" {
		p = b.Replace(p, p, string(c), p)
	}
	if bufText(b) != "abcdef" || len(b.undo) != 1 {
		t.Fatalf("typing not merged: %q %d", bufText(b), len(b.undo))
	}
	b.Replace(Pos{0, 0}, Pos{0, 1}, "\n", p)
	cur, _ := b.Undo()
	if bufText(b) != "abcdef" || cur != p {
		t.Fatalf("undo: %q %v", bufText(b), cur)
	}
	b.Undo()
	if bufText(b) != "abc" || b.Dirty() {
		t.Fatalf("undo2: %q dirty=%v", bufText(b), b.Dirty())
	}
	b.Redo()
	b.Redo()
	if bufText(b) != "\nbcdef" {
		t.Fatalf("redo: %q", bufText(b))
	}
}

func TestBackspaceMerge(t *testing.T) {
	b := NewBuffer("abcdef")
	p := Pos{0, 6}
	for range 3 {
		q := Pos{0, p.Col - 1}
		b.Replace(q, p, "", p)
		p = q
	}
	if bufText(b) != "abc" || len(b.undo) != 1 {
		t.Fatalf("%q %d", bufText(b), len(b.undo))
	}
	b.Undo()
	if bufText(b) != "abcdef" {
		t.Fatalf("%q", bufText(b))
	}
}

func TestFind(t *testing.T) {
	b := NewBuffer("Foo bar\nbaz foo\nFOO")
	cases := []struct {
		q    string
		from Pos
		back bool
		want Pos
	}{
		{"foo", Pos{0, 0}, false, Pos{0, 0}},
		{"foo", Pos{0, 1}, false, Pos{1, 4}},
		{"Foo", Pos{0, 1}, false, Pos{0, 0}}, // wraps, case-sensitive
		{"foo", Pos{1, 4}, true, Pos{0, 0}},
		{"foo", Pos{0, 0}, true, Pos{2, 0}}, // wraps backward
	}
	for _, c := range cases {
		got, ok := b.Find(c.q, c.from, c.back)
		if !ok || got != c.want {
			t.Errorf("Find(%q,%v,%v) = %v,%v want %v", c.q, c.from, c.back, got, ok, c.want)
		}
	}
}

func TestSaveCRLF(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.txt")
	os.WriteFile(path, []byte("a\r\nb\r\n"), 0o600)
	b, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if b.LineCount() != 3 || b.Line(0) != "a" {
		t.Fatalf("lines %q", bufText(b))
	}
	b.Replace(Pos{1, 1}, Pos{1, 1}, "!", Pos{})
	if err := b.Save(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	fi, _ := os.Stat(path)
	if string(data) != "a\r\nb!\r\n" || fi.Mode().Perm() != 0o600 || b.Dirty() {
		t.Fatalf("saved %q mode %v", data, fi.Mode())
	}
}

func TestIndexFold(t *testing.T) {
	cases := []struct {
		s, q string
		want int
	}{
		{"Hello World", "world", 6},
		{"aaaaAB", "ab", 4},
		{"xyz", "xyzw", -1},
		{"FOO_bar", "o_b", 2},
		{"ÄÖÜ foo", "foo", 7},
		{"", "a", -1},
	}
	for _, c := range cases {
		if got := matcher(c.q)(c.s); got != c.want {
			t.Errorf("matcher(%q)(%q) = %d, want %d", c.q, c.s, got, c.want)
		}
	}
}
