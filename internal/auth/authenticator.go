package auth

// Authenticator validates a username and password against an authentication
// backend (e.g. PAM, LDAP, or a test stub).
type Authenticator interface {
	Authenticate(username, password string) error
}
