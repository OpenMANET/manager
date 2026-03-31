package batmanadv

import (
	"encoding/binary"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/mdlayher/genetlink"
	"github.com/mdlayher/netlink"
	"github.com/rs/zerolog"
)

// syncFakeQuerier is a thread-safe Querier for concurrent tests.
type syncFakeQuerier struct {
	mu     sync.Mutex
	msgs   []genetlink.Message
	err    error
	calls  int
	closed bool
}

func (q *syncFakeQuerier) Execute(_ genetlink.Message, _ uint16, _ netlink.HeaderFlags) ([]genetlink.Message, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.calls++
	return q.msgs, q.err
}

func (q *syncFakeQuerier) Close() error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.closed = true
	return nil
}

func (q *syncFakeQuerier) isClosed() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.closed
}

// blockingQuerier blocks in Execute until unblocked via a channel.
// Used to test that Close() and reconnectLoop hold queryMu while closing the connection.
type blockingQuerier struct {
	mu      sync.Mutex
	closed  bool
	once    sync.Once
	started chan struct{} // closed when Execute first begins
	unblock chan struct{} // close to let Execute return
	err     error
}

func newBlockingQuerier(err error) *blockingQuerier {
	return &blockingQuerier{
		started: make(chan struct{}),
		unblock: make(chan struct{}),
		err:     err,
	}
}

func (q *blockingQuerier) Execute(_ genetlink.Message, _ uint16, _ netlink.HeaderFlags) ([]genetlink.Message, error) {
	q.once.Do(func() { close(q.started) })
	<-q.unblock
	return nil, q.err
}

func (q *blockingQuerier) Close() error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.closed = true
	return nil
}

func (q *blockingQuerier) isClosed() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.closed
}

// fakeQuerier is a mock Querier that returns preconfigured responses.
type fakeQuerier struct {
	responses [][]genetlink.Message
	err       error
	calls     int
	closed    bool
}

func (q *fakeQuerier) Execute(_ genetlink.Message, _ uint16, _ netlink.HeaderFlags) ([]genetlink.Message, error) {
	q.calls++

	if q.err != nil {
		return nil, q.err
	}

	if q.calls-1 < len(q.responses) {
		return q.responses[q.calls-1], nil
	}

	return nil, errors.New("no more mock responses")
}

func (q *fakeQuerier) Close() error {
	q.closed = true

	return nil
}

// buildMeshConfigMessage creates a genetlink.Message containing mesh config attrs.
func buildMeshConfigMessage() genetlink.Message {
	attrs := []netlink.Attribute{
		makeStringAttr(BatadvAttrVersion, "2023.1"),
		makeStringAttr(BatadvAttrAlgoName, "BATMAN_IV"),
		makeUint32Attr(BatadvAttrMeshIfindex, 10),
		makeStringAttr(BatadvAttrMeshIfname, "bat0"),
		makeMACAttr(BatadvAttrHardAddress, [6]byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}),
		makeUint8Attr(BatadvAttrGwMode, BatadvGwModeServer),
		makeUint32Attr(BatadvAttrGwBandwidthDown, 10000),
		makeUint32Attr(BatadvAttrGwBandwidthUp, 2000),
		makeUint32Attr(BatadvAttrOrigInterval, 1000),
		makeUint8Attr(BatadvAttrFragEnabled, 1),
	}

	data := marshalAttrs(attrs)

	return genetlink.Message{
		Header: genetlink.Header{Command: BatadvCmdGetMesh, Version: 1},
		Data:   data,
	}
}

// buildGatewayMessage creates a genetlink.Message for a single gateway entry.
func buildGatewayMessage(mac [6]byte, throughput uint32, best bool) genetlink.Message {
	bestVal := uint8(0)
	if best {
		bestVal = 1
	}

	attrs := []netlink.Attribute{
		makeStringAttr(BatadvAttrHardIfname, "wlan0"),
		makeMACAttr(BatadvAttrOrigAddress, mac),
		makeMACAttr(BatadvAttrRouter, mac),
		makeUint32Attr(BatadvAttrThroughput, throughput),
		makeUint32Attr(BatadvAttrBandwidthUp, 2000),
		makeUint32Attr(BatadvAttrBandwidthDown, 10000),
		makeUint8Attr(BatadvAttrFlagBest, bestVal),
	}

	return genetlink.Message{
		Header: genetlink.Header{Command: BatadvCmdGetGateways, Version: 1},
		Data:   marshalAttrs(attrs),
	}
}

