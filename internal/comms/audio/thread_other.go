//go:build !linux

package audio

import "github.com/rs/zerolog"

// elevateAudioThread on non-Linux builds is a no-op. The only production
// target is Linux (per CLAUDE.md — linux/amd64, linux/arm64, linux/mipsle).
// macOS and Windows are only used as developer environments and do not
// have a direct SCHED_FIFO equivalent exposed through golang.org/x/sys.
//
//nolint:gochecknoglobals // test injection point; mirrors thread_linux.go
var elevateAudioThread = noopElevateAudioThread

func noopElevateAudioThread(log zerolog.Logger, label string) {
	log.Debug().
		Str("callback", label).
		Msg("comms: audio thread elevation is a no-op on this OS")
}
