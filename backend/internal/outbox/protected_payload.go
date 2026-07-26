package outbox

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const protectedPayloadKeyBytes = 32

// Writer appends one immutable safe event and its separately protected
// delivery payload to a caller-owned PostgreSQL transaction.
type Writer struct {
	keyVersion string
	aead       cipher.AEAD
	random     io.Reader
}

func NewWriter(keyVersion string, key []byte) (*Writer, error) {
	keyVersion = strings.TrimSpace(keyVersion)
	if keyVersion == "" {
		return nil, errors.New("protected outbox key version is required")
	}
	if len(key) != protectedPayloadKeyBytes {
		return nil, fmt.Errorf("protected outbox key must be %d bytes", protectedPayloadKeyBytes)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("constructing protected outbox cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("constructing protected outbox AEAD: %w", err)
	}
	return &Writer{
		keyVersion: keyVersion,
		aead:       aead,
		random:     rand.Reader,
	}, nil
}

func (w *Writer) protect(
	ctx context.Context,
	event Event,
	payload any,
) (protectedPayload, error) {
	if err := ctx.Err(); err != nil {
		return protectedPayload{}, err
	}
	plaintext, err := json.Marshal(payload)
	if err != nil {
		return protectedPayload{}, fmt.Errorf("encoding protected outbox payload: %w", err)
	}
	if string(plaintext) == "null" {
		return protectedPayload{}, errors.New("protected outbox payload is required")
	}

	nonce := make([]byte, w.aead.NonceSize())
	if _, err := io.ReadFull(w.random, nonce); err != nil {
		return protectedPayload{}, fmt.Errorf("generating protected outbox nonce: %w", err)
	}
	aad, err := eventAssociatedData(event)
	if err != nil {
		return protectedPayload{}, err
	}
	return protectedPayload{
		KeyVersion: w.keyVersion,
		Nonce:      nonce,
		Ciphertext: w.aead.Seal(nil, nonce, plaintext, aad),
	}, nil
}

func eventAssociatedData(event Event) ([]byte, error) {
	return json.Marshal(struct {
		ID                string `json:"id"`
		Type              string `json:"type"`
		SchemaVersion     int    `json:"schema_version"`
		SourceModule      string `json:"source_module"`
		AggregateType     string `json:"aggregate_type"`
		AggregateID       string `json:"aggregate_id"`
		AggregateRevision int    `json:"aggregate_revision"`
	}{
		ID:                event.ID,
		Type:              event.Type,
		SchemaVersion:     event.SchemaVersion,
		SourceModule:      event.SourceModule,
		AggregateType:     event.AggregateType,
		AggregateID:       event.AggregateID,
		AggregateRevision: event.AggregateRevision,
	})
}