// buildNeighborMessage creates a genetlink.Message for a single neighbor entry.
func buildNeighborMessage(mac [6]byte, throughput uint32) genetlink.Message {
	attrs := []netlink.Attribute{
		makeStringAttr(BatadvAttrHardIfname, "wlan0"),
		makeMACAttr(BatadvAttrNeighAddress, mac),
		makeUint32Attr(BatadvAttrLastSeenMsecs, 500),
		makeUint32Attr(BatadvAttrThroughput, throughput),
	}

	return genetlink.Message{
		Header: genetlink.Header{Command: BatadvCmdGetNeighbors, Version: 1},
		Data:   marshalAttrs(attrs),
	}
}

// buildOriginatorMessage creates a genetlink.Message for a single originator entry.
func buildOriginatorMessage(origMAC, neighMAC [6]byte, tq uint8) genetlink.Message {
	attrs := []netlink.Attribute{
		makeMACAttr(BatadvAttrOrigAddress, origMAC),
		makeStringAttr(BatadvAttrHardIfname, "wlan0"),
		makeMACAttr(BatadvAttrNeighAddress, neighMAC),
		makeUint32Attr(BatadvAttrLastSeenMsecs, 1234),
		makeUint8Attr(BatadvAttrTQ, tq),
	}

	return genetlink.Message{
		Header: genetlink.Header{Command: BatadvCmdGetOriginators, Version: 1},
		Data:   marshalAttrs(attrs),
	}
}

// marshalAttrs manually encodes netlink attributes into wire format.
func marshalAttrs(attrs []netlink.Attribute) []byte {
	var out []byte

	for _, a := range attrs {
		// Attribute: 2 bytes len, 2 bytes type, data, pad to 4-byte boundary
		attrLen := 4 + len(a.Data)
		padLen := (4 - (attrLen % 4)) % 4

		hdr := make([]byte, 4)
		binary.LittleEndian.PutUint16(hdr[0:2], uint16(attrLen))
		binary.LittleEndian.PutUint16(hdr[2:4], a.Type)

		out = append(out, hdr...)
		out = append(out, a.Data...)
		out = append(out, make([]byte, padLen)...)
	}

	return out
}

func testClient(q Querier) *Client {
	family := genetlink.Family{ID: 30, Name: BatadvNLName}

	return newClientWithQuerier(q, family, "bat0", 10, zerolog.Nop())
}

func TestGetMeshConfig_Netlink(t *testing.T) {
	q := &fakeQuerier{
		responses: [][]genetlink.Message{
			{buildMeshConfigMessage()},
		},
	}

	c := testClient(q)
	defer c.Close()

	cfg, err := c.GetMeshConfig()
	if err != nil {
		t.Fatalf("GetMeshConfig() error = %v", err)
	}

	if cfg.Version != "2023.1" {
		t.Errorf("Version = %q, want %q", cfg.Version, "2023.1")
	}

	if cfg.AlgoName != "BATMAN_IV" {
		t.Errorf("AlgoName = %q, want %q", cfg.AlgoName, "BATMAN_IV")
	}

	if cfg.GwMode != "server" {
		t.Errorf("GwMode = %q, want %q", cfg.GwMode, "server")
	}

	if cfg.HardAddress != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("HardAddress = %q, want %q", cfg.HardAddress, "aa:bb:cc:dd:ee:ff")
	}

	if cfg.OrigInterval != 1000 {
		t.Errorf("OrigInterval = %d, want 1000", cfg.OrigInterval)
	}

	if !cfg.FragmentationEnabled {
		t.Error("FragmentationEnabled should be true")
	}

	if q.calls != 1 {
		t.Errorf("expected 1 query call, got %d", q.calls)
	}
}

