//go:build omd_omit_comms

package openmanet

import (
	"os"

	"github.com/openmanet/openmanetd/internal/config"
)

// startComms is a stub function that does nothing when the comms module is omitted via build tags.
func startComms(_ *config.Config, _ chan os.Signal) {}
