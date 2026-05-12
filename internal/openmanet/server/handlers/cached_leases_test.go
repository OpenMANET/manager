package handlers_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openmanet/openmanetd/internal/network"
	"github.com/openmanet/openmanetd/internal/openmanet/server/handlers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeCachedLeaseInner struct {
	calls atomic.Int64
	mu    sync.Mutex
	resp  *network.DHCPLeasesResponse
	err   error
	delay time.Duration
}

func (f *fakeCachedLeaseInner) GetCurrentDHCPLeases(_ context.Context) (*network.DHCPLeasesResponse, error) {
	f.calls.Add(1)

	if f.delay > 0 {
		time.Sleep(f.delay)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	return f.resp, f.err
}

func TestCachedLeaseProvider_ServesFromCacheWithinTTL(t *testing.T) {
	inner := &fakeCachedLeaseInner{resp: &network.DHCPLeasesResponse{}}
	p := handlers.NewCachedLeaseProvider(inner, 500*time.Millisecond)

	got1, err := p.GetCurrentDHCPLeases(context.Background())
	require.NoError(t, err)
	assert.Same(t, inner.resp, got1)

	got2, err := p.GetCurrentDHCPLeases(context.Background())
	require.NoError(t, err)
	assert.Same(t, inner.resp, got2)

	assert.Equal(t, int64(1), inner.calls.Load(),
		"inner provider should be called once while within TTL")
}

func TestCachedLeaseProvider_RefetchesAfterTTL(t *testing.T) {
	inner := &fakeCachedLeaseInner{resp: &network.DHCPLeasesResponse{}}
	p := handlers.NewCachedLeaseProvider(inner, 20*time.Millisecond)

	_, err := p.GetCurrentDHCPLeases(context.Background())
	require.NoError(t, err)

	time.Sleep(30 * time.Millisecond)

	_, err = p.GetCurrentDHCPLeases(context.Background())
	require.NoError(t, err)

	assert.Equal(t, int64(2), inner.calls.Load(),
		"inner provider should be called again after TTL expires")
}

func TestCachedLeaseProvider_PropagatesErrorsWithoutCaching(t *testing.T) {
	wantErr := errors.New("ubus failed")
	inner := &fakeCachedLeaseInner{err: wantErr}
	p := handlers.NewCachedLeaseProvider(inner, 5*time.Second)

	_, err := p.GetCurrentDHCPLeases(context.Background())
	assert.ErrorIs(t, err, wantErr)

	_, err = p.GetCurrentDHCPLeases(context.Background())
	assert.ErrorIs(t, err, wantErr)

	assert.Equal(t, int64(2), inner.calls.Load(),
		"errors must not be cached so transient ubus failures don't poison the cache")
}

func TestCachedLeaseProvider_CoalescesConcurrentRefreshes(t *testing.T) {
	inner := &fakeCachedLeaseInner{
		resp:  &network.DHCPLeasesResponse{},
		delay: 50 * time.Millisecond,
	}
	p := handlers.NewCachedLeaseProvider(inner, 5*time.Second)

	const workers = 10

	var wg sync.WaitGroup

	wg.Add(workers)

	for range workers {
		go func() {
			defer wg.Done()

			resp, err := p.GetCurrentDHCPLeases(context.Background())
			assert.NoError(t, err)
			assert.NotNil(t, resp)
		}()
	}

	wg.Wait()

	// The refresh mutex serializes fetches, but since all 10 workers
	// arrive before the first fetch completes, at most a handful of
	// them will still observe a stale cache on entry. In practice the
	// value hovers around 1–2; anything above 3 indicates the
	// coalescing guard is broken.
	assert.LessOrEqual(t, inner.calls.Load(), int64(3),
		"concurrent callers should coalesce onto a single in-flight fetch")
}

func TestNewCachedLeaseProvider_DefaultTTL(t *testing.T) {
	p := handlers.NewCachedLeaseProvider(&fakeCachedLeaseInner{}, 0)
	assert.Equal(t, handlers.DefaultDHCPLeaseCacheTTL, p.TTL)
}
