package handlers

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/openmanet/openmanetd/internal/network"
)

// DefaultDHCPLeaseCacheTTL is the staleness window for the DHCP lease
// cache. Matches the "~5s max staleness" target from the load-reduction
// plan: DHCP lease churn is slow relative to frontend polling, so a
// handful of seconds between ubus invocations is imperceptible to the
// operator.
const DefaultDHCPLeaseCacheTTL = 5 * time.Second

// CachedLeaseProvider wraps an inner LeaseProvider with a time-bounded
// in-memory cache. The default LeaseProvider shells out to
// `ubus call dnsmasq.leases get_leases` on every call and is invoked
// by both NetworkInterfaceService and WifiConfigService on their own
// polling cadences — on a busy device that's one ubus exec per poll
// from each handler, all returning identical data.
//
// The cache fast-paths reads via an atomic pointer (no lock) and
// serializes refreshes with a mutex so there is at most one in-flight
// ubus invocation at any time. If a refresh fails, the caller receives
// the underlying error; the stale cached value is not returned so error
// semantics match the uncached provider.
type CachedLeaseProvider struct {
	Inner     LeaseProvider
	val       atomic.Pointer[cachedLeasesEntry]
	TTL       time.Duration
	refreshMu sync.Mutex
}

type cachedLeasesEntry struct {
	at   time.Time
	resp *network.DHCPLeasesResponse
}

// NewCachedLeaseProvider wraps inner with a TTL-bounded cache. If ttl
// is <= 0, DefaultDHCPLeaseCacheTTL is used.
func NewCachedLeaseProvider(inner LeaseProvider, ttl time.Duration) *CachedLeaseProvider {
	if ttl <= 0 {
		ttl = DefaultDHCPLeaseCacheTTL
	}

	return &CachedLeaseProvider{Inner: inner, TTL: ttl}
}

// GetCurrentDHCPLeases returns cached leases when the cached entry is
// within TTL, otherwise re-fetches via the inner provider. Concurrent
// callers coalesce onto a single in-flight fetch.
func (p *CachedLeaseProvider) GetCurrentDHCPLeases(ctx context.Context) (*network.DHCPLeasesResponse, error) {
	if entry := p.val.Load(); entry != nil && time.Since(entry.at) < p.TTL {
		return entry.resp, nil
	}

	p.refreshMu.Lock()
	defer p.refreshMu.Unlock()

	// Re-check under the refresh lock: a concurrent caller may have
	// just populated the cache.
	if entry := p.val.Load(); entry != nil && time.Since(entry.at) < p.TTL {
		return entry.resp, nil
	}

	resp, err := p.Inner.GetCurrentDHCPLeases(ctx)
	if err != nil {
		return nil, err
	}

	p.val.Store(&cachedLeasesEntry{at: time.Now(), resp: resp})

	return resp, nil
}
