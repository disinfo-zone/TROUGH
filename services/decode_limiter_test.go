package services

import (
	"context"
	"testing"
	"time"
)

func TestAcquireDecodeSlotReleases(t *testing.T) {
	// Acquire and release several times; a leaked slot would eventually block.
	for i := 0; i < 32; i++ {
		release, ok := AcquireDecodeSlot(context.Background())
		if !ok {
			t.Fatalf("expected to acquire a decode slot")
		}
		release()
		release() // release must be idempotent
	}
}

func TestAcquireDecodeSlotRespectsContext(t *testing.T) {
	// Saturate the semaphore, then confirm a cancelled context returns quickly.
	n := cap(decodeSemaphore())
	held := make([]func(), 0, n)
	for i := 0; i < n; i++ {
		release, ok := AcquireDecodeSlot(context.Background())
		if !ok {
			t.Fatalf("expected to acquire slot %d", i)
		}
		held = append(held, release)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, ok := AcquireDecodeSlot(ctx); ok {
		t.Fatalf("expected acquisition to fail when semaphore is saturated and ctx expires")
	}
	for _, release := range held {
		release()
	}
}
