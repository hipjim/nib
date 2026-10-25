package main

import (
	"runtime"
	"slices"
	"testing"
)

func TestTerminalCmd(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TERM_PROGRAM", "ghostty")
	t.Setenv("NIB_TERMINAL", "")
	cmd, err := terminalCmd(dir)
	if runtime.GOOS == "darwin" {
		if err != nil || !slices.Equal(cmd.Args, []string{"open", "-a", "Ghostty", dir}) {
			t.Fatalf("got %v %v", cmd, err)
		}
	}
	t.Setenv("NIB_TERMINAL", "myterm --flag")
	cmd, err = terminalCmd(dir)
	if err != nil || cmd.Dir != dir {
		t.Fatalf("got %v %v", cmd, err)
	}
	want := []string{"myterm", "--flag"}
	if runtime.GOOS == "darwin" {
		want = []string{"open", "-a", "myterm --flag", dir}
	}
	if !slices.Equal(cmd.Args, want) {
		t.Fatalf("args %q, want %q", cmd.Args, want)
	}
}
