package ptt

import (
	"fmt"

	"github.com/gordonklaus/portaudio"
)

// AudioStream abstracts a PortAudio stream so the capture and playback paths
// can be replaced with test doubles in unit tests.
type AudioStream interface {
	Start() error
	Stop() error
	Close() error
}

// portaudioStream wraps *portaudio.Stream to satisfy AudioStream.
type portaudioStream struct{ s *portaudio.Stream }

func (p *portaudioStream) Start() error {
	if err := p.s.Start(); err != nil {
		return fmt.Errorf("start portaudio stream: %w", err)
	}

	return nil
}

func (p *portaudioStream) Stop() error {
	if err := p.s.Stop(); err != nil {
		return fmt.Errorf("stop portaudio stream: %w", err)
	}

	return nil
}

func (p *portaudioStream) Close() error { return p.s.Close() }
