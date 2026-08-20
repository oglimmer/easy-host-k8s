package crypto

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

// Tests run against the production Argon2id cost (DefaultParams), so they keep
// the number of key derivations small.
const (
	pass = "correct horse battery staple"
	slug = "my-page"
)

func TestEnvelopeRoundTrip(t *testing.T) {
	env, dek, err := NewEnvelope(pass, slug)
	if err != nil {
		t.Fatalf("NewEnvelope: %v", err)
	}
	if len(dek) != KeyLen {
		t.Fatalf("dek length = %d, want %d", len(dek), KeyLen)
	}
	if len(env.Salt) != SaltLen {
		t.Fatalf("salt length = %d, want %d", len(env.Salt), SaltLen)
	}
	if bytes.Contains(env.WrappedKey, dek) {
		t.Fatal("wrapped key contains the plaintext data key")
	}

	got, err := env.Unwrap(pass, slug)
	if err != nil {
		t.Fatalf("Unwrap: %v", err)
	}
	if !bytes.Equal(got, dek) {
		t.Fatal("unwrapped key differs from the generated key")
	}
}

func TestEnvelopeRejectsWrongPassphrase(t *testing.T) {
	env, _, err := NewEnvelope(pass, slug)
	if err != nil {
		t.Fatalf("NewEnvelope: %v", err)
	}
	for _, wrong := range []string{pass + "!", strings.ToUpper(pass), " " + pass} {
		if _, err := env.Unwrap(wrong, slug); !errors.Is(err, ErrWrongPassphrase) {
			t.Errorf("Unwrap(%q) error = %v, want ErrWrongPassphrase", wrong, err)
		}
	}
}

func TestEnvelopeIsBoundToSlug(t *testing.T) {
	env, _, err := NewEnvelope(pass, slug)
	if err != nil {
		t.Fatalf("NewEnvelope: %v", err)
	}
	// A wrapped key lifted into another content row must not open, even with the
	// right passphrase.
	if _, err := env.Unwrap(pass, "other-page"); !errors.Is(err, ErrWrongPassphrase) {
		t.Errorf("Unwrap with foreign slug error = %v, want ErrWrongPassphrase", err)
	}
}

func TestEnvelopeDetectsTampering(t *testing.T) {
	env, _, err := NewEnvelope(pass, slug)
	if err != nil {
		t.Fatalf("NewEnvelope: %v", err)
	}
	tampered := *env
	tampered.WrappedKey = append([]byte(nil), env.WrappedKey...)
	tampered.WrappedKey[len(tampered.WrappedKey)-1] ^= 0x01
	if _, err := tampered.Unwrap(pass, slug); !errors.Is(err, ErrWrongPassphrase) {
		t.Errorf("Unwrap of tampered key error = %v, want ErrWrongPassphrase", err)
	}

	short := *env
	short.WrappedKey = env.WrappedKey[:NonceLen]
	if _, err := short.Unwrap(pass, slug); !errors.Is(err, ErrWrongPassphrase) {
		t.Errorf("Unwrap of truncated key error = %v, want ErrWrongPassphrase", err)
	}
}

func TestEnvelopeSaltsAreUnique(t *testing.T) {
	a, _, err := NewEnvelope(pass, slug)
	if err != nil {
		t.Fatalf("NewEnvelope: %v", err)
	}
	b, _, err := NewEnvelope(pass, slug)
	if err != nil {
		t.Fatalf("NewEnvelope: %v", err)
	}
	if bytes.Equal(a.Salt, b.Salt) {
		t.Fatal("two envelopes reused the same salt")
	}
	if bytes.Equal(a.WrappedKey, b.WrappedKey) {
		t.Fatal("two envelopes produced identical wrapped keys")
	}
}

func TestEnvelopeRejectsEmptyPassphrase(t *testing.T) {
	if _, _, err := NewEnvelope("", slug); !errors.Is(err, ErrEmptyPassphrase) {
		t.Errorf("NewEnvelope(\"\") error = %v, want ErrEmptyPassphrase", err)
	}
	env, _, err := NewEnvelope(pass, slug)
	if err != nil {
		t.Fatalf("NewEnvelope: %v", err)
	}
	if _, err := env.Unwrap("", slug); !errors.Is(err, ErrEmptyPassphrase) {
		t.Errorf("Unwrap(\"\") error = %v, want ErrEmptyPassphrase", err)
	}
}

