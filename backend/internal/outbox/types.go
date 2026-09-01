// Package outbox owns Gradex's immutable asynchronous source-intent boundary.
// It does not claim provider acceptance or delivery and does not implement the
// future dispatcher lifecycle.
package outbox

import (
	"sync/atomic"
	"time"
)

// Event is the safe immutable portion of one asynchronous intent.
type Event struct {
	ID                string
	Type              string
	SchemaVersion     int
	SourceModule      string
	AggregateType     string
	AggregateID       string
	AggregateRevision int
	CorrelationID     string
	AvailableAt       *time.Time
	SafePayload       any
}

// VerificationDelivery is the minimum plaintext that the email dispatcher
// needs. It is marshalled only into authenticated ciphertext.
type VerificationDelivery struct {
	Destination       string    `json:"destination"`
	Locale            string    `json:"locale"`
	TemplateContract  string    `json:"template_contract"`
	VerificationToken string    `json:"verification_token"`
	ExpiresAt         time.Time `json:"expires_at"`
}

// VerificationCodeDelivery carries a one-time verification code.
//
// It is a distinct type from VerificationDelivery even though both encode a
// `verification_token` field, because the two secrets have different shapes and
// different handling rules: a bearer belongs in a URL fragment, a code belongs
// in the message body and must never reach a URL at all. Keeping the write
// sites apart means a caller cannot accidentally mail a code where a link was
// meant, or the reverse. The JSON field name is shared deliberately so the
// dispatcher continues to decode one payload struct.
type VerificationCodeDelivery struct {
	Destination      string    `json:"destination"`
	Locale           string    `json:"locale"`
	TemplateContract string    `json:"template_contract"`
	Code             string    `json:"verification_token"`
	ExpiresAt        time.Time `json:"expires_at"`
}

// NoticeDelivery is a message that carries no actionable secret — a statement
// that something already happened, such as a completed password reset.
//
// It is a distinct type from VerificationDelivery rather than that struct with
// an empty token, so the ciphertext cannot be read as carrying a usable token
// and a future consumer cannot mistake an absent one for a blank one. The
// destination is still PII, so this remains a protected payload.
type NoticeDelivery struct {
	Destination      string `json:"destination"`
	Locale           string `json:"locale"`
	TemplateContract string `json:"template_contract"`
}

type protectedPayload struct {
	KeyVersion string
	Nonce      []byte
	Ciphertext []byte
}

// StoredProtectedPayload is the encrypted half of an immutable outbox event.
// It is safe to move between repository and decryption code but never to log:
// ciphertext and nonce are intentionally byte slices rather than strings.
type StoredProtectedPayload struct {
	KeyVersion string
	Nonce      []byte
	Ciphertext []byte
}

// ProtectedPayloadReservation holds fresh nonce material reserved before a
// hidden Account lookup. Its fields stay opaque outside this package.
type ProtectedPayloadReservation struct {
	nonce []byte
	used  *atomic.Bool
}

type ReservedAppend struct {
	Event       Event
	Protected   any
	Reservation ProtectedPayloadReservation
}
