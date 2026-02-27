package comms

import "testing"

// TestSilenceAndRestoreALSAHandler verifies that the ALSA error-handler
// wrappers can be called without panicking. This covers the CGo paths in
// alsa_silence.go that are never exercised during normal testing because
// PortAudio is not initialised.
func TestSilenceAndRestoreALSAHandler_DoNotPanic(t *testing.T) {
	silenceALSAProbeNoise()
	restoreALSAErrorHandler()
}