func TestFileRoundTrip(t *testing.T) {
	dek := testKey(t)
	for _, plaintext := range [][]byte{nil, {}, []byte("<h1>hello</h1>"), bytes.Repeat([]byte("x"), 1<<16)} {
		sealed, err := EncryptFile(dek, slug, "index.html", plaintext)
		if err != nil {
			t.Fatalf("EncryptFile: %v", err)
		}
		if len(plaintext) > 0 && bytes.Contains(sealed, plaintext) {
			t.Fatal("ciphertext contains the plaintext")
		}
		got, err := DecryptFile(dek, slug, "index.html", sealed)
		if err != nil {
			t.Fatalf("DecryptFile: %v", err)
		}
		if !bytes.Equal(got, plaintext) && !(len(got) == 0 && len(plaintext) == 0) {
			t.Fatalf("round trip mismatch: got %d bytes, want %d", len(got), len(plaintext))
		}
	}
}

func TestFileIsBoundToSlugAndPath(t *testing.T) {
	dek := testKey(t)
	sealed, err := EncryptFile(dek, slug, "css/site.css", []byte("body{}"))
	if err != nil {
		t.Fatalf("EncryptFile: %v", err)
	}
	cases := []struct{ slug, path string }{
		{"other-page", "css/site.css"}, // moved to another content entry
		{slug, "index.html"},           // moved to another path
		{slug, "css/site.cs"},          // path prefix
		{slug, "css/site.csss"},
	}
	for _, c := range cases {
		if _, err := DecryptFile(dek, c.slug, c.path, sealed); !errors.Is(err, ErrCorrupted) {
			t.Errorf("DecryptFile(%q, %q) error = %v, want ErrCorrupted", c.slug, c.path, err)
		}
	}
}

// TestFileAADIsUnambiguous guards the length-prefixing in fileAAD: without it,
// ("ab", "c") and ("a", "bc") would authenticate the same bytes.
func TestFileAADIsUnambiguous(t *testing.T) {
	if bytes.Equal(fileAAD("ab", "c"), fileAAD("a", "bc")) {
		t.Fatal("fileAAD is ambiguous across slug/path boundaries")
	}
}

func TestFileDetectsTampering(t *testing.T) {
	dek := testKey(t)
	sealed, err := EncryptFile(dek, slug, "index.html", []byte("<h1>hello</h1>"))
	if err != nil {
		t.Fatalf("EncryptFile: %v", err)
	}

	flipped := append([]byte(nil), sealed...)
	flipped[len(flipped)-1] ^= 0x80
	if _, err := DecryptFile(dek, slug, "index.html", flipped); !errors.Is(err, ErrCorrupted) {
		t.Errorf("tampered tag error = %v, want ErrCorrupted", err)
	}

	nonceFlipped := append([]byte(nil), sealed...)
	nonceFlipped[0] ^= 0x01
	if _, err := DecryptFile(dek, slug, "index.html", nonceFlipped); !errors.Is(err, ErrCorrupted) {
		t.Errorf("tampered nonce error = %v, want ErrCorrupted", err)
	}

	if _, err := DecryptFile(dek, slug, "index.html", sealed[:NonceLen]); !errors.Is(err, ErrCorrupted) {
		t.Errorf("truncated payload error = %v, want ErrCorrupted", err)
	}

	other := testKey(t)
	if _, err := DecryptFile(other, slug, "index.html", sealed); !errors.Is(err, ErrCorrupted) {
		t.Errorf("wrong key error = %v, want ErrCorrupted", err)
	}
}

func TestFileNoncesAreUnique(t *testing.T) {
	dek := testKey(t)
	seen := map[string]bool{}
	for i := 0; i < 64; i++ {
		sealed, err := EncryptFile(dek, slug, "index.html", []byte("same plaintext"))
		if err != nil {
			t.Fatalf("EncryptFile: %v", err)
		}
		nonce := string(sealed[:NonceLen])
		if seen[nonce] {
			t.Fatal("nonce reused across encryptions")
		}
		seen[nonce] = true
	}
}

func TestParamsRoundTrip(t *testing.T) {
	want := DefaultParams()
	if got := want.String(); got != "argon2id$v=19$m=65536,t=3,p=4" {
		t.Fatalf("String() = %q", got)
	}
	got, err := ParseParams(want.String())
	if err != nil {
		t.Fatalf("ParseParams: %v", err)
	}
	if got != want {
		t.Fatalf("ParseParams round trip = %+v, want %+v", got, want)
	}
}

func TestParseParamsRejectsJunk(t *testing.T) {
	bad := []string{
		"",
		"argon2id",
		"bcrypt$v=19$m=65536,t=3,p=4",
		"argon2id$v=18$m=65536,t=3,p=4", // version we did not implement
		"argon2id$v=19$m=0,t=3,p=4",
		"argon2id$v=19$m=65536,t=3",
		"argon2id$v=19$m=abc,t=3,p=4",
	}
	for _, s := range bad {
		if _, err := ParseParams(s); err == nil {
			t.Errorf("ParseParams(%q) succeeded, want error", s)
		}
	}
}

