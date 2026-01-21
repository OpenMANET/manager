package gpsd

import (
	"bufio"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// mockGPSDServer simulates a GPSD server for testing
type mockGPSDServer struct {
	listener   net.Listener
	responses  []string
	requests   []string
	mutex      sync.Mutex
	t          *testing.T
	shouldFail bool
}

func newMockGPSDServer(t *testing.T, responses []string) (*mockGPSDServer, string) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create mock server: %v", err)
	}

	server := &mockGPSDServer{
		listener:  listener,
		responses: responses,
		requests:  make([]string, 0),
		t:         t,
	}

	go server.serve()

	return server, listener.Addr().String()
}

func (m *mockGPSDServer) serve() {
	for {
		conn, err := m.listener.Accept()
		if err != nil {
			return
		}
		go m.handleConnection(conn)
	}
}

func (m *mockGPSDServer) handleConnection(conn net.Conn) {
	defer conn.Close()

	// Send initial greeting (GPSD sends version info on connect)
	_, _ = conn.Write([]byte(`{"class":"VERSION","release":"3.20","rev":"3.20","proto_major":3,"proto_minor":14}` + "\n"))

	reader := bufio.NewReader(conn)
	for {
		line, err := reader.ReadString(';')
		if err != nil {
			return
		}

		m.mutex.Lock()
		m.requests = append(m.requests, line)

		if m.shouldFail {
			m.mutex.Unlock()
			conn.Close()
			return
		}

		// Send canned responses
		for _, response := range m.responses {
			_, _ = conn.Write([]byte(response + "\n"))
		}
		m.mutex.Unlock()
	}
}

func (m *mockGPSDServer) getRequests() []string {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return append([]string{}, m.requests...)
}

func (m *mockGPSDServer) close() {
	m.listener.Close()
}

func TestDial(t *testing.T) {
	server, addr := newMockGPSDServer(t, []string{})
	defer server.close()

	session, err := Dial(addr)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer session.Close()

	if session.address != addr {
		t.Errorf("Expected address %s, got %s", addr, session.address)
	}

	if session.socket == nil {
		t.Error("Socket should not be nil")
	}

	if session.reader == nil {
		t.Error("Reader should not be nil")
	}
}

func TestDial_ConnectionError(t *testing.T) {
	_, err := Dial("localhost:99999")
	if err == nil {
		t.Error("Expected error when connecting to invalid address")
	}
}

func TestSendCommand(t *testing.T) {
	server, addr := newMockGPSDServer(t, []string{})
	defer server.close()

	session, err := Dial(addr)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer session.Close()

	session.SendCommand("TEST")
	time.Sleep(100 * time.Millisecond)

	requests := server.getRequests()
	if len(requests) == 0 {
		t.Fatal("No requests received")
	}

	if !strings.Contains(requests[0], "?TEST;") {
		t.Errorf("Expected ?TEST; in request, got %s", requests[0])
	}
}

func TestVersion(t *testing.T) {
	versionResp := `{"class":"VERSION","release":"3.20","rev":"3.20","proto_major":3,"proto_minor":14}`
	server, addr := newMockGPSDServer(t, []string{versionResp})
	defer server.close()

	session, err := Dial(addr)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer session.Close()

	session.Version()
	time.Sleep(100 * time.Millisecond)

	requests := server.getRequests()
	if len(requests) == 0 {
		t.Fatal("No requests received")
	}

	if !strings.Contains(requests[0], "?VERSION;") {
		t.Errorf("Expected ?VERSION; in request, got %s", requests[0])
	}
}

func TestVersionSync(t *testing.T) {
	versionResp := `{"class":"VERSION","release":"3.20","rev":"3.20","proto_major":3,"proto_minor":14}`
	server, addr := newMockGPSDServer(t, []string{versionResp})
	defer server.close()

	session, err := Dial(addr)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer session.Close()

	response := session.VersionSync()
	if !strings.Contains(response, "VERSION") {
		t.Errorf("Expected VERSION in response, got %s", response)
	}
}

