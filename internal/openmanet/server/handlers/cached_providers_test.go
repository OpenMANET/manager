package handlers_test

import (
	"errors"
	"sync/atomic"
	"testing"

	"github.com/openmanet/openmanetd/internal/openmanet/server/handlers"
	"github.com/openmanet/openmanetd/internal/system"
	"github.com/openmanet/openmanetd/internal/util/board"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeBoardProvider struct {
	calls atomic.Int64
	board *board.Board
	err   error
}

func (f *fakeBoardProvider) GetBoard() (*board.Board, error) {
	f.calls.Add(1)

	return f.board, f.err
}

func TestCachedBoardProvider_MemoizesFirstSuccess(t *testing.T) {
	inner := &fakeBoardProvider{
		board: &board.Board{},
	}
	p := handlers.NewCachedBoardProvider(inner)

	got1, err1 := p.GetBoard()
	require.NoError(t, err1)
	assert.Same(t, inner.board, got1)

	got2, err2 := p.GetBoard()
	require.NoError(t, err2)
	assert.Same(t, inner.board, got2)

	// Inner was called exactly once even across two GetBoard() calls.
	assert.Equal(t, int64(1), inner.calls.Load())
}

func TestCachedBoardProvider_RetriesOnError(t *testing.T) {
	wantErr := errors.New("missing board.json")
	inner := &fakeBoardProvider{err: wantErr}
	p := handlers.NewCachedBoardProvider(inner)

	_, err := p.GetBoard()
	assert.ErrorIs(t, err, wantErr)

	_, err = p.GetBoard()
	assert.ErrorIs(t, err, wantErr)

	// Errors must not be memoized — each call retries so a
	// late-appearing board.json can be picked up.
	assert.Equal(t, int64(2), inner.calls.Load())

	// Once the inner starts returning a value, that value is cached
	// and subsequent calls stop hitting the inner.
	inner.board = &board.Board{}
	inner.err = nil

	got, err := p.GetBoard()
	require.NoError(t, err)
	assert.Same(t, inner.board, got)

	got2, err2 := p.GetBoard()
	require.NoError(t, err2)
	assert.Same(t, got, got2)

	// One additional inner call captured the success; the next call was served from the cache.
	assert.Equal(t, int64(3), inner.calls.Load())
}

type fakeFirmwareProvider struct {
	calls atomic.Int64
	info  *system.FirmwareInfo
	err   error
}

func (f *fakeFirmwareProvider) GetFirmwareInfo() (*system.FirmwareInfo, error) {
	f.calls.Add(1)

	return f.info, f.err
}

func TestCachedFirmwareProvider_MemoizesFirstSuccess(t *testing.T) {
	inner := &fakeFirmwareProvider{
		info: &system.FirmwareInfo{Description: "OpenWrt 23.05.3"},
	}
	p := handlers.NewCachedFirmwareProvider(inner)

	got1, err1 := p.GetFirmwareInfo()
	require.NoError(t, err1)
	assert.Equal(t, "OpenWrt 23.05.3", got1.Description)

	got2, err2 := p.GetFirmwareInfo()
	require.NoError(t, err2)
	assert.Same(t, got1, got2)

	assert.Equal(t, int64(1), inner.calls.Load())
}

func TestCachedFirmwareProvider_RetriesOnError(t *testing.T) {
	wantErr := errors.New("no openwrt_release")
	inner := &fakeFirmwareProvider{err: wantErr}
	p := handlers.NewCachedFirmwareProvider(inner)

	_, err := p.GetFirmwareInfo()
	assert.ErrorIs(t, err, wantErr)

	_, err = p.GetFirmwareInfo()
	assert.ErrorIs(t, err, wantErr)

	assert.Equal(t, int64(2), inner.calls.Load())
}