func TestUnwrapRejectsUnknownKDF(t *testing.T) {
	env := &Envelope{KDF: "scrypt$N=16384", Salt: make([]byte, SaltLen), WrappedKey: make([]byte, 72)}
	if _, err := env.Unwrap(pass, slug); err == nil {
		t.Fatal("Unwrap accepted an unknown KDF")
	}
}

func TestUnwrapRejectsIncompleteEnvelope(t *testing.T) {
	params := DefaultParams().String()
	for name, env := range map[string]*Envelope{
		"no salt":  {KDF: params, WrappedKey: make([]byte, 72)},
		"no key":   {KDF: params, Salt: make([]byte, SaltLen)},
		"no parts": {KDF: params},
	} {
		if _, err := env.Unwrap(pass, slug); !errors.Is(err, ErrCorrupted) {
			t.Errorf("%s: error = %v, want ErrCorrupted", name, err)
		}
	}
}

func TestUnlockTokenRoundTrip(t *testing.T) {
	u := testUnlocker(t, time.Hour)
	dek := testKey(t)

	token, err := u.Seal(slug, dek)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if strings.Contains(token, string(dek)) {
		t.Fatal("token exposes the data key")
	}
	got, err := u.Open(slug, token)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !bytes.Equal(got, dek) {
		t.Fatal("token round trip returned a different key")
	}
}

func TestUnlockTokenIsBoundToSlug(t *testing.T) {
	u := testUnlocker(t, time.Hour)
	token, err := u.Seal(slug, testKey(t))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if _, err := u.Open("other-page", token); !errors.Is(err, ErrUnlockToken) {
		t.Errorf("Open with foreign slug error = %v, want ErrUnlockToken", err)
	}
}

func TestUnlockTokenExpires(t *testing.T) {
	u := testUnlocker(t, time.Nanosecond)
	token, err := u.Seal(slug, testKey(t))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	// Expiry has one-second granularity, so a 1ns TTL is already in the past.
	if _, err := u.Open(slug, token); !errors.Is(err, ErrUnlockToken) {
		t.Errorf("Open of expired token error = %v, want ErrUnlockToken", err)
	}
}

func TestUnlockTokenRejectsForgeries(t *testing.T) {
	u := testUnlocker(t, time.Hour)
	token, err := u.Seal(slug, testKey(t))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	other := testUnlocker(t, time.Hour) // different server secret
	if _, err := other.Open(slug, token); !errors.Is(err, ErrUnlockToken) {
		t.Errorf("token from another secret opened: %v", err)
	}

	for name, bad := range map[string]string{
		"empty":      "",
		"not base64": "!!!not-base64!!!",
		"too short":  "AAAA",
		"flipped":    flipLastByte(token),
	} {
		if _, err := u.Open(slug, bad); !errors.Is(err, ErrUnlockToken) {
			t.Errorf("%s: error = %v, want ErrUnlockToken", name, err)
		}
	}
}

func TestNewUnlockerValidatesInput(t *testing.T) {
	if _, err := NewUnlocker("", time.Hour); err == nil {
		t.Error("NewUnlocker accepted an empty secret")
	}
	if _, err := NewUnlocker("secret", 0); err == nil {
		t.Error("NewUnlocker accepted a zero TTL")
	}
	u := testUnlocker(t, time.Hour)
	if _, err := u.Seal(slug, []byte("too short")); err == nil {
		t.Error("Seal accepted a short data key")
	}
}

// ---- helpers -------------------------------------------------------------

func testKey(t *testing.T) []byte {
	t.Helper()
	env, dek, err := NewEnvelope(pass, slug)
	if err != nil || env == nil {
		t.Fatalf("generating test key: %v", err)
	}
	return dek
}

func testUnlocker(t *testing.T, ttl time.Duration) *Unlocker {
	t.Helper()
	// A distinct secret per call, so cross-Unlocker tests cannot collide.
	env, dek, err := NewEnvelope(pass, slug)
	if err != nil || env == nil {
		t.Fatalf("generating secret: %v", err)
	}
	u, err := NewUnlocker(string(dek), ttl)
	if err != nil {
		t.Fatalf("NewUnlocker: %v", err)
	}
	return u
}

func flipLastByte(token string) string {
	b := []byte(token)
	if len(b) == 0 {
		return token
	}
	if b[len(b)-1] == 'A' {
		b[len(b)-1] = 'B'
	} else {
		b[len(b)-1] = 'A'
	}
	return string(b)
}