func TestPoll(t *testing.T) {
	pollResp := `{"class":"POLL","time":"2024-01-01T00:00:00.000Z","active":1}`
	server, addr := newMockGPSDServer(t, []string{pollResp})
	defer server.close()

	session, err := Dial(addr)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer session.Close()

	session.Poll()
	time.Sleep(100 * time.Millisecond)

	requests := server.getRequests()
	if len(requests) == 0 {
		t.Fatal("No requests received")
	}

	if !strings.Contains(requests[0], "?POLL;") {
		t.Errorf("Expected ?POLL; in request, got %s", requests[0])
	}
}

func TestPollSync(t *testing.T) {
	pollResp := `{"class":"POLL","time":"2024-01-01T00:00:00.000Z","active":1}`
	server, addr := newMockGPSDServer(t, []string{pollResp})
	defer server.close()

	session, err := Dial(addr)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer session.Close()

	response := session.PollSync()
	if !strings.Contains(response, "POLL") {
		t.Errorf("Expected POLL in response, got %s", response)
	}
}

func TestWatch(t *testing.T) {
	server, addr := newMockGPSDServer(t, []string{})
	defer server.close()

	session, err := Dial(addr)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer session.Close()

	// Test watch without parameters
	session.Watch()
	time.Sleep(100 * time.Millisecond)

	requests := server.getRequests()
	if len(requests) == 0 {
		t.Fatal("No requests received")
	}

	if !strings.Contains(requests[0], "?WATCH;") {
		t.Errorf("Expected ?WATCH; in request, got %s", requests[0])
	}
}

func TestWatch_WithParameters(t *testing.T) {
	server, addr := newMockGPSDServer(t, []string{})
	defer server.close()

	session, err := Dial(addr)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer session.Close()

	session.Watch(map[string]bool{"enable": true, "json": true})
	time.Sleep(100 * time.Millisecond)

	requests := server.getRequests()
	if len(requests) == 0 {
		t.Fatal("No requests received")
	}

	request := requests[0]
	if !strings.Contains(request, "?WATCH=") {
		t.Errorf("Expected ?WATCH= in request, got %s", request)
	}
	if !strings.Contains(request, `"enable":true`) {
		t.Errorf("Expected enable:true in request, got %s", request)
	}
	if !strings.Contains(request, `"json":true`) {
		t.Errorf("Expected json:true in request, got %s", request)
	}
}

func TestWatchSync(t *testing.T) {
	watchResp := `{"class":"WATCH","enable":true,"json":true}`
	server, addr := newMockGPSDServer(t, []string{watchResp})
	defer server.close()

	session, err := Dial(addr)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer session.Close()

	response := session.WatchSync(map[string]bool{"enable": true})
	if !strings.Contains(response, "WATCH") {
		t.Errorf("Expected WATCH in response, got %s", response)
	}
}

func TestSendCommandSync(t *testing.T) {
	testResp := `{"class":"TEST","result":"ok"}`
	server, addr := newMockGPSDServer(t, []string{testResp})
	defer server.close()

	session, err := Dial(addr)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer session.Close()

	response := session.SendCommandSync("TEST")
	if !strings.Contains(response, "TEST") {
		t.Errorf("Expected TEST in response, got %s", response)
	}
}

func TestSubscribe(t *testing.T) {
	server, addr := newMockGPSDServer(t, []string{})
	defer server.close()

	session, err := Dial(addr)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer session.Close()

	called := false
	filter := func(r interface{}) {
		called = true
	}

	session.Subscribe(msgClassTPV, filter)

	if len(session.filters[msgClassTPV]) != 1 {
		t.Errorf("Expected 1 filter for TPV, got %d", len(session.filters[msgClassTPV]))
	}

	// Test delivery
	session.deliverReport(msgClassTPV, &TPVReport{})
	if !called {
		t.Error("Filter was not called")
	}
}

