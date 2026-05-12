package websocket

import (
	"bytes"
	"testing"
)

func TestEncodeDecodeAudioRX(t *testing.T) {
	original := AudioRXMessage{
		Channel:  3,
		SSRC:     0x12345678,
		Seq:      0x0A0B,
		SrcIP:    [4]byte{192, 168, 1, 42},
		OpusData: []byte{0xDE, 0xAD, 0xBE, 0xEF},
	}

	buf := make([]byte, AudioRXHeaderSize+len(original.OpusData))
	n := EncodeAudioRX(buf, original)

	if n != AudioRXHeaderSize+len(original.OpusData) {
		t.Fatalf("EncodeAudioRX returned %d, want %d", n, AudioRXHeaderSize+len(original.OpusData))
	}

	if buf[0] != OpcodeAudioRX {
		t.Errorf("opcode = 0x%02x, want 0x%02x", buf[0], OpcodeAudioRX)
	}

	decoded, err := DecodeAudioRX(buf[:n])
	if err != nil {
		t.Fatalf("DecodeAudioRX error: %v", err)
	}

	if decoded.Channel != original.Channel {
		t.Errorf("Channel = %d, want %d", decoded.Channel, original.Channel)
	}

	if decoded.SSRC != original.SSRC {
		t.Errorf("SSRC = 0x%08x, want 0x%08x", decoded.SSRC, original.SSRC)
	}

	if decoded.Seq != original.Seq {
		t.Errorf("Seq = 0x%04x, want 0x%04x", decoded.Seq, original.Seq)
	}

	if decoded.SrcIP != original.SrcIP {
		t.Errorf("SrcIP = %v, want %v", decoded.SrcIP, original.SrcIP)
	}

	if !bytes.Equal(decoded.OpusData, original.OpusData) {
		t.Errorf("OpusData = %v, want %v", decoded.OpusData, original.OpusData)
	}
}

func TestDecodeAudioRX_TooShort(t *testing.T) {
	_, err := DecodeAudioRX([]byte{0x01, 0x03})
	if err == nil {
		t.Error("DecodeAudioRX with short data should return error")
	}
}

func TestDecodeToggle(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantCh  byte
		wantOn  bool
		wantErr bool
	}{
		{
			name:   "TX toggle on",
			data:   []byte{OpcodeTXToggle, 2, 1},
			wantCh: 2,
			wantOn: true,
		},
		{
			name:   "RX toggle off",
			data:   []byte{OpcodeRXToggle, 5, 0},
			wantCh: 5,
			wantOn: false,
		},
		{
			name:    "too short",
			data:    []byte{OpcodeTXToggle, 1},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := DecodeToggle(tt.data)
			if (err != nil) != tt.wantErr {
				t.Fatalf("DecodeToggle error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr {
				return
			}

			if msg.Channel != tt.wantCh {
				t.Errorf("Channel = %d, want %d", msg.Channel, tt.wantCh)
			}

			if msg.On != tt.wantOn {
				t.Errorf("On = %v, want %v", msg.On, tt.wantOn)
			}
		})
	}
}

func TestEncodeToggle(t *testing.T) {
	buf := make([]byte, 3)
	EncodeToggle(buf, OpcodeTXToggle, ToggleMessage{Channel: 4, On: true})

	if buf[0] != OpcodeTXToggle {
		t.Errorf("opcode = 0x%02x, want 0x%02x", buf[0], OpcodeTXToggle)
	}

	if buf[1] != 4 {
		t.Errorf("channel = %d, want 4", buf[1])
	}

	if buf[2] != 1 {
		t.Errorf("on = %d, want 1", buf[2])
	}
}

func TestExtractAudioTXPayload(t *testing.T) {
	data := []byte{OpcodeAudioTX, 0xAA, 0xBB, 0xCC}

	payload, err := ExtractAudioTXPayload(data)
	if err != nil {
		t.Fatalf("ExtractAudioTXPayload error: %v", err)
	}

	if !bytes.Equal(payload, []byte{0xAA, 0xBB, 0xCC}) {
		t.Errorf("payload = %v, want [0xAA 0xBB 0xCC]", payload)
	}
}

func TestExtractAudioTXPayload_TooShort(t *testing.T) {
	_, err := ExtractAudioTXPayload([]byte{OpcodeAudioTX})
	if err == nil {
		t.Error("ExtractAudioTXPayload with 1 byte should return error")
	}
}
