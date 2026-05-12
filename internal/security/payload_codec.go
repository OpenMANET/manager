package security

import (
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

const (
	envelopeVersion byte = 1

	envelopeVersionSize   = 1
	envelopeTimestampSize = 8
	envelopeHeaderSize    = envelopeVersionSize + envelopeTimestampSize + chacha20poly1305.NonceSizeX

	defaultReplayWindow = 10 * time.Minute
)

var (
	// ErrReplayDetected indicates a duplicated nonce from the same source.
	ErrReplayDetected = errors.New("payload replay detected")
	// ErrPayloadExpired indicates the payload timestamp is outside the replay window.
	ErrPayloadExpired = errors.New("payload outside replay window")
)

type replayEntry struct {
	receivedAt time.Time
}

// PayloadCodec encrypts and authenticates payloads using a key derived from
// the mesh passphrase.
type PayloadCodec struct {
	aead         cipher.AEAD
	now          func() time.Time
	seen         map[string]replayEntry
	replayWindow time.Duration
	mu           sync.Mutex
}

// NewPayloadCodecFromPassphrase creates a payload codec from the mesh
// passphrase.
func NewPayloadCodecFromPassphrase(passphrase string) (*PayloadCodec, error) {
	return newPayloadCodecFromPassphrase(passphrase, time.Now)
}

func newPayloadCodecFromPassphrase(passphrase string, nowFn func() time.Time) (*PayloadCodec, error) {
	passphrase = strings.TrimSpace(passphrase)
	if passphrase == "" {
		return nil, fmt.Errorf("mesh passphrase cannot be empty")
	}

	// Derive a key from the mesh passphrase with domain separation for
	// OpenMANET Alfred payload protection.
	key := argon2.IDKey([]byte(passphrase), []byte("openmanetd-alfred-aead-v1"), 1, 64*1024, 4, chacha20poly1305.KeySize)

	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize payload AEAD: %w", err)
	}

	return &PayloadCodec{
		aead:         aead,
		replayWindow: defaultReplayWindow,
		now:          nowFn,
		seen:         map[string]replayEntry{},
	}, nil
}

// Encrypt encrypts and authenticates a payload for the given data type.
func (c *PayloadCodec) Encrypt(dataType uint8, plaintext []byte) ([]byte, error) {
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	ts := c.now().Unix()
	aad := buildAAD(dataType, ts)
	ciphertext := c.aead.Seal(nil, nonce, plaintext, aad)

	out := make([]byte, envelopeHeaderSize+len(ciphertext))
	out[0] = envelopeVersion
	binary.BigEndian.PutUint64(out[1:9], uint64(ts))
	copy(out[9:9+chacha20poly1305.NonceSizeX], nonce)
	copy(out[envelopeHeaderSize:], ciphertext)

	return out, nil
}

// Decrypt verifies and decrypts a payload for the given data type and source.
func (c *PayloadCodec) Decrypt(dataType uint8, source net.HardwareAddr, encrypted []byte) ([]byte, error) {
	if len(encrypted) < envelopeHeaderSize+c.aead.Overhead() {
		return nil, fmt.Errorf("encrypted payload too short")
	}

	if encrypted[0] != envelopeVersion {
		return nil, fmt.Errorf("unsupported payload envelope version: %d", encrypted[0])
	}

	ts := int64(binary.BigEndian.Uint64(encrypted[1:9]))
	msgTime := time.Unix(ts, 0)
	now := c.now()

	if msgTime.Before(now.Add(-c.replayWindow)) || msgTime.After(now.Add(c.replayWindow)) {
		return nil, ErrPayloadExpired
	}

	nonce := encrypted[9:envelopeHeaderSize]

	key := replayKey(source, nonce)
	if c.isReplay(key, now) {
		return nil, ErrReplayDetected
	}

	aad := buildAAD(dataType, ts)

	plaintext, err := c.aead.Open(nil, nonce, encrypted[envelopeHeaderSize:], aad)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt payload: %w", err)
	}

	c.markSeen(key, now)

	return plaintext, nil
}

func (c *PayloadCodec) isReplay(key string, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.pruneSeen(now)
	_, exists := c.seen[key]

	return exists
}

func (c *PayloadCodec) markSeen(key string, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.pruneSeen(now)
	c.seen[key] = replayEntry{receivedAt: now}
}

func (c *PayloadCodec) pruneSeen(now time.Time) {
	cutoff := now.Add(-c.replayWindow)
	for key, entry := range c.seen {
		if entry.receivedAt.Before(cutoff) {
			delete(c.seen, key)
		}
	}
}

func replayKey(source net.HardwareAddr, nonce []byte) string {
	sourceID := "unknown"
	if len(source) > 0 {
		sourceID = source.String()
	}

	var b strings.Builder

	b.WriteString(sourceID)
	b.WriteByte(':')
	b.WriteString(hex.EncodeToString(nonce))

	return b.String()
}

func buildAAD(dataType uint8, timestamp int64) []byte {
	aad := make([]byte, 2+envelopeTimestampSize)
	aad[0] = envelopeVersion
	aad[1] = dataType
	binary.BigEndian.PutUint64(aad[2:], uint64(timestamp))

	return aad
}
