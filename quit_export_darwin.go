package main

import "C"

// quodMayQuit is called by the app delegate when the app is asked to quit.
// It lives in its own file: cgo allows no C definitions next to an export.
//
//export quodMayQuit
func quodMayQuit() C.int {
	if mayQuit() {
		return 1
	}
	return 0
}
