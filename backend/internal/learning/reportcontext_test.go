package learning

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// Report-context signer evidence (D-065).
//
// The context is the only thing standing between "the instance the Student saw" and "whatever is
// live now", so every way of forging, altering, replaying, or misdirecting one is refused here.

const (
	testReporter     = "11111111-1111-1111-1111-111111111111"
	testOtherStudent = "99999999-9999-9999-9999-999999999999"
	testSession      = "5e551011-0000-0000-0000-00000000551d"
	testCourse       = "22222222-2222-2222-2222-222222222222"
	testLesson       = "33333333-3333-3333-3333-333333333333"
	testRevisionA    = "55555555-5555-5555-5555-555555555555"
	testVersionA     = "66666666-6666-6666-6666-666666666666"
)

func testSigner(t *testing.T, at time.Time) *ReportContextSigner {
	t.Helper()
	// A deterministic key and nonce source: production never sees either.
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	counter := byte(0)
	signer, err := NewReportContextSigner(key, DefaultReportContextLifetime, func() time.Time { return at },
		func(b []byte) error {
			counter++
			for i := range b {
				b[i] = byte(i) + counter
			}
			return nil
		})
	if err != nil {
		t.Fatalf("constructing signer: %v", err)
	}
	return signer
}

func lessonContextRequest() ReportContextRequest {
	return ReportContextRequest{
		ReporterAccountID:       testReporter,
		SessionID:               testSession,
		CourseID:                testCourse,
		TargetKind:              ReportTargetLesson,
		StableTargetID:          testLesson,
		VisibleCourseRevisionID: testRevisionA,
	}
}

func TestReportContextRoundTripsTheRenderedInstance(t *testing.T) {
	at := time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)
	signer := testSigner(t, at)

	token, err := signer.Mint(lessonContextRequest())
	if err != nil {
		t.Fatalf("minting: %v", err)
	}
	binding, err := signer.Verify(token, testReporter, testSession)
	if err != nil {
		t.Fatalf("verifying: %v", err)
	}
	if binding.StableTargetID != testLesson || binding.VisibleCourseRevisionID != testRevisionA {
		t.Fatalf("binding lost the rendered instance: %+v", binding)
	}
	if binding.CourseID != testCourse || binding.TargetKind != ReportTargetLesson {
		t.Fatalf("binding lost target identity: %+v", binding)
	}
	if binding.VisibleAssetVersionID != "" {
		t.Fatalf("a Lesson context must carry no Asset Version, got %q", binding.VisibleAssetVersionID)
	}
	if !strings.HasPrefix(token, reportContextVersion+".") || len(strings.Split(token, ".")) != 2 {
		t.Fatalf("unexpected token shape")
	}

	// Opacity, which is the whole point: the token string, and the bytes inside its envelope,
	// reveal none of the internal binding. A signed-but-readable payload would hand the client the
	// exact identifiers D-063 keeps out of read models.
	envelope, err := base64.RawURLEncoding.DecodeString(strings.Split(token, ".")[1])
	if err != nil {
		t.Fatalf("decoding envelope: %v", err)
	}
	for _, secret := range []string{testReporter, testSession, testCourse, testLesson, testRevisionA} {
		if strings.Contains(token, secret) {
			t.Fatal("the token string exposes an internal identifier")
		}
		if strings.Contains(string(envelope), secret) {
			t.Fatal("the decoded envelope exposes an internal identifier")
		}
	}
	// No JSON structure survives either. Single punctuation bytes are not checked: ciphertext is
	// random, so any given byte value occurs by chance — only multi-byte structure is meaningful.
	for _, marker := range []string{"\"acc\"", "\"rev\"", "\"tgt\"", "\"crs\"", reportContextPurpose, reportContextVersion + "\","} {
		if strings.Contains(string(envelope), marker) {
			t.Fatal("the decoded envelope exposes payload structure")
		}
	}
	var probe map[string]any
	if json.Unmarshal(envelope, &probe) == nil {
		t.Fatal("the envelope decoded straight into JSON; it is not encrypted")
	}
}

// Encryption must be non-deterministic: identical input yields different ciphertext, and both
// still verify to the same binding.
func TestReportContextUsesAFreshNoncePerMint(t *testing.T) {
	at := time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)
	signer := testSigner(t, at)

	first, err := signer.Mint(lessonContextRequest())
	if err != nil {
		t.Fatalf("minting: %v", err)
	}
	second, err := signer.Mint(lessonContextRequest())
	if err != nil {
		t.Fatalf("minting: %v", err)
	}
	if first == second {
		t.Fatal("identical input produced an identical token; the nonce is not fresh")
	}

	firstBinding, err := signer.Verify(first, testReporter, testSession)
	if err != nil {
		t.Fatalf("verifying first: %v", err)
	}
	secondBinding, err := signer.Verify(second, testReporter, testSession)
	if err != nil {
		t.Fatalf("verifying second: %v", err)
	}
	if firstBinding != secondBinding {
		t.Fatal("two mints of the same input verified to different bindings")
	}
}

