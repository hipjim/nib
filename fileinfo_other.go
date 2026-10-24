//go:build !darwin && !linux && !windows

package main

import (
	"os"
	"time"
)

// statTimes is not supported here.
func statTimes(path string, fi os.FileInfo) (created, accessed time.Time) { return }

func statOwner(fi os.FileInfo) string { return "" }
