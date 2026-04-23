package auth_test

import (
	"context"
	"sync"
)

// fakePasswordSetter is a hand-rolled fake implementing auth.PasswordSetter.
// It captures the last call and can return a configured error.
type fakePasswordSetter struct {
	mu sync.Mutex

	err error

	calls   int
	gotUser string
	gotPass string
}

func (f *fakePasswordSetter) SetPassword(_ context.Context, username, newPassword string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls++
	f.gotUser = username
	f.gotPass = newPassword

	return f.err
}

func (f *fakePasswordSetter) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.calls
}

func (f *fakePasswordSetter) GotUser() string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.gotUser
}

func (f *fakePasswordSetter) GotPass() string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.gotPass
}

func (f *fakePasswordSetter) SetErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.err = err
}
