package mgmt

import (
	"context"
	"fmt"
	"strings"

	"github.com/openmanet/openmanetd/internal/iwinfo"
	"github.com/openmanet/openmanetd/internal/network"
)

const (
	defaultMeshInterfaceMTU = 1532
)

// setTransportInterfaceMTU sets the MTU (Maximum Transmission Unit) for all
// wireless mesh interfaces defined in the WirelessConfig. It retrieves the list
// of mesh interfaces and attempts to set each interface's MTU to
// defaultMeshInterfaceMTU. If setting the MTU for a particular interface fails,
// the error is logged and the process continues with the remaining interfaces.
// A successful MTU update is also logged at the Info level.
//
// Returns an error if the mesh interfaces cannot be retrieved from WirelessConfig,
// otherwise returns nil even if individual interface MTU updates fail.
func (m *ManagementConfig) setTransportInterfaceMTU() error {
	wirelessInterfaces, err := m.WirelessConfig.GetMeshInterfaces()
	if err != nil {
		return err
	}

	for _, iface := range wirelessInterfaces {
		if err := network.SetMTU(iface.Name, defaultMeshInterfaceMTU); err != nil {
			m.Log.Error().Err(err).Str("interface", iface.Name).Msg("Failed to set MTU for mesh interface")
		} else {
			m.Log.Info().Str("interface", iface.Name).Int("mtu", defaultMeshInterfaceMTU).Msg("Set MTU for mesh interface")
		}
	}

	return nil
}

// setupBatMesh1Interface configures a new 2.4 GHz batman-adv batmesh1 wireless
// interface if it has not already been configured. It creates concrete UCI and
// iwinfo dependencies and delegates to setupBatMesh1InterfaceWithDeps.
func (m *ManagementConfig) setupBatMesh1Interface(ctx context.Context) error {
	return m.setupBatMesh1InterfaceWithDeps(
		ctx,
		m.uciOpenMANETConfig,
		network.NewUCIWirelessConfigReader(),
		iwinfo.NewClient(),
	)
}

// setupBatMesh1InterfaceWithDeps configures a new 2.4 GHz batman-adv batmesh1
// wireless interface. It is idempotent: if batmesh1configured is already set to
// true the method returns immediately. The method:
//
//  1. Checks that at least one wireless interface uses a supported MediaTek
//     chipset (MT7915 or MT7916) via iwinfo.
//  2. Locates an existing wifi-iface with mode=mesh and borrows its mesh_id and
//     key values.
//  3. Locates the wifi-device with band=2g and derives the new interface section
//     name as "default_<radioSection>".
//  4. Creates the new wifi-iface with network=batmesh1, mode=mesh,
//     mesh_fwding=0, encryption=sae, and the borrowed credentials.
//  5. Updates the matched 2g radio with channel=8 and htmode=HE20.
//  6. Marks batmesh1 as configured.
func (m *ManagementConfig) setupBatMesh1InterfaceWithDeps(
	ctx context.Context,
	openmanetReader network.OpenMANETConfigReader,
	wirelessReader network.ConfigReader,
	iwinfoProvider iwinfo.IwinfoProvider,
) error {
	// Step 1: guard — return early if already configured.
	configured, err := network.IsBatMesh1ConfiguredWithReader(openmanetReader)
	if err != nil {
		return fmt.Errorf("check batmesh1 configured: %w", err)
	}

	if configured {
		m.Log.Debug().Msg("batmesh1 already configured, skipping")

		return nil
	}

	// Step 2: hardware check — require MT7915 or MT7916 chipset.
	allInfo, err := iwinfoProvider.GetInfoForAll(ctx)
	if err != nil {
		return fmt.Errorf("get iwinfo for all devices: %w", err)
	}

	var supportedHardwareFound bool

	for _, info := range allInfo {
		name := info.Hardware.GetName()
		if strings.Contains(name, "MT7915") || strings.Contains(name, "MT7916") {
			m.Log.Debug().Str("hardware", name).Msg("Found supported MediaTek chipset for batmesh1")

			supportedHardwareFound = true

			break
		}
	}

	if !supportedHardwareFound {
		return fmt.Errorf("no supported hardware (MT7915 or MT7916) found for batmesh1 configuration")
	}

	// Step 3: find mesh credentials from an existing mesh wifi-iface.
	ifaceSections, err := wirelessReader.GetSections("wireless", "wifi-iface")
	if err != nil {
		return fmt.Errorf("get wifi-iface sections: %w", err)
	}

	var meshID, meshKey string

	for _, section := range ifaceSections {
		iface, ierr := network.GetWirelessIfaceByNameWithReader(section, wirelessReader)
		if ierr != nil {
			continue
		}

		if iface.Mode == "mesh" {
			meshID = iface.MeshID
			meshKey = iface.Key

			break
		}
	}

	if meshID == "" {
		return fmt.Errorf("no existing wifi-iface with mode=mesh found; cannot determine mesh credentials")
	}

	// Step 4: find the 2g radio.
	deviceSections, err := wirelessReader.GetSections("wireless", "wifi-device")
	if err != nil {
		return fmt.Errorf("get wifi-device sections: %w", err)
	}

	var radioSection string

	for _, section := range deviceSections {
		dev, derr := network.GetWirelessDeviceByNameWithReader(section, wirelessReader)
		if derr != nil {
			continue
		}

		if dev.Band == "2g" {
			radioSection = section

			break
		}
	}

	if radioSection == "" {
		return fmt.Errorf("no wifi-device with band=2g found")
	}

	newIfaceSection := "default_" + radioSection

	// Step 5: create the new wifi-iface.
	newIface := &network.UCIWirelessIface{
		Device:     radioSection,
		Network:    "batmesh1",
		Mode:       "mesh",
		MeshID:     meshID,
		Key:        meshKey,
		MeshFwding: "0",
		Encryption: "sae",
	}

	if err := network.SetWirelessIfaceConfigWithReader(newIfaceSection, newIface, wirelessReader); err != nil {
		return fmt.Errorf("create wifi-iface %s: %w", newIfaceSection, err)
	}

	m.Log.Info().Str("section", newIfaceSection).Str("device", radioSection).Msg("Created batmesh1 wifi-iface")

	// Step 6: update the 2g radio device.
	radioUpdate := &network.UCIWirelessDevice{
		Channel: "8",
		HTMode:  "HE20",
	}

	if err := network.SetWirelessDeviceConfigWithReader(radioSection, radioUpdate, wirelessReader); err != nil {
		return fmt.Errorf("update wifi-device %s: %w", radioSection, err)
	}

	m.Log.Info().Str("section", radioSection).Str("channel", "8").Str("htmode", "HE20").Msg("Updated 2g radio for batmesh1")

	// Step 7: mark batmesh1 as configured.
	if err := network.SetBatMesh1ConfiguredWithReader(openmanetReader); err != nil {
		return fmt.Errorf("mark batmesh1 configured: %w", err)
	}

	m.Log.Info().Msg("batmesh1 interface configuration complete")

	// Reload the UCI config to apply changes and ensure the new interface is active.
	_ = network.ForceReloadConfig(ctx)

	return nil
}
