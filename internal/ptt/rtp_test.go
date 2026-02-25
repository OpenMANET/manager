package ptt

import (
	"testing"
)

func TestNormalizeProtocol(t *testing.T) {
	cases := []struct{ in, want string }{
		{"udp", protocolUDP},
		{"UDP", protocolUDP},
		{"rtp", protocolRTP},
		{"RTP", protocolRTP},
		{"  rtp  ", protocolRTP},
		{"", protocolUDP},
		{"unknown", protocolUDP},
	}
	for _, tc := range cases {
		got := normalizeProtocol(tc.in)
		if got != tc.want {
			t.Errorf("normalizeProtocol(%q): got %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestWrapAndUnwrapRTP(t *testing.T) {
	ptt := &PTTConfig{Protocol: protocolRTP}
	rt := &PTTRuntime{rtpSeq: 42, rtpSSRC: 0xdeadbeef}
	payload := []byte{0x01, 0x02, 0x03, 0x04}

	wrapped := ptt.wrapRTP(payload, rt)
	if len(wrapped) != rtpHeaderSize+len(payload) {
		t.Fatalf("wrapped len %d, want %d", len(wrapped), rtpHeaderSize+len(payload))
	}

	// Version byte must be 0x80.
	if wrapped[0] != 0x80 {
		t.Errorf("RTP version byte: got 0x%02x, want 0x80", wrapped[0])
	}

	// rtpSeq should have been incremented.
	if rt.rtpSeq != 43 {
		t.Errorf("rtpSeq after wrap: got %d, want 43", rt.rtpSeq)
	}

	// Unwrap should recover the original payload.
	got, ok := unwrapRTP(wrapped)
	if !ok {
		t.Fatal("unwrapRTP returned ok=false on a valid RTP packet")
	}

	if string(got) != string(payload) {
		t.Errorf("unwrapped payload: got %v, want %v", got, payload)
	}
}

func TestUnwrapRTP_TooShort(t *testing.T) {
	_, ok := unwrapRTP([]byte{0x80, 0x00, 0x00})
	if ok {
		t.Error("expected ok=false for short packet")
	}
}

func TestUnwrapRTP_NotRTP(t *testing.T) {
	pkt := make([]byte, rtpHeaderSize+4)
	pkt[0] = 0x40 // not 0x80 or 0x81

	_, ok := unwrapRTP(pkt)
	if ok {
		t.Error("expected ok=false for non-RTP first byte")
	}
}

func TestParseRTPHeader(t *testing.T) {
	ptt := &PTTConfig{}
	rt := &PTTRuntime{rtpSeq: 7, rtpSSRC: 0x12345678}
	payload := []byte{0xaa, 0xbb}

	wrapped := ptt.wrapRTP(payload, rt)

	seq, _, ssrc, ok := parseRTPHeader(wrapped)
	if !ok {
		t.Fatal("parseRTPHeader returned ok=false")
	}

	if seq != 7 {
		t.Errorf("seq: got %d, want 7", seq)
	}

	if ssrc != 0x12345678 {
		t.Errorf("ssrc: got 0x%08x, want 0x12345678", ssrc)
	}
}

func TestParseRTPHeader_TooShort(t *testing.T) {
	_, _, _, ok := parseRTPHeader([]byte{0x80, 0x00})
	if ok {
		t.Error("expected ok=false for short packet")
	}
}

func TestRtpSSRCFromID(t *testing.T) {
	a := rtpSSRCFromID("device-a")
	b := rtpSSRCFromID("device-b")
	same := rtpSSRCFromID("device-a")

	if a == b {
		t.Error("different IDs should produce different SSRCs")
	}

	if a != same {
		t.Error("same ID should produce same SSRC")
	}
}

func TestRandomRTPSeq_Range(t *testing.T) {
	// We just verify it doesn't panic and returns a uint16.
	_ = randomRTPSeq()
}
