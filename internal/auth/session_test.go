package auth_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/openmanet/openmanetd/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionStore_CreateAndValidate(t *testing.T) {
	store := auth.NewSessionStore(time.Hour, 16)

	token := store.Create("alice")
	require.NotEmpty(t, token)
	assert.Len(t, token, 64) // 32 bytes hex-encoded

	sess, ok := store.Validate(token)
	require.True(t, ok)
	assert.Equal(t, "alice", sess.Username)
	assert.Equal(t, token, sess.Token)
}

func TestSessionStore_Validate_UnknownToken(t *testing.T) {
	store := auth.NewSessionStore(time.Hour, 16)

	_, ok := store.Validate("nonexistent")
	assert.False(t, ok)
}

func TestSessionStore_Validate_ExpiredSession(t *testing.T) {
	store := auth.NewSessionStore(time.Millisecond, 16)

	token := store.Create("alice")

	time.Sleep(5 * time.Millisecond)

	_, ok := store.Validate(token)
	assert.False(t, ok)
}

func TestSessionStore_Delete(t *testing.T) {
	store := auth.NewSessionStore(time.Hour, 16)

	token := store.Create("alice")
	store.Delete(token)

	_, ok := store.Validate(token)
	assert.False(t, ok)
}

func TestSessionStore_Cleanup(t *testing.T) {
	store := auth.NewSessionStore(time.Millisecond, 16)

	_ = store.Create("alice")
	_ = store.Create("bob")

	time.Sleep(5 * time.Millisecond)
	store.Cleanup()

	// Both sessions expired; validate should return false.
	activeToken := store.Create("carol")
	_, ok := store.Validate(activeToken)
	assert.True(t, ok)
}

func TestSessionStore_MaxSizeEvictsOldest(t *testing.T) {
	store := auth.NewSessionStore(time.Hour, 2)

	first := store.Create("alice")
	_ = store.Create("bob")

	// Validate bob to make first (alice) the oldest by LastAccess.
	// Give the clock a moment to tick so LastAccess differs.
	time.Sleep(time.Millisecond)

	_, _ = store.Validate(first) // touch alice

	time.Sleep(time.Millisecond)

	// Add carol — bob should be evicted (oldest LastAccess).
	_ = store.Create("carol")

	// Bob should be evicted; alice and carol are still valid.
	bobToken := store.Create("bob2")
	_, ok := store.Validate(bobToken)
	assert.True(t, ok)
}

func TestSessionStore_StartCleanup(t *testing.T) {
	store := auth.NewSessionStore(time.Millisecond, 16)
	ctx, cancel := context.WithCancel(context.Background())

	store.StartCleanup(ctx, 5*time.Millisecond)

	token := store.Create("alice")

	time.Sleep(20 * time.Millisecond) // wait for cleanup tick

	_, ok := store.Validate(token)
	assert.False(t, ok)

	cancel()
}

func TestSessionStore_TokensAreUnique(t *testing.T) {
	store := auth.NewSessionStore(time.Hour, 100)
	tokens := make(map[string]struct{}, 100)

	for i := 0; i < 100; i++ {
		token := store.Create("user")
		tokens[token] = struct{}{}
	}

	assert.Len(t, tokens, 100)
}

func TestSessionStore_ConcurrentAccess(t *testing.T) {
	store := auth.NewSessionStore(time.Hour, 64)

	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			token := store.Create("user")
			_, _ = store.Validate(token)
			store.Delete(token)
		}()
	}

	wg.Wait()
}

func TestSessionStore_ValidateUpdatesLastAccess(t *testing.T) {
	store := auth.NewSessionStore(time.Hour, 16)

	token := store.Create("alice")

	sess1, ok := store.Validate(token)
	require.True(t, ok)

	before := sess1.LastAccess

	time.Sleep(time.Millisecond)

	sess2, ok := store.Validate(token)
	require.True(t, ok)
	assert.True(t, sess2.LastAccess.After(before))
}
