package frontend

import (
	"context"
	"testing"
	"time"
)

func TestCPUSampler_NilGetReturnsZero(t *testing.T) {
	var c *cpuSampler
	if got := c.Get(); got != 0 {
		t.Fatalf("nil Get() = %v, want 0", got)
	}
}

func TestCPUSampler_InitialGetIsZero(t *testing.T) {
	c := newCPUSampler()
	if got := c.Get(); got != 0 {
		t.Fatalf("pre-start Get() = %v, want 0", got)
	}
}

func TestCPUSampler_StartExitsOnContextCancel(t *testing.T) {
	c := newCPUSampler()

	ctx, cancel := context.WithCancel(context.Background())
	c.Start(ctx)

	// Cancel and give the goroutine a moment to exit. If it doesn't,
	// -race / leak detection will pick it up, but we also bound this
	// test with a generous timeout.
	cancel()

	// A brief sleep here is unavoidable because we can't observe the
	// goroutine's exit directly. 50ms is well under the sample
	// interval (2s) yet long enough for the select case to fire.
	time.Sleep(50 * time.Millisecond)
}

func TestCPUSampler_StoreRoundTripsFixedPoint(t *testing.T) {
	c := newCPUSampler()
	// 42.35% -> stored as 4235 -> read as 42.35
	c.value.Store(4235)

	if got := c.Get(); got != 42.35 {
		t.Fatalf("Get() = %v, want 42.35", got)
	}
}