func TestSubscribe_MultipleFilters(t *testing.T) {
	server, addr := newMockGPSDServer(t, []string{})
	defer server.close()

	session, err := Dial(addr)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer session.Close()

	callCount := 0
	filter1 := func(r interface{}) {
		callCount++
	}
	filter2 := func(r interface{}) {
		callCount++
	}

	session.Subscribe(msgClassTPV, filter1)
	session.Subscribe(msgClassTPV, filter2)

	session.deliverReport(msgClassTPV, &TPVReport{})

	if callCount != 2 {
		t.Errorf("Expected both filters to be called, got %d calls", callCount)
	}
}

func TestGetClass(t *testing.T) {
	tests := []struct {
		name     string
		json     string
		expected string
	}{
		{
			name:     "TPV report",
			json:     `{"class":"TPV","device":"/dev/pts/1","mode":3}`,
			expected: "TPV",
		},
		{
			name:     "SKY report",
			json:     `{"class":"SKY","device":"/dev/pts/1"}`,
			expected: "SKY",
		},
		{
			name:     "VERSION report",
			json:     `{"class":"VERSION","release":"3.20"}`,
			expected: "VERSION",
		},
		{
			name:     "Invalid JSON",
			json:     `{invalid json}`,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			class := getClass([]byte(tt.json))
			if class != tt.expected {
				t.Errorf("Expected class %s, got %s", tt.expected, class)
			}
		})
	}
}

func TestUnmarshalReport_TPV(t *testing.T) {
	jsonData := `{
		"class":"TPV",
		"device":"/dev/pts/1",
		"mode":3,
		"time":"2005-06-08T10:34:48.283Z",
		"lat":46.498293369,
		"lon":7.567411672,
		"alt":1343.127,
		"track":10.3788,
		"speed":0.091,
		"climb":-0.085
	}`

	report, err := unmarshalReport(msgClassTPV, []byte(jsonData))
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	tpv, ok := report.(*TPVReport)
	if !ok {
		t.Fatal("Report is not a TPVReport")
	}

	if tpv.Class != "TPV" {
		t.Errorf("Expected class TPV, got %s", tpv.Class)
	}
	if tpv.Mode != Mode3D {
		t.Errorf("Expected mode 3, got %d", tpv.Mode)
	}
	if tpv.Lat != 46.498293369 {
		t.Errorf("Expected lat 46.498293369, got %f", tpv.Lat)
	}
	if tpv.Lon != 7.567411672 {
		t.Errorf("Expected lon 7.567411672, got %f", tpv.Lon)
	}
}

func TestUnmarshalReport_SKY(t *testing.T) {
	jsonData := `{
		"class":"SKY",
		"device":"/dev/pts/1",
		"hdop":1.24,
		"pdop":1.99,
		"satellites":[
			{"PRN":8,"el":66,"az":189,"ss":44,"used":true},
			{"PRN":10,"el":51,"az":304,"ss":29,"used":true}
		]
	}`

	report, err := unmarshalReport(msgClassSKY, []byte(jsonData))
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	sky, ok := report.(*SKYReport)
	if !ok {
		t.Fatal("Report is not a SKYReport")
	}

	if sky.Class != "SKY" {
		t.Errorf("Expected class SKY, got %s", sky.Class)
	}
	if sky.Hdop != 1.24 {
		t.Errorf("Expected hdop 1.24, got %f", sky.Hdop)
	}
	if len(sky.Satellites) != 2 {
		t.Errorf("Expected 2 satellites, got %d", len(sky.Satellites))
	}
	if sky.Satellites[0].PRN != 8 {
		t.Errorf("Expected first satellite PRN 8, got %f", sky.Satellites[0].PRN)
	}
	if !sky.Satellites[0].Used {
		t.Error("Expected first satellite to be used")
	}
}

