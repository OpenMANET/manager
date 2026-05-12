package system

import (
	"context"
	"os/exec"
)

// Reboot initiates a system reboot by executing the "reboot" command.
func Reboot() error {
	cmd := exec.CommandContext(context.Background(), "reboot")

	return cmd.Run()
}
