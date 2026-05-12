package security

import (
	"net"
	"testing"
	"time"
)

func FuzzPayloadCodecDecrypt(f *testing.F) {
	// Seed with a valid encrypted payload so the fuzzer has structure to mutate.
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	codec, err := newPayloadCodecFromPassphrase("fuzz-passphrase", func() time.Time { return now })
	if err != nil {
		f.Fatalf("setup codec: %v", err)
	}

	valid, err := codec.Encrypt(42, []byte("seed payload"))
	if err != nil {
		f.Fatalf("encrypt seed: %v", err)
	}

	f.Add(valid)
	f.Add([]byte{})
	f.Add([]byte{0x00})
	f.Add([]byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})

	source := net.HardwareAddr{0x02, 0x42, 0x42, 0x42, 0x42, 0x42}

	f.Fuzz(func(t *testing.T, data []byte) {
		// Must not panic regardless of input.
		_, _ = codec.Decrypt(42, source, data)
	})
}
