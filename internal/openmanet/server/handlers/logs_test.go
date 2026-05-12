package handlers_test

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	logsv1 "github.com/openmanet/openmanetd/internal/api/openmanet/logs/v1"
	"github.com/openmanet/openmanetd/internal/logs"
	"github.com/openmanet/openmanetd/internal/openmanet/server/handlers"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeLogProvider is a hand-written fake implementing logs.LogProvider.
// Set snap/err to control return values; call counters are mutex-guarded.
type fakeLogProvider struct {
	mu    sync.Mutex
	calls int
	snap  *logs.Snapshot
	err   error
}

func (f *fakeLogProvider) Fetch(_ context.Context, _ uint32) (*logs.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls++

	return f.snap, f.err
}

func (f *fakeLogProvider) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.calls
}

func newLogsService(logread, dmesg logs.LogProvider) *handlers.LogsService {
	return &handlers.LogsService{
		Log:     zerolog.Nop(),
		Logread: logread,
		Dmesg:   dmesg,
	}
}

func snapshotOf(lines []string, truncated bool) *logs.Snapshot {
	return &logs.Snapshot{
		CollectedAt: time.Date(2026, 4, 25, 14, 30, 0, 0, time.UTC),
		Lines:       lines,
		Truncated:   truncated,
	}
}

func TestLogsService_GetLogs_Logread(t *testing.T) {
	t.Parallel()

	logread := &fakeLogProvider{snap: snapshotOf([]string{"a", "b", "c"}, false)}
	dmesg := &fakeLogProvider{}

	svc := newLogsService(logread, dmesg)

	resp, err := svc.GetLogs(t.Context(), &logsv1.GetLogsRequest{
		Source:   logsv1.LogSource_LOG_SOURCE_LOGREAD,
		MaxLines: 100,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	require.Len(t, resp.Lines, 3)
	assert.Equal(t, "a", resp.Lines[0].Raw)
	assert.Equal(t, "c", resp.Lines[2].Raw)
	assert.False(t, resp.Truncated)
	assert.NotNil(t, resp.CollectedAt)
	assert.Equal(t, 1, logread.Calls())
	assert.Equal(t, 0, dmesg.Calls(), "dmesg provider should not be called for logread source")
}

func TestLogsService_GetLogs_Dmesg(t *testing.T) {
	t.Parallel()

	logread := &fakeLogProvider{}
	dmesg := &fakeLogProvider{snap: snapshotOf([]string{"kern1", "kern2"}, true)}

	svc := newLogsService(logread, dmesg)

	resp, err := svc.GetLogs(t.Context(), &logsv1.GetLogsRequest{
		Source:   logsv1.LogSource_LOG_SOURCE_DMESG,
		MaxLines: 100,
	})
	require.NoError(t, err)

	require.Len(t, resp.Lines, 2)
	assert.Equal(t, "kern1", resp.Lines[0].Raw)
	assert.True(t, resp.Truncated)
	assert.Equal(t, 0, logread.Calls())
	assert.Equal(t, 1, dmesg.Calls())
}

func TestLogsService_GetLogs_UnspecifiedSource(t *testing.T) {
	t.Parallel()

	svc := newLogsService(&fakeLogProvider{}, &fakeLogProvider{})

	_, err := svc.GetLogs(t.Context(), &logsv1.GetLogsRequest{
		Source:   logsv1.LogSource_LOG_SOURCE_UNSPECIFIED,
		MaxLines: 100,
	})

	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeInvalidArgument, connectErr.Code())
}

func TestLogsService_GetLogs_NilProviderReturnsFailedPrecondition(t *testing.T) {
	t.Parallel()

	// Logread provider is nil — handler should not panic and should
	// surface a clear FailedPrecondition.
	svc := newLogsService(nil, &fakeLogProvider{})

	_, err := svc.GetLogs(t.Context(), &logsv1.GetLogsRequest{
		Source:   logsv1.LogSource_LOG_SOURCE_LOGREAD,
		MaxLines: 100,
	})

	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeFailedPrecondition, connectErr.Code())
	assert.Contains(t, connectErr.Message(), "logread")
}

