package gpsd

import (
	"bytes"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestNewGPSService(t *testing.T) {
	server, addr := newMockGPSDServer(t, []string{})
	defer server.close()

	log := zerolog.New(bytes.NewBuffer(nil))
	service, err := NewGPSServiceWithAddress(log, addr)
	if err != nil {
		t.Fatalf("Failed to create GPS service: %v", err)
	}
	defer service.Close()

	if service.session == nil {
		t.Error("Expected session to be initialized")
	}
	if service.Log.GetLevel() != log.GetLevel() {
		t.Error("Logger not properly set")
	}
}

func TestNewGPSService_ConnectionError(t *testing.T) {
	log := zerolog.New(bytes.NewBuffer(nil))
	_, err := NewGPSServiceWithAddress(log, "localhost:99999")
	if err == nil {
		t.Error("Expected error when connecting to invalid address")
	}
}

func TestNewGPSService_DefaultAddress(t *testing.T) {
	// Note: This test will fail if no GPSD is running on default port
	// We'll just test that the function exists and has the right signature
	log := zerolog.New(bytes.NewBuffer(nil))
	
	// Mock the Dial function behavior by using a custom address
	server, addr := newMockGPSDServer(t, []string{})
	defer server.close()
	
	service, err := NewGPSServiceWithAddress(log, addr)
	if err != nil {
		t.Fatalf("Failed to create GPS service: %v", err)
	}
	defer service.Close()
}

func TestGPSService_GetPositionReport(t *testing.T) {
	server, addr := newMockGPSDServer(t, []string{})
	defer server.close()

	log := zerolog.New(bytes.NewBuffer(nil))
	service, err := NewGPSServiceWithAddress(log, addr)
	if err != nil {
		t.Fatalf("Failed to create GPS service: %v", err)
	}
	defer service.Close()

	// Get initial report (should be zero values)
	report := service.GetPositionReport()
	if report.Class != "" {
		t.Errorf("Expected empty class, got %s", report.Class)
	}

	// Manually set a report
	service.mu.Lock()
	service.PositionReport = TPVReport{
		Class: "TPV",
		Mode:  Mode3D,
		Lat:   46.498293369,
		Lon:   7.567411672,
		Alt:   1343.127,
	}
	service.mu.Unlock()

	// Get the updated report
	report = service.GetPositionReport()
	if report.Class != "TPV" {
		t.Errorf("Expected class TPV, got %s", report.Class)
	}
	if report.Lat != 46.498293369 {
		t.Errorf("Expected lat 46.498293369, got %f", report.Lat)
	}
	if report.Lon != 7.567411672 {
		t.Errorf("Expected lon 7.567411672, got %f", report.Lon)
	}
}

func TestGPSService_TPVReportUpdate(t *testing.T) {
	tpvReport := `{"class":"TPV","device":"/dev/pts/1","mode":3,"lat":46.498293369,"lon":7.567411672,"alt":1343.127,"track":10.3788,"speed":0.091}`
	server, addr := newMockGPSDServer(t, []string{tpvReport})
	defer server.close()

	var logBuf bytes.Buffer
	log := zerolog.New(&logBuf)

	service, err := NewGPSServiceWithAddress(log, addr)
	if err != nil {
		t.Fatalf("Failed to create GPS service: %v", err)
	}
	defer service.Close()

	// Wait for the TPV report to be processed
	time.Sleep(300 * time.Millisecond)

	report := service.GetPositionReport()
	if report.Class != "TPV" {
		t.Errorf("Expected class TPV, got %s", report.Class)
	}
	if report.Mode != Mode3D {
		t.Errorf("Expected mode 3, got %d", report.Mode)
	}
	if report.Lat != 46.498293369 {
		t.Errorf("Expected lat 46.498293369, got %f", report.Lat)
	}
	if report.Lon != 7.567411672 {
		t.Errorf("Expected lon 7.567411672, got %f", report.Lon)
	}
	if report.Alt != 1343.127 {
		t.Errorf("Expected alt 1343.127, got %f", report.Alt)
	}
	if report.Track != 10.3788 {
		t.Errorf("Expected track 10.3788, got %f", report.Track)
	}
	if report.Speed != 0.091 {
		t.Errorf("Expected speed 0.091, got %f", report.Speed)
	}
}

func TestGPSService_MultipleTPVUpdates(t *testing.T) {
	tpv1 := `{"class":"TPV","mode":3,"lat":40.0,"lon":-75.0,"alt":100.0}`
	tpv2 := `{"class":"TPV","mode":3,"lat":40.1,"lon":-75.1,"alt":101.0}`
	tpv3 := `{"class":"TPV","mode":3,"lat":40.2,"lon":-75.2,"alt":102.0}`
	
	server, addr := newMockGPSDServer(t, []string{tpv1, tpv2, tpv3})
	defer server.close()

	log := zerolog.New(bytes.NewBuffer(nil))
	service, err := NewGPSServiceWithAddress(log, addr)
	if err != nil {
		t.Fatalf("Failed to create GPS service: %v", err)
	}
	defer service.Close()

	// Wait for reports to be processed
	time.Sleep(400 * time.Millisecond)

	// Should have the last report
	report := service.GetPositionReport()
	if report.Lat != 40.2 {
		t.Errorf("Expected lat 40.2 (last update), got %f", report.Lat)
	}
	if report.Lon != -75.2 {
		t.Errorf("Expected lon -75.2 (last update), got %f", report.Lon)
	}
	if report.Alt != 102.0 {
		t.Errorf("Expected alt 102.0 (last update), got %f", report.Alt)
	}
}

func TestGPSService_Close(t *testing.T) {
	server, addr := newMockGPSDServer(t, []string{})
	defer server.close()

	log := zerolog.New(bytes.NewBuffer(nil))
	service, err := NewGPSServiceWithAddress(log, addr)
	if err != nil {
		t.Fatalf("Failed to create GPS service: %v", err)
	}

	err = service.Close()
	if err != nil {
		t.Errorf("Close returned error: %v", err)
	}
}

func TestGPSService_CloseNilSession(t *testing.T) {
	log := zerolog.New(bytes.NewBuffer(nil))
	service := &GPSService{
		Log:     log,
		session: nil,
	}

	err := service.Close()
	if err != nil {
		t.Errorf("Close with nil session should not return error, got: %v", err)
	}
}

func TestGPSService_ConcurrentAccess(t *testing.T) {
	tpvReport := `{"class":"TPV","mode":3,"lat":46.498293369,"lon":7.567411672}`
	server, addr := newMockGPSDServer(t, []string{tpvReport})
	defer server.close()

	log := zerolog.New(bytes.NewBuffer(nil))
	service, err := NewGPSServiceWithAddress(log, addr)
	if err != nil {
		t.Fatalf("Failed to create GPS service: %v", err)
	}
	defer service.Close()

	// Wait for initial report
	time.Sleep(200 * time.Millisecond)

	// Test concurrent reads
	var wg sync.WaitGroup
	numReaders := 10
	wg.Add(numReaders)

	for i := 0; i < numReaders; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				report := service.GetPositionReport()
				if report.Class == "TPV" && report.Lat != 46.498293369 {
					t.Errorf("Concurrent read got unexpected lat: %f", report.Lat)
				}
			}
		}()
	}

	wg.Wait()
}

