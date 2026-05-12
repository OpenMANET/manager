package frontend

import (
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGenerateSelfSignedTLSConfig(t *testing.T) {
	cfg, err := generateSelfSignedTLSConfig()
	if err != nil {
		t.Fatalf("generateSelfSignedTLSConfig() error: %v", err)
	}

	if len(cfg.Certificates) != 1 {
		t.Fatalf("expected 1 certificate, got %d", len(cfg.Certificates))
	}

	if cfg.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %d, want %d (TLS 1.2)", cfg.MinVersion, tls.VersionTLS12)
	}

	// Parse the leaf certificate and check SANs.
	leaf, err := x509.ParseCertificate(cfg.Certificates[0].Certificate[0])
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}

	if leaf.Subject.CommonName != "OpenMANET Frontend" {
		t.Errorf("CN = %q, want %q", leaf.Subject.CommonName, "OpenMANET Frontend")
	}

	// Must not be expired.
	now := time.Now()
	if now.Before(leaf.NotBefore) || now.After(leaf.NotAfter) {
		t.Errorf("certificate not valid at current time: notBefore=%v, notAfter=%v", leaf.NotBefore, leaf.NotAfter)
	}

	// Must include localhost as a DNS SAN.
	foundLocalhost := false

	for _, dns := range leaf.DNSNames {
		if dns == "localhost" {
			foundLocalhost = true
		}
	}

	if !foundLocalhost {
		t.Errorf("certificate DNSNames %v does not include localhost", leaf.DNSNames)
	}

	// Must include 127.0.0.1 as an IP SAN.
	foundLoopback := false

	for _, ip := range leaf.IPAddresses {
		if ip.Equal(net.IPv4(127, 0, 0, 1)) {
			foundLoopback = true
		}
	}

	if !foundLoopback {
		t.Errorf("certificate IPAddresses %v does not include 127.0.0.1", leaf.IPAddresses)
	}
}

func TestBuildTLSConfig_SelfSigned(t *testing.T) {
	cfg, err := buildTLSConfig("", "")
	if err != nil {
		t.Fatalf("buildTLSConfig(\"\", \"\") error: %v", err)
	}

	if cfg == nil {
		t.Fatal("expected non-nil TLS config")
	}

	if len(cfg.Certificates) != 1 {
		t.Fatalf("expected 1 certificate, got %d", len(cfg.Certificates))
	}
}

func TestBuildTLSConfig_MismatchedFiles(t *testing.T) {
	_, err := buildTLSConfig("cert.pem", "")
	if err == nil {
		t.Fatal("expected error for mismatched cert/key, got nil")
	}

	_, err = buildTLSConfig("", "key.pem")
	if err == nil {
		t.Fatal("expected error for mismatched cert/key, got nil")
	}
}

func TestBuildTLSConfig_LoadsFromFiles(t *testing.T) {
	// Generate a self-signed config to extract cert/key data.
	selfSigned, err := generateSelfSignedTLSConfig()
	if err != nil {
		t.Fatalf("generate self-signed: %v", err)
	}

	cert := selfSigned.Certificates[0]

	// Write cert PEM.
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]})

	ecKey, ok := cert.PrivateKey.(*ecdsa.PrivateKey)
	if !ok {
		t.Fatal("expected ECDSA private key")
	}

	keyBytes, err := x509.MarshalECPrivateKey(ecKey)
	if err != nil {
		t.Fatalf("marshal EC key: %v", err)
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}

	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}

	cfg, err := buildTLSConfig(certPath, keyPath)
	if err != nil {
		t.Fatalf("buildTLSConfig(%q, %q) error: %v", certPath, keyPath, err)
	}

	if len(cfg.Certificates) != 1 {
		t.Fatalf("expected 1 certificate, got %d", len(cfg.Certificates))
	}
}

func TestTLSServer_ServesHandler(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	tlsConfig, err := generateSelfSignedTLSConfig()
	if err != nil {
		t.Fatalf("generate TLS config: %v", err)
	}

	ts := httptest.NewUnstartedServer(handler)
	ts.TLS = tlsConfig
	ts.StartTLS()

	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET / error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "OK" {
		t.Errorf("body = %q, want %q", string(body), "OK")
	}
}

func TestDiscoverIPs(t *testing.T) {
	ips := discoverIPs()

	found127 := false
	found000 := false

	for _, ip := range ips {
		if ip.Equal(net.IPv4(127, 0, 0, 1)) {
			found127 = true
		}

		if ip.Equal(net.IPv4(0, 0, 0, 0)) {
			found000 = true
		}
	}

	if !found127 {
		t.Errorf("discoverIPs() missing 127.0.0.1: %v", ips)
	}

	if !found000 {
		t.Errorf("discoverIPs() missing 0.0.0.0: %v", ips)
	}
}
