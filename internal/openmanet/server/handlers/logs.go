package handlers

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"connectrpc.com/connect"
	logsv1 "github.com/openmanet/openmanetd/internal/api/openmanet/logs/v1"
	"github.com/openmanet/openmanetd/internal/logs"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// logsFetchTimeout caps how long a single GetLogs RPC will wait on the
// underlying busybox applet. A wedged dmesg on a broken kernel would
// otherwise pin an HTTP worker until the client disconnects.
const logsFetchTimeout = 10 * time.Second

// LogsService serves read-only views of the OpenWRT system log and kernel
// ring buffer. The handler owns two providers — one per source — and
// dispatches on the request's LogSource enum.
type LogsService struct {
	Log     zerolog.Logger
	Logread logs.LogProvider
	Dmesg   logs.LogProvider
}

// GetLogs returns the most-recent lines from the selected source. When
// the underlying binary is absent (e.g. the dev container lacks busybox
// logread/dmesg) the handler returns CodeFailedPrecondition so the UI
// can render a clear "source unavailable" message. All other exec
// failures return CodeInternal.
func (s *LogsService) GetLogs(ctx context.Context, req *logsv1.GetLogsRequest) (*logsv1.GetLogsResponse, error) {
	var (
		provider   logs.LogProvider
		sourceName string
	)

	switch req.GetSource() {
	case logsv1.LogSource_LOG_SOURCE_LOGREAD:
		provider = s.Logread
		sourceName = "logread"
	case logsv1.LogSource_LOG_SOURCE_DMESG:
		provider = s.Dmesg
		sourceName = "dmesg"
	default:
		// The proto-validate interceptor rejects UNSPECIFIED and unknown
		// values before reaching this handler; this branch is a defense
		// in depth against future enum additions without handler updates.
		return nil, connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("unsupported log source: %s", req.GetSource()))
	}

	if provider == nil {
		s.Log.Warn().Str("source", sourceName).Msg("log provider not configured")

		return nil, connect.NewError(connect.CodeFailedPrecondition,
			fmt.Errorf("%s provider not configured on this host", sourceName))
	}

	fetchCtx, cancel := context.WithTimeout(ctx, logsFetchTimeout)
	defer cancel()

	snap, err := provider.Fetch(fetchCtx, req.GetMaxLines())
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			s.Log.Warn().Str("source", sourceName).Err(err).Msg("log source binary unavailable")

			return nil, connect.NewError(connect.CodeFailedPrecondition,
				fmt.Errorf("%s binary unavailable on this host", sourceName))
		}

		if errors.Is(err, logs.ErrPermissionDenied) {
			s.Log.Warn().Str("source", sourceName).Err(err).Msg("log source permission denied")

			return nil, connect.NewError(connect.CodeFailedPrecondition,
				fmt.Errorf("%s requires elevated privileges on this host", sourceName))
		}

		s.Log.Error().Str("source", sourceName).Err(err).Msg("fetch logs failed")

		return nil, connect.NewError(connect.CodeInternal, err)
	}

	resp := &logsv1.GetLogsResponse{
		Lines:       make([]*logsv1.LogLine, 0, len(snap.Lines)),
		CollectedAt: timestamppb.New(snap.CollectedAt),
		Truncated:   snap.Truncated,
	}

	for _, line := range snap.Lines {
		resp.Lines = append(resp.Lines, &logsv1.LogLine{Raw: line})
	}

	return resp, nil
}
