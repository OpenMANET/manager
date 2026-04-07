package comms

import (
	"testing"
)

func FuzzParseIncomingRTP(f *testing.F) {
	// Seed with a minimal valid RTP packet (version 2, no CSRC, no extensions).
	// RTP header: V=2, P=0, X=0, CC=0, M=0, PT=111, Seq=1, TS=160, SSRC=1
	seed := []byte{
		0x80, 0x6f, // V=2, PT=111
		0x00, 0x01, // Seq=1
		0x00, 0x00, 0x00, 0xa0, // Timestamp=160
		0x00, 0x00, 0x00, 0x01, // SSRC=1
	}

	f.Add(seed)
	f.Add([]byte{})
	f.Add([]byte{0x00})

	f.Fuzz(func(t *testing.T, data []byte) {
		// Must not panic regardless of input.
		_, _ = ParseIncomingRTP(data)
	})
}
