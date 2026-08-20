// Package crypto implements easy-host's at-rest encryption for passphrase
// protected content.
//
// # Scheme
//
// Content is protected with envelope encryption:
//
//		passphrase --Argon2id(salt)--> KEK --XChaCha20-Poly1305--> unwraps DEK
//		DEK --XChaCha20-Poly1305--> encrypts/decrypts each file
//
//	  - The key-encryption key (KEK) is derived from the visitor's passphrase with
//	    Argon2id (RFC 9106), using the memory-hard parameters in DefaultParams.
//	  - A random 32-byte data-encryption key (DEK) is generated once per content
//	    entry and stored only in wrapped form, so the passphrase itself is never
//	    persisted and never recoverable from the database.
//	  - Both the DEK wrapping and the file payloads use XChaCha20-Poly1305, an
//	    AEAD with 192-bit nonces: nonces are drawn at random per operation with no
//	    practical collision risk and no counter state to track.
//	  - Every ciphertext is bound to its location through additional
//	    authenticated data (the content slug, plus the file path for file
//	    payloads), so ciphertexts cannot be moved between slugs or file paths
//	    without detection.
//
// The indirection through a DEK means the passphrase is verified by one cheap
// AEAD check on 32 bytes rather than by trial-decrypting content, and that a
// visitor's browser can hold the DEK for the length of a visit (see Unlocker)
// without the passphrase ever being stored anywhere.
package crypto

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

const (
	// KeyLen is the length of both the KEK and the DEK.
	KeyLen = chacha20poly1305.KeySize // 32
	// SaltLen is the length of the per-content Argon2id salt.
	SaltLen = 16
	// NonceLen is the XChaCha20-Poly1305 nonce length.
	NonceLen = chacha20poly1305.NonceSizeX // 24

	// argon2idVersion is the Argon2 version implemented by x/crypto/argon2
	// (0x13 == 19). Recorded in the parameter string so a future version change
	// is detected rather than silently producing a different key.
	argon2idVersion = argon2.Version

	dekAADPrefix  = "easy-host/dek/v1"
	fileAADPrefix = "easy-host/file/v1"
)

var (
	// ErrWrongPassphrase means the passphrase did not unwrap the DEK. It is also
	// returned when the wrapped key has been tampered with, as the two are
	// cryptographically indistinguishable.
	ErrWrongPassphrase = errors.New("wrong passphrase")
	// ErrCorrupted means an authenticated payload failed verification: the
	// ciphertext, its nonce, or the data bound into it has been altered.
	ErrCorrupted = errors.New("encrypted payload is corrupted or was tampered with")
	// ErrEmptyPassphrase rejects a blank passphrase.
	ErrEmptyPassphrase = errors.New("passphrase must not be empty")
)

// Params holds the Argon2id cost parameters used to derive a KEK.
type Params struct {
	Memory  uint32 // KiB
	Time    uint32 // iterations
	Threads uint8  // lanes
}

// DefaultParams returns the second recommended option of RFC 9106 §4:
// 64 MiB of memory, 3 iterations, 4 lanes. Parameters are stored per content
// entry, so raising these later leaves existing content readable.
func DefaultParams() Params {
	return Params{Memory: 64 * 1024, Time: 3, Threads: 4}
}

// String encodes the parameters in a self-describing, PHC-style form, e.g.
// "argon2id$v=19$m=65536,t=3,p=4". This is what gets persisted.
func (p Params) String() string {
	return fmt.Sprintf("argon2id$v=%d$m=%d,t=%d,p=%d", argon2idVersion, p.Memory, p.Time, p.Threads)
}

// ParseParams reads back the encoding produced by Params.String.
func ParseParams(s string) (Params, error) {
	var p Params
	var version int
	n, err := fmt.Sscanf(s, "argon2id$v=%d$m=%d,t=%d,p=%d", &version, &p.Memory, &p.Time, &p.Threads)
	if err != nil || n != 4 {
		return Params{}, fmt.Errorf("unsupported KDF parameters %q", s)
	}
	if version != argon2idVersion {
		return Params{}, fmt.Errorf("unsupported argon2id version %d in %q", version, s)
	}
	if p.Memory == 0 || p.Time == 0 || p.Threads == 0 {
		return Params{}, fmt.Errorf("invalid KDF parameters %q", s)
	}
	return p, nil
}

// kdfSem bounds how many Argon2id derivations run at once. Each one allocates
// Params.Memory (64 MiB by default), so an unbounded number of concurrent
// unlock attempts would be a memory-exhaustion lever for anyone who can reach a
// passphrase-protected URL. Derivations queue instead.
var kdfSem = make(chan struct{}, kdfConcurrency())

func kdfConcurrency() int {
	n := runtime.NumCPU() / 2
	if n < 1 {
		n = 1
	}
	if n > 4 {
		n = 4
	}
	return n
}

