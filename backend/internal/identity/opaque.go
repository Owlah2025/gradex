package identity

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"

	"github.com/Owlah2025/gradex/backend/internal/config"
)

// The opaque-credential primitive shared by every unguessable first-party
// browser value Gradex mints.
//
// It exists as its own file because a second consumer arrived. Session
// credentials had these properties first, but they are not *session* properties
// — they are the properties of any 256-bit CSPRNG value a browser stores and
// the server recognizes by digest. A trusted-device credential needs exactly
// them and needs to be conceptually unrelated to a session credential: it
// authenticates nobody, it grants no capability by itself, and giving it a
// session-domain type would invite exactly the confusion where one is later
// accepted where the other was meant.
//
// So the primitive is extracted rather than shared by reference: both callers
// get identical cryptography and neither borrows the other's meaning.

// OpaqueCredentialBytes is the security floor for a bearer-equivalent value.
const OpaqueCredentialBytes = 32

// NewOpaqueCredential mints one credential and returns the plaintext the
// browser receives together with the digest the database stores.
//
// The plaintext is wrapped in config.Secret so logging or formatting whatever
// carries it cannot emit a usable value. The two are returned together because
// they must never be derived apart: a caller that stores a digest it did not
// mint here has no guarantee about the entropy behind it.
func NewOpaqueCredential() (config.Secret, string, error) {
	plaintext, err := newOpaquePlaintext()
	if err != nil {
		return config.Secret{}, "", err
	}
	return config.NewSecret(plaintext), DigestOpaqueCredential(plaintext), nil
}

// newOpaquePlaintext is the generator itself.
//
// It returns a bare string so a caller that is going to wrap the value in its
// own domain type does not have to unwrap a config.Secret to do it — an
// Expose() that exists only to immediately re-wrap is a plaintext read the
// reviewed boundary should never have to account for. The two callers are
// NewOpaqueCredential above and the session credential minter, and both wrap
// the result before it leaves the package.
func newOpaquePlaintext() (string, error) {
	buffer := make([]byte, OpaqueCredentialBytes)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

// DigestOpaqueCredential is the one-way transform applied before an opaque
// credential touches the database.
//
// A plain SHA-256, not a password hash, and that is deliberate rather than an
// oversight: these values are 256 bits of CSPRNG output with no structure to
// guess, so the slow-hash property that protects a human-chosen password buys
// nothing here while costing an Argon2id computation on every request that
// presents one.
func DigestOpaqueCredential(credential string) string {
	sum := sha256.Sum256([]byte(credential))
	return base64.RawStdEncoding.EncodeToString(sum[:])
}

// OpaqueDigestEqual compares two digests without leaking their divergence point
// through timing. Digests are not secret in the way the credential is, but a
// lookup that short-circuits on the first differing byte is an oracle worth not
// building in the first place.
func OpaqueDigestEqual(left, right string) bool {
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

// ValidOpaqueCredential reports whether a presented value has the exact shape
// this package mints.
//
// Checked before the value reaches a database lookup so that a malformed or
// oversized cookie is refused by shape rather than by failing to match a row —
// which keeps a hostile client from turning the credential column into a search
// interface.
func ValidOpaqueCredential(credential string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(credential)
	if err != nil || len(decoded) != OpaqueCredentialBytes {
		return false
	}
	return base64.RawURLEncoding.EncodeToString(decoded) == credential
}
