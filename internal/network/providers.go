package network

import (
	"context"
	"fmt"
)

// UCIDHCPConfigProvider implements handlers.DHCPConfigProvider using UCI readers.
type UCIDHCPConfigProvider struct {
	DHCPReader    DHCPConfigReader
	NetworkReader ConfigReader
}

func (p *UCIDHCPConfigProvider) GetDHCPConfig(section string) (*UCIDHCP, error) {
	return GetDHCPConfigWithReader(section, p.DHCPReader)
}

func (p *UCIDHCPConfigProvider) GetDnsmasqConfig() (*UCIDnsmasq, error) {
	return GetDnsmasqConfigWithReader(p.DHCPReader)
}

func (p *UCIDHCPConfigProvider) GetStaticHosts() ([]UCIStaticHost, error) {
	return GetStaticHostsWithReader(p.DHCPReader)
}

func (p *UCIDHCPConfigProvider) GetNetworkBaseIP(interfaceName string) (string, error) {
	cfg, err := GetUCINetworkByNameWithReader(interfaceName, p.NetworkReader)
	if err != nil {
		return "", err
	}

	if cfg.IPAddr == "" {
		return DefaultNetworkAddress, nil
	}

	// Derive network base from the IP + netmask. For simplicity, use
	// the default /16 base by zeroing the last two octets.
	ip := parseIPToUint32(cfg.IPAddr)
	if ip == 0 {
		return DefaultNetworkAddress, nil
	}

	// Mask to /16 to get the network base.
	base := ip & 0xFFFF0000

	return uint32ToIP(base), nil
}

func parseIPToUint32(s string) uint32 {
	parts := [4]byte{}
	n := 0
	val := 0

	for _, c := range s {
		if c == '.' {
			parts[n] = byte(val)
			n++
			val = 0
		} else {
			val = val*10 + int(c-'0')
		}
	}

	parts[n] = byte(val)

	return uint32(parts[0])<<24 | uint32(parts[1])<<16 | uint32(parts[2])<<8 | uint32(parts[3])
}

func uint32ToIP(v uint32) string {
	return fmt.Sprintf("%d.%d.%d.%d", byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

// UbusLeaseProvider implements handlers.LeaseProvider using ubus command execution.
type UbusLeaseProvider struct {
	Executor UbusCommandExecutor
}

func (p *UbusLeaseProvider) GetCurrentDHCPLeases(ctx context.Context) (*DHCPLeasesResponse, error) {
	return GetCurrentDHCPLeasesWithExecutor(ctx, p.Executor)
}
