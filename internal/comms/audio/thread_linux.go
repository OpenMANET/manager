//go:build linux

package audio

import (
	"errors"

	"github.com/rs/zerolog"
	"golang.org/x/sys/unix"
)

// audioThreadPriority is the SCHED_FIFO priority we request for the
// PortAudio capture and playback callback threads. Conventional audio
// thread priorities on Linux fall in the 40-80 range; 50 is a mid-range
// value that sits well above standard kernel threads (priority 0 under
// SCHED_OTHER is not comparable, but SCHED_FIFO 50 is well above the
// typical 50 used by softirq worker threads and below the ~80-99 range
// often reserved for kernel real-time threads like watchdog).
//
// The goal is not to starve anything — our audio callbacks are trivial
// (copy one int16 frame into a pooled buffer, non-blocking channel
// send) and complete in well under a millisecond. The elevation simply
// ensures the kernel does not preempt the audio thread for tens of
// milliseconds in favor of ordinary Go goroutines running other work
// on the same CPU.
const audioThreadPriority uint32 = 50

// elevateAudioThread is the package-level function pointer used to
// elevate the PortAudio callback thread to SCHED_FIFO. It is a variable
// (not a plain function call) so that tests can substitute a fake
// elevator that records invocations without touching the real
// sched_setattr syscall — which would require CAP_SYS_NICE in CI.
//
// The default implementation is realElevateAudioThread below on Linux
// and noopElevateAudioThread in thread_other.go on every other OS.
//
//nolint:gochecknoglobals // test injection point; see swapElevator in encoder_test.go
var elevateAudioThread = realElevateAudioThread

// realElevateAudioThread applies SCHED_FIFO (priority audioThreadPriority)
// to the current OS thread via the sched_setattr(2) syscall. It is
// designed to be called once from the first invocation of a PortAudio
// capture or playback callback, via a sync.Once guard at the call site.
//
// Why pid 0: unix.SchedSetAttr with pid 0 targets the calling thread,
// which inside a cgo-exported Go function is the C thread that PortAudio
// created to run the audio callback. Because gordonklaus/portaudio uses
// //export streamCallback (direct cgocallback, no Go-side channel
// indirection), the Go function runs on the same OS thread that
// PortAudio's internal audio thread is driving. Elevating pid 0
// therefore elevates the real audio thread, and the elevation sticks
// across subsequent callback invocations from the same C thread.
//
// Graceful failure: sched_setattr returns EPERM when the caller lacks
// CAP_SYS_NICE (e.g. dev containers, unprivileged deployments). We log
// once at Warn and return without an error — the audio pipeline
// continues to run on SCHED_OTHER, which was the prior behavior. The
// log line carries the label ("capture" or "playback") so operators
// can tell which callback failed.
func realElevateAudioThread(log zerolog.Logger, label string) {
	attr := &unix.SchedAttr{
		Policy:   unix.SCHED_FIFO,
		Priority: audioThreadPriority,
	}
	if err := unix.SchedSetAttr(0, attr, 0); err != nil {
		switch {
		case errors.Is(err, unix.EPERM):
			log.Warn().
				Str("callback", label).
				Uint32("requested_priority", audioThreadPriority).
				Msg("comms: failed to elevate audio thread to SCHED_FIFO " +
					"(missing CAP_SYS_NICE); callback will run on SCHED_OTHER — " +
					"expect capture/playback jitter under CPU load")
		default:
			log.Warn().
				Err(err).
				Str("callback", label).
				Msg("comms: failed to elevate audio thread to SCHED_FIFO")
		}

		return
	}

	log.Debug().
		Str("callback", label).
		Uint32("priority", audioThreadPriority).
		Msg("comms: audio callback thread elevated to SCHED_FIFO")
}
