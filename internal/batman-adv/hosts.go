package batmanadv

import (
	"fmt"
	"os"
)

// ClearBatHosts clears the batman-adv hosts file by writing empty content to /tmp/bat-hosts.
// Returns an error if the file write operation fails.
func ClearBatHosts() error {
	if err := os.WriteFile("/tmp/bat-hosts", []byte{}, 0644); err != nil { //nolint:gosec // Fixed path required by batman-adv
		return fmt.Errorf("clear bat-hosts: %w", err)
	}

	return nil
}
