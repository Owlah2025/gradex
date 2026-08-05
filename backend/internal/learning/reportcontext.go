package learning

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// The report context: evidence of what the Student was actually looking at (D-065).
//
// A report has to name the content instance the Student saw, and the server cannot recover that
// after the fact. Resolving "whatever is live now" at submission records the wrong instance the
// moment a new revision or a replacement Asset Version goes live while a page sits open — the
// Student reports A and the record says B.
//
// So the instance is captured at the render that produced the page, and carried back with the
// submission as an opaque context. The context is *evidence*, never capability: it authorises
// nothing, and possessing one grants no playback, Progress, material, Enrollment, Entitlement, or
// moderation ability whatsoever.
//
// The token is **authenticated-encrypted** (AES-256-GCM), not merely signed. D-063 keeps raw
// revision and Asset Version identifiers out of public read models, and a signed-but-readable
// payload would put them straight back: base64 is an encoding, not a secret, so anyone holding
// the token could decode the internal graph. Encryption keeps the binding confidential as well as
// tamper-evident, so a client can neither read, choose, edit, nor forge what it sends back.
//
// Decryption and verification are the HTTP layer's job (T063). Relational integrity of the
// decrypted binding is still this package's job, because authenticity only proves the server
// minted those values, not that they form a coherent target.

const (
	// reportContextVersion is checked before anything else is parsed. An unknown version is
	// refused rather than interpreted, so the format can change without a downgrade path.
	reportContextVersion = "grc1"

	// reportContextPurpose is the audience separation, bound as authenticated additional data. A
	// value minted for anything other than content reporting is refused even if it decrypts.
	reportContextPurpose = "content-report"

	// DefaultReportContextLifetime bounds how long a rendered page may still report what it shows.
	// It is deliberately finite: an expired context is refused and the Student reloads, which
	// legitimately re-renders — and re-reports — the newer content.
	DefaultReportContextLifetime = 30 * time.Minute

	// reportContextMaxClockSkew tolerates ordinary clock drift while still refusing a context
	// minted implausibly far in the future.
	reportContextMaxClockSkew = 2 * time.Minute

	// reportContextKeyLabel is the domain-separation label. Deriving the encryption key with an
	// explicit label means a report context can never be confused with, or produced by, any other
	// artefact in the system even though the root secret is shared.
	reportContextKeyLabel = "gradex/learning/report-context/encryption/v1"

	// reportContextMaxTokenBytes bounds what will be decoded at all, so a hostile oversized token
	// is refused before any allocation-heavy work.
	reportContextMaxTokenBytes = 4096
)

var (
	ErrReportContextInvalid = errors.New("learning report context is invalid")
	ErrReportContextExpired = errors.New("learning report context has expired")
)

// reportContextPayload is the encrypted content. Every field is confidential and authenticated; it
// is never public API, and a client cannot recover any of it from the token string.
type reportContextPayload struct {
	Version string `json:"v"`
	Purpose string `json:"p"`
	// Reporter and Session together stop a context being replayed by anyone but the Student it was
	// rendered for, in the session it was rendered in.
	Reporter string `json:"acc"`
	Session  string `json:"sid"`
	Course   string `json:"crs"`
	Kind     string `json:"knd"`
	// Target is the stable logical identity; Revision and AssetVersion are the exact visible
	// instance at render time.
	Target       string `json:"tgt"`
	Revision     string `json:"rev"`
	AssetVersion string `json:"ver,omitempty"`
	IssuedAt     int64  `json:"iat"`
	ExpiresAt    int64  `json:"exp"`
	Nonce        string `json:"nnc"`
}

// VerifiedReportBinding is trusted internal data: what the server itself minted and has just
// re-verified. It is the only way to state which instance a report is about.
type VerifiedReportBinding struct {
	ReporterAccountID       string
	SessionID               string
	CourseID                string
	TargetKind              ReportTargetKind
	StableTargetID          string
	VisibleCourseRevisionID string
	// VisibleAssetVersionID is empty for COURSE and LESSON, and required for VIDEO, RESOURCE, and
	// LAB_MATERIAL.
	VisibleAssetVersionID string
}

// ReportContextRequest is what a read model states about the instance it just rendered.
type ReportContextRequest struct {
	ReporterAccountID       string
	SessionID               string
	CourseID                string
	TargetKind              ReportTargetKind
	StableTargetID          string
	VisibleCourseRevisionID string
	VisibleAssetVersionID   string
}

