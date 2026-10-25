package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// macTerminals maps $TERM_PROGRAM to the app to open, so quod opens the
// terminal it was started from.
var macTerminals = map[string]string{
	"Apple_Terminal": "Terminal",
	"iTerm.app":      "iTerm",
	"ghostty":        "Ghostty",
	"WezTerm":        "WezTerm",
	"WarpTerminal":   "Warp",
}

// linuxTerminals are tried in order when $TERMINAL is not set.
var linuxTerminals = []string{
	"x-terminal-emulator", "gnome-terminal", "konsole", "xfce4-terminal",
	"ptyxis", "kgx", "alacritty", "kitty", "foot", "wezterm", "xterm",
}

// terminalCmd returns the command that opens a terminal window in dir.
// $QUOD_TERMINAL overrides the choice: an app name on macOS, a command
// elsewhere.
func terminalCmd(dir string) (*exec.Cmd, error) {
	custom := os.Getenv("QUOD_TERMINAL")
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		app := custom
		if app == "" {
			app = macTerminals[os.Getenv("TERM_PROGRAM")]
		}
		if app == "" {
			app = "Terminal"
		}
		cmd = exec.Command("open", "-a", app, dir)
	case "windows":
		switch {
		case custom != "":
			f := strings.Fields(custom)
			cmd = exec.Command(f[0], f[1:]...)
		case lookPath("wt"):
			cmd = exec.Command("wt", "-d", dir)
		default:
			cmd = exec.Command("cmd", "/c", "start", "cmd")
		}
	default:
		name := custom
		if name == "" {
			name = os.Getenv("TERMINAL")
		}
		for _, t := range linuxTerminals {
			if name != "" {
				break
			}
			if lookPath(t) {
				name = t
			}
		}
		if name == "" {
			return nil, errors.New("no terminal found; set $QUOD_TERMINAL")
		}
		f := strings.Fields(name)
		cmd = exec.Command(f[0], f[1:]...)
	}
	cmd.Dir = dir // the new shell starts here
	return cmd, nil
}

func lookPath(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// openTerminal opens a terminal in the folder of the current file, or the
// folder shown, or the root folder.
func (e *Editor) openTerminal() {
	dir := e.root
	switch {
	case e.dir != nil:
		dir = e.path
	case e.path != "":
		dir = filepath.Dir(e.path)
	}
	cmd, err := terminalCmd(dir)
	if err == nil {
		err = cmd.Start()
	}
	if err != nil {
		e.flash("terminal: "+err.Error(), true)
		return
	}
	go cmd.Wait()
	e.flash("terminal opened in "+shortPath(dir), false)
}
