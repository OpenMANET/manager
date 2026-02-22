package network

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetWirelessMeshPassphraseFromPath(t *testing.T) {
	path := filepath.Join("..", "..", "testfixtures", "uci", "wireless")

	key, err := GetWirelessMeshPassphraseFromPath(path)
	if err != nil {
		t.Fatalf("GetWirelessMeshPassphraseFromPath failed: %v", err)
	}

	if key != "thisisnotarealpassword" {
		t.Fatalf("expected mesh passphrase %q, got %q", "thisisnotarealpassword", key)
	}
}

func TestGetWirelessMeshPassphraseFromPathSkipsDisabled(t *testing.T) {
	path := writeWirelessFixture(t, `
config wifi-iface 'mesh_disabled'
	option mode 'mesh'
	option encryption 'sae'
	option key 'disabled-key'
	option disabled '1'

config wifi-iface 'mesh_enabled'
	option mode 'mesh'
	option encryption 'sae'
	option key 'enabled-key'
`)

	key, err := GetWirelessMeshPassphraseFromPath(path)
	if err != nil {
		t.Fatalf("GetWirelessMeshPassphraseFromPath failed: %v", err)
	}

	if key != "enabled-key" {
		t.Fatalf("expected mesh passphrase %q, got %q", "enabled-key", key)
	}
}

func TestGetWirelessMeshPassphraseFromPathMissingKey(t *testing.T) {
	path := writeWirelessFixture(t, `
config wifi-iface 'mesh0'
	option mode 'mesh'
	option encryption 'sae'
`)

	_, err := GetWirelessMeshPassphraseFromPath(path)
	if err == nil {
		t.Fatalf("expected error when mesh key is missing")
	}

	if !strings.Contains(err.Error(), "missing key") {
		t.Fatalf("expected missing key error, got %v", err)
	}
}

func TestGetWirelessMeshPassphraseFromPathNoMeshInterface(t *testing.T) {
	path := writeWirelessFixture(t, `
config wifi-iface 'ap0'
	option mode 'ap'
	option key 'ap-password'
`)

	_, err := GetWirelessMeshPassphraseFromPath(path)
	if err == nil {
		t.Fatalf("expected error when no mesh interface exists")
	}

	if !strings.Contains(err.Error(), "no enabled mesh") {
		t.Fatalf("expected no mesh section error, got %v", err)
	}
}

func writeWirelessFixture(t *testing.T, content string) string {
	t.Helper()

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "wireless")

	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o600); err != nil {
		t.Fatalf("failed to write wireless fixture: %v", err)
	}

	return path
}