func TestReportContextRefusesTamperingAndForgery(t *testing.T) {
	at := time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)
	signer := testSigner(t, at)
	token, err := signer.Mint(lessonContextRequest())
	if err != nil {
		t.Fatalf("minting: %v", err)
	}
	envelope, _ := base64.RawURLEncoding.DecodeString(strings.Split(token, ".")[1])

	flip := func(index int) string {
		altered := append([]byte(nil), envelope...)
		altered[index] ^= 0x01
		return reportContextVersion + "." + base64.RawURLEncoding.EncodeToString(altered)
	}

	// A signer holding a different derived key.
	otherKey := DeriveReportContextKey([]byte("an-entirely-different-application-secret"))
	otherSigner, err := NewReportContextSigner(otherKey, DefaultReportContextLifetime,
		func() time.Time { return at }, func(b []byte) error { return nil })
	if err != nil {
		t.Fatalf("constructing other signer: %v", err)
	}

	cases := []struct {
		name  string
		token string
	}{
		{"modified nonce", flip(0)},
		{"modified ciphertext", flip(len(envelope) / 2)},
		{"modified authentication tag", flip(len(envelope) - 1)},
		{"unknown version", "grc9." + strings.Split(token, ".")[1]},
		{"missing envelope", reportContextVersion},
		{"extra segments", token + ".extra"},
		{"empty token", ""},
		{"undecodable envelope", reportContextVersion + ".!!!"},
		{"truncated envelope", reportContextVersion + "." + base64.RawURLEncoding.EncodeToString(envelope[:4])},
		{"oversized token", reportContextVersion + "." + strings.Repeat("A", reportContextMaxTokenBytes+1)},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := signer.Verify(testCase.token, testReporter, testSession); !errors.Is(err, ErrReportContextInvalid) {
				t.Fatalf("expected ErrReportContextInvalid, got %v", err)
			}
		})
	}

	// A foreign key cannot decrypt, and reveals nothing about why.
	if _, err := otherSigner.Verify(token, testReporter, testSession); !errors.Is(err, ErrReportContextInvalid) {
		t.Fatalf("a foreign key must not decrypt, got %v", err)
	}
}

func TestReportContextRefusesWrongPurposeAndUnknownKind(t *testing.T) {
	at := time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)
	signer := testSigner(t, at)

	// Correctly signed by this signer, but minted for another audience or naming a kind outside the
	// closed set. Both are refused despite a valid signature.
	for _, payload := range []reportContextPayload{
		{
			Version: reportContextVersion, Purpose: "media-playback", Reporter: testReporter, Session: testSession,
			Course: testCourse, Kind: string(ReportTargetLesson), Target: testLesson, Revision: testRevisionA,
			IssuedAt: at.Unix(), ExpiresAt: at.Add(time.Hour).Unix(), Nonce: "n",
		},
		{
			Version: reportContextVersion, Purpose: reportContextPurpose, Reporter: testReporter, Session: testSession,
			Course: testCourse, Kind: "SECTION", Target: testLesson, Revision: testRevisionA,
			IssuedAt: at.Unix(), ExpiresAt: at.Add(time.Hour).Unix(), Nonce: "n",
		},
		{
			Version: reportContextVersion, Purpose: reportContextPurpose, Reporter: testReporter, Session: testSession,
			Course: testCourse, Kind: string(ReportTargetVideo), Target: testLesson, Revision: testRevisionA,
			IssuedAt: at.Unix(), ExpiresAt: at.Add(time.Hour).Unix(), Nonce: "n",
		},
	} {
		encoded, _ := json.Marshal(payload)
		nonce := make([]byte, signer.aead.NonceSize())
		sealed := signer.aead.Seal(nil, nonce, encoded, reportContextAAD())
		token := reportContextVersion + "." + base64.RawURLEncoding.EncodeToString(append(nonce, sealed...))
		if _, err := signer.Verify(token, testReporter, testSession); !errors.Is(err, ErrReportContextInvalid) {
			t.Fatalf("purpose/kind %q/%q must be refused, got %v", payload.Purpose, payload.Kind, err)
		}
	}
}

func TestReportContextExpiresAndIsNeverRenewedAgainstNewerContent(t *testing.T) {
	minted := time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)
	token, err := testSigner(t, minted).Mint(lessonContextRequest())
	if err != nil {
		t.Fatalf("minting: %v", err)
	}

	// Still inside the window.
	if _, err := testSigner(t, minted.Add(DefaultReportContextLifetime-time.Second)).Verify(token, testReporter, testSession); err != nil {
		t.Fatalf("a live context must verify: %v", err)
	}
	// At and past expiry it is refused — the page must reload, which legitimately re-renders (and
	// re-reports) the newer content rather than silently rebinding this report to it.
	for _, at := range []time.Time{minted.Add(DefaultReportContextLifetime), minted.Add(2 * time.Hour)} {
		if _, err := testSigner(t, at).Verify(token, testReporter, testSession); !errors.Is(err, ErrReportContextExpired) {
			t.Fatalf("expected ErrReportContextExpired at %s, got %v", at, err)
		}
	}
	// A context minted implausibly far ahead of the verifier's clock is refused too.
	future, err := testSigner(t, minted.Add(time.Hour)).Mint(lessonContextRequest())
	if err != nil {
		t.Fatalf("minting future context: %v", err)
	}
	if _, err := testSigner(t, minted).Verify(future, testReporter, testSession); !errors.Is(err, ErrReportContextInvalid) {
		t.Fatalf("a far-future context must be refused, got %v", err)
	}
}

