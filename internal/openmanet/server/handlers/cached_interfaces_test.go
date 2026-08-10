package handlers_test

import (
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

type fakeCachedInterfaceInner struct {
	calls atomic.Int64
	infos []network.NetworkInterfaceInfo
	err   error
	delay time.Duration
}

func (f *fakeCachedInterfaceInner) ListAll() ([]network.NetworkInterfaceInfo, error) {
	f.calls.Add(1)

	if f.delay > 0 {
		time.Sleep(f.delay)
	}

	return f.infos, f.err
}

func TestCachedInterfaceProvider_ServesFromCacheWithinTTL(t *testing.T) {
	inner := &fakeCachedInterfaceInner{
		infos: []network.NetworkInterfaceInfo{{Name: "eth0"}, {Name: "wlh0"}},
	}
	p := handlers.NewCachedInterfaceProvider(inner, 500*time.Millisecond)

	got1, err := p.ListAll()
	require.NoError(t, err)
	assert.Len(t, got1, 2)

	got2, err := p.ListAll()
	require.NoError(t, err)
	assert.Equal(t, got1, got2)

	assert.Equal(t, int64(1), inner.calls.Load(),
		"inner provider should be called once while within TTL")
}

func TestCachedInterfaceProvider_RefetchesAfterTTL(t *testing.T) {
	inner := &fakeCachedInterfaceInner{
		infos: []network.NetworkInterfaceInfo{{Name: "eth0"}},
	}
	p := handlers.NewCachedInterfaceProvider(inner, 20*time.Millisecond)

	_, err := p.ListAll()
	require.NoError(t, err)

	time.Sleep(30 * time.Millisecond)

	_, err = p.ListAll()
	require.NoError(t, err)

	assert.Equal(t, int64(2), inner.calls.Load(),
		"inner provider should be re-fetched once TTL expires")
}

func TestCachedInterfaceProvider_PropagatesErrorsWithoutCaching(t *testing.T) {
	wantErr := errors.New("netlink down")
	inner := &fakeCachedInterfaceInner{err: wantErr}
	p := handlers.NewCachedInterfaceProvider(inner, 5*time.Second)

	_, err := p.ListAll()
	assert.ErrorIs(t, err, wantErr)

	_, err = p.ListAll()
	assert.ErrorIs(t, err, wantErr)

	assert.Equal(t, int64(2), inner.calls.Load(),
		"errors must not be cached so transient netlink failures don't poison the cache")
}

func TestCachedInterfaceProvider_CoalescesConcurrentRefreshes(t *testing.T) {
	inner := &fakeCachedInterfaceInner{
		infos: []network.NetworkInterfaceInfo{{Name: "eth0"}},
		delay: 50 * time.Millisecond,
	}
	p := handlers.NewCachedInterfaceProvider(inner, 5*time.Second)

	const workers = 10

	var wg sync.WaitGroup

	wg.Add(workers)

	for range workers {
		go func() {
			defer wg.Done()

			got, err := p.ListAll()
			assert.NoError(t, err)
			assert.Len(t, got, 1)
		}()
	}

	wg.Wait()

	assert.LessOrEqual(t, inner.calls.Load(), int64(3),
		"concurrent callers should coalesce onto a single in-flight netlink walk")
}

func TestNewCachedInterfaceProvider_DefaultTTL(t *testing.T) {
	p := handlers.NewCachedInterfaceProvider(&fakeCachedInterfaceInner{}, 0)
	assert.Equal(t, handlers.DefaultInterfaceCacheTTL, p.TTL)
}
