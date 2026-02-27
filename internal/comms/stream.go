package comms

import (
	"fmt"

	"github.com/gordonklaus/portaudio"
)

// AudioStream wraps a PortAudio stream with a minimal lifecycle interface so
// that audio streams can be started, stopped, and closed without exposing
// PortAudio types to the rest of the package.
type AudioStream interface {
	Start() error
	Stop() error
	Close() error
}

// portaudioStream satisfies AudioStream by wrapping *portaudio.Stream.
type portaudioStream struct {
	s *portaudio.Stream
}

func (p *portaudioStream) Start() error {
	if err := p.s.Start(); err != nil {
		return fmt.Errorf("portaudio stream start: %w", err)
	}

	return nil
}

func (p *portaudioStream) Stop() error {
	if err := p.s.Stop(); err != nil {
		return fmt.Errorf("portaudio stream stop: %w", err)
	}

	return nil
}

func (p *portaudioStream) Close() error { return p.s.Close() }