func TestUnmarshalReport_GST(t *testing.T) {
	jsonData := `{
		"class":"GST",
		"device":"/dev/ttyUSB0",
		"time":"2010-12-07T10:23:07.096Z",
		"rms":2.440,
		"major":1.660,
		"minor":1.120,
		"orient":68.989,
		"lat":1.600,
		"lon":1.200,
		"alt":2.520
	}`

	report, err := unmarshalReport(msgClassGST, []byte(jsonData))
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	gst, ok := report.(*GSTReport)
	if !ok {
		t.Fatal("Report is not a GSTReport")
	}

	if gst.Class != "GST" {
		t.Errorf("Expected class GST, got %s", gst.Class)
	}
	if gst.Rms != 2.440 {
		t.Errorf("Expected rms 2.440, got %f", gst.Rms)
	}
	if gst.Major != 1.660 {
		t.Errorf("Expected major 1.660, got %f", gst.Major)
	}
}

func TestUnmarshalReport_ATT(t *testing.T) {
	jsonData := `{
		"class":"ATT",
		"device":"/dev/ttyUSB0",
		"time":"2010-12-07T10:23:07.096Z",
		"heading":14223.00,
		"mag_st":"N",
		"pitch":169.00,
		"pitch_st":"N",
		"roll":-43.00,
		"roll_st":"N"
	}`

	report, err := unmarshalReport(msgClassATT, []byte(jsonData))
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	att, ok := report.(*ATTReport)
	if !ok {
		t.Fatal("Report is not an ATTReport")
	}

	if att.Class != "ATT" {
		t.Errorf("Expected class ATT, got %s", att.Class)
	}
	if att.Heading != 14223.00 {
		t.Errorf("Expected heading 14223.00, got %f", att.Heading)
	}
	if att.MagSt != "N" {
		t.Errorf("Expected mag_st N, got %s", att.MagSt)
	}
}

func TestUnmarshalReport_VERSION(t *testing.T) {
	jsonData := `{
		"class":"VERSION",
		"release":"3.20",
		"rev":"3.20.1",
		"proto_major":3,
		"proto_minor":14
	}`

	report, err := unmarshalReport(msgClassVersion, []byte(jsonData))
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	ver, ok := report.(*VERSIONReport)
	if !ok {
		t.Fatal("Report is not a VERSIONReport")
	}

	if ver.Class != "VERSION" {
		t.Errorf("Expected class VERSION, got %s", ver.Class)
	}
	if ver.Release != "3.20" {
		t.Errorf("Expected release 3.20, got %s", ver.Release)
	}
	if ver.ProtoMajor != 3 {
		t.Errorf("Expected proto_major 3, got %d", ver.ProtoMajor)
	}
}

func TestUnmarshalReport_DEVICES(t *testing.T) {
	jsonData := `{
		"class":"DEVICES",
		"devices":[
			{
				"class":"DEVICE",
				"path":"/dev/pts/1",
				"driver":"SiRF binary",
				"flags":1
			}
		]
	}`

	report, err := unmarshalReport(msgClassDevices, []byte(jsonData))
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	devices, ok := report.(*DEVICESReport)
	if !ok {
		t.Fatal("Report is not a DEVICESReport")
	}

	if devices.Class != "DEVICES" {
		t.Errorf("Expected class DEVICES, got %s", devices.Class)
	}
	if len(devices.Devices) != 1 {
		t.Errorf("Expected 1 device, got %d", len(devices.Devices))
	}
	if devices.Devices[0].Path != "/dev/pts/1" {
		t.Errorf("Expected path /dev/pts/1, got %s", devices.Devices[0].Path)
	}
}

