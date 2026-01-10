package system

import "os/exec"

// Reboot initiates a system reboot by executing the "reboot" command.
func Reboot() error {
	cmd := exec.Command("reboot")
	return cmd.Run()
}
