package ptt

import (
	"encoding/binary"
	"hash/fnv"
	"math/rand"
	"strings"
	"time"
)

const (
	protocolUDP   = "udp"
	protocolRTP   = "rtp"
	rtpHeaderSize = 12
)

func normalizeProtocol(val string) string {
	switch strings.ToLower(strings.TrimSpace(val)) {
	case protocolRTP:
		return protocolRTP
	case protocolUDP:
		return protocolUDP
	default:
		return protocolUDP
	}
}

func rtpSSRCFromID(id string) uint32 {
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(id))
	return hasher.Sum32()
}

func randomRTPSeq() uint16 {
	rand.Seed(time.Now().UnixNano())
	return uint16(rand.Intn(65536))
}

func (ptt *PTTConfig) wrapRTP(payload []byte) []byte {
	seq := ptt.runtime.rtpSeq
	ptt.runtime.rtpSeq++

	packet := make([]byte, rtpHeaderSize+len(payload))
	packet[0] = 0x80
	packet[1] = 0x00
	binary.BigEndian.PutUint16(packet[2:], seq)
	binary.BigEndian.PutUint32(packet[4:], uint32(time.Now().Unix()))
	binary.BigEndian.PutUint32(packet[8:], ptt.runtime.rtpSSRC)
	copy(packet[rtpHeaderSize:], payload)
	return packet
}

func unwrapRTP(packet []byte) ([]byte, bool) {
	if len(packet) < rtpHeaderSize {
		return nil, false
	}
	if packet[0] != 0x80 && packet[0] != 0x81 {
		return nil, false
	}
	return packet[rtpHeaderSize:], true
}
