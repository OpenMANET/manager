package security

import (
	"errors"
	"net"
	"testing"
	"time"
)

func TestPayloadCodecRoundTrip(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	codec, err := newPayloadCodecFromPassphrase("mesh-password", func() time.Time { return now })
	if err != nil {
		t.Fatalf("newPayloadCodecFromPassphrase failed: %v", err)
	}

	dataType := uint8(42)
	plaintext := []byte("hello mesh")
	source := net.HardwareAddr{0x02, 0x42, 0x42, 0x42, 0x42, 0x42}

	encrypted, err := codec.Encrypt(dataType, plaintext)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	decrypted, err := codec.Decrypt(dataType, source, encrypted)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Fatalf("expected %q, got %q", plaintext, decrypted)
	}
}

func TestPayloadCodecReplayDetection(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	codec, err := newPayloadCodecFromPassphrase("mesh-password", func() time.Time { return now })
	if err != nil {
		t.Fatalf("newPayloadCodecFromPassphrase failed: %v", err)
	}

	dataType := uint8(7)
	source := net.HardwareAddr{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}

	encrypted, err := codec.Encrypt(dataType, []byte("payload"))
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	if _, err := codec.Decrypt(dataType, source, encrypted); err != nil {
		t.Fatalf("Decrypt first payload failed: %v", err)
	}

	if _, err := codec.Decrypt(dataType, source, encrypted); !errors.Is(err, ErrReplayDetected) {
		t.Fatalf("expected ErrReplayDetected, got %v", err)
	}
}

func TestPayloadCodecRejectsExpiredPayload(t *testing.T) {
	current := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	codec, err := newPayloadCodecFromPassphrase("mesh-password", func() time.Time { return current })
	if err != nil {
		t.Fatalf("newPayloadCodecFromPassphrase failed: %v", err)
	}

	dataType := uint8(9)
	source := net.HardwareAddr{0x00, 0x01, 0x02, 0x03, 0x04, 0x05}

	encrypted, err := codec.Encrypt(dataType, []byte("payload"))
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	codec.now = func() time.Time {
		return current.Add(defaultReplayWindow + time.Second)
	}

	if _, err := codec.Decrypt(dataType, source, encrypted); !errors.Is(err, ErrPayloadExpired) {
		t.Fatalf("expected ErrPayloadExpired, got %v", err)
	}
}

func TestPayloadCodecEmptyPassphrase(t *testing.T) {
	if _, err := NewPayloadCodecFromPassphrase("   "); err == nil {
		t.Fatalf("expected error for empty passphrase")
	}
}