func TestGetMeshConfig_Cache(t *testing.T) {
	q := &fakeQuerier{
		responses: [][]genetlink.Message{
			{buildMeshConfigMessage()},
			{buildMeshConfigMessage()},
		},
	}

	c := testClient(q)
	defer c.Close()

	// First call: queries netlink
	cfg1, err := c.GetMeshConfig()
	if err != nil {
		t.Fatalf("first GetMeshConfig() error = %v", err)
	}

	if q.calls != 1 {
		t.Fatalf("expected 1 call after first query, got %d", q.calls)
	}

	// Second call: should hit cache
	cfg2, err := c.GetMeshConfig()
	if err != nil {
		t.Fatalf("second GetMeshConfig() error = %v", err)
	}

	if q.calls != 1 {
		t.Errorf("expected 1 call after second query (cached), got %d", q.calls)
	}

	if cfg1.Version != cfg2.Version {
		t.Errorf("cached result differs: %q vs %q", cfg1.Version, cfg2.Version)
	}

	// Invalidate cache
	c.InvalidateCache()

	// Third call: should query again
	_, err = c.GetMeshConfig()
	if err != nil {
		t.Fatalf("third GetMeshConfig() error = %v", err)
	}

	if q.calls != 2 {
		t.Errorf("expected 2 calls after invalidation, got %d", q.calls)
	}
}

func TestGetMeshConfig_Fallback(t *testing.T) {
	c := testClient(&fakeQuerier{})

	c.useBatctl.Store(true)
	defer c.Close()

	// In fallback mode, should call batctl — which will fail in test env.
	// We just verify it doesn't call the querier.
	_, err := c.GetMeshConfig()
	if err == nil {
		t.Skip("batctl available in test environment, skipping fallback behavior check")
	}

	// The error should be from batctl exec, not from our querier
	q := c.querier.(*fakeQuerier)
	if q.calls != 0 {
		t.Errorf("expected 0 querier calls in fallback mode, got %d", q.calls)
	}
}

func TestGetMeshGateways_Netlink(t *testing.T) {
	q := &fakeQuerier{
		responses: [][]genetlink.Message{
			{
				buildGatewayMessage([6]byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0x01}, 10000, true),
				buildGatewayMessage([6]byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0x02}, 5000, false),
				buildGatewayMessage([6]byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0x03}, 7500, false),
			},
		},
	}

	c := testClient(q)
	defer c.Close()

	gws, err := c.GetMeshGateways()
	if err != nil {
		t.Fatalf("GetMeshGateways() error = %v", err)
	}

	if gws.Count() != 3 {
		t.Fatalf("Count() = %d, want 3", gws.Count())
	}

	best := gws.GetBest()
	if best == nil {
		t.Fatal("GetBest() = nil, want non-nil")
	}

	if best.OrigAddress != "aa:bb:cc:dd:ee:01" {
		t.Errorf("best.OrigAddress = %q, want %q", best.OrigAddress, "aa:bb:cc:dd:ee:01")
	}

	if best.Throughput != 10000 {
		t.Errorf("best.Throughput = %d, want 10000", best.Throughput)
	}
}

func TestGetMeshNeighbors_Netlink(t *testing.T) {
	q := &fakeQuerier{
		responses: [][]genetlink.Message{
			{
				buildNeighborMessage([6]byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66}, 8000),
				buildNeighborMessage([6]byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}, 6000),
			},
		},
	}

	c := testClient(q)
	defer c.Close()

	ns, err := c.GetMeshNeighbors()
	if err != nil {
		t.Fatalf("GetMeshNeighbors() error = %v", err)
	}

	if ns.Count() != 2 {
		t.Fatalf("Count() = %d, want 2", ns.Count())
	}

	n := ns.FindByNeighAddress("11:22:33:44:55:66")
	if n == nil {
		t.Fatal("FindByNeighAddress() = nil, want non-nil")
	}

	if n.Throughput != 8000 {
		t.Errorf("Throughput = %d, want 8000", n.Throughput)
	}
}

