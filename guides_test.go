package main

import "testing"

func TestGuides(t *testing.T) {
	e := newTestEditor("func f() {\n\tif x {\n\t\ty()\n\n\t}\n\n}\n\n\n", Pos{})
	got := e.guides(0, 9)
	want := []int{0, 4, 8, 8, 4, 4, 0, 0, 0, 0}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	if g := e.guides(3, 3); g[0] != 8 { // a blank line alone on screen still looks around
		t.Fatalf("line 3 alone: %v", g)
	}
	e.doc.dir = &dirView{}
	if g := e.guides(0, 2); g[0] != 0 || g[1] != 0 {
		t.Fatalf("listing has guides: %v", g)
	}
}
