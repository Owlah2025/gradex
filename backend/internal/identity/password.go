// Package identity owns Accounts, credentials, and the operations that create
// them. It is the only package that turns a plaintext password into a stored
// hash, so the rules about what may happen to a plaintext live in one place.
package identity

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"

	"github.com/Owlah2025/gradex/backend/internal/config"
)

// Password policy, resolved in specs/002-auth-rbac/spec.md §265: 15–128 Unicode
// characters including spaces, common/compromised rejection, Argon2id, and no
// composition or periodic-rotation rule.
//
// The bounds are counted in runes, not bytes. A byte-length check would reject
// a compliant Arabic passphrase — every character costs two bytes — which on an
// Arabic-default platform is not a hypothetical.
const (
	MinPasswordRunes = 15
	MaxPasswordRunes = 128
)

// ErrPasswordPolicy wraps every policy rejection so callers can distinguish a
// bad password from an infrastructure failure without matching on strings.
var ErrPasswordPolicy = errors.New("password does not meet policy")

// commonPasswords is a deliberately small embedded denylist covering the
// shapes a human picks when a deployment checklist says "choose a long
// password": repeated words, keyboard runs, and the platform's own name.
//
// It is not a breach-corpus check. The full compromised-credential requirement
// needs a dataset and a lookup path that are not chosen yet, so this stays
// honest about being a floor rather than pretending to be that check. The seam
// is CompromisedChecker below.
var commonPasswords = []string{
	"password", "passw0rd", "letmein", "welcome", "qwerty", "azerty",
	"admin", "administrator", "gradex", "changeme", "secret", "iloveyou",
	"123456", "1234567890", "abc123", "qwertyuiop", "asdfghjkl", "zxcvbnm",
}

// CompromisedChecker is the seam for the real compromised-password check once
// LG-driven provider selection settles. Returning true rejects the password.
//
// It is an interface rather than a function field so the eventual
// implementation can carry a client, a cache, and a fail-closed policy of its
// own.
type CompromisedChecker interface {
	IsCompromised(password string) (bool, error)
}

// ValidatePassword applies the policy to a plaintext.
//
// The plaintext is a parameter and never a field, a log value, or an error
// message: returned errors describe the rule that failed, never the input that
// failed it. A policy error is frequently the thing that gets pasted into a
// deploy log.
func ValidatePassword(password string, checker CompromisedChecker) error {
	runes := utf8.RuneCountInString(password)
	if runes < MinPasswordRunes {
		return fmt.Errorf("%w: needs at least %d characters, got %d",
			ErrPasswordPolicy, MinPasswordRunes, runes)
	}
	if runes > MaxPasswordRunes {
		return fmt.Errorf("%w: needs at most %d characters, got %d",
			ErrPasswordPolicy, MaxPasswordRunes, runes)
	}

	// Case-folded containment, not equality: "Password123!!!!" is the same bad
	// choice as "password" with padding bolted on to clear the length rule.
	lowered := strings.ToLower(password)
	for _, common := range commonPasswords {
		if strings.Contains(lowered, common) {
			return fmt.Errorf("%w: contains a common or easily guessed value", ErrPasswordPolicy)
		}
	}

	if checker != nil {
		compromised, err := checker.IsCompromised(password)
		if err != nil {
			// Fail closed. A checker that cannot answer must not be read as
			// having answered "not compromised".
			return fmt.Errorf("checking password against compromised set: %w", err)
		}
		if compromised {
			return fmt.Errorf("%w: appears in a known compromised set", ErrPasswordPolicy)
		}
	}
	return nil
}

// Argon2id parameters. RFC 9106's second recommended option — 64 MiB of memory,
// three passes, four lanes — chosen over the first (2 GiB) because the launch
// API container is nowhere near that envelope and a login that exhausts memory
// is an availability defect, not a security win.
//
// They are encoded into every hash, so raising them later does not invalidate
// existing credentials: verification reads the parameters from the stored hash
// and NeedsRehash reports which stored hashes are below current strength.
const (
	argonTimeCost   uint32 = 3
	argonMemoryKiB  uint32 = 64 * 1024
	argonThreads    uint8  = 4
	argonKeyLength  uint32 = 32
	argonSaltLength        = 16
)

// HashPassword returns the encoded Argon2id hash of a plaintext.
//
// The return type is config.Secret rather than string. The encoded hash is not
// a plaintext, but it is credential material: wrapping it means an accidental
// log or error-formatting call renders the redaction marker instead. Storing it
// requires an explicit Expose() at the one call site that writes to the
// database, which is greppable in review.
func HashPassword(password string) (config.Secret, error) {
	salt := make([]byte, argonSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return config.Secret{}, fmt.Errorf("generating password salt: %w", err)
	}

	key := argon2.IDKey([]byte(password), salt, argonTimeCost, argonMemoryKiB, argonThreads, argonKeyLength)

	// The PHC string format, which is what the
	// password_credentials_hash_is_argon2id CHECK constraint recognizes.
	encoded := fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemoryKiB, argonTimeCost, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	)
	return config.NewSecret(encoded), nil
}

// VerifyPassword reports whether a plaintext matches an encoded hash.
//
// It returns (false, nil) for a mismatch and an error only when the stored hash
// could not be parsed — a corrupt row is an operational problem, not a failed
// login, and conflating them hides it.
func VerifyPassword(encodedHash, password string) (bool, error) {
	params, salt, want, err := decodeHash(encodedHash)
	if err != nil {
		return false, err
	}

	got := argon2.IDKey([]byte(password), salt, params.time, params.memory, params.threads, uint32(len(want)))

	// Constant-time: a timing-variable comparison leaks how much of a guess was
	// correct, which is exactly the feedback an attacker wants.
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// NeedsRehash reports whether a stored hash was produced with weaker parameters
// than the current ones, so a successful login can transparently upgrade it.
func NeedsRehash(encodedHash string) (bool, error) {
	params, _, _, err := decodeHash(encodedHash)
	if err != nil {
		return false, err
	}
	return params.memory < argonMemoryKiB ||
		params.time < argonTimeCost ||
		params.threads < argonThreads, nil
}

type argonParams struct {
	memory  uint32
	time    uint32
	threads uint8
}

func decodeHash(encoded string) (argonParams, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	// "", "argon2id", "v=19", "m=...,t=...,p=...", salt, key
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return argonParams{}, nil, nil, errors.New("stored password hash is not in Argon2id PHC format")
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return argonParams{}, nil, nil, fmt.Errorf("reading Argon2id version: %w", err)
	}
	if version != argon2.Version {
		return argonParams{}, nil, nil, fmt.Errorf("unsupported Argon2id version %d", version)
	}

	var p argonParams
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.memory, &p.time, &p.threads); err != nil {
		return argonParams{}, nil, nil, fmt.Errorf("reading Argon2id parameters: %w", err)
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return argonParams{}, nil, nil, fmt.Errorf("decoding password salt: %w", err)
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return argonParams{}, nil, nil, fmt.Errorf("decoding password hash: %w", err)
	}
	if len(salt) == 0 || len(key) == 0 {
		return argonParams{}, nil, nil, errors.New("stored password hash has an empty salt or key")
	}
	return p, salt, key, nil
}
