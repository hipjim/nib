//go:build darwin || linux

package main

import (
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

// mapFile maps f read-only. The mapping is never unmapped: lines and undo
// history keep pointing into it for as long as the process runs.
func mapFile(f *os.File, size int) (string, error) {
	data, err := unix.Mmap(int(f.Fd()), 0, size, unix.PROT_READ, unix.MAP_PRIVATE)
	if err != nil {
		return "", err
	}
	// Read ahead in large chunks: page faults one page at a time are 4x
	// slower. WILLNEED blocks while it queues reads, so keep it off the
	// path to the first screen.
	unix.Madvise(data, unix.MADV_SEQUENTIAL)
	go unix.Madvise(data, unix.MADV_WILLNEED)
	return unsafe.String(&data[0], size), nil
}
