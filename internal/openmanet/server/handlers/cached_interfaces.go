package handlers

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/openmanet/openmanetd/internal/network"
)

// DefaultInterfaceCacheTTL is the staleness window for the network-interface
// enumeration cache. Kernel netlink state changes slowly relative to the
// frontend's per-page poll rate, so a small window collapses the
// LinkList + AddrList walk cost across concurrent RPC handlers.
const DefaultInterfaceCacheTTL = 5 * time.Second

// CachedInterfaceProvider wraps an inner InterfaceProvider (typically
// network.NetlinkInterfaceProvider) with a time-bounded cache.
// Dashboard.buildNetworkSummary and NetworkInterfaceService.ListNetworkInterfaces
// both call ListAll on their own polling cadences — each call walks every
// netlink link and issues an AddrList per link. Coalescing them through the
// cache lets the kernel see at most one enumeration per TTL.
//
// Reads are lock-free via an atomic pointer; refreshes are serialized by
// a mutex so at most one in-flight netlink walk exists at any time.
// Errors are not cached — transient netlink failures retry on the next
// call, matching the uncached provider's semantics.
type CachedInterfaceProvider struct {
	Inner     InterfaceProvider
	val       atomic.Pointer[cachedInterfacesEntry]
	TTL       time.Duration
	refreshMu sync.Mutex
}

type cachedInterfacesEntry struct {
	at    time.Time
	infos []network.NetworkInterfaceInfo
}

// NewCachedInterfaceProvider wraps inner with a TTL-bounded cache. If ttl
// is <= 0, DefaultInterfaceCacheTTL is used.
func NewCachedInterfaceProvider(inner InterfaceProvider, ttl time.Duration) *CachedInterfaceProvider {
	if ttl <= 0 {
		ttl = DefaultInterfaceCacheTTL
	}

	return &CachedInterfaceProvider{Inner: inner, TTL: ttl}
}

// ListAll returns cached interface info when the entry is within TTL,
// otherwise re-fetches via the inner provider. Concurrent callers
// coalesce onto a single in-flight fetch.
func (p *CachedInterfaceProvider) ListAll() ([]network.NetworkInterfaceInfo, error) {
	if entry := p.val.Load(); entry != nil && time.Since(entry.at) < p.TTL {
		return entry.infos, nil
	}

	p.refreshMu.Lock()
	defer p.refreshMu.Unlock()

	if entry := p.val.Load(); entry != nil && time.Since(entry.at) < p.TTL {
		return entry.infos, nil
	}

	infos, err := p.Inner.ListAll()
	if err != nil {
		return nil, err
	}

	p.val.Store(&cachedInterfacesEntry{at: time.Now(), infos: infos})

	return infos, nil
}
