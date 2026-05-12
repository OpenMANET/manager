//go:build !linux

package auth

import "errors"

// PAMAuthenticator is a stub for non-Linux platforms where PAM is unavailable.
// It always returns an error; enable auth only on Linux deployments.
type PAMAuthenticator struct {
	ServiceName string
}

// Authenticate always returns an error on non-Linux platforms.
func (a *PAMAuthenticator) Authenticate(_, _ string) error {
	return errors.New("PAM authentication is not supported on this platform")
}