// ReportContextSigner mints and verifies report contexts. It holds no database handle and makes no
// authorisation decision.
type ReportContextSigner struct {
	aead     cipher.AEAD
	lifetime time.Duration
	now      func() time.Time
	random   func([]byte) error
}

// DeriveReportContextKey produces the 32-byte report-context encryption key from an application
// secret using explicit domain separation, so this key cannot encrypt — or decrypt — anything else.
// The root secret is never used as the AES key directly.
func DeriveReportContextKey(rootSecret []byte) []byte {
	mac := hmac.New(sha256.New, rootSecret)
	mac.Write([]byte(reportContextKeyLabel))
	return mac.Sum(nil)
}

// reportContextAAD binds the version and purpose into the authentication tag, so neither can be
// swapped and no algorithm is ever selected by the token itself.
func reportContextAAD() []byte {
	return []byte(reportContextVersion + "\x00" + reportContextPurpose)
}

// NewReportContextSigner fails closed: without a key of sufficient length there is no signer, and
// a caller that requires one cannot silently proceed without it.
func NewReportContextSigner(key []byte, lifetime time.Duration, now func() time.Time, random func([]byte) error) (*ReportContextSigner, error) {
	if len(key) != 32 {
		return nil, errors.New("report context encryption key must be exactly 32 bytes (AES-256)")
	}
	if lifetime <= 0 {
		return nil, errors.New("report context lifetime must be positive")
	}
	if now == nil {
		return nil, errors.New("report context signer requires a clock")
	}
	if random == nil {
		return nil, errors.New("report context signer requires a nonce source")
	}
	block, err := aes.NewCipher(append([]byte(nil), key...))
	if err != nil {
		return nil, fmt.Errorf("constructing report context cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("constructing report context AEAD: %w", err)
	}
	return &ReportContextSigner{aead: aead, lifetime: lifetime, now: now, random: random}, nil
}

// Mint captures one rendered instance. It is called from the read that produced the page, with the
// same resolved values that read returned — never from a later lookup of current state.
func (s *ReportContextSigner) Mint(request ReportContextRequest) (string, error) {
	if s == nil {
		return "", errors.New("report context signer is unavailable")
	}
	if err := validateContextShape(request.TargetKind, request.VisibleAssetVersionID); err != nil {
		return "", err
	}
	for name, value := range map[string]string{
		"reporter": request.ReporterAccountID,
		"course":   request.CourseID,
		"target":   request.StableTargetID,
		"revision": request.VisibleCourseRevisionID,
	} {
		if strings.TrimSpace(value) == "" {
			return "", fmt.Errorf("report context requires %s", name)
		}
	}

	nonce := make([]byte, 16)
	if err := s.random(nonce); err != nil {
		return "", fmt.Errorf("generating report context nonce: %w", err)
	}
	issued := s.now().UTC()

	payload := reportContextPayload{
		Version:      reportContextVersion,
		Purpose:      reportContextPurpose,
		Reporter:     request.ReporterAccountID,
		Session:      request.SessionID,
		Course:       request.CourseID,
		Kind:         string(request.TargetKind),
		Target:       request.StableTargetID,
		Revision:     request.VisibleCourseRevisionID,
		AssetVersion: request.VisibleAssetVersionID,
		IssuedAt:     issued.Unix(),
		ExpiresAt:    issued.Add(s.lifetime).Unix(),
		Nonce:        base64.RawURLEncoding.EncodeToString(nonce),
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encoding report context: %w", err)
	}

	// A fresh nonce per mint: two contexts over identical input are different ciphertexts.
	sealNonce := make([]byte, s.aead.NonceSize())
	if err := s.random(sealNonce); err != nil {
		return "", fmt.Errorf("generating report context nonce: %w", err)
	}
	sealed := s.aead.Seal(nil, sealNonce, encoded, reportContextAAD())

	envelope := make([]byte, 0, len(sealNonce)+len(sealed))
	envelope = append(envelope, sealNonce...)
	envelope = append(envelope, sealed...)
	return reportContextVersion + "." + base64.RawURLEncoding.EncodeToString(envelope), nil
}

// Verify authenticates a context and returns the binding it attests to. It proves the server
// minted these values for this Student and session, and that they have not been altered — it does
// not prove they still describe live content, which is the entire point.
func (s *ReportContextSigner) Verify(token, expectedReporter, expectedSession string) (VerifiedReportBinding, error) {
	if s == nil {
		return VerifiedReportBinding{}, errors.New("report context signer is unavailable")
	}

	// Bounded before anything is decoded.
	if len(token) > reportContextMaxTokenBytes {
		return VerifiedReportBinding{}, fmt.Errorf("%w: oversized context", ErrReportContextInvalid)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return VerifiedReportBinding{}, fmt.Errorf("%w: malformed context", ErrReportContextInvalid)
	}
	// The version prefix is checked before decryption, and never negotiated from the token.
	if parts[0] != reportContextVersion {
		return VerifiedReportBinding{}, fmt.Errorf("%w: unsupported context version", ErrReportContextInvalid)
	}

	envelope, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return VerifiedReportBinding{}, fmt.Errorf("%w: undecodable context", ErrReportContextInvalid)
	}
	if len(envelope) < s.aead.NonceSize()+s.aead.Overhead() {
		return VerifiedReportBinding{}, fmt.Errorf("%w: truncated context", ErrReportContextInvalid)
	}

	// Authentication failure is generic and yields no plaintext: a tampered nonce, ciphertext, or
	// tag, and a wrong or foreign key, are all indistinguishable to the caller.
	raw, err := s.aead.Open(nil, envelope[:s.aead.NonceSize()], envelope[s.aead.NonceSize():], reportContextAAD())
	if err != nil {
		return VerifiedReportBinding{}, fmt.Errorf("%w: authentication failed", ErrReportContextInvalid)
	}

	var payload reportContextPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return VerifiedReportBinding{}, fmt.Errorf("%w: unreadable context", ErrReportContextInvalid)
	}

	if payload.Version != reportContextVersion {
		return VerifiedReportBinding{}, fmt.Errorf("%w: unsupported context version", ErrReportContextInvalid)
	}
	if payload.Purpose != reportContextPurpose {
		return VerifiedReportBinding{}, fmt.Errorf("%w: wrong purpose", ErrReportContextInvalid)
	}

	now := s.now().UTC()
	if payload.ExpiresAt <= 0 || now.Unix() >= payload.ExpiresAt {
		// Refused, never renewed: a renewal here would silently rebind the report to whatever is
		// current, which is the defect this design exists to prevent.
		return VerifiedReportBinding{}, ErrReportContextExpired
	}
	if payload.IssuedAt > now.Add(reportContextMaxClockSkew).Unix() {
		return VerifiedReportBinding{}, fmt.Errorf("%w: issued in the future", ErrReportContextInvalid)
	}

	// The context belongs to one Student in one session. A copied token is refused.
	if expectedReporter == "" || payload.Reporter != expectedReporter {
		return VerifiedReportBinding{}, fmt.Errorf("%w: reporter mismatch", ErrReportContextInvalid)
	}
	if payload.Session != expectedSession {
		return VerifiedReportBinding{}, fmt.Errorf("%w: session mismatch", ErrReportContextInvalid)
	}

	kind := ReportTargetKind(payload.Kind)
	if !kind.valid() {
		return VerifiedReportBinding{}, fmt.Errorf("%w: unknown target kind", ErrReportContextInvalid)
	}
	if err := validateContextShape(kind, payload.AssetVersion); err != nil {
		return VerifiedReportBinding{}, fmt.Errorf("%w: %s", ErrReportContextInvalid, err)
	}
	if payload.Course == "" || payload.Target == "" || payload.Revision == "" {
		return VerifiedReportBinding{}, fmt.Errorf("%w: incomplete context", ErrReportContextInvalid)
	}

	return VerifiedReportBinding{
		ReporterAccountID:       payload.Reporter,
		SessionID:               payload.Session,
		CourseID:                payload.Course,
		TargetKind:              kind,
		StableTargetID:          payload.Target,
		VisibleCourseRevisionID: payload.Revision,
		VisibleAssetVersionID:   payload.AssetVersion,
	}, nil
}

// validateContextShape enforces which kinds carry an Asset Version at all: a Course or Lesson
// report has none, and a media report cannot be missing one.
func validateContextShape(kind ReportTargetKind, assetVersion string) error {
	switch kind {
	case ReportTargetCourse, ReportTargetLesson:
		if assetVersion != "" {
			return fmt.Errorf("%s report carries no Asset Version", kind)
		}
	case ReportTargetVideo, ReportTargetResource, ReportTargetLabMaterial:
		if strings.TrimSpace(assetVersion) == "" {
			return fmt.Errorf("%s report requires an Asset Version", kind)
		}
	default:
		return fmt.Errorf("unknown target kind %q", kind)
	}
	return nil
}
