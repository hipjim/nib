package main

import (
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
)

// parallelFor calls fn on contiguous ranges that together cover [0, n), one
// range per core, and waits for them all. A range holds at least grain
// items, so a small n runs as a single range. The worker number lets fn
// keep results apart without locking.
//
// The workers may read a memory-mapped file that is truncated under them,
// which faults. A fault ends that worker and parallelFor returns false; the
// caller must then leave everything as it was. Any other panic is raised
// again on the calling goroutine, so bugs are not hidden.
func parallelFor(n, grain int, fn func(lo, hi, worker int)) bool {
	workers := max(1, min(runtime.NumCPU(), n/max(1, grain)))
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		faulted bool
		failure any
	)
	for w := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			debug.SetPanicOnFault(true)
			defer func() {
				if r := recover(); r != nil {
					mu.Lock()
					if isFault(r) {
						faulted = true
					} else if failure == nil {
						failure = r
					}
					mu.Unlock()
				}
			}()
			fn(w*n/workers, (w+1)*n/workers, w)
		}()
	}
	wg.Wait()
	if failure != nil {
		panic(failure)
	}
	return !faulted
}

// isFault reports whether a recovered panic is a memory fault, which a
// memory-mapped file truncated on disk causes.
func isFault(r any) bool {
	err, ok := r.(error)
	return ok && strings.Contains(err.Error(), "fault")
}