func TestUnmarshalReport_PPS(t *testing.T) {
	jsonData := `{
		"class":"PPS",
		"device":"/dev/pps0",
		"real_sec":1234567890.0,
		"real_musec":123456.0,
		"clock_sec":1234567890.0,
		"clock_musec":123456.0
	}`

	report, err := unmarshalReport(msgClassPPS, []byte(jsonData))
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	pps, ok := report.(*PPSReport)
	if !ok {
		t.Fatal("Report is not a PPSReport")
	}

	if pps.Class != "PPS" {
		t.Errorf("Expected class PPS, got %s", pps.Class)
	}
	if pps.Device != "/dev/pps0" {
		t.Errorf("Expected device /dev/pps0, got %s", pps.Device)
	}
}

func TestUnmarshalReport_ERROR(t *testing.T) {
	jsonData := `{
		"class":"ERROR",
		"message":"Test error message"
	}`

	report, err := unmarshalReport(msgClassError, []byte(jsonData))
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	errReport, ok := report.(*ERRORReport)
	if !ok {
		t.Fatal("Report is not an ERRORReport")
	}

	if errReport.Class != "ERROR" {
		t.Errorf("Expected class ERROR, got %s", errReport.Class)
	}
	if errReport.Message != "Test error message" {
		t.Errorf("Expected message 'Test error message', got %s", errReport.Message)
	}
}

func TestWatchJSON_Integration(t *testing.T) {
	tpvReport := `{"class":"TPV","device":"/dev/pts/1","mode":3,"lat":46.498293369,"lon":7.567411672}`
	skyReport := `{"class":"SKY","device":"/dev/pts/1","hdop":1.24}`

	server, addr := newMockGPSDServer(t, []string{tpvReport, skyReport})
	defer server.close()

	session, err := Dial(addr)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer session.Close()

	tpvReceived := make(chan *TPVReport, 1)
	skyReceived := make(chan *SKYReport, 1)

	session.Subscribe(msgClassTPV, func(r interface{}) {
		if tpv, ok := r.(*TPVReport); ok {
			tpvReceived <- tpv
		}
	})

	session.Subscribe(msgClassSKY, func(r interface{}) {
		if sky, ok := r.(*SKYReport); ok {
			skyReceived <- sky
		}
	})

	session.Run(formatJSON)
	time.Sleep(200 * time.Millisecond)

	select {
	case tpv := <-tpvReceived:
		if tpv.Lat != 46.498293369 {
			t.Errorf("Expected lat 46.498293369, got %f", tpv.Lat)
		}
	case <-time.After(2 * time.Second):
		t.Error("Timeout waiting for TPV report")
	}

	select {
	case sky := <-skyReceived:
		if sky.Hdop != 1.24 {
			t.Errorf("Expected hdop 1.24, got %f", sky.Hdop)
		}
	case <-time.After(2 * time.Second):
		t.Error("Timeout waiting for SKY report")
	}
}

func TestWatchNMEA_Integration(t *testing.T) {
	devicesReport := `{"class":"DEVICES","devices":[]}`
	nmeaReport := `$GPGGA,123519,4807.038,N,01131.000,E,1,08,0.9,545.4,M,46.9,M,,*47`

	server, addr := newMockGPSDServer(t, []string{devicesReport, nmeaReport})
	defer server.close()

	session, err := Dial(addr)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer session.Close()

	devicesReceived := make(chan *DEVICESReport, 1)
	nmeaReceived := make(chan string, 1)

	session.Subscribe(msgClassDevices, func(r interface{}) {
		if devices, ok := r.(*DEVICESReport); ok {
			devicesReceived <- devices
		}
	})

	session.Subscribe("GPGGA", func(r interface{}) {
		if nmea, ok := r.(string); ok {
			nmeaReceived <- nmea
		}
	})

	session.Run(formatNMEA)
	time.Sleep(200 * time.Millisecond)

	select {
	case devices := <-devicesReceived:
		if devices.Class != "DEVICES" {
			t.Errorf("Expected class DEVICES, got %s", devices.Class)
		}
	case <-time.After(2 * time.Second):
		t.Error("Timeout waiting for DEVICES report")
	}

	select {
	case nmea := <-nmeaReceived:
		if !strings.HasPrefix(nmea, "$GPGGA") {
			t.Errorf("Expected NMEA to start with $GPGGA, got %s", nmea)
		}
	case <-time.After(2 * time.Second):
		t.Error("Timeout waiting for NMEA report")
	}
}

