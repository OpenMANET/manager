package batmanadv

import "os"

// ClearBatHosts clears the batman-adv hosts file by writing empty content to /etc/bat-hosts.
// Returns an error if the file write operation fails.
func ClearBatHosts() error {
	return os.WriteFile("/etc/bat-hosts", []byte{}, 0777)
}
