package handlers

import (
	"sync"

	"github.com/openmanet/openmanetd/internal/system"
	"github.com/openmanet/openmanetd/internal/util/board"
)

// Board and firmware metadata are effectively immutable during the
// daemon's lifetime — /etc/board.json is written once by the OS image
// and /etc/openwrt_release changes only on reflash. The uncached paths
// re-open and re-parse those files on every GetDashboardStatus request.
//
// The providers in this file wrap the default implementations and
// memoize the first successful result. On error they fall through to
// the inner provider every call so a file that only appears after
// start-up can still be picked up — this preserves the existing
// best-effort semantics callers rely on.

// CachedBoardProvider caches a successful Board read from the inner
// provider. It is safe for concurrent use.
type CachedBoardProvider struct {
	Inner  BoardProvider
	cached *board.Board
	mu     sync.Mutex
}

// NewCachedBoardProvider wraps inner with a successful-read memo.
func NewCachedBoardProvider(inner BoardProvider) *CachedBoardProvider {
	return &CachedBoardProvider{Inner: inner}
}

// GetBoard returns the cached board configuration, or delegates to the
// inner provider and memoizes the first success.
func (p *CachedBoardProvider) GetBoard() (*board.Board, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cached != nil {
		return p.cached, nil
	}

	b, err := p.Inner.GetBoard()
	if err != nil {
		return nil, err
	}

	p.cached = b

	return b, nil
}

// CachedFirmwareProvider caches a successful FirmwareInfo read from the
// inner provider. It is safe for concurrent use.
type CachedFirmwareProvider struct {
	Inner  system.FirmwareProvider
	cached *system.FirmwareInfo
	mu     sync.Mutex
}

// NewCachedFirmwareProvider wraps inner with a successful-read memo.
func NewCachedFirmwareProvider(inner system.FirmwareProvider) *CachedFirmwareProvider {
	return &CachedFirmwareProvider{Inner: inner}
}

// GetFirmwareInfo returns the cached firmware info, or delegates to the
// inner provider and memoizes the first success.
func (p *CachedFirmwareProvider) GetFirmwareInfo() (*system.FirmwareInfo, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cached != nil {
		return p.cached, nil
	}

	fw, err := p.Inner.GetFirmwareInfo()
	if err != nil {
		return nil, err
	}

	p.cached = fw

	return fw, nil
}
