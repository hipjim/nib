package main

import (
	"os"
	"syscall"
	"time"
)

// statTimes returns when a file was created and last read, if known.
func statTimes(path string, fi os.FileInfo) (created, accessed time.Time) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return
	}
	return time.Unix(st.Birthtimespec.Unix()), time.Unix(st.Atimespec.Unix())
}

func statOwner(fi os.FileInfo) string {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return ownerName(st.Uid, st.Gid)
	}
	return ""
}
