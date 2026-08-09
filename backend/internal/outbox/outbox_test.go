package outbox

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func openProtectedForTest(
	writer *Writer,
	event Event,
	protected protectedPayload,
) ([]byte, error) {
	aad, err := eventAssociatedData(event)
	if err != nil {
		return nil, err
	}
	return writer.aead.Open(nil, protected.Nonce, protected.Ciphertext, aad)
}

func TestProtectedPayloadUsesAuthenticatedEncryption(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, 32)
	writer, err := NewWriter("test-v1", key)
	if err != nil {
		t.Fatalf("constructing writer: %v", err)
	}

	event := Event{
		ID:                "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		Type:              "identity.email_verification_requested",
		SchemaVersion:     1,
		SourceModule:      "IDENTITY_AND_ACCESS",
		AggregateType:     "ACCOUNT",
		AggregateID:       "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
		AggregateRevision: 1,
		CorrelationID:     "request-1",
	}
	payload := VerificationDelivery{
		Destination:       "Student@Example.com",
		Locale:            "en",
		TemplateContract:  "student-email-verification-v1",
		VerificationToken: "BEARER_CANARY_123",
		ExpiresAt:         time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC),
	}

	reservation, err := writer.ReserveProtectedPayload(context.Background())
	if err != nil {
		t.Fatalf("reserving payload: %v", err)
	}
	protected, err := writer.protect(context.Background(), event, payload, reservation)
	if err != nil {
		t.Fatalf("protecting payload: %v", err)
	}
	if bytes.Contains(protected.Ciphertext, []byte(payload.Destination)) ||
		bytes.Contains(protected.Ciphertext, []byte(payload.VerificationToken)) {
		t.Fatal("ciphertext contains a plaintext delivery canary")
	}

	plaintext, err := openProtectedForTest(writer, event, protected)
	if err != nil {
		t.Fatalf("opening protected payload: %v", err)
	}
	var got VerificationDelivery
	if err := json.Unmarshal(plaintext, &got); err != nil {
		t.Fatalf("decoding protected payload: %v", err)
	}
	if got != payload {
		t.Fatalf("round trip = %#v, want %#v", got, payload)
	}
	wrongWriter, err := NewWriter("test-v2", bytes.Repeat([]byte{0x24}, 32))
	if err != nil {
		t.Fatalf("constructing wrong-key writer: %v", err)
	}
	if _, err := openProtectedForTest(wrongWriter, event, protected); err == nil {
		t.Fatal("a different configured key version authenticated the protected payload")
	}

	changed := event
	changed.AggregateRevision++
	if _, err := openProtectedForTest(writer, changed, protected); err == nil {
		t.Fatal("changed associated event data authenticated successfully")
	}
}

func TestProtectedPayloadReservationIsSingleUseAcrossCopies(t *testing.T) {
	writer, err := NewWriter("test-v1", bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("constructing writer: %v", err)
	}
	event := Event{
		ID:                "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		Type:              "identity.email_verification_requested",
		SchemaVersion:     1,
		SourceModule:      "IDENTITY_AND_ACCESS",
		AggregateType:     "ACCOUNT",
		AggregateID:       "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
		AggregateRevision: 1,
		CorrelationID:     "request-1",
	}
	reservation, err := writer.ReserveProtectedPayload(context.Background())
	if err != nil {
		t.Fatalf("reserving payload: %v", err)
	}
	copied := reservation
	if _, err := writer.protect(
		context.Background(), event, map[string]string{"value": "first"}, reservation,
	); err != nil {
		t.Fatalf("using reservation: %v", err)
	}
	if _, err := writer.protect(
		context.Background(), event, map[string]string{"value": "second"}, copied,
	); err == nil {
		t.Fatal("a copied reservation reused authenticated-encryption nonce material")
	}
}

func TestWriterRejectsUnsafeKeyConfiguration(t *testing.T) {
	for name, tc := range map[string]struct {
		version string
		key     []byte
	}{
		"missing version": {"", bytes.Repeat([]byte{1}, 32)},
		"short key":       {"v1", bytes.Repeat([]byte{1}, 16)},
		"long key":        {"v1", bytes.Repeat([]byte{1}, 64)},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewWriter(tc.version, tc.key); err == nil {
				t.Fatal("expected invalid writer configuration to fail")
			}
		})
	}
}

func TestEventValidationRejectsSecretBearingSafePayload(t *testing.T) {
	writer, err := NewWriter("test-v1", bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("constructing writer: %v", err)
	}
	event := Event{
		Type:              "identity.email_verification_requested",
		SchemaVersion:     1,
		SourceModule:      "IDENTITY_AND_ACCESS",
		AggregateType:     "ACCOUNT",
		AggregateID:       "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
		AggregateRevision: 1,
		CorrelationID:     "request-1",
		SafePayload: map[string]any{
			"verification_token": "must-not-be-here",
		},
	}
	err = writer.validateEvent(event)
	if err == nil || !strings.Contains(err.Error(), "safe payload") {
		t.Fatalf("error = %v, want safe-payload rejection", err)
	}
}

func TestOpenProtectedPayloadAuthenticatesEventAndDoesNotExposePlaintext(t *testing.T) {
	writer, err := NewWriter("test-v1", bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	event := Event{ID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Type: "identity.email_verification_requested", SchemaVersion: 1, SourceModule: "IDENTITY_AND_ACCESS", AggregateType: "ACCOUNT", AggregateID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", AggregateRevision: 1, CorrelationID: "request-1"}
	reservation, err := writer.ReserveProtectedPayload(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := writer.protect(context.Background(), event, VerificationDelivery{Destination: "student@example.com", Locale: "en", TemplateContract: "student-email-verification-v1", VerificationToken: "TOKEN_CANARY", ExpiresAt: time.Now().Add(time.Hour)}, reservation)
	if err != nil {
		t.Fatal(err)
	}
	stored := StoredProtectedPayload{KeyVersion: sealed.KeyVersion, Nonce: sealed.Nonce, Ciphertext: sealed.Ciphertext}
	var opened VerificationDelivery
	if err := writer.OpenProtectedPayload(context.Background(), event, stored, &opened); err != nil {
		t.Fatal(err)
	}
	if opened.VerificationToken != "TOKEN_CANARY" || opened.Destination != "student@example.com" {
		t.Fatalf("opened=%+v", opened)
	}
	event.AggregateRevision++
	err = writer.OpenProtectedPayload(context.Background(), event, stored, &opened)
	if err == nil {
		t.Fatal("tampered associated data authenticated")
	}
	if strings.Contains(err.Error(), "TOKEN_CANARY") || strings.Contains(err.Error(), "student@example.com") {
		t.Fatal("decryption error exposed protected plaintext")
	}
}