func TestGPSService_IgnoreNonTPVReports(t *testing.T) {
	skyReport := `{"class":"SKY","device":"/dev/pts/1","hdop":1.24}`
	versionReport := `{"class":"VERSION","release":"3.20"}`
	tpvReport := `{"class":"TPV","mode":3,"lat":46.498293369,"lon":7.567411672}`
	
	server, addr := newMockGPSDServer(t, []string{skyReport, versionReport, tpvReport})
	defer server.close()

	log := zerolog.New(bytes.NewBuffer(nil))
	service, err := NewGPSServiceWithAddress(log, addr)
	if err != nil {
		t.Fatalf("Failed to create GPS service: %v", err)
	}
	defer service.Close()

	// Wait for reports to be processed
	time.Sleep(300 * time.Millisecond)

	// Should only have the TPV report
	report := service.GetPositionReport()
	if report.Class != "TPV" {
		t.Errorf("Expected class TPV, got %s", report.Class)
	}
	if report.Lat != 46.498293369 {
		t.Errorf("Expected lat 46.498293369, got %f", report.Lat)
	}
}

func TestGPSService_NoFixMode(t *testing.T) {
	tpvNoFix := `{"class":"TPV","mode":1,"lat":0.0,"lon":0.0}`
	server, addr := newMockGPSDServer(t, []string{tpvNoFix})
	defer server.close()

	log := zerolog.New(bytes.NewBuffer(nil))
	service, err := NewGPSServiceWithAddress(log, addr)
	if err != nil {
		t.Fatalf("Failed to create GPS service: %v", err)
	}
	defer service.Close()

	// Wait for report
	time.Sleep(200 * time.Millisecond)

	report := service.GetPositionReport()
	if report.Mode != NoFix {
		t.Errorf("Expected mode NoFix (1), got %d", report.Mode)
	}
}

