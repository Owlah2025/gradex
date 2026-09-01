package identity

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Owlah2025/gradex/backend/internal/config"
)

// The Student email verification OTP.
//
// A six-digit code carries under 20 bits of entropy. That is deliberate — it
// has to be readable off a phone screen and typed by hand — and it is why the
// stored value is NOT a bare digest of the code. SHA-256(code) over a
// million-value space is an offline dictionary an attacker completes in
// microseconds from a database copy, so a leaked table would hand over every
// live verification challenge.
//
// What is stored instead is a keyed HMAC over a server-held pepper that never
// enters the database, domain-separated by purpose and bound to the challenge
// identity. Without the pepper the space is not searchable at all; with the
// challenge binding, one recovered code is worth nothing anywhere else.
//
// The compensating controls are the other half: a short TTL, one live
// challenge per Account, a bounded attempt budget, and a resend cooldown. Low
// entropy is survivable only when online guessing is metered, so those are
// enforced in the same transaction that reads the digest.
const (
	emailOTPDigits = 6

	// emailOTPDomain separates this HMAC input space from every other use of
	// the same pepper. A value derived here can never validate elsewhere.
	emailOTPDomain = "gradex-student-email-verification-otp-v1"

	// EmailOTPMaxAttempts is the per-challenge guessing budget. Exhausting it
	// makes the challenge terminal: the Student must request a new code, which
	// resets the budget and supersedes whatever the guesser was working on.
	EmailOTPMaxAttempts = 5

	// EmailOTPResendCooldown is the minimum interval between two codes for one
	// challenge chain. It bounds mailbox flooding and the rate at which an
	// attacker can trade a burnt attempt budget for a fresh one.
	EmailOTPResendCooldown = 60 * time.Second

	// emailOTPPepperMinimumBytes is the HMAC key floor. Shorter key material is
	// refused at construction rather than silently weakening every digest.
	emailOTPPepperMinimumBytes = 32
)

var (
	// ErrOTPInvalid is the single answer to a wrong, unknown, expired,
	// superseded, or already-consumed code. One error for all of them so the
	// response cannot be used to tell those states apart.
	ErrOTPInvalid = errors.New("verification code is invalid")

	// ErrOTPAttemptsExhausted is reported once the budget is spent, because the
	// recovery differs: a new code is required, and saying so is not a leak —
	// the caller already holds the challenge.
	ErrOTPAttemptsExhausted = errors.New("verification code attempts are exhausted")

	// ErrOTPResendTooSoon is the cooldown refusal.
	ErrOTPResendTooSoon = errors.New("verification code was already sent recently")

	// ErrOTPUnavailable means the pepper is absent or unusable. It fails closed:
	// no challenge is issued and no code is accepted.
	ErrOTPUnavailable = errors.New("email verification OTP is unavailable")
)

// EmailOTPPepper is the server-held HMAC key for verification codes.
//
// It is a distinct type rather than a []byte so it cannot be passed where an
// unrelated key is expected, and it holds a config.Secret so formatting or
// logging the value that carries it cannot print key material.
type EmailOTPPepper struct {
	key config.Secret
}

// NewEmailOTPPepper validates key material at construction.
//
// An absent or short pepper is a configuration fault, not a runtime condition
// to work around: returning an error here is what makes the production
// deployment fail closed instead of issuing codes nobody can safely store.
func NewEmailOTPPepper(secret config.Secret) (EmailOTPPepper, error) {
	if len(secret.Expose()) < emailOTPPepperMinimumBytes {
		return EmailOTPPepper{}, errors.New("email OTP pepper must contain at least 32 bytes")
	}
	return EmailOTPPepper{key: secret}, nil
}

func (p EmailOTPPepper) usable() bool { return len(p.key.Expose()) >= emailOTPPepperMinimumBytes }

// digest derives the stored value for one code under one challenge.
//
// Length-prefixing each field keeps the concatenation unambiguous: without it
// a challenge id ending in a digit and a code missing one would hash to the
// same input as the honest pair.
func (p EmailOTPPepper) digest(challengeID, code string) ([]byte, error) {
	if !p.usable() {
		return nil, ErrOTPUnavailable
	}
	if challengeID == "" || code == "" {
		return nil, ErrOTPInvalid
	}
	mac := hmac.New(sha256.New, []byte(p.key.Expose()))
	for _, field := range []string{emailOTPDomain, challengeID, code} {
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(field)))
		mac.Write(length[:])
		mac.Write([]byte(field))
	}
	return mac.Sum(nil), nil
}