func TestClose(t *testing.T) {
	watchResp := `{"class":"WATCH","enable":false}`
	server, addr := newMockGPSDServer(t, []string{watchResp})
	defer server.close()

	session, err := Dial(addr)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}

	err = session.Close()
	if err != nil {
		t.Errorf("Close returned error: %v", err)
	}

	// Verify that Watch command with enable:false was sent
	time.Sleep(100 * time.Millisecond)
	requests := server.getRequests()
	foundDisableWatch := false
	for _, req := range requests {
		if strings.Contains(req, "WATCH") && strings.Contains(req, `"enable":false`) {
			foundDisableWatch = true
			break
		}
	}
	if !foundDisableWatch {
		t.Error("Expected WATCH disable command on close")
	}
}

func TestDeliverReport(t *testing.T) {
	server, addr := newMockGPSDServer(t, []string{})
	defer server.close()

	session, err := Dial(addr)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer session.Close()

	callCount := 0
	session.Subscribe(msgClassTPV, func(r interface{}) {
		callCount++
		if _, ok := r.(*TPVReport); !ok {
			t.Error("Report is not a TPVReport")
		}
	})

	tpv := &TPVReport{Class: "TPV", Mode: Mode3D}
	session.deliverReport(msgClassTPV, tpv)

	if callCount != 1 {
		t.Errorf("Expected 1 call, got %d", callCount)
	}
}

func TestDeliverReport_NoSubscribers(t *testing.T) {
	server, addr := newMockGPSDServer(t, []string{})
	defer server.close()

	session, err := Dial(addr)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer session.Close()

	// Should not panic when no subscribers exist
	tpv := &TPVReport{Class: "TPV", Mode: Mode3D}
	session.deliverReport(msgClassTPV, tpv)
}

func TestConstants(t *testing.T) {
	if DefaultAddress != "localhost:2947" {
		t.Errorf("Expected DefaultAddress to be localhost:2947, got %s", DefaultAddress)
	}

	if WatchCommand != "WATCH" {
		t.Errorf("Expected WatchCommand to be WATCH, got %s", WatchCommand)
	}

	if PollCommand != "POLL" {
		t.Errorf("Expected PollCommand to be POLL, got %s", PollCommand)
	}

	if VersionCommand != "VERSION" {
		t.Errorf("Expected VersionCommand to be VERSION, got %s", VersionCommand)
	}
}

// Benchmark tests
func BenchmarkUnmarshalTPV(b *testing.B) {
	jsonData := []byte(`{"class":"TPV","device":"/dev/pts/1","mode":3,"time":"2005-06-08T10:34:48.283Z","lat":46.498293369,"lon":7.567411672,"alt":1343.127}`)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = unmarshalReport(msgClassTPV, jsonData)
	}
}

func BenchmarkUnmarshalSKY(b *testing.B) {
	jsonData := []byte(`{"class":"SKY","device":"/dev/pts/1","hdop":1.24,"pdop":1.99,"satellites":[{"PRN":8,"el":66,"az":189,"ss":44,"used":true}]}`)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = unmarshalReport(msgClassSKY, jsonData)
	}
}

func BenchmarkGetClass(b *testing.B) {
	jsonData := []byte(`{"class":"TPV","device":"/dev/pts/1","mode":3}`)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = getClass(jsonData)
	}
}

func BenchmarkDeliverReport(b *testing.B) {
	server, addr := newMockGPSDServer(nil, []string{})
	defer server.close()

	session, _ := Dial(addr)
	defer session.Close()

	session.Subscribe(msgClassTPV, func(r interface{}) {})
	tpv := &TPVReport{Class: "TPV"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		session.deliverReport(msgClassTPV, tpv)
	}
}
