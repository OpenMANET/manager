package comms

import (
	"errors"
	"testing"
	"time"

	"github.com/gordonklaus/portaudio"
	"github.com/rs/zerolog"
)

// fakePAStream satisfies paStream so broadcast_encoder tests can run without
// opening real audio hardware. Start/Stop/Close error injection works the
// same way as mockStream.
type fakePAStream struct {
	startErr   error
	stopErr    error
	closeErr   error
	info       *portaudio.StreamInfo
	startCalls int
	stopCalls  int
	closeCalls int
}

func (f *fakePAStream) Start() error {
	f.startCalls++

	return f.startErr
}

func (f *fakePAStream) Stop() error {
	f.stopCalls++

	return f.stopErr
}

func (f *fakePAStream) Close() error {
	f.closeCalls++

	return f.closeErr
}

func (f *fakePAStream) Info() *portaudio.StreamInfo { return f.info }

// newTestBroadcastEncoder builds a broadcastEncoder wired to an in-memory
// fake stream, encoder, and a single send-enabled mockRTPSender. It does NOT
// spawn the encode goroutine — tests opt in by calling go be.encodeLoop()
// when they need to exercise the consumer side.
func newTestBroadcastEncoder(t *testing.T, enc *mockEncoder) (*broadcastEncoder, *mockRTPSender, *fakePAStream) {
	t.Helper()

	pc := &PortChannel{
		cfg:     McastPortConfig{Send: true},
		RTPSess: &mockRTPSender{},
	}
	pc.SendEnabled.Store(true)

	rt := &CommsRuntime{
		Ports:   []*PortChannel{pc},
		Encoder: enc,
	}

	cfg := &CommsConfig{Log: zerolog.Nop()}

	stream := &fakePAStream{}

	be := &broadcastEncoder{
		s:     stream,
		cfg:   cfg,
		rt:    rt,
		encCh: make(chan *[]int16, broadcastEncoderChanDepth),
		done:  make(chan struct{}),
	}

	return be, pc.RTPSess.(*mockRTPSender), stream
}

// silentFrame returns a frameSize-long int16 slice filled with zeros. Phase 5
// moved the capture callback to int16; no float32 conversion runs here.
func silentFrame() []int16 {
	return make([]int16, frameSize)
}

func TestBroadcastEncoder_HappyPath(t *testing.T) {
	enc := &mockEncoder{payloadN: 16}
	be, sender, _ := newTestBroadcastEncoder(t, enc)

	// Pump exactly broadcastEncoderChanDepth frames so they all fit in the
	// channel before we close it. This makes the test deterministic without
	// depending on the consumer goroutine racing the producer — once we
	// close encCh, encodeLoop will drain everything and then exit, and the
	// blocking <-be.done establishes a happens-before edge with all the
	// counter writes inside encodeOne.
	go be.encodeLoop()

	in := silentFrame()

	const frames = broadcastEncoderChanDepth

	for range frames {
		be.captureCallback(in)
	}

	close(be.encCh)
	<-be.done

	if got := be.framesCaptured.Load(); got != frames {
		t.Errorf("framesCaptured = %d, want %d", got, frames)
	}

	if got := be.framesEncoded.Load(); got != frames {
		t.Errorf("framesEncoded = %d, want %d", got, frames)
	}

	if got := be.framesDropped.Load(); got != 0 {
		t.Errorf("framesDropped = %d, want 0", got)
	}

	if got := be.encodeErrors.Load(); got != 0 {
		t.Errorf("encodeErrors = %d, want 0", got)
	}

	if got := enc.calls.Load(); got != frames {
		t.Errorf("encoder calls = %d, want %d", got, frames)
	}

	sender.mu.Lock()
	defer sender.mu.Unlock()

	if got := len(sender.Payloads); got != frames {
		t.Errorf("sender.Payloads len = %d, want %d", got, frames)
	}
}

func TestBroadcastEncoder_ChannelFullDropsAndCounts(t *testing.T) {
	// Do not spawn encodeLoop — leaving the consumer absent makes the
	// channel-full path fully deterministic. The producer fills encCh to
	// its capacity (broadcastEncoderChanDepth) and every additional frame
	// must be counted as dropped.
	enc := &mockEncoder{}
	be, _, _ := newTestBroadcastEncoder(t, enc)

	const extra = 5

	in := silentFrame()
	for range broadcastEncoderChanDepth + extra {
		be.captureCallback(in)
	}

	if got := be.framesCaptured.Load(); got != int64(broadcastEncoderChanDepth+extra) {
		t.Errorf("framesCaptured = %d, want %d", got, broadcastEncoderChanDepth+extra)
	}

	if got := be.framesDropped.Load(); got != int64(extra) {
		t.Errorf("framesDropped = %d, want %d", got, extra)
	}

	if got := len(be.encCh); got != broadcastEncoderChanDepth {
		t.Errorf("encCh len = %d, want %d", got, broadcastEncoderChanDepth)
	}

	if got := enc.calls.Load(); got != 0 {
		t.Errorf("encoder calls = %d, want 0 (no consumer)", got)
	}

	// Drain the channel so the pooled int16 slices are returned to the
	// pool rather than leaking. Each fp must be released the same way the
	// encode loop would.
	for range broadcastEncoderChanDepth {
		fp := <-be.encCh
		Int16Pool.Put(fp)
	}
}

