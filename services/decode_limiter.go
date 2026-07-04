package services

import (
	"context"
	"os"
	"runtime"
	"strconv"
	"sync"
)

// Full image decoding turns a small compressed file into a large uncompressed RGBA
// buffer (a 4096x4096 image is ~67 MB). EnforceDecodeLimits caps a single decode, but
// N concurrent uploads still multiply that. This semaphore bounds how many decodes run
// at once so peak memory stays within the container limit under load.
var (
	decodeSemOnce sync.Once
	decodeSem     chan struct{}
)

func decodeSemaphore() chan struct{} {
	decodeSemOnce.Do(func() {
		n := 0
		if v := os.Getenv("MAX_CONCURRENT_DECODES"); v != "" {
			if parsed, err := strconv.Atoi(v); err == nil {
				n = parsed
			}
		}
		if n <= 0 {
			// Default derived from available parallelism (after automaxprocs has run,
			// since this initializes lazily on first upload, not at startup).
			n = runtime.GOMAXPROCS(0)
		}
		if n < 1 {
			n = 1
		}
		if n > 8 {
			n = 8
		}
		decodeSem = make(chan struct{}, n)
	})
	return decodeSem
}

// AcquireDecodeSlot blocks until a decode slot is free or ctx is done. It returns a
// release func that MUST be called when decoding completes. On ctx cancellation it
// returns ok=false and a no-op release.
func AcquireDecodeSlot(ctx context.Context) (release func(), ok bool) {
	sem := decodeSemaphore()
	select {
	case sem <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-sem }) }, true
	case <-ctx.Done():
		return func() {}, false
	}
}