// IssuedEmailOTP is one freshly minted challenge.
//
// Code is a config.Secret for the same reason the session credential is: the
// plaintext exists for exactly as long as it takes to hand it to the encrypted
// outbox payload, and no log line, error, or struct dump may reproduce it.
type IssuedEmailOTP struct {
	ChallengeID string
	Code        config.Secret
	Digest      []byte
	IssuedAt    time.Time
	ExpiresAt   time.Time
}

// ResendAvailableAt is the earliest moment a replacement may be requested.
func (o IssuedEmailOTP) ResendAvailableAt() time.Time {
	return o.IssuedAt.Add(EmailOTPResendCooldown)
}

type emailOTPOptions struct {
	Pepper EmailOTPPepper
	Now    time.Time
	TTL    time.Duration
	Random io.Reader
}

func newEmailOTP(options emailOTPOptions) (IssuedEmailOTP, error) {
	if !options.Pepper.usable() {
		return IssuedEmailOTP{}, ErrOTPUnavailable
	}
	if options.Now.IsZero() {
		return IssuedEmailOTP{}, errors.New("email OTP clock is required")
	}
	if options.TTL <= 0 {
		return IssuedEmailOTP{}, errors.New("email OTP TTL must be positive")
	}
	source := options.Random
	if source == nil {
		source = rand.Reader
	}
	code, err := randomNumericCode(source, emailOTPDigits)
	if err != nil {
		return IssuedEmailOTP{}, err
	}
	challengeID := uuid.NewString()
	digest, err := options.Pepper.digest(challengeID, code)
	if err != nil {
		return IssuedEmailOTP{}, err
	}
	return IssuedEmailOTP{
		ChallengeID: challengeID,
		Code:        config.NewSecret(code),
		Digest:      digest,
		IssuedAt:    options.Now,
		ExpiresAt:   options.Now.Add(options.TTL),
	}, nil
}

// randomNumericCode draws a uniform decimal string of the requested length.
//
// Rejection sampling rather than modular reduction: `n % 10` over uniform
// bytes is biased toward the low digits, and a biased code is a smaller search
// space than the one the attempt budget was sized for.
func randomNumericCode(source io.Reader, digits int) (string, error) {
	const bound = 250 // the largest multiple of 10 at or below 255, exclusive
	out := make([]byte, 0, digits)
	buffer := make([]byte, 1)
	for len(out) < digits {
		if _, err := io.ReadFull(source, buffer); err != nil {
			return "", errors.New("generating verification code")
		}
		if buffer[0] >= bound {
			continue
		}
		out = append(out, '0'+buffer[0]%10)
	}
	return string(out), nil
}

// NormalizeEmailOTPInput accepts what a human actually types.
//
// Spaces and separators survive a paste from a mail client and a phone
// keyboard; anything else is not a near-miss of a six-digit code and is
// refused before it can spend an attempt.
func NormalizeEmailOTPInput(raw string) (string, bool) {
	var builder strings.Builder
	for _, r := range raw {
		switch {
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == ' ' || r == '-' || r == '\t' || r == ' ' || r == '‏' || r == '‎':
			continue
		default:
			return "", false
		}
	}
	code := builder.String()
	if len(code) != emailOTPDigits {
		return "", false
	}
	return code, true
}

// MatchesEmailOTP compares a presented code against a stored digest in
// constant time. A derivation failure answers "no match" rather than
// distinguishing itself from a wrong code.
func (p EmailOTPPepper) MatchesEmailOTP(challengeID, code string, storedDigest []byte) bool {
	candidate, err := p.digest(challengeID, code)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(candidate, storedDigest) == 1
}

// MaskEmail renders an address for the verification screen without disclosing
// it. The result is never parsed back and never used to look an Account up; it
// exists so a Student can confirm they recognise the mailbox they are waiting
// on.
func MaskEmail(address string) string {
	// Lower-cased before masking. Two spellings of one address must produce one
	// mask, or the response echoes the caller's own casing back as a
	// distinguishing feature — and comparing the eligible and hidden paths for
	// equivalence is exactly how that becomes an account-existence signal.
	trimmed := strings.ToLower(strings.TrimSpace(address))
	at := strings.LastIndex(trimmed, "@")
	if at <= 0 || at == len(trimmed)-1 {
		return "***"
	}
	local, domain := trimmed[:at], trimmed[at+1:]
	visible := 2
	if len(local) < visible {
		visible = len(local)
	}
	return local[:visible] + "***@" + maskDomain(domain)
}

// maskDomain keeps the public suffix and the first character of the label, so
// a Student recognises their own provider without the full address appearing
// on a shared screen.
func maskDomain(domain string) string {
	dot := strings.Index(domain, ".")
	if dot <= 0 {
		return "***"
	}
	return domain[:1] + "***" + domain[dot:]
}