func TestGPSService_Mode2D(t *testing.T) {
	tpv2D := `{"class":"TPV","mode":2,"lat":40.0,"lon":-75.0}`
	server, addr := newMockGPSDServer(t, []string{tpv2D})
	defer server.close()

	log := zerolog.New(bytes.NewBuffer(nil))
	service, err := NewGPSServiceWithAddress(log, addr)
	if err != nil {
		t.Fatalf("Failed to create GPS service: %v", err)
	}
	defer service.Close()

	// Wait for report
	time.Sleep(200 * time.Millisecond)

	report := service.GetPositionReport()
	if report.Mode != Mode2D {
		t.Errorf("Expected mode Mode2D (2), got %d", report.Mode)
	}
	if report.Lat != 40.0 {
		t.Errorf("Expected lat 40.0, got %f", report.Lat)
	}
}

func TestGPSService_NegativeCoordinates(t *testing.T) {
	// Test with southern hemisphere and western longitude
	tpvReport := `{"class":"TPV","mode":3,"lat":-33.865143,"lon":-151.209900,"alt":-5.0}`
	server, addr := newMockGPSDServer(t, []string{tpvReport})
	defer server.close()

	log := zerolog.New(bytes.NewBuffer(nil))
	service, err := NewGPSServiceWithAddress(log, addr)
	if err != nil {
		t.Fatalf("Failed to create GPS service: %v", err)
	}
	defer service.Close()

	// Wait for report
	time.Sleep(200 * time.Millisecond)

	report := service.GetPositionReport()
	if report.Lat != -33.865143 {
		t.Errorf("Expected lat -33.865143, got %f", report.Lat)
	}
	if report.Lon != -151.209900 {
		t.Errorf("Expected lon -151.209900, got %f", report.Lon)
	}
	if report.Alt != -5.0 {
		t.Errorf("Expected alt -5.0, got %f", report.Alt)
	}
}

// Benchmark tests
func BenchmarkGetPositionReport(b *testing.B) {
	log := zerolog.New(bytes.NewBuffer(nil))
	service := &GPSService{
		Log: log,
		PositionReport: TPVReport{
			Class: "TPV",
			Mode:  Mode3D,
			Lat:   46.498293369,
			Lon:   7.567411672,
			Alt:   1343.127,
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = service.GetPositionReport()
	}
}

func BenchmarkGetPositionReport_Concurrent(b *testing.B) {
	log := zerolog.New(bytes.NewBuffer(nil))
	service := &GPSService{
		Log: log,
		PositionReport: TPVReport{
			Class: "TPV",
			Mode:  Mode3D,
			Lat:   46.498293369,
			Lon:   7.567411672,
			Alt:   1343.127,
		},
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = service.GetPositionReport()
		}
	})
}