func TestLogsService_GetLogs_BinaryMissingReturnsFailedPrecondition(t *testing.T) {
	t.Parallel()

	logread := &fakeLogProvider{err: &exec.Error{Name: "logread", Err: exec.ErrNotFound}}
	svc := newLogsService(logread, &fakeLogProvider{})

	_, err := svc.GetLogs(t.Context(), &logsv1.GetLogsRequest{
		Source:   logsv1.LogSource_LOG_SOURCE_LOGREAD,
		MaxLines: 100,
	})

	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeFailedPrecondition, connectErr.Code())
	assert.Contains(t, connectErr.Message(), "logread")
}

func TestLogsService_GetLogs_PermissionDeniedReturnsFailedPrecondition(t *testing.T) {
	t.Parallel()

	dmesg := &fakeLogProvider{err: fmt.Errorf("dmesg: %w", logs.ErrPermissionDenied)}
	svc := newLogsService(&fakeLogProvider{}, dmesg)

	_, err := svc.GetLogs(t.Context(), &logsv1.GetLogsRequest{
		Source:   logsv1.LogSource_LOG_SOURCE_DMESG,
		MaxLines: 100,
	})

	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeFailedPrecondition, connectErr.Code())
	assert.Contains(t, connectErr.Message(), "elevated privileges")
}

func TestLogsService_GetLogs_OtherErrorReturnsInternal(t *testing.T) {
	t.Parallel()

	logread := &fakeLogProvider{err: errors.New("boom")}
	svc := newLogsService(logread, &fakeLogProvider{})

	_, err := svc.GetLogs(t.Context(), &logsv1.GetLogsRequest{
		Source:   logsv1.LogSource_LOG_SOURCE_LOGREAD,
		MaxLines: 100,
	})

	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeInternal, connectErr.Code())
}

func TestLogsService_GetLogs_EmptySnapshot(t *testing.T) {
	t.Parallel()

	logread := &fakeLogProvider{snap: snapshotOf(nil, false)}
	svc := newLogsService(logread, &fakeLogProvider{})

	resp, err := svc.GetLogs(t.Context(), &logsv1.GetLogsRequest{
		Source:   logsv1.LogSource_LOG_SOURCE_LOGREAD,
		MaxLines: 100,
	})
	require.NoError(t, err)
	assert.Empty(t, resp.Lines)
	assert.False(t, resp.Truncated)
}

// blockingLogProvider blocks Fetch until the context is canceled, then
// returns the wrapped ctx error. This proves the handler propagates its
// timeout context to the provider rather than holding indefinitely.
type blockingLogProvider struct {
	mu      sync.Mutex
	calls   int
	gotErr  error
	entered chan struct{} // closed once Fetch is actually blocked on ctx
	enterOK bool
}

func (b *blockingLogProvider) Fetch(ctx context.Context, _ uint32) (*logs.Snapshot, error) {
	b.mu.Lock()
	b.calls++

	if !b.enterOK {
		b.enterOK = true
		entered := b.entered
		b.mu.Unlock()

		if entered != nil {
			close(entered)
		}
	} else {
		b.mu.Unlock()
	}

	<-ctx.Done()

	err := fmt.Errorf("blocking provider observed ctx done: %w", ctx.Err())

	b.mu.Lock()
	b.gotErr = err
	b.mu.Unlock()

	return nil, err
}

func TestLogsService_GetLogs_PropagatesContextCancel(t *testing.T) {
	t.Parallel()

	prov := &blockingLogProvider{entered: make(chan struct{})}
	svc := newLogsService(prov, &fakeLogProvider{})

	ctx, cancel := context.WithCancel(t.Context())

	errCh := make(chan error, 1)

	go func() {
		_, err := svc.GetLogs(ctx, &logsv1.GetLogsRequest{
			Source:   logsv1.LogSource_LOG_SOURCE_LOGREAD,
			MaxLines: 100,
		})
		errCh <- err
	}()

	// Wait until the provider is actually blocked inside Fetch before we
	// cancel — otherwise the cancel could race with the goroutine starting.
	select {
	case <-prov.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("provider Fetch was not entered within 2s")
	}

	cancel()

	select {
	case err := <-errCh:
		require.Error(t, err)

		var connectErr *connect.Error
		require.ErrorAs(t, err, &connectErr)
		assert.Equal(t, connect.CodeInternal, connectErr.Code())
	case <-time.After(2 * time.Second):
		t.Fatal("GetLogs did not return within 2s after parent context cancel")
	}

	prov.mu.Lock()
	defer prov.mu.Unlock()

	require.NotNil(t, prov.gotErr, "provider should have observed ctx done")
	assert.ErrorIs(t, prov.gotErr, context.Canceled)
}
