package gpsd

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestTPVReport_Unmarshal(t *testing.T) {
	jsonData := `{
		"class":"TPV",
		"tag":"MID2",
		"device":"/dev/ttyUSB0",
		"mode":3,
		"time":"2005-06-08T10:34:48.283Z",
		"ept":0.005,
		"lat":46.498293369,
		"lon":7.567411672,
		"alt":1343.127,
		"epx":15.319,
		"epy":17.054,
		"epv":32.321,
		"track":10.3788,
		"speed":0.091,
		"climb":-0.085,
		"epd":12.0,
		"eps":0.5,
		"epc":0.3
	}`

	var report TPVReport
	err := json.Unmarshal([]byte(jsonData), &report)
	if err != nil {
		t.Fatalf("Failed to unmarshal TPV report: %v", err)
	}

	if report.Class != "TPV" {
		t.Errorf("Expected class TPV, got %s", report.Class)
	}
	if report.Tag != "MID2" {
		t.Errorf("Expected tag MID2, got %s", report.Tag)
	}
	if report.Device != "/dev/ttyUSB0" {
		t.Errorf("Expected device /dev/ttyUSB0, got %s", report.Device)
	}
	if report.Mode != Mode3D {
		t.Errorf("Expected mode 3, got %d", report.Mode)
	}
	if report.Ept != 0.005 {
		t.Errorf("Expected ept 0.005, got %f", report.Ept)
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
	if report.Epx != 15.319 {
		t.Errorf("Expected epx 15.319, got %f", report.Epx)
	}
	if report.Epy != 17.054 {
		t.Errorf("Expected epy 17.054, got %f", report.Epy)
	}
	if report.Epv != 32.321 {
		t.Errorf("Expected epv 32.321, got %f", report.Epv)
	}
	if report.Track != 10.3788 {
		t.Errorf("Expected track 10.3788, got %f", report.Track)
	}
	if report.Speed != 0.091 {
		t.Errorf("Expected speed 0.091, got %f", report.Speed)
	}
	if report.Climb != -0.085 {
		t.Errorf("Expected climb -0.085, got %f", report.Climb)
	}
	if report.Epd != 12.0 {
		t.Errorf("Expected epd 12.0, got %f", report.Epd)
	}
	if report.Eps != 0.5 {
		t.Errorf("Expected eps 0.5, got %f", report.Eps)
	}
	if report.Epc != 0.3 {
		t.Errorf("Expected epc 0.3, got %f", report.Epc)
	}

	expectedTime, _ := time.Parse(time.RFC3339, "2005-06-08T10:34:48.283Z")
	if !report.Time.Equal(expectedTime) {
		t.Errorf("Expected time %v, got %v", expectedTime, report.Time)
	}
}

func TestTPVReport_MinimalFields(t *testing.T) {
	jsonData := `{
		"class":"TPV",
		"mode":1
	}`

	var report TPVReport
	err := json.Unmarshal([]byte(jsonData), &report)
	if err != nil {
		t.Fatalf("Failed to unmarshal minimal TPV report: %v", err)
	}

	if report.Class != "TPV" {
		t.Errorf("Expected class TPV, got %s", report.Class)
	}
	if report.Mode != NoFix {
		t.Errorf("Expected mode 1 (NoFix), got %d", report.Mode)
	}
}

func TestSKYReport_Unmarshal(t *testing.T) {
	jsonData := `{
		"class":"SKY",
		"tag":"MID2",
		"device":"/dev/ttyUSB0",
		"time":"2005-07-08T11:28:07.114Z",
		"xdop":1.55,
		"ydop":1.35,
		"vdop":1.89,
		"tdop":1.45,
		"hdop":1.24,
		"pdop":1.99,
		"gdop":2.34,
		"satellites":[
			{"PRN":23,"el":6,"az":84,"ss":0,"used":false},
			{"PRN":8,"el":66,"az":189,"ss":44,"used":true},
			{"PRN":10,"el":51,"az":304,"ss":29,"used":true}
		]
	}`

	var report SKYReport
	err := json.Unmarshal([]byte(jsonData), &report)
	if err != nil {
		t.Fatalf("Failed to unmarshal SKY report: %v", err)
	}

	if report.Class != "SKY" {
		t.Errorf("Expected class SKY, got %s", report.Class)
	}
	if report.Tag != "MID2" {
		t.Errorf("Expected tag MID2, got %s", report.Tag)
	}
	if report.Device != "/dev/ttyUSB0" {
		t.Errorf("Expected device /dev/ttyUSB0, got %s", report.Device)
	}
	if report.Xdop != 1.55 {
		t.Errorf("Expected xdop 1.55, got %f", report.Xdop)
	}
	if report.Ydop != 1.35 {
		t.Errorf("Expected ydop 1.35, got %f", report.Ydop)
	}
	if report.Vdop != 1.89 {
		t.Errorf("Expected vdop 1.89, got %f", report.Vdop)
	}
	if report.Tdop != 1.45 {
		t.Errorf("Expected tdop 1.45, got %f", report.Tdop)
	}
	if report.Hdop != 1.24 {
		t.Errorf("Expected hdop 1.24, got %f", report.Hdop)
	}
	if report.Pdop != 1.99 {
		t.Errorf("Expected pdop 1.99, got %f", report.Pdop)
	}
	if report.Gdop != 2.34 {
		t.Errorf("Expected gdop 2.34, got %f", report.Gdop)
	}
	if len(report.Satellites) != 3 {
		t.Fatalf("Expected 3 satellites, got %d", len(report.Satellites))
	}

	// Check first satellite
	sat := report.Satellites[0]
	if sat.PRN != 23 {
		t.Errorf("Expected satellite 0 PRN 23, got %f", sat.PRN)
	}
	if sat.El != 6 {
		t.Errorf("Expected satellite 0 elevation 6, got %f", sat.El)
	}
	if sat.Az != 84 {
		t.Errorf("Expected satellite 0 azimuth 84, got %f", sat.Az)
	}
	if sat.Ss != 0 {
		t.Errorf("Expected satellite 0 signal strength 0, got %f", sat.Ss)
	}
	if sat.Used {
		t.Error("Expected satellite 0 to not be used")
	}

	// Check used satellite
	usedSat := report.Satellites[1]
	if !usedSat.Used {
		t.Error("Expected satellite 1 to be used")
	}
	if usedSat.Ss != 44 {
		t.Errorf("Expected satellite 1 signal strength 44, got %f", usedSat.Ss)
	}
}

func TestSKYReport_EmptySatellites(t *testing.T) {
	jsonData := `{
		"class":"SKY",
		"device":"/dev/ttyUSB0",
		"satellites":[]
	}`

	var report SKYReport
	err := json.Unmarshal([]byte(jsonData), &report)
	if err != nil {
		t.Fatalf("Failed to unmarshal SKY report with empty satellites: %v", err)
	}

	if len(report.Satellites) != 0 {
		t.Errorf("Expected 0 satellites, got %d", len(report.Satellites))
	}
}

func TestGSTReport_Unmarshal(t *testing.T) {
	jsonData := `{
		"class":"GST",
		"tag":"MID2",
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

	var report GSTReport
	err := json.Unmarshal([]byte(jsonData), &report)
	if err != nil {
		t.Fatalf("Failed to unmarshal GST report: %v", err)
	}

	if report.Class != "GST" {
		t.Errorf("Expected class GST, got %s", report.Class)
	}
	if report.Tag != "MID2" {
		t.Errorf("Expected tag MID2, got %s", report.Tag)
	}
	if report.Device != "/dev/ttyUSB0" {
		t.Errorf("Expected device /dev/ttyUSB0, got %s", report.Device)
	}
	if report.Rms != 2.440 {
		t.Errorf("Expected rms 2.440, got %f", report.Rms)
	}
	if report.Major != 1.660 {
		t.Errorf("Expected major 1.660, got %f", report.Major)
	}
	if report.Minor != 1.120 {
		t.Errorf("Expected minor 1.120, got %f", report.Minor)
	}
	if report.Orient != 68.989 {
		t.Errorf("Expected orient 68.989, got %f", report.Orient)
	}
	if report.Lat != 1.600 {
		t.Errorf("Expected lat 1.600, got %f", report.Lat)
	}
	if report.Lon != 1.200 {
		t.Errorf("Expected lon 1.200, got %f", report.Lon)
	}
	if report.Alt != 2.520 {
		t.Errorf("Expected alt 2.520, got %f", report.Alt)
	}

	expectedTime, _ := time.Parse(time.RFC3339, "2010-12-07T10:23:07.096Z")
	if !report.Time.Equal(expectedTime) {
		t.Errorf("Expected time %v, got %v", expectedTime, report.Time)
	}
}

func TestATTReport_Unmarshal(t *testing.T) {
	jsonData := `{
		"class":"ATT",
		"tag":"MID2",
		"device":"/dev/ttyUSB0",
		"time":"2010-12-07T10:23:07.096Z",
		"heading":14223.00,
		"mag_st":"N",
		"pitch":169.00,
		"pitch_st":"N",
		"yaw":42.00,
		"yaw_st":"N",
		"roll":-43.00,
		"roll_st":"N",
		"dip":13641.000,
		"mag_len":2500.0,
		"mag_x":2454.000,
		"mag_y":123.000,
		"mag_z":-456.000,
		"acc_len":9.8,
		"acc_x":0.1,
		"acc_y":0.2,
		"acc_z":9.7,
		"gyro_x":1.5,
		"gyro_y":2.3,
		"depth":150.5,
		"temperature":22.5
	}`

	var report ATTReport
	err := json.Unmarshal([]byte(jsonData), &report)
	if err != nil {
		t.Fatalf("Failed to unmarshal ATT report: %v", err)
	}

	if report.Class != "ATT" {
		t.Errorf("Expected class ATT, got %s", report.Class)
	}
	if report.Tag != "MID2" {
		t.Errorf("Expected tag MID2, got %s", report.Tag)
	}
	if report.Device != "/dev/ttyUSB0" {
		t.Errorf("Expected device /dev/ttyUSB0, got %s", report.Device)
	}
	if report.Heading != 14223.00 {
		t.Errorf("Expected heading 14223.00, got %f", report.Heading)
	}
	if report.MagSt != "N" {
		t.Errorf("Expected mag_st N, got %s", report.MagSt)
	}
	if report.Pitch != 169.00 {
		t.Errorf("Expected pitch 169.00, got %f", report.Pitch)
	}
	if report.PitchSt != "N" {
		t.Errorf("Expected pitch_st N, got %s", report.PitchSt)
	}
	if report.Yaw != 42.00 {
		t.Errorf("Expected yaw 42.00, got %f", report.Yaw)
	}
	if report.YawSt != "N" {
		t.Errorf("Expected yaw_st N, got %s", report.YawSt)
	}
	if report.Roll != -43.00 {
		t.Errorf("Expected roll -43.00, got %f", report.Roll)
	}
	if report.RollSt != "N" {
		t.Errorf("Expected roll_st N, got %s", report.RollSt)
	}
	if report.Dip != 13641.000 {
		t.Errorf("Expected dip 13641.000, got %f", report.Dip)
	}
	if report.MagLen != 2500.0 {
		t.Errorf("Expected mag_len 2500.0, got %f", report.MagLen)
	}
	if report.MagX != 2454.000 {
		t.Errorf("Expected mag_x 2454.000, got %f", report.MagX)
	}
	if report.MagY != 123.000 {
		t.Errorf("Expected mag_y 123.000, got %f", report.MagY)
	}
	if report.MagZ != -456.000 {
		t.Errorf("Expected mag_z -456.000, got %f", report.MagZ)
	}
	if report.AccLen != 9.8 {
		t.Errorf("Expected acc_len 9.8, got %f", report.AccLen)
	}
	if report.AccX != 0.1 {
		t.Errorf("Expected acc_x 0.1, got %f", report.AccX)
	}
	if report.AccY != 0.2 {
		t.Errorf("Expected acc_y 0.2, got %f", report.AccY)
	}
	if report.AccZ != 9.7 {
		t.Errorf("Expected acc_z 9.7, got %f", report.AccZ)
	}
	if report.GyroX != 1.5 {
		t.Errorf("Expected gyro_x 1.5, got %f", report.GyroX)
	}
	if report.GyroY != 2.3 {
		t.Errorf("Expected gyro_y 2.3, got %f", report.GyroY)
	}
	if report.Depth != 150.5 {
		t.Errorf("Expected depth 150.5, got %f", report.Depth)
	}
	if report.Temperature != 22.5 {
		t.Errorf("Expected temperature 22.5, got %f", report.Temperature)
	}
}

func TestVERSIONReport_Unmarshal(t *testing.T) {
	jsonData := `{
		"class":"VERSION",
		"release":"3.20",
		"rev":"3.20.1-rc2",
		"proto_major":3,
		"proto_minor":14,
		"remote":"somehost"
	}`

	var report VERSIONReport
	err := json.Unmarshal([]byte(jsonData), &report)
	if err != nil {
		t.Fatalf("Failed to unmarshal VERSION report: %v", err)
	}

	if report.Class != "VERSION" {
		t.Errorf("Expected class VERSION, got %s", report.Class)
	}
	if report.Release != "3.20" {
		t.Errorf("Expected release 3.20, got %s", report.Release)
	}
	if report.Rev != "3.20.1-rc2" {
		t.Errorf("Expected rev 3.20.1-rc2, got %s", report.Rev)
	}
	if report.ProtoMajor != 3 {
		t.Errorf("Expected proto_major 3, got %d", report.ProtoMajor)
	}
	if report.ProtoMinor != 14 {
		t.Errorf("Expected proto_minor 14, got %d", report.ProtoMinor)
	}
	if report.Remote != "somehost" {
		t.Errorf("Expected remote somehost, got %s", report.Remote)
	}
}

func TestDEVICESReport_Unmarshal(t *testing.T) {
	jsonData := `{
		"class":"DEVICES",
		"devices":[
			{
				"class":"DEVICE",
				"path":"/dev/ttyUSB0",
				"activated":"2010-12-07T10:23:07.096Z",
				"flags":1,
				"driver":"SiRF binary",
				"subtype":"Mode 1",
				"bps":4800,
				"parity":"N",
				"stopbits":1,
				"native":1,
				"cycle":1.00,
				"mincycle":0.25
			},
			{
				"class":"DEVICE",
				"path":"/dev/ttyUSB1",
				"flags":0,
				"driver":"NMEA"
			}
		],
		"remote":"gpsd://localhost:2947"
	}`

	var report DEVICESReport
	err := json.Unmarshal([]byte(jsonData), &report)
	if err != nil {
		t.Fatalf("Failed to unmarshal DEVICES report: %v", err)
	}

	if report.Class != "DEVICES" {
		t.Errorf("Expected class DEVICES, got %s", report.Class)
	}
	if report.Remote != "gpsd://localhost:2947" {
		t.Errorf("Expected remote gpsd://localhost:2947, got %s", report.Remote)
	}
	if len(report.Devices) != 2 {
		t.Fatalf("Expected 2 devices, got %d", len(report.Devices))
	}

	// Check first device
	dev := report.Devices[0]
	if dev.Class != "DEVICE" {
		t.Errorf("Expected device class DEVICE, got %s", dev.Class)
	}
	if dev.Path != "/dev/ttyUSB0" {
		t.Errorf("Expected path /dev/ttyUSB0, got %s", dev.Path)
	}
	if dev.Activated != "2010-12-07T10:23:07.096Z" {
		t.Errorf("Expected activated 2010-12-07T10:23:07.096Z, got %s", dev.Activated)
	}
	if dev.Flags != 1 {
		t.Errorf("Expected flags 1, got %d", dev.Flags)
	}
	if dev.Driver != "SiRF binary" {
		t.Errorf("Expected driver SiRF binary, got %s", dev.Driver)
	}
	if dev.Subtype != "Mode 1" {
		t.Errorf("Expected subtype Mode 1, got %s", dev.Subtype)
	}
	if dev.Bps != 4800 {
		t.Errorf("Expected bps 4800, got %d", dev.Bps)
	}
	if dev.Parity != "N" {
		t.Errorf("Expected parity N, got %s", dev.Parity)
	}
	if dev.Stopbits != 1 {
		t.Errorf("Expected stopbits 1, got %d", dev.Stopbits)
	}
	if dev.Native != 1 {
		t.Errorf("Expected native 1, got %d", dev.Native)
	}
	if dev.Cycle != 1.00 {
		t.Errorf("Expected cycle 1.00, got %f", dev.Cycle)
	}
	if dev.Mincycle != 0.25 {
		t.Errorf("Expected mincycle 0.25, got %f", dev.Mincycle)
	}

	// Check second device (minimal fields)
	dev2 := report.Devices[1]
	if dev2.Path != "/dev/ttyUSB1" {
		t.Errorf("Expected path /dev/ttyUSB1, got %s", dev2.Path)
	}
	if dev2.Driver != "NMEA" {
		t.Errorf("Expected driver NMEA, got %s", dev2.Driver)
	}
}

func TestDEVICEReport_String(t *testing.T) {
	dev := DEVICEReport{
		Path:      "/dev/ttyUSB0",
		Activated: "2010-12-07T10:23:07.096Z",
		Flags:     1,
		Driver:    "SiRF binary",
		Subtype:   "Mode 1",
		Bps:       4800,
		Parity:    "N",
		Stopbits:  1,
		Native:    1,
		Cycle:     1.00,
		Mincycle:  0.25,
	}

	str := dev.String()

	expectedFields := []string{
		"path=/dev/ttyUSB0",
		"activated=2010-12-07T10:23:07.096Z",
		"flags=1",
		"driver=SiRF binary",
		"subtype=Mode 1",
		"bps=4800",
		"parity=N",
		"stopbits=1",
		"native=1",
		"cycle=1.000000",
		"mincycle=0.250000",
	}

	for _, field := range expectedFields {
		if !strings.Contains(str, field) {
			t.Errorf("Expected string to contain '%s', got: %s", field, str)
		}
	}
}

func TestPPSReport_Unmarshal(t *testing.T) {
	jsonData := `{
		"class":"PPS",
		"device":"/dev/pps0",
		"real_sec":1234567890.0,
		"real_musec":123456.0,
		"clock_sec":1234567891.0,
		"clock_musec":234567.0
	}`

	var report PPSReport
	err := json.Unmarshal([]byte(jsonData), &report)
	if err != nil {
		t.Fatalf("Failed to unmarshal PPS report: %v", err)
	}

	if report.Class != "PPS" {
		t.Errorf("Expected class PPS, got %s", report.Class)
	}
	if report.Device != "/dev/pps0" {
		t.Errorf("Expected device /dev/pps0, got %s", report.Device)
	}
	if report.RealSec != 1234567890.0 {
		t.Errorf("Expected real_sec 1234567890.0, got %f", report.RealSec)
	}
	if report.RealMusec != 123456.0 {
		t.Errorf("Expected real_musec 123456.0, got %f", report.RealMusec)
	}
	if report.ClockSec != 1234567891.0 {
		t.Errorf("Expected clock_sec 1234567891.0, got %f", report.ClockSec)
	}
	if report.ClockMusec != 234567.0 {
		t.Errorf("Expected clock_musec 234567.0, got %f", report.ClockMusec)
	}
}

func TestERRORReport_Unmarshal(t *testing.T) {
	jsonData := `{
		"class":"ERROR",
		"message":"Invalid command"
	}`

	var report ERRORReport
	err := json.Unmarshal([]byte(jsonData), &report)
	if err != nil {
		t.Fatalf("Failed to unmarshal ERROR report: %v", err)
	}

	if report.Class != "ERROR" {
		t.Errorf("Expected class ERROR, got %s", report.Class)
	}
	if report.Message != "Invalid command" {
		t.Errorf("Expected message 'Invalid command', got %s", report.Message)
	}
}

func TestMode_Constants(t *testing.T) {
	tests := []struct {
		name     string
		mode     Mode
		expected byte
	}{
		{"NoValueSeen", NoValueSeen, 0},
		{"NoFix", NoFix, 1},
		{"Mode2D", Mode2D, 2},
		{"Mode3D", Mode3D, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if byte(tt.mode) != tt.expected {
				t.Errorf("Expected mode %s to be %d, got %d", tt.name, tt.expected, tt.mode)
			}
		})
	}
}

func TestMode_JSONMarshalUnmarshal(t *testing.T) {
	type testStruct struct {
		Mode Mode `json:"mode"`
	}

	tests := []struct {
		name     string
		json     string
		expected Mode
	}{
		{"NoValueSeen", `{"mode":0}`, NoValueSeen},
		{"NoFix", `{"mode":1}`, NoFix},
		{"Mode2D", `{"mode":2}`, Mode2D},
		{"Mode3D", `{"mode":3}`, Mode3D},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ts testStruct
			err := json.Unmarshal([]byte(tt.json), &ts)
			if err != nil {
				t.Fatalf("Failed to unmarshal: %v", err)
			}
			if ts.Mode != tt.expected {
				t.Errorf("Expected mode %d, got %d", tt.expected, ts.Mode)
			}

			// Test marshaling
			data, err := json.Marshal(ts)
			if err != nil {
				t.Fatalf("Failed to marshal: %v", err)
			}
			if string(data) != tt.json {
				t.Errorf("Expected JSON %s, got %s", tt.json, string(data))
			}
		})
	}
}

func TestGpsdReport_Unmarshal(t *testing.T) {
	jsonData := `{"class":"TEST"}`

	var report gpsdReport
	err := json.Unmarshal([]byte(jsonData), &report)
	if err != nil {
		t.Fatalf("Failed to unmarshal gpsdReport: %v", err)
	}

	if report.Class != "TEST" {
		t.Errorf("Expected class TEST, got %s", report.Class)
	}
}

func TestSatellite_AllFields(t *testing.T) {
	jsonData := `{
		"PRN":8,
		"az":189.5,
		"el":66.3,
		"ss":44.2,
		"used":true
	}`

	var sat Satellite
	err := json.Unmarshal([]byte(jsonData), &sat)
	if err != nil {
		t.Fatalf("Failed to unmarshal Satellite: %v", err)
	}

	if sat.PRN != 8 {
		t.Errorf("Expected PRN 8, got %f", sat.PRN)
	}
	if sat.Az != 189.5 {
		t.Errorf("Expected azimuth 189.5, got %f", sat.Az)
	}
	if sat.El != 66.3 {
		t.Errorf("Expected elevation 66.3, got %f", sat.El)
	}
	if sat.Ss != 44.2 {
		t.Errorf("Expected signal strength 44.2, got %f", sat.Ss)
	}
	if !sat.Used {
		t.Error("Expected satellite to be used")
	}
}

func TestTPVReport_ZeroValues(t *testing.T) {
	jsonData := `{
		"class":"TPV",
		"mode":0,
		"lat":0.0,
		"lon":0.0,
		"alt":0.0
	}`

	var report TPVReport
	err := json.Unmarshal([]byte(jsonData), &report)
	if err != nil {
		t.Fatalf("Failed to unmarshal TPV report with zero values: %v", err)
	}

	if report.Mode != NoValueSeen {
		t.Errorf("Expected mode 0, got %d", report.Mode)
	}
	if report.Lat != 0.0 {
		t.Errorf("Expected lat 0.0, got %f", report.Lat)
	}
	if report.Lon != 0.0 {
		t.Errorf("Expected lon 0.0, got %f", report.Lon)
	}
	if report.Alt != 0.0 {
		t.Errorf("Expected alt 0.0, got %f", report.Alt)
	}
}

func TestTPVReport_NegativeValues(t *testing.T) {
	jsonData := `{
		"class":"TPV",
		"mode":3,
		"lat":-33.865143,
		"lon":-151.209900,
		"alt":-5.0,
		"climb":-2.5,
		"speed":15.3
	}`

	var report TPVReport
	err := json.Unmarshal([]byte(jsonData), &report)
	if err != nil {
		t.Fatalf("Failed to unmarshal TPV report with negative values: %v", err)
	}

	if report.Lat != -33.865143 {
		t.Errorf("Expected lat -33.865143, got %f", report.Lat)
	}
	if report.Lon != -151.209900 {
		t.Errorf("Expected lon -151.209900, got %f", report.Lon)
	}
	if report.Alt != -5.0 {
		t.Errorf("Expected alt -5.0, got %f", report.Alt)
	}
	if report.Climb != -2.5 {
		t.Errorf("Expected climb -2.5, got %f", report.Climb)
	}
}

// Test edge cases for satellite PRN ranges
func TestSatellite_PRNRanges(t *testing.T) {
	tests := []struct {
		name     string
		prn      float64
		satType  string
	}{
		{"GNSS lower bound", 1, "GNSS"},
		{"GNSS upper bound", 63, "GNSS"},
		{"GLONASS lower bound", 64, "GLONASS"},
		{"GLONASS upper bound", 96, "GLONASS"},
		{"SBAS lower bound", 100, "SBAS"},
		{"SBAS upper bound", 164, "SBAS"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sat := Satellite{PRN: tt.prn}
			if sat.PRN != tt.prn {
				t.Errorf("Expected PRN %f, got %f", tt.prn, sat.PRN)
			}
		})
	}
}

// Benchmark tests for report unmarshaling
func BenchmarkTPVReport_Unmarshal(b *testing.B) {
	jsonData := []byte(`{"class":"TPV","device":"/dev/pts/1","mode":3,"time":"2005-06-08T10:34:48.283Z","lat":46.498293369,"lon":7.567411672,"alt":1343.127,"track":10.3788,"speed":0.091,"climb":-0.085}`)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var report TPVReport
		_ = json.Unmarshal(jsonData, &report)
	}
}

func BenchmarkSKYReport_Unmarshal(b *testing.B) {
	jsonData := []byte(`{"class":"SKY","device":"/dev/pts/1","hdop":1.24,"pdop":1.99,"satellites":[{"PRN":8,"el":66,"az":189,"ss":44,"used":true},{"PRN":10,"el":51,"az":304,"ss":29,"used":true}]}`)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var report SKYReport
		_ = json.Unmarshal(jsonData, &report)
	}
}

func BenchmarkGSTReport_Unmarshal(b *testing.B) {
	jsonData := []byte(`{"class":"GST","device":"/dev/ttyUSB0","time":"2010-12-07T10:23:07.096Z","rms":2.440,"major":1.660,"minor":1.120,"orient":68.989,"lat":1.600,"lon":1.200,"alt":2.520}`)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var report GSTReport
		_ = json.Unmarshal(jsonData, &report)
	}
}

func BenchmarkDEVICEReport_String(b *testing.B) {
	dev := DEVICEReport{
		Path:      "/dev/ttyUSB0",
		Activated: "2010-12-07T10:23:07.096Z",
		Flags:     1,
		Driver:    "SiRF binary",
		Subtype:   "Mode 1",
		Bps:       4800,
		Parity:    "N",
		Stopbits:  1,
		Native:    1,
		Cycle:     1.00,
		Mincycle:  0.25,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = dev.String()
	}
}