func TestBroadcastEncoder_EncoderErrorIncrementsCounter(t *testing.T) {
	enc := &mockEncoder{encodeErr: errors.New("boom")}
	be, sender, _ := newTestBroadcastEncoder(t, enc)

	go be.encodeLoop()

	in := silentFrame()

	const frames = 5

	// Push one frame at a time and wait for the encode worker to consume it
	// before queueing the next, so the test is independent of
	// broadcastEncoderChanDepth: a tighter channel depth still produces the
	// same total error count, just with more producer↔consumer interleaving.
	for i := int64(1); i <= frames; i++ {
		be.captureCallback(in)

		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			if enc.calls.Load() >= i {
				break
			}

			time.Sleep(time.Millisecond)
		}

		if got := enc.calls.Load(); got < i {
			t.Fatalf("encoder calls = %d after frame %d, want at least %d", got, i, i)
		}
	}

	close(be.encCh)
	<-be.done

	if got := be.framesCaptured.Load(); got != frames {
		t.Errorf("framesCaptured = %d, want %d", got, frames)
	}

	if got := be.framesEncoded.Load(); got != 0 {
		t.Errorf("framesEncoded = %d, want 0", got)
	}

	if got := be.encodeErrors.Load(); got != frames {
		t.Errorf("encodeErrors = %d, want %d", got, frames)
	}

	if got := enc.calls.Load(); got != frames {
		t.Errorf("encoder calls = %d, want %d", got, frames)
	}

	sender.mu.Lock()
	defer sender.mu.Unlock()

	if got := len(sender.Payloads); got != 0 {
		t.Errorf("sender.Payloads len = %d, want 0 (encode failed)", got)
	}
}

func TestBroadcastEncoder_StartResetsCountersAndCallsStream(t *testing.T) {
	be, _, stream := newTestBroadcastEncoder(t, &mockEncoder{})

	be.framesCaptured.Store(42)
	be.framesEncoded.Store(17)
	be.framesDropped.Store(3)
	be.encodeErrors.Store(9)

	if err := be.Start(); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	if stream.startCalls != 1 {
		t.Errorf("stream.startCalls = %d, want 1", stream.startCalls)
	}

	if got := be.framesCaptured.Load(); got != 0 {
		t.Errorf("framesCaptured = %d, want 0", got)
	}

	if got := be.framesEncoded.Load(); got != 0 {
		t.Errorf("framesEncoded = %d, want 0", got)
	}

	if got := be.framesDropped.Load(); got != 0 {
		t.Errorf("framesDropped = %d, want 0", got)
	}

	if got := be.encodeErrors.Load(); got != 0 {
		t.Errorf("encodeErrors = %d, want 0", got)
	}
}

func TestBroadcastEncoder_StartPropagatesError(t *testing.T) {
	startErr := errors.New("simulated start failure")
	be, _, stream := newTestBroadcastEncoder(t, &mockEncoder{})
	stream.startErr = startErr

	err := be.Start()
	if err == nil {
		t.Fatal("Start returned nil, want error")
	}

	if !errors.Is(err, startErr) {
		t.Errorf("Start error = %v, want it to wrap %v", err, startErr)
	}
}

func TestBroadcastEncoder_StopLogsAndPropagatesError(t *testing.T) {
	stopErr := errors.New("simulated stop failure")
	be, _, stream := newTestBroadcastEncoder(t, &mockEncoder{})
	stream.stopErr = stopErr

	err := be.Stop()
	if err == nil {
		t.Fatal("Stop returned nil, want error")
	}

	if !errors.Is(err, stopErr) {
		t.Errorf("Stop error = %v, want it to wrap %v", err, stopErr)
	}

	if stream.stopCalls != 1 {
		t.Errorf("stream.stopCalls = %d, want 1", stream.stopCalls)
	}
}

func TestBroadcastEncoder_CloseTerminatesGoroutine(t *testing.T) {
	be, _, stream := newTestBroadcastEncoder(t, &mockEncoder{})

	go be.encodeLoop()

	if err := be.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	if stream.stopCalls != 1 {
		t.Errorf("stream.stopCalls = %d, want 1", stream.stopCalls)
	}

	if stream.closeCalls != 1 {
		t.Errorf("stream.closeCalls = %d, want 1", stream.closeCalls)
	}

	// done must be closed by encodeLoop's defer; if Close had not waited
	// the channel would still be open.
	select {
	case <-be.done:
	case <-time.After(time.Second):
		t.Fatal("Close did not wait for encode goroutine to exit")
	}
}
