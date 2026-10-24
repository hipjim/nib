package main

import (
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// statTimes returns when a file was created and last read, if known.
// Creation time needs statx and a file system that records it.
func statTimes(path string, fi os.FileInfo) (created, accessed time.Time) {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		accessed = time.Unix(st.Atim.Unix())
	}
	var sx unix.Statx_t
	if unix.Statx(unix.AT_FDCWD, path, unix.AT_SYMLINK_NOFOLLOW, unix.STATX_BTIME, &sx) == nil &&
		sx.Mask&unix.STATX_BTIME != 0 {
		created = time.Unix(sx.Btime.Sec, int64(sx.Btime.Nsec))
	}
	return
}

func statOwner(fi os.FileInfo) string {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return ownerName(st.Uid, st.Gid)
	}
	return ""
}
