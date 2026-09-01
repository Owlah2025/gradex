package media

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// The protected-playback security model, and the exact line this file sits on.
//
// Free protection for a Student Lesson is layered: an entitlement decision, a
// short-lived signed playback session, a protected HLS manifest the API renders
// rather than stores, expiring presigned segment URLs, and — added here — a
// Student-specific visible watermark, with browser Picture-in-Picture disabled
// on the player so the raw video element cannot be presented without it.
//
// This is NOT DRM. There is no key exchange, no licence server, and no
// protected media path. An authorized Student can still capture what they are
// entitled to watch: a browser cannot prevent OS screenshots, OBS, desktop or
// GPU capture, or a phone pointed at the screen. The watermark is deterrence
// and leak attribution — it makes a captured recording carry the account that
// captured it — and nothing in this package should be described as more.
//
// Everything the Student sees is decided here, on the server, after identity
// and entitlement have already been established. The client never supplies,
// selects, or influences any watermark field.

// PlaybackWatermark is the identity a Student player renders over protected
// video. It is deliberately the smallest set of values that lets a leaked
// recording be traced back to one Account without publishing that Account.
//
// It carries no internal Student UUID, no authentication or session
// identifier, no playback-session token, no storage identity, and no full
// email address. DisplayName and MaskedIdentifier are what a human recognises;
// Code is what actually resolves the Account (see watermarkCode).
type PlaybackWatermark struct {
	// A shortened public form of the Account's display name, absent when the
	// Account has nothing renderable.
	DisplayName string `json:"display_name,omitempty"`
	// The Account's correspondence address with its local part masked.
	MaskedIdentifier string `json:"masked_identifier"`
	// The short attribution code. Not a credential and not a secret.
	Code string `json:"code"`
}

// The attribution code alphabet: Crockford base32, which drops I, L, O and U
// so a code read off a screen recording cannot be transcribed ambiguously.
const watermarkCodeAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

const watermarkCodeLength = 4

// The first name is bounded so a pathological display name cannot become a
// banner across the picture. Bounded in runes, because Arabic display names
// are the common case.
const maxWatermarkNameRunes = 18

// playbackWatermark builds the Student's watermark identity.
//
// It is called only after IssuePlayback's entitlement decision has allowed the
// request, so it reads an Account that has already proved it may watch this
// Lesson. It fails closed like every other read on this path: a Student whose
// identity cannot be read does not receive an unwatermarked authorization.
func (s *DeliveryService) playbackWatermark(ctx context.Context, studentID string) (*PlaybackWatermark, error) {
	var displayName, email string
	err := s.db.QueryRow(ctx,
		`SELECT display_name, email FROM accounts WHERE id = $1::uuid`, studentID,
	).Scan(&displayName, &email)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrProtectedUnavailable
	}
	if err != nil {
		return nil, ErrProtectedUnavailable
	}
	return &PlaybackWatermark{
		DisplayName:      watermarkDisplayName(displayName),
		MaskedIdentifier: maskWatermarkIdentifier(email),
		Code:             s.watermarkCode(studentID),
	}, nil
}

// watermarkCode derives the short attribution code for one Account.
//
// It is an HMAC over the Account identifier under its own domain separation
// tag, so it shares the delivery signing key without sharing any other
// derivation's meaning: this tag produces a value that is used for nothing
// else, and no other tag can be made to produce this one. That is the same
// pattern the Lab Material buyer tag and the playback session already use, so
// this introduces no new secret and no new deployment step.
//
// The code is not reversible by the client — the key never leaves the server —
// and it is not security-sensitive: it authorizes nothing, and knowing another
// Student's code grants no access. Attribution runs the other way. Given a code
// recovered from a leaked recording, the operator recomputes this derivation
// over candidate Accounts and matches; the masked identifier and display name
// on the same frame settle the remaining ambiguity from the truncation.
//
// It is stable per Account and independent of Lesson, session and time, which
// is what makes a single captured frame attributable.
func (s *DeliveryService) watermarkCode(studentID string) string {
	mac := hmac.New(sha256.New, s.buyerTagKey)
	_, _ = mac.Write([]byte("gradex:s4:playback-watermark-code:v1\x00"))
	_, _ = mac.Write([]byte(studentID))
	sum := mac.Sum(nil)
	value := binary.BigEndian.Uint32(sum[:4])
	code := make([]byte, watermarkCodeLength)
	for i := watermarkCodeLength - 1; i >= 0; i-- {
		code[i] = watermarkCodeAlphabet[value%uint32(len(watermarkCodeAlphabet))]
		value /= uint32(len(watermarkCodeAlphabet))
	}
	return string(code)
}

// watermarkDisplayName shortens a display name to a first name and a surname
// initial — "Ahmed Hazem Elmelegy" becomes "Ahmed E.".
//
// Enough for the Student to recognise themselves on their own screen and for a
// recipient of a leak to recognise a colleague, without publishing the full
// legal name of a paying Student over every frame of every Lesson they watch.
func watermarkDisplayName(displayName string) string {
	fields := strings.Fields(displayName)
	if len(fields) == 0 {
		return ""
	}
	first := boundedRunes(fields[0], maxWatermarkNameRunes)
	if len(fields) == 1 {
		return first
	}
	last := []rune(fields[len(fields)-1])
	if len(last) == 0 {
		return first
	}
	return first + " " + string(last[0]) + "."
}

// maskWatermarkIdentifier reduces a correspondence address to a recognisable
// but non-deliverable form — "ahmed@example.com" becomes "ah***@example.com".
//
// The domain is kept because it is the part that tells an investigator which
// population an Account belongs to; the local part is what would make the
// address usable, so only its opening runes survive. An address this function
// cannot parse is masked entirely rather than passed through.
func maskWatermarkIdentifier(email string) string {
	trimmed := strings.TrimSpace(email)
	at := strings.LastIndex(trimmed, "@")
	if at <= 0 || at == len(trimmed)-1 {
		return "***"
	}
	local, domain := trimmed[:at], trimmed[at+1:]
	visible := boundedRunes(local, 2)
	if visible == "" {
		return "***"
	}
	return visible + "***@" + domain
}

// boundedRunes truncates on a rune boundary, so a multi-byte name is never cut
// into invalid UTF-8.
func boundedRunes(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	return string([]rune(value)[:limit])
}