// deriveKEK stretches the passphrase into a key-encryption key.
func deriveKEK(passphrase string, salt []byte, p Params) []byte {
	kdfSem <- struct{}{}
	defer func() { <-kdfSem }()
	return argon2.IDKey([]byte(passphrase), salt, p.Time, p.Memory, p.Threads, KeyLen)
}

// Envelope is the persisted, passphrase-protected wrapping of a content
// entry's DEK. It carries no secret that is usable without the passphrase.
type Envelope struct {
	// KDF is the Params encoding used to derive the KEK.
	KDF string
	// Salt is the per-content Argon2id salt.
	Salt []byte
	// WrappedKey is the DEK sealed under the KEK: nonce || ciphertext || tag.
	WrappedKey []byte
}

// NewEnvelope generates a fresh DEK and wraps it under a KEK derived from the
// passphrase. It returns the envelope to persist and the DEK to encrypt with;
// the caller must not persist the DEK.
func NewEnvelope(passphrase, slug string) (*Envelope, []byte, error) {
	if passphrase == "" {
		return nil, nil, ErrEmptyPassphrase
	}
	dek := make([]byte, KeyLen)
	if _, err := rand.Read(dek); err != nil {
		return nil, nil, fmt.Errorf("generating data key: %w", err)
	}
	env, err := wrap(dek, passphrase, slug)
	if err != nil {
		return nil, nil, err
	}
	return env, dek, nil
}

func wrap(dek []byte, passphrase, slug string) (*Envelope, error) {
	salt := make([]byte, SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("generating salt: %w", err)
	}
	params := DefaultParams()
	kek := deriveKEK(passphrase, salt, params)
	sealed, err := seal(kek, dekAAD(slug), dek)
	if err != nil {
		return nil, fmt.Errorf("wrapping data key: %w", err)
	}
	return &Envelope{KDF: params.String(), Salt: salt, WrappedKey: sealed}, nil
}

// Unwrap re-derives the KEK from the passphrase and recovers the DEK. A
// passphrase that does not match yields ErrWrongPassphrase — the AEAD tag on the
// wrapped key is what verifies the passphrase, so no separate verifier value is
// stored.
func (e *Envelope) Unwrap(passphrase, slug string) ([]byte, error) {
	if passphrase == "" {
		return nil, ErrEmptyPassphrase
	}
	params, err := ParseParams(e.KDF)
	if err != nil {
		return nil, err
	}
	if len(e.Salt) == 0 || len(e.WrappedKey) == 0 {
		return nil, fmt.Errorf("%w: envelope is incomplete", ErrCorrupted)
	}
	kek := deriveKEK(passphrase, e.Salt, params)
	dek, err := open(kek, dekAAD(slug), e.WrappedKey)
	if err != nil {
		return nil, ErrWrongPassphrase
	}
	if len(dek) != KeyLen {
		return nil, fmt.Errorf("%w: unwrapped key has wrong length", ErrCorrupted)
	}
	return dek, nil
}

// EncryptFile seals a file payload under the content's DEK, binding it to the
// slug and file path it is stored at.
func EncryptFile(dek []byte, slug, filePath string, plaintext []byte) ([]byte, error) {
	return seal(dek, fileAAD(slug, filePath), plaintext)
}

// DecryptFile opens a payload sealed by EncryptFile. It fails if the payload,
// the slug, or the file path differ from what was sealed.
func DecryptFile(dek []byte, slug, filePath string, sealed []byte) ([]byte, error) {
	out, err := open(dek, fileAAD(slug, filePath), sealed)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCorrupted, err)
	}
	return out, nil
}

// seal produces nonce || ciphertext || tag.
func seal(key, aad, plaintext []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	out := make([]byte, NonceLen, NonceLen+len(plaintext)+aead.Overhead())
	if _, err := rand.Read(out[:NonceLen]); err != nil {
		return nil, fmt.Errorf("generating nonce: %w", err)
	}
	return aead.Seal(out, out[:NonceLen], plaintext, aad), nil
}

// open reverses seal.
func open(key, aad, sealed []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	if len(sealed) < NonceLen+aead.Overhead() {
		return nil, errors.New("payload too short")
	}
	return aead.Open(nil, sealed[:NonceLen], sealed[NonceLen:], aad)
}

// dekAAD binds a wrapped DEK to the content entry it belongs to.
func dekAAD(slug string) []byte {
	return []byte(dekAADPrefix + "|" + slug)
}

// fileAAD binds a file payload to its slug and path. The path is length-prefixed
// so that no two (slug, path) pairs can produce the same AAD.
func fileAAD(slug, filePath string) []byte {
	var b strings.Builder
	b.WriteString(fileAADPrefix)
	b.WriteByte('|')
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], uint64(len(slug)))
	b.Write(n[:])
	b.WriteString(slug)
	binary.BigEndian.PutUint64(n[:], uint64(len(filePath)))
	b.Write(n[:])
	b.WriteString(filePath)
	return []byte(b.String())
}
