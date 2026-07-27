package identity

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"time"

	"github.com/google/uuid"

	"github.com/Owlah2025/gradex/backend/internal/config"
)

const actionSecretBytes = 32

type ActionSecretPurpose string

const (
	ActionEmailVerification ActionSecretPurpose = "EMAIL_VERIFICATION"
	ActionPasswordReset     ActionSecretPurpose = "PASSWORD_RESET"
	ActionStaffInvitation   ActionSecretPurpose = "STAFF_INVITATION"
)

// valid reports whether the purpose is one the database allowlist admits.
// Keeping this beside the constants means a new purpose fails in Go before it
// reaches the identity_action_secrets CHECK constraint.
func (p ActionSecretPurpose) valid() bool {
	return p == ActionEmailVerification || p == ActionPasswordReset || p == ActionStaffInvitation
}

var ErrTokenInvalid = errors.New("action secret is invalid")

type IssuedActionSecret struct {
	ID        string
	Purpose   ActionSecretPurpose
	Bearer    config.Secret
	Digest    []byte
	IssuedAt  time.Time
	ExpiresAt time.Time
}

type actionSecretOptions struct {
	Purpose ActionSecretPurpose
	Now     time.Time
	TTL     time.Duration
	Random  io.Reader
}

func newActionSecret(options actionSecretOptions) (IssuedActionSecret, error) {
	if !options.Purpose.valid() {
		return IssuedActionSecret{}, errors.New("action-secret purpose is required")
	}
	if options.Now.IsZero() {
		return IssuedActionSecret{}, errors.New("action-secret clock is required")
	}
	if options.TTL <= 0 {
		return IssuedActionSecret{}, errors.New("action-secret TTL must be positive")
	}
	if options.Random == nil {
		options.Random = rand.Reader
	}

	raw := make([]byte, actionSecretBytes)
	if _, err := io.ReadFull(options.Random, raw); err != nil {
		return IssuedActionSecret{}, errors.New("generating action secret")
	}
	bearer := base64.RawURLEncoding.EncodeToString(raw)
	digest := sha256.Sum256([]byte(bearer))
	return IssuedActionSecret{
		ID:        uuid.NewString(),
		Purpose:   options.Purpose,
		Bearer:    config.NewSecret(bearer),
		Digest:    digest[:],
		IssuedAt:  options.Now,
		ExpiresAt: options.Now.Add(options.TTL),
	}, nil
}

func DigestActionSecret(bearer string) ([]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(bearer)
	if err != nil || len(raw) != actionSecretBytes {
		return nil, ErrTokenInvalid
	}
	digest := sha256.Sum256([]byte(bearer))
	return digest[:], nil
}
