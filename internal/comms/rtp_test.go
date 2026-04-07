package comms

import (
	"testing"

	pionrtp "github.com/pion/rtp"
	"github.com/rs/zerolog"
)

func TestSSRCFromID_Deterministic(t *testing.T) {
	a := SSRCFromID("node-alpha")

	b := SSRCFromID("node-alpha")
	if a != b {
		t.Errorf("SSRCFromID not deterministic; got %d and %d", a, b)
	}
}

func TestSSRCFromID_Different(t *testing.T) {
	a := SSRCFromID("node-alpha")

	b := SSRCFromID("node-beta")
	if a == b {
		t.Error("different IDs should produce different SSRCs")
	}
}

func TestParseIncomingRTP_Valid(t *testing.T) {
	orig := &pionrtp.Packet{
		Header:  pionrtp.Header{Version: 2, PayloadType: RTPPayloadTypeOpus, SequenceNumber: 42, Timestamp: 1000, SSRC: 0xDEADBEEF},
		Payload: []byte{1, 2, 3, 4},
	}

	raw, err := orig.Marshal()
	if err != nil {
		t.Fatal(err)
	}

	parsed, err := ParseIncomingRTP(raw)
	if err != nil {
		t.Fatal(err)
	}

	if parsed.SequenceNumber != 42 {
		t.Errorf("seq: got %d, want 42", parsed.SequenceNumber)
	}

	if parsed.SSRC != 0xDEADBEEF {
		t.Errorf("ssrc: got 0x%X", parsed.SSRC)
	}
}

func TestParseIncomingRTP_Invalid(t *testing.T) {
	_, err := ParseIncomingRTP([]byte{0xFF, 0x00, 0x01})
	if err == nil {
		t.Error("expected error for invalid bytes")
	}
}

func TestParseIncomingRTP_Nil(t *testing.T) {
	_, err := ParseIncomingRTP(nil)
	if err == nil {
		t.Error("expected error for nil input")
	}
}

func TestPionRTPSession_Send(t *testing.T) {
	rtpW := &mockWriter{}
	rtcpW := &mockWriter{}

	sess, err := NewRTPSession(0xABCDEF, rtpW, rtcpW, zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close() //nolint:errcheck

	if err := sess.Send([]byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}

	if len(rtpW.Packets) == 0 {
		t.Fatal("expected at least one packet")
	}

	var pkt pionrtp.Packet
	if err := pkt.Unmarshal(rtpW.Packets[0]); err != nil {
		t.Fatalf("invalid RTP: %v", err)
	}

	if pkt.PayloadType != RTPPayloadTypeOpus {
		t.Errorf("PT: got %d", pkt.PayloadType)
	}

	if pkt.SSRC != 0xABCDEF {
		t.Errorf("SSRC: got 0x%X", pkt.SSRC)
	}
}

func TestPionRTPSession_SequenceIncrement(t *testing.T) {
	rtpW := &mockWriter{}

	sess, err := NewRTPSession(1, rtpW, &mockWriter{}, zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close() //nolint:errcheck

	for range 3 {
		if err := sess.Send([]byte{1}); err != nil {
			t.Fatal(err)
		}
	}

	if len(rtpW.Packets) != 3 {
		t.Fatalf("expected 3 packets; got %d", len(rtpW.Packets))
	}

	var seqs [3]uint16

	for i, raw := range rtpW.Packets {
		var pkt pionrtp.Packet
		if err := pkt.Unmarshal(raw); err != nil {
			t.Fatalf("packet %d invalid: %v", i, err)
		}

		seqs[i] = pkt.SequenceNumber
	}

	if seqs[1]-seqs[0] != 1 {
		t.Errorf("seq[1]-seq[0]=%d want 1", seqs[1]-seqs[0])
	}

	if seqs[2]-seqs[1] != 1 {
		t.Errorf("seq[2]-seq[1]=%d want 1", seqs[2]-seqs[1])
	}
}

func TestPionRTPSession_Close(t *testing.T) {
	sess, err := NewRTPSession(1, &mockWriter{}, &mockWriter{}, zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}

	if err := sess.Close(); err != nil {
		t.Errorf("close error: %v", err)
	}
}
