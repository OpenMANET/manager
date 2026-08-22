package network

import (
	"fmt"

	"github.com/digineo/go-uci/v2"
)

const umdnsConfigName = "umdns"

// StageUmdnsNetworksWithReader writes the interface list umdns
// advertises on (umdns.@umdns[0].network), creating the section when
// absent — factory images ship a zero-byte /etc/config/umdns. Without
// this, umdns never announces the device and <hostname>.local never
// resolves, even though the setup wizard's terminal event promises
// exactly that URL. The section is named (wizard_umdns): go-uci's
// AddSection("") collides with leading anonymous sections. Does not
// commit; the wizard's phase-12 atomic commit covers it (umdns is in
// wizardConfigs).
func StageUmdnsNetworksWithReader(reader ConfigReader, networks []string) error {
	if len(networks) == 0 {
		return fmt.Errorf("networks are required")
	}

	sections, err := reader.GetSections(umdnsConfigName, umdnsConfigName)
	if err != nil {
		return fmt.Errorf("listing umdns sections: %w", err)
	}

	if len(sections) == 0 {
		const section = "wizard_umdns"
		if err := reader.AddSection(umdnsConfigName, section, umdnsConfigName); err != nil {
			return fmt.Errorf("creating umdns section: %w", err)
		}

		sections = []string{section}
	}

	if err := reader.SetType(umdnsConfigName, sections[0], "network", uci.TypeList, networks...); err != nil {
		return fmt.Errorf("setting umdns network list: %w", err)
	}

	return nil
}
