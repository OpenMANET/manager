package frontend

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"math/big"
	"net"
	"time"
)

// buildTLSConfig returns a *tls.Config for the frontend TLS server.
// If certFile and keyFile are both non-empty, the certificate is loaded from
// disk. If both are empty, a self-signed certificate is generated. If only
// one is set, an error is returned.
func buildTLSConfig(certFile, keyFile string) (*tls.Config, error) {
	hasCert := certFile != ""
	hasKey := keyFile != ""

	if hasCert != hasKey {
		return nil, errors.New("both frontend.tlsCertFile and frontend.tlsKeyFile must be set, or both must be empty")
	}

	if hasCert {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("load TLS key pair: %w", err)
		}

		return &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		}, nil
	}

	return generateSelfSignedTLSConfig()
}

// generateSelfSignedTLSConfig creates an in-memory ECDSA P-256 self-signed
// certificate valid for 1 year. The certificate includes localhost, loopback
// addresses, and all unicast IPs discovered on the host's network interfaces
// so that browsers accessing the node by its mesh IP won't see a SAN mismatch.
func generateSelfSignedTLSConfig() (*tls.Config, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ECDSA key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("generate serial number: %w", err)
	}

	now := time.Now()

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "OpenMANET Frontend"},
		NotBefore:    now,
		NotAfter:     now.Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},

		DNSNames:    []string{"localhost"},
		IPAddresses: discoverIPs(),
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("create certificate: %w", err)
	}

	tlsCert := tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  key,
	}

	return &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
		MinVersion:   tls.VersionTLS12,
	}, nil
}

// discoverIPs returns loopback and all unicast IP addresses from the host's
// network interfaces. This ensures the self-signed certificate covers the
// node's current mesh IP, which may change between restarts.
func discoverIPs() []net.IP {
	ips := []net.IP{
		net.IPv4(127, 0, 0, 1),
		net.IPv4(0, 0, 0, 0),
		net.IPv6loopback,
	}

	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ips
	}

	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}

		// Skip loopback — already included above.
		if ipNet.IP.IsLoopback() {
			continue
		}

		if ipNet.IP.IsGlobalUnicast() || ipNet.IP.IsLinkLocalUnicast() {
			ips = append(ips, ipNet.IP)
		}
	}

	return ips
}
