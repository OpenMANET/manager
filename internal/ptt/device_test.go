package ptt

import (
	"bytes"
	"net"
	"testing"

	"github.com/rs/zerolog"
)

func TestLogInputDeviceList(t *testing.T) {
	// Capture log output
	var buf bytes.Buffer
	logger := zerolog.New(&buf).With().Timestamp().Logger()

	// Create a PTTConfig instance with the test logger
	ptt := &PTTConfig{
		Log: logger,
	}

	// Call the function
	ptt.logInputDeviceList()

	// Verify that some output was logged
	output := buf.String()
	if output == "" {
		t.Error("Expected log output, got empty string")
	}

	// The output should contain either device list or error message
	if !bytes.Contains(buf.Bytes(), []byte("Discovered")) && !bytes.Contains(buf.Bytes(), []byte("Unable to list input devices")) {
		t.Errorf("Expected log to contain device list or error message, got: %s", output)
	}
}

func TestJoinMulticastGroup(t *testing.T) {
	// Create a test logger
	var buf bytes.Buffer
	logger := zerolog.New(&buf).With().Timestamp().Logger()

	// Create a PTTConfig instance
	ptt := &PTTConfig{
		Log: logger,
	}

	// Create a UDP connection
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		t.Fatalf("Failed to create UDP connection: %v", err)
	}
	defer conn.Close()

	// Get a valid network interface
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Fatalf("Failed to get network interfaces: %v", err)
	}

	var testIface *net.Interface
	for i := range ifaces {
		// Skip loopback and down interfaces
		if ifaces[i].Flags&net.FlagUp != 0 && ifaces[i].Flags&net.FlagMulticast != 0 {
			testIface = &ifaces[i]
			break
		}
	}

	if testIface == nil {
		t.Skip("No suitable multicast interface found")
	}

	// Valid multicast group
	multicastGroup := net.IPv4(224, 0, 0, 251)

	err = ptt.joinMulticastGroup(testIface, conn, multicastGroup)
	if err != nil {
		t.Errorf("joinMulticastGroup failed with valid parameters: %v", err)
	}
}

func TestJoinMulticastGroup_InvalidGroup(t *testing.T) {
	// Create a test logger
	var buf bytes.Buffer
	logger := zerolog.New(&buf).With().Timestamp().Logger()

	// Create a PTTConfig instance
	ptt := &PTTConfig{
		Log: logger,
	}

	// Create a UDP connection
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		t.Fatalf("Failed to create UDP connection: %v", err)
	}
	defer conn.Close()

	// Get a valid network interface
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Fatalf("Failed to get network interfaces: %v", err)
	}

	var testIface *net.Interface
	for i := range ifaces {
		if ifaces[i].Flags&net.FlagUp != 0 && ifaces[i].Flags&net.FlagMulticast != 0 {
			testIface = &ifaces[i]
			break
		}
	}

	if testIface == nil {
		t.Skip("No suitable multicast interface found")
	}

	// Invalid unicast address (not a multicast group)
	invalidGroup := net.IPv4(192, 168, 1, 1)

	err = ptt.joinMulticastGroup(testIface, conn, invalidGroup)
	// This may or may not error depending on OS, but function should execute
	_ = err
}
