// Package outbox owns Gradex's immutable asynchronous source-intent boundary.
// It does not claim provider acceptance or delivery and does not implement the
// future dispatcher lifecycle.
package outbox

import "time"

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

// VerificationDelivery is the minimum plaintext that the future email
// consumer needs. It is marshalled only into authenticated ciphertext.
type VerificationDelivery struct {
	Destination       string    `json:"destination"`
	Locale            string    `json:"locale"`
	TemplateContract  string    `json:"template_contract"`
	VerificationToken string    `json:"verification_token"`
	ExpiresAt         time.Time `json:"expires_at"`
}

type protectedPayload struct {
	KeyVersion string
	Nonce      []byte
	Ciphertext []byte
}
