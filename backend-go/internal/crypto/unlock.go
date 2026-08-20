package crypto

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"

	"crypto/sha256"
	"golang.org/x/crypto/hkdf"
)

// ErrUnlockToken means an unlock token is unusable: malformed, expired, issued
// for a different slug, or not authentic.
var ErrUnlockToken = errors.New("invalid or expired unlock token")

// unlockTokenLen is the fixed plaintext size: 8-byte expiry + DEK.
const unlockTokenLen = 8 + KeyLen

// Unlocker mints and verifies the short-lived tokens that let a visitor keep
// viewing an encrypted site after entering its passphrase once.
//
// A token carries the content's DEK sealed under a server-side key, so the
// server never has to hold visitor keys in memory between requests and a stolen
// token is useless without the server secret. The token is bound to its slug
// and carries its own expiry, both authenticated.
type Unlocker struct {
	key []byte
	ttl time.Duration
}

// NewUnlocker derives the token-sealing key from secret via HKDF, so the same
// application secret can safely be reused for unrelated purposes.
func NewUnlocker(secret string, ttl time.Duration) (*Unlocker, error) {
	if secret == "" {
		return nil, errors.New("unlock token secret must not be empty")
	}
	if ttl <= 0 {
		return nil, fmt.Errorf("unlock token TTL must be positive, got %s", ttl)
	}
	key := make([]byte, KeyLen)
	kdf := hkdf.New(sha256.New, []byte(secret), nil, []byte("easy-host/unlock-token/v1"))
	if _, err := io.ReadFull(kdf, key); err != nil {
		return nil, fmt.Errorf("deriving unlock token key: %w", err)
	}
	return &Unlocker{key: key, ttl: ttl}, nil
}

// TTL is how long minted tokens stay valid.
func (u *Unlocker) TTL() time.Duration { return u.ttl }

// Seal mints a token carrying dek for slug, valid for the Unlocker's TTL.
func (u *Unlocker) Seal(slug string, dek []byte) (string, error) {
	if len(dek) != KeyLen {
		return "", fmt.Errorf("data key must be %d bytes, got %d", KeyLen, len(dek))
	}
	payload := make([]byte, unlockTokenLen)
	binary.BigEndian.PutUint64(payload[:8], uint64(time.Now().Add(u.ttl).Unix()))
	copy(payload[8:], dek)

	sealed, err := seal(u.key, unlockAAD(slug), payload)
	if err != nil {
		return "", fmt.Errorf("sealing unlock token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

// Open recovers the DEK from a token, rejecting anything that is not an
// unexpired token minted for this slug.
func (u *Unlocker) Open(slug, token string) ([]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, ErrUnlockToken
	}
	payload, err := open(u.key, unlockAAD(slug), raw)
	if err != nil || len(payload) != unlockTokenLen {
		return nil, ErrUnlockToken
	}
	if expiry := int64(binary.BigEndian.Uint64(payload[:8])); time.Now().Unix() >= expiry {
		return nil, ErrUnlockToken
	}
	dek := make([]byte, KeyLen)
	copy(dek, payload[8:])
	return dek, nil
}

// unlockAAD binds a token to one slug, so a token for one encrypted site cannot
// be replayed against another.
func unlockAAD(slug string) []byte {
	return []byte("easy-host/unlock/v1|" + slug)
}
