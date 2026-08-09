package network

import (
	"net"
	"testing"

	"github.com/mdlayher/wifi"
	"github.com/stretchr/testify/assert"
	"github.com/vishvananda/netlink"
)

// ── classifyLink ─────────────────────────────────────────────────────────────

type fakeLink struct {
	netlink.LinkAttrs
	linkType string
}

func (f *fakeLink) Attrs() *netlink.LinkAttrs { return &f.LinkAttrs }
func (f *fakeLink) Type() string              { return f.linkType }

func TestClassifyLink_NetlinkTypes(t *testing.T) {
	tests := []struct {
		name     string
		linkName string
		linkType string
		flags    net.Flags
		want     InterfaceLinkType
	}{
		{"bridge type", "br-ahwlan", "bridge", 0, LinkTypeBridge},
		{"vxlan type", "vxlan0", "vxlan", 0, LinkTypeVXLAN},
		{"batadv type", "bat0", "batadv", 0, LinkTypeBatman},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			link := &fakeLink{
				LinkAttrs: netlink.LinkAttrs{Name: tt.linkName, Flags: tt.flags},
				linkType:  tt.linkType,
			}
			assert.Equal(t, tt.want, classifyLink(link, nil))
		})
	}
}

func TestClassifyLink_NameHeuristics(t *testing.T) {
	tests := []struct {
		name     string
		linkName string
		flags    net.Flags
		want     InterfaceLinkType
	}{
		{"bat prefix", "bat1", 0, LinkTypeBatman},
		{"loopback flag", "lo", net.FlagLoopback, LinkTypeLoopback},
		{"bridge prefix", "br-lan", 0, LinkTypeBridge},
		{"ethernet eth", "eth0", 0, LinkTypeEthernet},
		{"ethernet en", "enp0s3", 0, LinkTypeEthernet},
		{"unknown", "tun0", 0, LinkTypeUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			link := &fakeLink{
				LinkAttrs: netlink.LinkAttrs{Name: tt.linkName, Flags: tt.flags},
				linkType:  "device",
			}
			assert.Equal(t, tt.want, classifyLink(link, nil))
		})
	}
}

func TestClassifyLink_WifiTypes(t *testing.T) {
	wifiTypes := map[string]wifi.InterfaceType{
		"wlh0":       wifi.InterfaceTypeMeshPoint,
		"phy0-mesh0": wifi.InterfaceTypeMeshPoint,
		"wlh1":       wifi.InterfaceTypeAP,
	}

	tests := []struct {
		name     string
		linkName string
		want     InterfaceLinkType
	}{
		{"mesh point wlh0", "wlh0", LinkTypeHaLowMesh},
		{"mesh point phy0-mesh0", "phy0-mesh0", LinkTypeHaLowMesh},
		{"ap wlh1", "wlh1", LinkTypeWiFiAP},
		{"unknown wifi not in map", "wlh9", LinkTypeUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			link := &fakeLink{
				LinkAttrs: netlink.LinkAttrs{Name: tt.linkName},
				linkType:  "device",
			}
			assert.Equal(t, tt.want, classifyLink(link, wifiTypes))
		})
	}
}

// ── operState ────────────────────────────────────────────────────────────────

func TestOperState_Up(t *testing.T) {
	attrs := &netlink.LinkAttrs{OperState: netlink.OperUp}
	assert.Equal(t, OperStateUp, operState(attrs))
}

func TestOperState_UpByFlag(t *testing.T) {
	attrs := &netlink.LinkAttrs{Flags: net.FlagUp}
	assert.Equal(t, OperStateUp, operState(attrs))
}

func TestOperState_Down(t *testing.T) {
	attrs := &netlink.LinkAttrs{OperState: netlink.OperDown}
	assert.Equal(t, OperStateDown, operState(attrs))
}
