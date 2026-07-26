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

const ActionEmailVerification ActionSecretPurpose = "EMAIL_VERIFICATION"

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
	Now    time.Time
	TTL    time.Duration
	Random io.Reader
}

func newActionSecret(options actionSecretOptions) (IssuedActionSecret, error) {
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
		Purpose:   ActionEmailVerification,
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