func TestGetOriginators_Netlink(t *testing.T) {
	q := &fakeQuerier{
		responses: [][]genetlink.Message{
			{
				buildOriginatorMessage(
					[6]byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0x01},
					[6]byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0x02},
					200,
				),
				buildOriginatorMessage(
					[6]byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0x03},
					[6]byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0x04},
					150,
				),
				buildOriginatorMessage(
					[6]byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0x05},
					[6]byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0x06},
					100,
				),
			},
		},
	}

	c := testClient(q)
	defer c.Close()

	origs, err := c.GetOriginators()
	if err != nil {
		t.Fatalf("GetOriginators() error = %v", err)
	}

	if len(origs) != 3 {
		t.Fatalf("len(origs) = %d, want 3", len(origs))
	}

	if origs[0].OrigAddress != "aa:bb:cc:dd:ee:01" {
		t.Errorf("origs[0].OrigAddress = %q, want %q", origs[0].OrigAddress, "aa:bb:cc:dd:ee:01")
	}

	if origs[0].BestNeigh != "aa:bb:cc:dd:ee:02" {
		t.Errorf("origs[0].BestNeigh = %q, want %q", origs[0].BestNeigh, "aa:bb:cc:dd:ee:02")
	}

	if origs[0].TQ != 200 {
		t.Errorf("origs[0].TQ = %d, want 200", origs[0].TQ)
	}

	if origs[0].LastSeenMs != 1234 {
		t.Errorf("origs[0].LastSeenMs = %d, want 1234", origs[0].LastSeenMs)
	}

	// Verify originator count helper works with netlink-sourced data
	count := GetOriginatorCount(origs)
	if count != 3 {
		t.Errorf("GetOriginatorCount() = %d, want 3", count)
	}
}

func TestClient_ConnectionLost_Fallback(t *testing.T) {
	q := &fakeQuerier{
		err: syscall.ECONNRESET,
	}

	c := testClient(q)
	defer c.Close()

	// The query will fail with ECONNRESET, client should fall back to batctl
	_, err := c.GetMeshConfig()
	if err == nil {
		t.Skip("batctl available in test environment")
	}

	// Should have switched to fallback mode
	if !c.useBatctl.Load() {
		t.Error("expected useBatctl to be true after connection loss")
	}
}

func TestClient_ConnectionLost_SetsReconnecting(t *testing.T) {
	q := &fakeQuerier{
		err: syscall.ECONNRESET,
	}

	c := testClient(q)
	defer c.Close()

	// Trigger connection loss
	c.handleConnectionLoss()

	if !c.useBatctl.Load() {
		t.Error("expected useBatctl after connection loss")
	}

	if !c.reconnecting.Load() {
		t.Error("expected reconnecting to be true")
	}

	// Cancel context to stop reconnect loop
	c.cancel()
}

func TestClient_Close_Idempotent(t *testing.T) {
	q := &fakeQuerier{}
	c := testClient(q)

	// Close twice — should not panic
	if err := c.Close(); err != nil {
		t.Errorf("first Close() error = %v", err)
	}

	if err := c.Close(); err != nil {
		t.Errorf("second Close() error = %v", err)
	}

	if !q.closed {
		t.Error("expected querier to be closed")
	}
}

func TestClient_QueryAfterClose(t *testing.T) {
	q := &fakeQuerier{}
	c := testClient(q)
	c.Close()

	_, err := c.GetMeshConfig()
	if err == nil {
		t.Error("expected error after Close, got nil")
	}

	_, err = c.GetMeshGateways()
	if err == nil {
		t.Error("expected error after Close, got nil")
	}

	_, err = c.GetMeshNeighbors()
	if err == nil {
		t.Error("expected error after Close, got nil")
	}

	_, err = c.GetOriginators()
	if err == nil {
		t.Error("expected error after Close, got nil")
	}
}

func TestClient_ImplementsOriginatorProvider(t *testing.T) {
	// Compile-time check is in client.go, but verify at test time too
	var _ OriginatorProvider = (*Client)(nil)
}

