package main

import "testing"

func TestQuitHooked(t *testing.T) {
	if !quitHooked() {
		t.Fatal("applicationShouldTerminate: was not added to GioAppDelegate")
	}
}
