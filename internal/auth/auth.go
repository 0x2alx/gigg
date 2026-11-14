// Package auth holds gigg's credential primitives: argon2id password hashes, API key tokens and
// session tokens. Tokens carry 256 random bits, so a plain SHA-256 of the token is a safe stored
// form; comparisons are constant-time.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/crypto/argon2"
)

// argon2id parameters (RFC 9106 second recommendation, memory-constrained): 64 MiB, 3 passes.
const (
	argonTime    = 3
	argonMemory  = 64 * 1024
	argonThreads = 2
	argonKeyLen  = 32
	saltLen      = 16
)

var b64 = base64.RawStdEncoding

// HashPassword returns a PHC-style argon2id string.
func HashPassword(pw string) (string, error) {
	if err := CheckPasswordPolicy(pw); err != nil {
		return "", err
	}
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemory, argonTime, argonThreads,
		b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// CheckPasswordPolicy enforces the only rule worth having: length.
func CheckPasswordPolicy(pw string) error {
	if n := len([]rune(pw)); n < 12 || n > 256 {
		return errors.New("passwords are 12 to 256 characters")
	}
	return nil
}

// VerifyPassword reports whether pw matches the stored hash. Unknown formats never match.
func VerifyPassword(stored, pw string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var v int
	var m uint32
	var t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[2], "v=%d", &v); err != nil || v != argon2.Version {
		return false
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil || m > 1<<20 || t > 16 || p == 0 {
		return false
	}
	salt, err1 := b64.DecodeString(parts[4])
	want, err2 := b64.DecodeString(parts[5])
	if err1 != nil || err2 != nil || len(want) == 0 {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// DummyVerify spends the same time as a real verification, so a login for an unknown user
// takes as long as one with a wrong password.
func DummyVerify(pw string) {
	argon2.IDKey([]byte(pw), make([]byte, saltLen), argonTime, argonMemory, argonThreads, argonKeyLen)
}

// Key tokens: gigg_<8-char prefix>_<43 base64url chars>. The prefix is stored in clear (lookup,
// logs, UI); the whole token is stored as SHA-256.
var keyRe = regexp.MustCompile(`^gigg_([a-z0-9]{8})_([A-Za-z0-9_-]{43})$`)

// NewKeyToken returns a fresh token and its prefix.
func NewKeyToken() (token, prefix string, err error) {
	pb := make([]byte, 5)
	sb := make([]byte, 32)
	if _, err = rand.Read(pb); err != nil {
		return
	}
	if _, err = rand.Read(sb); err != nil {
		return
	}
	prefix = strings.ToLower(base32(pb))
	token = "gigg_" + prefix + "_" + base64.RawURLEncoding.EncodeToString(sb)
	return
}

// ParseKeyToken returns the prefix of a well-formed token.
func ParseKeyToken(token string) (prefix string, ok bool) {
	m := keyRe.FindStringSubmatch(token)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// HashToken is the stored form of a key or session token.
func HashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// TokenMatches compares a presented token with a stored hash in constant time.
func TokenMatches(token string, stored []byte) bool {
	return subtle.ConstantTimeCompare(HashToken(token), stored) == 1
}

// NewSessionToken returns 256 random bits, base64url.
func NewSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

const b32 = "abcdefghijklmnopqrstuvwxyz234567"

// base32 encodes 5 bytes into 8 lowercase characters (no padding).
func base32(b []byte) string {
	v := uint64(b[0])<<32 | uint64(b[1])<<24 | uint64(b[2])<<16 | uint64(b[3])<<8 | uint64(b[4])
	out := make([]byte, 8)
	for i := 7; i >= 0; i-- {
		out[i] = b32[v&31]
		v >>= 5
	}
	return string(out)
}
