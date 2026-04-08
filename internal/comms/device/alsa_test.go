package device

import "testing"

// TestSilenceAndRestoreALSAHandler verifies that the ALSA error-handler
// wrappers can be called without panicking. This covers the CGo paths in
// alsa_silence.go that are never exercised during normal testing because
// PortAudio is not initialized.
func TestSilenceAndRestoreALSAHandler_DoNotPanic(_ *testing.T) {
	SilenceALSAProbeNoise()
	RestoreALSAErrorHandler()
}