func TestIsConnectionLost(t *testing.T) {
	c := testClient(&fakeQuerier{})
	defer c.Close()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"ENOENT", syscall.ENOENT, true},
		{"ECONNRESET", syscall.ECONNRESET, true},
		{"ECONNREFUSED", syscall.ECONNREFUSED, true},
		{"EPIPE", syscall.EPIPE, true},
		{"ENODEV", syscall.ENODEV, true},
		{"random error", errors.New("something else"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := c.isConnectionLost(tt.err)
			if got != tt.want {
				t.Errorf("isConnectionLost(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestGetMeshConfig_EmptyResponse(t *testing.T) {
	q := &fakeQuerier{
		responses: [][]genetlink.Message{
			{}, // empty response
		},
	}

	c := testClient(q)
	defer c.Close()

	_, err := c.GetMeshConfig()
	if err == nil {
		t.Error("expected error for empty response, got nil")
	}
}

func TestDefaultClient_Delegation(t *testing.T) {
	// Save and restore the default client
	old := defaultClient.Load()

	defer func() {
		if old != nil {
			defaultClient.Store(old)
		} else {
			defaultClient.Store(nil)
		}
	}()

	q := &fakeQuerier{
		responses: [][]genetlink.Message{
			{buildMeshConfigMessage()},
		},
	}

	c := testClient(q)
	defer c.Close()

	SetDefaultClient(c)

	// getDefaultClient should return our client
	got := getDefaultClient()
	if got != c {
		t.Error("getDefaultClient() did not return the set client")
	}

	// Clear it
	var empty atomic.Pointer[Client]
	defaultClient.Store(empty.Load())

	if getDefaultClient() != nil {
		t.Error("getDefaultClient() should be nil after clearing")
	}
}

func TestGetMeshGateways_EmptyDump(t *testing.T) {
	q := &fakeQuerier{
		responses: [][]genetlink.Message{
			{}, // no gateways
		},
	}

	c := testClient(q)
	defer c.Close()

	gws, err := c.GetMeshGateways()
	if err != nil {
		t.Fatalf("GetMeshGateways() error = %v", err)
	}

	if gws.Count() != 0 {
		t.Errorf("Count() = %d, want 0", gws.Count())
	}
}

func TestGetMeshNeighbors_EmptyDump(t *testing.T) {
	q := &fakeQuerier{
		responses: [][]genetlink.Message{
			{}, // no neighbors
		},
	}

	c := testClient(q)
	defer c.Close()

	ns, err := c.GetMeshNeighbors()
	if err != nil {
		t.Fatalf("GetMeshNeighbors() error = %v", err)
	}

	if ns.Count() != 0 {
		t.Errorf("Count() = %d, want 0", ns.Count())
	}
}

func TestGetOriginators_EmptyDump(t *testing.T) {
	q := &fakeQuerier{
		responses: [][]genetlink.Message{
			{}, // no originators
		},
	}

	c := testClient(q)
	defer c.Close()

	origs, err := c.GetOriginators()
	if err != nil {
		t.Fatalf("GetOriginators() error = %v", err)
	}

	if len(origs) != 0 {
		t.Errorf("len(origs) = %d, want 0", len(origs))
	}
}

// --- Race condition fix tests ---

func TestIsConnectionLost_EBADFAndErrClosed(t *testing.T) {
	c := testClient(&fakeQuerier{})
	defer c.cancel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"EBADF", syscall.EBADF, true},
		{"os.ErrClosed", os.ErrClosed, true},
		{"EBADF wrapped", errors.Join(syscall.EBADF, errors.New("outer")), true},
		{"os.ErrClosed wrapped", errors.Join(os.ErrClosed, errors.New("outer")), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := c.isConnectionLost(tt.err)
			if got != tt.want {
				t.Errorf("isConnectionLost(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// TestGetMeshConfig_EBADF_FallsBackToBatctl verifies that an EBADF error from
// the querier is treated as a connection loss, switching to batctl fallback
// instead of propagating the raw fd error.
func TestGetMeshConfig_EBADF_FallsBackToBatctl(t *testing.T) {
	q := &fakeQuerier{err: syscall.EBADF}
	c := testClient(q)
	defer c.cancel()

	_, _ = c.GetMeshConfig()

	if !c.useBatctl.Load() {
		t.Error("expected useBatctl=true after EBADF error")
	}
}

func TestGetMeshGateways_EBADF_FallsBackToBatctl(t *testing.T) {
	q := &fakeQuerier{err: syscall.EBADF}
	c := testClient(q)
	defer c.cancel()

	_, _ = c.GetMeshGateways()

	if !c.useBatctl.Load() {
		t.Error("expected useBatctl=true after EBADF error")
	}
}

func TestGetMeshNeighbors_EBADF_FallsBackToBatctl(t *testing.T) {
	q := &fakeQuerier{err: syscall.EBADF}
	c := testClient(q)
	defer c.cancel()

	_, _ = c.GetMeshNeighbors()

	if !c.useBatctl.Load() {
		t.Error("expected useBatctl=true after EBADF error")
	}
}

func TestGetOriginators_EBADF_FallsBackToBatctl(t *testing.T) {
	q := &fakeQuerier{err: syscall.EBADF}
	c := testClient(q)
	defer c.cancel()

	_, _ = c.GetOriginators()

	if !c.useBatctl.Load() {
		t.Error("expected useBatctl=true after EBADF error")
	}
}

// TestGetMeshConfig_ErrClosed_FallsBackToBatctl verifies that an os.ErrClosed
// error ("use of closed file") triggers batctl fallback rather than a hard error.
func TestGetMeshConfig_ErrClosed_FallsBackToBatctl(t *testing.T) {
	q := &fakeQuerier{err: os.ErrClosed}
	c := testClient(q)
	defer c.cancel()

	_, _ = c.GetMeshConfig()

	if !c.useBatctl.Load() {
		t.Error("expected useBatctl=true after os.ErrClosed error")
	}
}

// TestClose_NilsQuerier verifies that Close sets c.querier to nil so that any
// code path that checks c.querier cannot accidentally use the closed fd.
func TestClose_NilsQuerier(t *testing.T) {
	q := &fakeQuerier{}
	c := testClient(q)

	if err := c.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if c.querier != nil {
		t.Error("expected c.querier to be nil after Close()")
	}
}

// TestClose_BlocksUntilQueryCompletes verifies that Close() holds queryMu
// before closing the querier, so it cannot close a connection mid-Execute.
// This guards against the race: query goroutine calls Execute while Close
// concurrently frees the underlying fd.
func TestClose_BlocksUntilQueryCompletes(t *testing.T) {
	q := newBlockingQuerier(errors.New("query error"))
	c := testClient(q)

	// Start a query that will block in Execute (holding queryMu).
	go func() {
		_, _ = c.queryMeshConfig()
	}()

	// Wait for Execute to be running.
	<-q.started

	// Call Close() concurrently; with the fix it must wait for Execute to finish.
	closeDone := make(chan struct{})
	go func() {
		c.Close()
		close(closeDone)
	}()

	// Give Close a chance to (incorrectly) proceed without the lock.
	time.Sleep(20 * time.Millisecond)

	if q.isClosed() {
		t.Error("Close() closed the querier while Execute was still blocked — queryMu not held")
	}

	// Unblock Execute; Close() should now be able to acquire queryMu and proceed.
	close(q.unblock)

	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for Close() to complete after Execute returned")
	}

	if !q.isClosed() {
		t.Error("querier was not closed after Execute returned and Close() proceeded")
	}
}

// TestConcurrentQueriesAndClose_NoRace exercises concurrent queryMeshConfig and
// Close calls. Run with -race to verify the queryMu fix eliminates the data race
// on c.querier between the query path and the close path.
func TestConcurrentQueriesAndClose_NoRace(t *testing.T) {
	q := &syncFakeQuerier{msgs: []genetlink.Message{buildMeshConfigMessage()}}
	c := testClient(q)

	var wg sync.WaitGroup

	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = c.queryMeshConfig()
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		c.Close()
	}()

	wg.Wait()
}

// TestReconnectLoop_ClosesQuerier verifies that the reconnect loop closes the
// old querier after a connection loss, even when re-dialling batman-adv fails
// (which it always will in the test environment with no kernel module).
func TestReconnectLoop_ClosesQuerier(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping: reconnectLoop has a 1 s initial backoff")
	}

	q := &syncFakeQuerier{}
	c := testClient(q)

	c.handleConnectionLoss()

	deadline := time.After(5 * time.Second)
	poll := time.NewTicker(50 * time.Millisecond)
	defer poll.Stop()

	for {
		select {
		case <-deadline:
			t.Fatal("timed out: reconnectLoop did not close the old querier")
		case <-poll.C:
			if q.isClosed() {
				c.cancel() // stop the loop
				return
			}
		}
	}
}
