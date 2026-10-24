package main

import (
	"os"
	"syscall"
	"time"
)

// statTimes returns when a file was created and last read, if known.
func statTimes(path string, fi os.FileInfo) (created, accessed time.Time) {
	if d, ok := fi.Sys().(*syscall.Win32FileAttributeData); ok {
		return time.Unix(0, d.CreationTime.Nanoseconds()), time.Unix(0, d.LastAccessTime.Nanoseconds())
	}
	return
}

func statOwner(fi os.FileInfo) string { return "" }
