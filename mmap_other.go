//go:build !darwin && !linux

package main

import (
	"errors"
	"os"
)

// mapFile is not supported here; readFile falls back to reading the file.
func mapFile(f *os.File, size int) (string, error) {
	return "", errors.New("mmap not supported")
}
