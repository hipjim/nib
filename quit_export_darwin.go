package main

import "C"

// nibMayQuit is called by the app delegate when the app is asked to quit.
// It lives in its own file: cgo allows no C definitions next to an export.
//
//export nibMayQuit
func nibMayQuit() C.int {
	if mayQuit() {
		return 1
	}
	return 0
}
