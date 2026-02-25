package ptt

import "github.com/gordonklaus/portaudio"

// AudioStream abstracts a PortAudio stream so the capture and playback paths
// can be replaced with test doubles in unit tests.
type AudioStream interface {
	Start() error
	Stop() error
	Close() error
}

// portaudioStream wraps *portaudio.Stream to satisfy AudioStream.
type portaudioStream struct{ s *portaudio.Stream }

func (p *portaudioStream) Start() error { return p.s.Start() }
func (p *portaudioStream) Stop() error  { return p.s.Stop() }
func (p *portaudioStream) Close() error { return p.s.Close() }
