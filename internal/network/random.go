package network

import (
	"fmt"
	"math/rand"
	"net"
	"strings"
)

const (
	// WizardWifiKeyCharset is the alphabet used by getDefaultWifiKey() in
	// LuCI's morseconfig/uci.js: 26 lowercase letters plus the ten digits
	// minus "1" (visually ambiguous with lowercase "l"). 35 characters
	// total. Exported so callers outside RandomWifiKey can canonicalize
	// generated keys against it (e.g. compat-test wildcard matching).
	WizardWifiKeyCharset = "abcdefghijklmnopqrstuvwxyz023456789"

	// WizardWifiKeyLen is the length of an auto-generated AP passphrase.
	WizardWifiKeyLen = 8

	// FactoryMeshIP is the IP address baked into the OpenMANET factory
	// firmware image (lan.ipaddr). Random mesh IPs must avoid this so
	// that two fresh devices provisioned simultaneously don't both land
	// on it before AddressReservationWorker negotiates peer-unique
	// addresses on the mesh.
	FactoryMeshIP = "10.41.254.1"
)

// RandomMAC returns a randomly-generated MAC address with the "F2" OUI
// prefix used by the LuCI Morse wizard. Mirrors getRandomMAC() in
// morseconfig/uci.js so the setup wizard handler emits the same bridge
// MAC distribution. The F2 prefix is a Morse-only marker that lets the
// daemon recognize its own auto-generated bridges later.
func RandomMAC(r *rand.Rand) string {
	octets := make([]string, 5)
	for i := range octets {
		octets[i] = fmt.Sprintf("%02x", r.Intn(256))
	}

	return "F2:" + strings.Join(octets, ":")
}

// RandomWifiKey returns an 8-character random passphrase drawn from
// WizardWifiKeyCharset. Used as the AP passphrase when the operator
// hasn't supplied one. Mirrors LuCI's getDefaultWifiKey() exactly.
func RandomWifiKey(r *rand.Rand) string {
	b := make([]byte, WizardWifiKeyLen)
	for i := range b {
		b[i] = WizardWifiKeyCharset[r.Intn(len(WizardWifiKeyCharset))]
	}

	return string(b)
}

// RandomDhcpStart returns a DHCP `start` offset matching LuCI's
// createDhcp() formula: 255 + 16*rand(0..14), giving 15 discrete values
// in the range [255, 479] aligned on /28 (16-address) boundaries. The
// offset is applied by dnsmasq to the network base, so the actual lease
// range falls in the 4th octet (~x.x.0.255 .. x.x.1.244).
func RandomDhcpStart(r *rand.Rand) int {
	return 255 + 16*r.Intn(15)
}

// RandomMeshIP returns a random IPv4 address derived from the supplied
// base IP, matching LuCI's getRandomIpaddr() in morseconfig/uci.js: the
// result is "<base[0]>.<base[1]>.254.<rand 0..253>". The third octet is
// pinned to 254 by design so peer-coordinated address negotiation
// (handled later by AddressReservationWorker) has a clean window to
// assign mesh-wide unique addresses in the lower /16.
//
// FactoryMeshIP is avoided explicitly so two simultaneously-provisioned
// fresh devices don't collide on the same address. Returns an error if
// base cannot be parsed as IPv4.
func RandomMeshIP(base string, r *rand.Rand) (string, error) {
	ip := net.ParseIP(base)
	if ip == nil {
		return "", fmt.Errorf("invalid base IP %q", base)
	}

	v4 := ip.To4()
	if v4 == nil {
		return "", fmt.Errorf("base IP %q is not IPv4", base)
	}

	for {
		octet := r.Intn(254) // [0, 253]
		out := fmt.Sprintf("%d.%d.254.%d", v4[0], v4[1], octet)

		if out != FactoryMeshIP {
			return out, nil
		}
	}
}
