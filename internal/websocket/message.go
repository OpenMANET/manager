package websocket

import (
	"encoding/binary"
	"fmt"
)

// Opcodes for the binary WebSocket protocol.
const (
	OpcodeAudioRX  byte = 0x01 // Server→Client: [0x01][ch][ssrc 4B][seq 2B][srcIP 4B][opus...]
	OpcodeAudioTX  byte = 0x02 // Client→Server: [0x02][opus data...]
	OpcodeTXToggle byte = 0x03 // Client→Server: [0x03][ch][on/off]
	OpcodeRXToggle byte = 0x04 // Client→Server: [0x04][ch][on/off]
	OpcodeRXAllOn  byte = 0x05 // Client→Server: [0x05]
	OpcodeRXAllOff byte = 0x06 // Client→Server: [0x06]
	OpcodeTXAllOn  byte = 0x07 // Client→Server: [0x07]
	OpcodeTXAllOff byte = 0x08 // Client→Server: [0x08]
	OpcodePTTDown  byte = 0x09 // Client→Server: [0x09] PTT pressed
	OpcodePTTUp    byte = 0x0A // Client→Server: [0x0A] PTT released
)

// AudioRXHeader is the fixed-size portion of an AudioRX message.
const AudioRXHeaderSize = 1 + 1 + 4 + 2 + 4 // opcode + ch + ssrc + seq + srcIP = 12

// AudioRXMessage represents a decoded audio RX frame from the mesh.
type AudioRXMessage struct {
	OpusData []byte
	SSRC     uint32
	Seq      uint16
	SrcIP    [4]byte
	Channel  byte
}

// ToggleMessage represents a channel toggle command.
type ToggleMessage struct {
	Channel byte
	On      bool
}

// DecodeAudioRX decodes an AudioRX binary message. The input must include the opcode byte.
func DecodeAudioRX(data []byte) (AudioRXMessage, error) {
	if len(data) < AudioRXHeaderSize {
		return AudioRXMessage{}, fmt.Errorf("audio RX message too short: %d bytes", len(data))
	}

	var msg AudioRXMessage

	msg.Channel = data[1]
	msg.SSRC = binary.BigEndian.Uint32(data[2:6])
	msg.Seq = binary.BigEndian.Uint16(data[6:8])
	copy(msg.SrcIP[:], data[8:12])
	msg.OpusData = data[12:]

	return msg, nil
}

// EncodeAudioRX encodes an AudioRX message into dst. dst must be at least
// AudioRXHeaderSize + len(opusData) bytes. Returns the number of bytes written.
func EncodeAudioRX(dst []byte, msg AudioRXMessage) int {
	dst[0] = OpcodeAudioRX
	dst[1] = msg.Channel
	binary.BigEndian.PutUint32(dst[2:6], msg.SSRC)
	binary.BigEndian.PutUint16(dst[6:8], msg.Seq)
	copy(dst[8:12], msg.SrcIP[:])
	copy(dst[12:], msg.OpusData)

	return AudioRXHeaderSize + len(msg.OpusData)
}

// DecodeToggle decodes a TX or RX toggle message. The input must include the opcode byte.
func DecodeToggle(data []byte) (ToggleMessage, error) {
	if len(data) < 3 {
		return ToggleMessage{}, fmt.Errorf("toggle message too short: %d bytes", len(data))
	}

	return ToggleMessage{
		Channel: data[1],
		On:      data[2] != 0,
	}, nil
}

// EncodeToggle encodes a toggle message into dst. dst must be at least 3 bytes.
func EncodeToggle(dst []byte, opcode byte, msg ToggleMessage) {
	dst[0] = opcode

	dst[1] = msg.Channel
	if msg.On {
		dst[2] = 1
	} else {
		dst[2] = 0
	}
}

// ExtractAudioTXPayload returns the opus data from an AudioTX message (skips the opcode byte).
func ExtractAudioTXPayload(data []byte) ([]byte, error) {
	if len(data) < 2 {
		return nil, fmt.Errorf("audio TX message too short: %d bytes", len(data))
	}

	return data[1:], nil
}
