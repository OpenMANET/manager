//go:build linux

package auth

import (
	"errors"
	"fmt"

	"github.com/msteinert/pam/v2"
)

// PAMAuthenticator authenticates users against the local PAM stack.
type PAMAuthenticator struct {
	// ServiceName is the PAM service name, which selects the
	// /etc/pam.d/<ServiceName> policy file. Defaults to "login".
	ServiceName string
}

// Authenticate validates the given username and password via PAM.
// The error message is deliberately vague to avoid leaking information about
// whether the user account exists.
func (a *PAMAuthenticator) Authenticate(username, password string) error {
	svc := a.ServiceName
	if svc == "" {
		svc = "login"
	}

	t, err := pam.StartFunc(svc, username, func(s pam.Style, _ string) (string, error) {
		switch s { //nolint:exhaustive
		case pam.PromptEchoOff:
			// Password prompt.
			return password, nil
		case pam.PromptEchoOn, pam.ErrorMsg, pam.TextInfo:
			return "", nil
		default:
			return "", fmt.Errorf("unsupported PAM message style %d", s)
		}
	})
	if err != nil {
		return fmt.Errorf("pam init: %w", err)
	}

	if err = t.Authenticate(0); err != nil {
		// Return a generic error to avoid leaking credential details.
		return errors.New("authentication failed")
	}

	return nil
}
