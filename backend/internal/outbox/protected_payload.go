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
	"sync/atomic"
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
	reservation ProtectedPayloadReservation,
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

	if len(reservation.nonce) != w.aead.NonceSize() {
		return protectedPayload{}, errors.New("protected outbox reservation is invalid")
	}
	if reservation.used == nil {
		return protectedPayload{}, errors.New("protected outbox reservation is invalid")
	}
	aad, err := eventAssociatedData(event)
	if err != nil {
		return protectedPayload{}, err
	}
	if !reservation.used.CompareAndSwap(false, true) {
		return protectedPayload{}, errors.New("protected outbox reservation was already used")
	}
	return protectedPayload{
		KeyVersion: w.keyVersion,
		Nonce:      append([]byte(nil), reservation.nonce...),
		Ciphertext: w.aead.Seal(nil, reservation.nonce, plaintext, aad),
	}, nil
}

// ReserveProtectedPayload performs the only fallible entropy read before a
// caller resolves hidden Account state.
func (w *Writer) ReserveProtectedPayload(
	ctx context.Context,
) (ProtectedPayloadReservation, error) {
	if err := ctx.Err(); err != nil {
		return ProtectedPayloadReservation{}, err
	}
	nonce := make([]byte, w.aead.NonceSize())
	if _, err := io.ReadFull(w.random, nonce); err != nil {
		return ProtectedPayloadReservation{}, fmt.Errorf("generating protected outbox nonce: %w", err)
	}
	return ProtectedPayloadReservation{nonce: nonce, used: &atomic.Bool{}}, nil
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
