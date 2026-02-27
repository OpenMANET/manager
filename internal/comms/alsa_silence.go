//go:build !omd_omit_comms

package comms

// This file uses CGo to temporarily suppress ALSA diagnostic noise that is
// printed to stderr during portaudio.Initialize(). PortAudio's ALSA backend
// probes every virtual PCM alias defined in /usr/share/alsa/alsa.conf
// (cards.pcm.rear, cards.pcm.hdmi, etc.) while enumerating devices. When
// those aliases are absent from the active sound card's profile (e.g. the
// CM108 dongle only exposes a basic stereo device) ALSA prints "Unknown PCM"
// for every missing alias. These are probe failures, not real errors.
//
// silenceALSAProbeNoise() replaces the ALSA error handler with a no-op for
// the duration of PortAudio initialisation. restoreALSAErrorHandler() puts
// the default handler back so that genuine ALSA errors are still reported.

/*
#cgo LDFLAGS: -lasound
#include <alsa/asoundlib.h>
#include <stdarg.h>

// silentHandler discards all ALSA diagnostic messages.
static void silentHandler(const char *file, int line,
                          const char *function, int err,
                          const char *fmt, ...) {
    (void)file; (void)line; (void)function; (void)err; (void)fmt;
}

static void silenceALSAErrors(void) {
    snd_lib_error_set_handler(silentHandler);
}

// Passing NULL restores the built-in default handler.
static void restoreALSAErrors(void) {
    snd_lib_error_set_handler(NULL);
}
*/
import "C"

// silenceALSAProbeNoise replaces the ALSA error handler with a no-op.
// Call this immediately before portaudio.Initialize() and pair it with
// restoreALSAErrorHandler() immediately after.
func silenceALSAProbeNoise() {
	C.silenceALSAErrors()
}

// restoreALSAErrorHandler reinstates the default ALSA error handler so that
// genuine ALSA errors continue to be reported after PortAudio has finished
// its device enumeration.
func restoreALSAErrorHandler() {
	C.restoreALSAErrors()
}
