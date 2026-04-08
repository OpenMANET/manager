package device

import (
	"fmt"

	"github.com/gordonklaus/portaudio"
)

// AudioStream wraps a PortAudio stream with a minimal lifecycle interface so
// that audio streams can be started, stopped, and closed without exposing
// PortAudio types to the rest of the codebase.
type AudioStream interface {
	Start() error
	Stop() error
	Close() error
}

// portaudioStream satisfies AudioStream by wrapping *portaudio.Stream. It is
// unexported so the portaudio binding does not leak into the public surface;
// callers obtain instances via NewPortAudioStream.
type portaudioStream struct {
	s *portaudio.Stream
}

// NewPortAudioStream wraps s as an AudioStream. Returns nil if s is nil.
func NewPortAudioStream(s *portaudio.Stream) AudioStream {
	if s == nil {
		return nil
	}

	return &portaudioStream{s: s}
}

// Start begins audio processing on the underlying PortAudio stream.
// It returns an error wrapping the PortAudio failure if the stream cannot be started.
func (p *portaudioStream) Start() error {
	if err := p.s.Start(); err != nil {
		return fmt.Errorf("portaudio stream start: %w", err)
	}

	return nil
}

// Stop halts audio processing on the underlying PortAudio stream, flushing
// any pending buffers. It returns an error wrapping the PortAudio failure if
// the stream cannot be stopped.
func (p *portaudioStream) Stop() error {
	if err := p.s.Stop(); err != nil {
		return fmt.Errorf("portaudio stream stop: %w", err)
	}

	return nil
}

// Close releases all resources associated with the underlying PortAudio stream.
// The stream must not be used after Close returns.
func (p *portaudioStream) Close() error { return p.s.Close() }