func TestReportContextIsBoundToItsStudentAndSession(t *testing.T) {
	at := time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)
	signer := testSigner(t, at)
	token, err := signer.Mint(lessonContextRequest())
	if err != nil {
		t.Fatalf("minting: %v", err)
	}

	cases := []struct {
		name     string
		reporter string
		session  string
	}{
		{"another Student's session", testOtherStudent, testSession},
		{"copied into another session", testReporter, "another-session"},
		{"anonymous client", "", ""},
		{"right Student, no session", testReporter, ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := signer.Verify(token, testCase.reporter, testCase.session); !errors.Is(err, ErrReportContextInvalid) {
				t.Fatalf("expected refusal, got %v", err)
			}
		})
	}
}

func TestReportContextSignerFailsClosedAndSeparatesDomains(t *testing.T) {
	valid := make([]byte, 32)
	clock := func() time.Time { return time.Unix(0, 0) }
	random := func(b []byte) error { return nil }

	// No signer without a usable key, lifetime, clock, or nonce source — production composition
	// cannot silently proceed without one.
	if _, err := NewReportContextSigner(make([]byte, 31), DefaultReportContextLifetime, clock, random); err == nil {
		t.Fatal("a short key must be refused")
	}
	if _, err := NewReportContextSigner(make([]byte, 64), DefaultReportContextLifetime, clock, random); err == nil {
		t.Fatal("a key that is not exactly 32 bytes must be refused")
	}
	if _, err := NewReportContextSigner(valid, 0, clock, random); err == nil {
		t.Fatal("a non-positive lifetime must be refused")
	}
	if _, err := NewReportContextSigner(valid, DefaultReportContextLifetime, nil, random); err == nil {
		t.Fatal("a missing clock must be refused")
	}
	if _, err := NewReportContextSigner(valid, DefaultReportContextLifetime, clock, nil); err == nil {
		t.Fatal("a missing nonce source must be refused")
	}

	// The derived key is domain-separated from the root secret, so a report context cannot be
	// signed by — or confused with — any other artefact built from that secret.
	root := []byte("an-application-secret-of-sufficient-length")
	derived := DeriveReportContextKey(root)
	if len(derived) != 32 {
		t.Fatalf("derived key length %d", len(derived))
	}
	if hmac.Equal(derived, root[:min(len(root), 32)]) {
		t.Fatal("the derived key must not be the root secret")
	}
	_ = sha256.Size
	if !hmac.Equal(derived, DeriveReportContextKey(root)) {
		t.Fatal("derivation must be deterministic")
	}
	if hmac.Equal(derived, DeriveReportContextKey([]byte("a-different-secret-of-sufficient-length"))) {
		t.Fatal("different roots must derive different keys")
	}

	// A token minted under one derived key does not verify under another.
	first, _ := NewReportContextSigner(derived, DefaultReportContextLifetime, func() time.Time { return time.Unix(1_800_000_000, 0) }, func(b []byte) error { return nil })
	second, _ := NewReportContextSigner(DeriveReportContextKey([]byte("another-secret-of-sufficient-length!!")), DefaultReportContextLifetime, func() time.Time { return time.Unix(1_800_000_000, 0) }, func(b []byte) error { return nil })
	_ = first
	token, err := first.Mint(lessonContextRequest())
	if err != nil {
		t.Fatalf("minting: %v", err)
	}
	if _, err := second.Verify(token, testReporter, testSession); !errors.Is(err, ErrReportContextInvalid) {
		t.Fatalf("a foreign key must not verify, got %v", err)
	}
}

func TestReportContextMintRefusesIncoherentShapes(t *testing.T) {
	signer := testSigner(t, time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC))

	video := lessonContextRequest()
	video.TargetKind = ReportTargetVideo
	if _, err := signer.Mint(video); err == nil {
		t.Fatal("a VIDEO context without an Asset Version must be refused")
	}

	lessonWithVersion := lessonContextRequest()
	lessonWithVersion.VisibleAssetVersionID = testVersionA
	if _, err := signer.Mint(lessonWithVersion); err == nil {
		t.Fatal("a LESSON context carrying an Asset Version must be refused")
	}

	missingRevision := lessonContextRequest()
	missingRevision.VisibleCourseRevisionID = ""
	if _, err := signer.Mint(missingRevision); err == nil {
		t.Fatal("a context without the rendered revision must be refused")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
