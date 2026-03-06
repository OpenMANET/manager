package comms

import (
	"fmt"
	"hash/fnv"
	"sync"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/report"
	pionrtcp "github.com/pion/rtcp"
	pionrtp "github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/rs/zerolog"
)

const (
	rtpPayloadTypeOpus = uint8(111)
	rtpClockRate       = uint32(48000)
	rtpFrameSamples    = uint32(960) // 20 ms at 48 kHz
	rtpMTU             = uint16(1400)

	// rtpBufSize is the pool buffer capacity for RTP packet serialization.
	// Must be >= rtpMTU + maximum RTP header size (~60 bytes for 15 CSRCs).
	rtpBufSize = 1500

	// streamMIMETypeOpus is the MIME type for Opus audio streams, used in StreamInfo.
	streamMIMETypeOpus = "audio/opus"
)

// rtpMarshalPool pools serialization buffers for the baseRTPWriter hot path,
// eliminating both the pionrtp.Packet struct allocation and the Marshal()
// output slice allocation that would otherwise occur on every RTP send.
var rtpMarshalPool = sync.Pool{ //nolint:gochecknoglobals
	New: func() any {
		s := make([]byte, rtpBufSize)

		return &s
	},
}

// rtpSender is the interface the broadcast PortAudio callback uses to ship an
// encoded Opus frame over the network. Backed by pionRTPSession in production;
// tests inject mockRTPSender.
type rtpSender interface {
	send(payload []byte) error
}

// pionRTPSession wraps a pion Packetizer and an interceptor chain that
// generates periodic RTCP Sender Reports. One session represents one local
// SSRC (the node running this software).
//
// The RTCP path is one-way outbound: the SR generator fires every 5 seconds
// and writes to the provided rtcpTransport. Inbound RTP is parsed externally
// with parseIncomingRTP; the session does not handle receive stats (each
// remote SSRC in a multicast group would require a separate receiver).
type pionRTPSession struct {
	log        zerolog.Logger
	packetizer pionrtp.Packetizer
	rtpWriter  interceptor.RTPWriter
	intercept  interceptor.Interceptor
	ssrc       uint32
	mu         sync.Mutex
}

// ssrcFromID returns a deterministic uint32 SSRC derived from an ID string
// using the FNV-1a 32-bit hash.
func ssrcFromID(id string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(id))

	return h.Sum32()
}

// newPionRTPSession creates a pionRTPSession that sends RTP via rtpTransport
// and RTCP Sender Reports via rtcpTransport.
//
// The interceptor chain contains a single report.SenderInterceptor that
// generates an outbound RTCP SR every 5 seconds. Inbound RTCP (e.g. Receiver
// Reports from peers) is not processed — in a multicast PTT topology each
// transmission has multiple receivers and no single feedback path is meaningful.
func newPionRTPSession(
	ssrc uint32,
	rtpTransport PacketWriter,
	rtcpTransport PacketWriter,
	log zerolog.Logger,
) (*pionRTPSession, error) {
	registry := &interceptor.Registry{}

	// Sender report: generates RTCP SR packets on a 5-second timer.
	senderReport, err := report.NewSenderInterceptor(
		report.SenderInterval(5 * time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("report.NewSenderInterceptor: %w", err)
	}

	registry.Add(senderReport)

	i, err := registry.Build("")
	if err != nil {
		return nil, fmt.Errorf("interceptor.Registry.Build: %w", err)
	}

	// Bind the RTCP writer. The SenderInterceptor stores this internally and
	// calls it on its timer tick to deliver SR packets.
	_ = i.BindRTCPWriter(interceptor.RTCPWriterFunc(
		func(pkts []pionrtcp.Packet, _ interceptor.Attributes) (int, error) {
			var totalBytes int

			for _, pkt := range pkts {
				data, marshalErr := pkt.Marshal()
				if marshalErr != nil {
					log.Warn().Err(marshalErr).Msg("comms: RTCP marshal error")

					continue
				}

				n, writeErr := rtcpTransport.Write(data)
				totalBytes += n

				if writeErr != nil {
					log.Debug().Err(writeErr).Msg("comms: RTCP write error")
				}
			}

			return totalBytes, nil
		},
	))

	// StreamInfo describes our outbound Opus stream to the interceptors.
	streamInfo := &interceptor.StreamInfo{
		SSRC:        ssrc,
		PayloadType: rtpPayloadTypeOpus,
		ClockRate:   rtpClockRate,
		MimeType:    streamMIMETypeOpus,
	}

	// baseRTPWriter serializes the RTP header + payload into a pooled buffer
	// and writes it to the UDP transport. MarshalPacketTo avoids both the
	// Packet struct allocation and the output slice allocation from Marshal().
	baseRTPWriter := interceptor.RTPWriterFunc(
		func(header *pionrtp.Header, payload []byte, _ interceptor.Attributes) (int, error) {
			var pkt pionrtp.Packet

			pkt.Header = *header
			pkt.Payload = payload

			size := pkt.MarshalSize()
			bufPtr := rtpMarshalPool.Get().(*[]byte) //nolint:forcetypeassert
			buf := (*bufPtr)[:size]

			n, marshalErr := pkt.MarshalTo(buf)
			if marshalErr != nil {
				rtpMarshalPool.Put(bufPtr)

				return 0, fmt.Errorf("rtp.Packet.MarshalTo: %w", marshalErr)
			}

			wrote, writeErr := rtpTransport.Write(buf[:n])

			rtpMarshalPool.Put(bufPtr)

			return wrote, writeErr
		},
	)

	// Bind the local stream — this connects the packetizer through the
	// interceptor chain down to baseRTPWriter.
	rtpWriter := i.BindLocalStream(streamInfo, baseRTPWriter)

	packetizer := pionrtp.NewPacketizer(
		rtpMTU,
		rtpPayloadTypeOpus,
		ssrc,
		&codecs.OpusPayloader{},
		pionrtp.NewRandomSequencer(),
		rtpClockRate,
	)

	return &pionRTPSession{
		log:        log,
		packetizer: packetizer,
		rtpWriter:  rtpWriter,
		intercept:  i,
		ssrc:       ssrc,
	}, nil
}

// send encodes payload into one or more RTP packets using the Packetizer and
// writes them through the interceptor chain (and ultimately the UDP socket).
// For Opus, Packetize always returns exactly one packet.
func (s *pionRTPSession) send(payload []byte) error {
	s.mu.Lock()
	packets := s.packetizer.Packetize(payload, rtpFrameSamples)
	s.mu.Unlock()

	for _, pkt := range packets {
		if _, err := s.rtpWriter.Write(&pkt.Header, pkt.Payload, nil); err != nil {
			s.log.Debug().Err(err).Msg("comms: RTP send error")

			return fmt.Errorf("rtp write: %w", err)
		}
	}

	return nil
}

// close shuts down the interceptor chain, stopping internal timer goroutines.
func (s *pionRTPSession) close() error {
	return s.intercept.Close()
}

// parseIncomingRTP parses a raw UDP datagram into a pion RTP Packet.
// Returns an error if the bytes are not a valid RTP packet.
func parseIncomingRTP(raw []byte) (*pionrtp.Packet, error) {
	var pkt pionrtp.Packet
	if err := pkt.Unmarshal(raw); err != nil {
		return nil, fmt.Errorf("rtp.Packet.Unmarshal: %w", err)
	}

	return &pkt, nil
}
