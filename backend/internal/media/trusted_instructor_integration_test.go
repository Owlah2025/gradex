//go:build integration

package media

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// The D-088 trusted-Instructor launch profile, end to end.
//
// Every test here asserts two things at once: that the approved MP4/PDF/DOCX
// Lesson media actually reaches the Instructor's intended outcome, and that
// nothing on the way records or implies malware scanning. Anything outside the
// profile must stay refused or scanner-gated.

// trustedFixture wraps the shared media fixture with a TRUSTED_INSTRUCTOR
// Service over the same database and object store, so a single run can compare
// the two operating modes against identical data.
type trustedInstructorFixture struct {
	*mediaFixture
	trusted  *Service
	lessonID string
}

func newTrustedInstructorFixture(t *testing.T) *trustedInstructorFixture {
	t.Helper()
	base := newMediaFixture(t)
	return &trustedInstructorFixture{
		mediaFixture: base,
		trusted:      trustedServiceOver(t, base, ServiceOptions{}),
		lessonID:     uuid.NewString(),
	}
}

// trustedServiceOver builds a TRUSTED_INSTRUCTOR Service sharing the fixture's
// pool, store and outbox. The scanner boundary is deliberately one that always
// errors: a trusted deployment has no scanner, so any test that accidentally
// reached a scan would fail rather than quietly pass.
func trustedServiceOver(t *testing.T, base *mediaFixture, overrides ServiceOptions) *Service {
	t.Helper()
	unavailable, err := NewUnavailableScanner("no scanner is configured in the D-088 launch profile")
	if err != nil {
		t.Fatalf("constructing the unavailable scanner: %v", err)
	}
	adapter, err := NewScannerAdapter(unavailable)
	if err != nil {
		t.Fatalf("constructing the scanner adapter: %v", err)
	}
	options := ServiceOptions{
		DB: base.pool, Store: base.store, Outbox: base.writer, Scanner: adapter,
		UploadURLExpiry: 15 * time.Minute, MaxUploadBytes: 10 * 1024 * 1024,
		OperatingMode:             OperatingModeTrustedInstructor,
		ResourceMaxBytes:          overrides.ResourceMaxBytes,
		ResourceLessonMaxBytes:    overrides.ResourceLessonMaxBytes,
		LabMaterialMaxBytes:       overrides.LabMaterialMaxBytes,
		LabMaterialLessonMaxBytes: overrides.LabMaterialLessonMaxBytes,
	}
	service, err := NewService(options)
	if err != nil {
		t.Fatalf("constructing the trusted media service: %v", err)
	}
	return service
}

func trustedMP4() []byte {
	header := append([]byte{0, 0, 0, 24}, []byte("ftypisom")...)
	return append(header, make([]byte, 32)...)
}

func trustedPDF() []byte {
	return []byte("%PDF-1.7\n1 0 obj\n<<>>\nendobj\ntrailer\n%%EOF\n")
}

// trustedDOCX builds a minimal, macro-free OOXML WordprocessingML package.
func trustedDOCX(t *testing.T) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	parts := []struct{ name, body string }{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Override PartName="/word/document.xml" ContentType="` + ContentTypeDOCX + `.main+xml"/>` +
			`</Types>`},
		{"_rels/.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"/>`},
		{"word/document.xml", `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body/></w:document>`},
	}
	for _, part := range parts {
		w, err := writer.Create(part.name)
		if err != nil {
			t.Fatalf("creating DOCX part %q: %v", part.name, err)
		}
		if _, err := w.Write([]byte(part.body)); err != nil {
			t.Fatalf("writing DOCX part %q: %v", part.name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("closing the DOCX package: %v", err)
	}
	return buffer.Bytes()
}

// stage issues an upload intent through the given Service, puts the bytes in
// the fake store under the returned quarantine key, and returns the completion
// evidence a browser would send.
func (f *trustedInstructorFixture) stage(
	t *testing.T, service *Service, kind AssetKind, contentType string, body []byte, objectVersion string,
) (CompleteUploadRequest, error) {
	t.Helper()
	ticket, err := service.BeginUpload(f.ctx, UploadRequest{
		OwnerAccountID: f.instructorID, CourseID: f.courseID, LessonID: f.lessonID,
		Kind: kind, ContentType: contentType, SizeBytes: int64(len(body)),
	})
	if err != nil {
		return CompleteUploadRequest{}, err
	}
	key := fmt.Sprintf("quarantine/%s/%s/source", f.courseID, ticket.AssetVersionID)
	f.store.put(key, objectVersion, body)
	sum := sha256.Sum256(body)
	return CompleteUploadRequest{
		OwnerAccountID: f.instructorID, AssetVersionID: ticket.AssetVersionID,
		ProviderEventID: "upload-" + ticket.AssetVersionID, StorageObjectKey: key,
		StorageObjectVersion: objectVersion, ContentType: contentType,
		SizeBytes: int64(len(body)), SHA256Hex: hex.EncodeToString(sum[:]),
	}, nil
}

func (f *trustedInstructorFixture) mustStage(
	t *testing.T, kind AssetKind, contentType string, body []byte, objectVersion string,
) CompleteUploadRequest {
	t.Helper()
	request, err := f.stage(t, f.trusted, kind, contentType, body, objectVersion)
	if err != nil {
		t.Fatalf("staging a trusted %s upload: %v", kind, err)
	}
	return request
}

// draftRevision seeds one editable Course revision, which is what a public
// preview upload must bind to.
func (f *trustedInstructorFixture) draftRevision(t *testing.T) string {
	t.Helper()
	revisionID := uuid.NewString()
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO course_revisions (id, course_id, state, revision_number, title_ar, title_en)
		VALUES ($1::uuid, $2::uuid, 'DRAFT', 1, 'معاينة', 'Preview')
	`, revisionID, f.courseID); err != nil {
		t.Fatalf("seeding a draft Course revision: %v", err)
	}
	return revisionID
}

// stagePreview is stage() for a public preview: Course-level and revision-bound
// rather than Lesson-bound.
func (f *trustedInstructorFixture) stagePreview(
	t *testing.T, service *Service, revisionID, contentType string, body []byte, objectVersion string,
) (CompleteUploadRequest, error) {
	t.Helper()
	ticket, err := service.BeginUpload(f.ctx, UploadRequest{
		OwnerAccountID: f.instructorID, CourseID: f.courseID, RevisionID: revisionID,
		Kind: KindPreview, ContentType: contentType, SizeBytes: int64(len(body)),
	})
	if err != nil {
		return CompleteUploadRequest{}, err
	}
	key := fmt.Sprintf("quarantine/%s/%s/source", f.courseID, ticket.AssetVersionID)
	f.store.put(key, objectVersion, body)
	sum := sha256.Sum256(body)
	return CompleteUploadRequest{
		OwnerAccountID: f.instructorID, AssetVersionID: ticket.AssetVersionID,
		ProviderEventID: "upload-" + ticket.AssetVersionID, StorageObjectKey: key,
		StorageObjectVersion: objectVersion, ContentType: contentType,
		SizeBytes: int64(len(body)), SHA256Hex: hex.EncodeToString(sum[:]),
	}, nil
}

func (f *trustedInstructorFixture) mustStagePreview(
	t *testing.T, revisionID, objectVersion string,
) CompleteUploadRequest {
	t.Helper()
	request, err := f.stagePreview(t, f.trusted, revisionID, "video/mp4", trustedMP4(), objectVersion)
	if err != nil {
		t.Fatalf("staging a trusted public preview: %v", err)
	}
	return request
}

func previewTranscodeOperation(t *testing.T, f *trustedInstructorFixture, assetVersionID string) string {
	t.Helper()
	var operationID string
	if err := f.pool.QueryRow(f.ctx, `
		SELECT safe_payload->>'operation_id' FROM outbox_events
		WHERE event_type = 'media.transcode_requested' AND aggregate_id = $1::uuid
	`, assetVersionID).Scan(&operationID); err != nil {
		t.Fatalf("reading the transcode operation ID: %v", err)
	}
	return operationID
}

func countEvents(t *testing.T, f *trustedInstructorFixture, eventType, assetVersionID string) int {
	t.Helper()
	var count int
	if err := f.pool.QueryRow(f.ctx, `
		SELECT count(*) FROM outbox_events
		WHERE source_module = 'MEDIA_AND_ASSETS' AND event_type = $1 AND aggregate_id = $2::uuid
	`, eventType, assetVersionID).Scan(&count); err != nil {
		t.Fatalf("counting %s events: %v", eventType, err)
	}
	return count
}

// assertNoScanEvidence is the D-088 §5 assertion, applied everywhere the
// trusted path runs: no scan attempt row, no scan provenance, no scan work.
func assertNoScanEvidence(t *testing.T, f *trustedInstructorFixture, assetVersionID string) {
	t.Helper()
	var scanAttempts int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM scan_attempts WHERE asset_version_id = $1::uuid`, assetVersionID).Scan(&scanAttempts); err != nil {
		t.Fatalf("counting scan attempts: %v", err)
	}
	if scanAttempts != 0 {
		t.Fatalf("scan attempts = %d, want 0: the trusted path fabricated scan evidence", scanAttempts)
	}
	var scanProvenance *string
	if err := f.pool.QueryRow(f.ctx, `SELECT successful_scan_attempt_id::text FROM media_asset_versions WHERE id = $1::uuid`, assetVersionID).Scan(&scanProvenance); err != nil {
		t.Fatalf("reading scan provenance: %v", err)
	}
	if scanProvenance != nil {
		t.Fatal("the trusted path recorded successful scan provenance")
	}
	if scans := countEvents(t, f, "media.scan_requested", assetVersionID); scans != 0 {
		t.Fatalf("media.scan_requested events = %d, want 0", scans)
	}
}

func assertValidationEvidence(t *testing.T, f *trustedInstructorFixture, request CompleteUploadRequest) {
	t.Helper()
	var outcome, validator, profile, declaredType, objectVersion, sha string
	var verifiedSize int64
	if err := f.pool.QueryRow(f.ctx, `
		SELECT va.outcome::text, va.validator_identity, va.profile, va.declared_content_type,
		       va.storage_object_version, va.sha256_hex, va.verified_size_bytes
		FROM validation_attempts va
		JOIN media_asset_versions mav ON mav.successful_validation_attempt_id = va.id
		WHERE mav.id = $1::uuid
	`, request.AssetVersionID).Scan(&outcome, &validator, &profile, &declaredType, &objectVersion, &sha, &verifiedSize); err != nil {
		t.Fatalf("reading validation evidence: %v", err)
	}
	if outcome != "PASSED" || profile != TrustedValidationProfile {
		t.Fatalf("validation evidence = %s/%s, want PASSED/%s", outcome, profile, TrustedValidationProfile)
	}
	if strings.Contains(strings.ToLower(validator), "scan") {
		t.Fatalf("validator identity %q reads as a scanner", validator)
	}
	if objectVersion != request.StorageObjectVersion || !strings.EqualFold(sha, request.SHA256Hex) {
		t.Fatal("validation evidence is not bound to the exact stored object version")
	}
	if verifiedSize != request.SizeBytes || !strings.EqualFold(declaredType, request.ContentType) {
		t.Fatal("validation evidence does not record the verified size and declared type")
	}
}

func TestD088TrustedVideoValidatesThenProcessesToReady(t *testing.T) {
	f := newTrustedInstructorFixture(t)
	request := f.mustStage(t, KindVideo, "video/mp4", trustedMP4(), "object-v1")

	completed, err := f.trusted.CompleteUpload(f.ctx, request)
	if err != nil {
		t.Fatalf("completing a trusted video upload: %v", err)
	}
	if completed.State != StateValidated {
		t.Fatalf("state after completion = %q, want VALIDATED", completed.State)
	}
	if got := mediaState(t, f.pool, request.AssetVersionID); got != StateValidated {
		t.Fatalf("persisted state = %q, want VALIDATED", got)
	}
	assertNoScanEvidence(t, f, request.AssetVersionID)
	assertValidationEvidence(t, f, request)
	if transcodes := countEvents(t, f, "media.transcode_requested", request.AssetVersionID); transcodes != 1 {
		t.Fatalf("media.transcode_requested events = %d, want exactly 1", transcodes)
	}

	// The worker is mode-agnostic: it claims the version on its validation
	// provenance and still demands real FFmpeg evidence before READY.
	var operationID string
	if err := f.pool.QueryRow(f.ctx, `
		SELECT safe_payload->>'operation_id' FROM outbox_events
		WHERE event_type = 'media.transcode_requested' AND aggregate_id = $1::uuid
	`, request.AssetVersionID).Scan(&operationID); err != nil {
		t.Fatalf("reading the transcode operation ID: %v", err)
	}
	worker := trustedWorker(t, f, func(_ context.Context, object ObjectVersion) (TranscodeResult, error) {
		return TranscodeResult{
			TrustedDurationMS: 90000,
			OutputPrefix:      "media/" + object.AssetVersionID + "/hls",
			Renditions: []Rendition{{
				Name: "720p", StorageObjectKey: "media/" + object.AssetVersionID + "/hls/720p/playlist.m3u8",
				Width: 1280, Height: 720, BitrateKbps: 2800, DurationMS: 90000,
			}},
		}, nil
	})
	if err := worker.Transcode(f.ctx, request.AssetVersionID, operationID); err != nil {
		t.Fatalf("transcoding a validated video: %v", err)
	}
	if got := mediaState(t, f.pool, request.AssetVersionID); got != StateReady {
		t.Fatalf("state after processing = %q, want READY", got)
	}
	assertNoScanEvidence(t, f, request.AssetVersionID)

	var processingAttempt *string
	var duration *int64
	if err := f.pool.QueryRow(f.ctx, `
		SELECT successful_processing_attempt_id::text, trusted_duration_ms
		FROM media_asset_versions WHERE id = $1::uuid
	`, request.AssetVersionID).Scan(&processingAttempt, &duration); err != nil {
		t.Fatalf("reading processing evidence: %v", err)
	}
	if processingAttempt == nil || duration == nil || *duration != 90000 {
		t.Fatal("a READY trusted video lacks trusted processing evidence")
	}
}

func TestD088TrustedLessonResourcesReachReadyWithoutScanOrTranscode(t *testing.T) {
	cases := map[string]struct {
		contentType string
		body        func(*testing.T) []byte
	}{
		"PDF":  {contentType: "application/pdf", body: func(*testing.T) []byte { return trustedPDF() }},
		"DOCX": {contentType: ContentTypeDOCX, body: trustedDOCX},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newTrustedInstructorFixture(t)
			request := f.mustStage(t, KindResource, tc.contentType, tc.body(t), "object-v1")

			completed, err := f.trusted.CompleteUpload(f.ctx, request)
			if err != nil {
				t.Fatalf("completing a trusted %s upload: %v", name, err)
			}
			if completed.State != StateReady {
				t.Fatalf("state after completion = %q, want READY", completed.State)
			}
			if got := mediaState(t, f.pool, request.AssetVersionID); got != StateReady {
				t.Fatalf("persisted state = %q, want READY", got)
			}
			assertNoScanEvidence(t, f, request.AssetVersionID)
			assertValidationEvidence(t, f, request)
			if transcodes := countEvents(t, f, "media.transcode_requested", request.AssetVersionID); transcodes != 0 {
				t.Fatalf("media.transcode_requested events = %d, want 0 for a Lesson Resource", transcodes)
			}
		})
	}
}

func TestD088TrustedModeRefusesEverythingOutsideTheProfile(t *testing.T) {
	f := newTrustedInstructorFixture(t)
	cases := map[string]struct {
		kind        AssetKind
		contentType string
	}{
		"public preview pdf":   {KindPreview, "application/pdf"},
		"public preview image": {KindPreview, "image/png"},
		"lab material pdf":     {KindLabMaterial, "application/pdf"},
		"lab material archive": {KindLabMaterial, "application/zip"},
		"quicktime video":      {KindVideo, "video/quicktime"},
		"resource image":       {KindResource, "image/png"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := f.trusted.BeginUpload(f.ctx, UploadRequest{
				OwnerAccountID: f.instructorID, CourseID: f.courseID, LessonID: f.lessonID,
				Kind: tc.kind, ContentType: tc.contentType, SizeBytes: 1024,
			})
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("BeginUpload(%s, %s) = %v, want a validation refusal", tc.kind, tc.contentType, err)
			}
		})
	}
}

func TestD088TrustedCompletionAuthorizesTheOwningActiveInstructor(t *testing.T) {
	f := newTrustedInstructorFixture(t)

	t.Run("another instructor cannot complete", func(t *testing.T) {
		request := f.mustStage(t, KindResource, "application/pdf", trustedPDF(), "object-other-instructor")
		other := seedAccount(t, f, "INSTRUCTOR", "ACTIVE")
		request.OwnerAccountID = other
		if _, err := f.trusted.CompleteUpload(f.ctx, request); !errors.Is(err, ErrNotAuthorized) {
			t.Fatalf("completion by another Instructor = %v, want ErrNotAuthorized", err)
		}
	})

	t.Run("a student cannot complete", func(t *testing.T) {
		request := f.mustStage(t, KindResource, "application/pdf", trustedPDF(), "object-student")
		request.OwnerAccountID = seedAccount(t, f, "STUDENT", "ACTIVE")
		if _, err := f.trusted.CompleteUpload(f.ctx, request); !errors.Is(err, ErrNotAuthorized) {
			t.Fatalf("completion by a Student = %v, want ErrNotAuthorized", err)
		}
	})

	t.Run("a suspended owner cannot complete", func(t *testing.T) {
		request := f.mustStage(t, KindResource, "application/pdf", trustedPDF(), "object-suspended")
		if _, err := f.pool.Exec(f.ctx, `UPDATE accounts SET status = 'SUSPENDED' WHERE id = $1::uuid`, f.instructorID); err != nil {
			t.Fatalf("suspending the Instructor: %v", err)
		}
		defer func() {
			if _, err := f.pool.Exec(f.ctx, `UPDATE accounts SET status = 'ACTIVE' WHERE id = $1::uuid`, f.instructorID); err != nil {
				t.Fatalf("restoring the Instructor: %v", err)
			}
		}()
		if _, err := f.trusted.CompleteUpload(f.ctx, request); !errors.Is(err, ErrNotAuthorized) {
			t.Fatalf("completion by a suspended Instructor = %v, want ErrNotAuthorized", err)
		}
	})

	t.Run("a course this instructor does not own is refused at intent", func(t *testing.T) {
		otherInstructor := seedAccount(t, f, "INSTRUCTOR", "ACTIVE")
		var otherCourse string
		if err := f.pool.QueryRow(f.ctx, `
			INSERT INTO courses (owner_account_id, lifecycle) VALUES ($1::uuid, 'DRAFT') RETURNING id::text
		`, otherInstructor).Scan(&otherCourse); err != nil {
			t.Fatalf("seeding another Instructor's Course: %v", err)
		}
		_, err := f.trusted.BeginUpload(f.ctx, UploadRequest{
			OwnerAccountID: f.instructorID, CourseID: otherCourse, LessonID: f.lessonID,
			Kind: KindResource, ContentType: "application/pdf", SizeBytes: 1024,
		})
		if !errors.Is(err, ErrNotAuthorized) {
			t.Fatalf("upload into an unowned Course = %v, want ErrNotAuthorized", err)
		}
	})
}

func TestD088TrustedValidationRefusesEveryEvidenceMismatch(t *testing.T) {
	docx := trustedDOCX(t)
	cases := map[string]struct {
		kind        AssetKind
		contentType string
		body        []byte
		mutate      func(*CompleteUploadRequest)
	}{
		"actual size mismatch": {
			KindResource, "application/pdf", trustedPDF(),
			func(r *CompleteUploadRequest) { r.SizeBytes += 8 },
		},
		"checksum mismatch": {
			KindResource, "application/pdf", trustedPDF(),
			func(r *CompleteUploadRequest) { r.SHA256Hex = strings.Repeat("a", 64) },
		},
		"wrong object version": {
			KindResource, "application/pdf", trustedPDF(),
			func(r *CompleteUploadRequest) { r.StorageObjectVersion = "object-does-not-exist" },
		},
		"declared PDF is really a DOCX": {
			KindResource, "application/pdf", docx, nil,
		},
		"declared MP4 is really a PDF": {
			KindVideo, "video/mp4", trustedPDF(), nil,
		},
		"declared DOCX is really a PDF": {
			KindResource, ContentTypeDOCX, trustedPDF(), nil,
		},
		"declared DOCX is an arbitrary ZIP": {
			KindResource, ContentTypeDOCX, arbitraryZip(t), nil,
		},
		"declared DOCX is macro-enabled": {
			KindResource, ContentTypeDOCX, macroEnabledDOCX(t), nil,
		},
		"declared DOCX has a hostile entry path": {
			KindResource, ContentTypeDOCX, traversalDOCX(t), nil,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newTrustedInstructorFixture(t)
			request := f.mustStage(t, tc.kind, tc.contentType, tc.body, "object-v1")
			if tc.mutate != nil {
				tc.mutate(&request)
			}
			_, err := f.trusted.CompleteUpload(f.ctx, request)
			if err == nil {
				t.Fatal("invalid completion evidence was accepted")
			}
			if !errors.Is(err, ErrValidation) && !errors.Is(err, ErrConflict) && !errors.Is(err, ErrNotFound) {
				t.Fatalf("refusal = %v, want a validation, conflict or not-found refusal", err)
			}
			// A refused object is non-deliverable and carries no evidence at all.
			if got := mediaState(t, f.pool, request.AssetVersionID); got == StateReady || got == StateValidated {
				t.Fatalf("state after a refused completion = %q, want a non-deliverable state", got)
			}
			var validations int
			if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM validation_attempts WHERE asset_version_id = $1::uuid`, request.AssetVersionID).Scan(&validations); err != nil {
				t.Fatalf("counting validation attempts: %v", err)
			}
			if validations != 0 {
				t.Fatalf("validation attempts = %d, want 0 after a refusal", validations)
			}
			assertNoScanEvidence(t, f, request.AssetVersionID)
		})
	}
}

func TestD088ScannerModeIsUnchangedByTheTrustedProfile(t *testing.T) {
	f := newTrustedInstructorFixture(t)
	// f.service is the ordinary SCANNER-mode Service over the same database.
	request, _ := f.beginUpload(KindVideo, "object-scanner-v1")
	completed, err := f.service.CompleteUpload(f.ctx, request)
	if err != nil {
		t.Fatalf("completing a scanner-mode upload: %v", err)
	}
	if completed.State != StateQuarantined {
		t.Fatalf("scanner-mode completion state = %q, want QUARANTINED", completed.State)
	}
	if scans := countEvents(t, f, "media.scan_requested", request.AssetVersionID); scans != 1 {
		t.Fatalf("media.scan_requested events = %d, want exactly 1 in scanner mode", scans)
	}
	var validations int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM validation_attempts WHERE asset_version_id = $1::uuid`, request.AssetVersionID).Scan(&validations); err != nil {
		t.Fatalf("counting validation attempts: %v", err)
	}
	if validations != 0 {
		t.Fatalf("validation attempts = %d, want 0 in scanner mode", validations)
	}
}

func TestD088TrustedCompletionIsIdempotentUnderReplay(t *testing.T) {
	f := newTrustedInstructorFixture(t)
	request := f.mustStage(t, KindResource, "application/pdf", trustedPDF(), "object-v1")

	first, err := f.trusted.CompleteUpload(f.ctx, request)
	if err != nil || first.State != StateReady {
		t.Fatalf("first completion = %+v, err=%v; want READY", first, err)
	}
	replay, err := f.trusted.CompleteUpload(f.ctx, request)
	if err != nil {
		t.Fatalf("replaying the completion: %v", err)
	}
	if !replay.Duplicate || replay.State != StateReady {
		t.Fatalf("replay = %+v, want a duplicate still READY", replay)
	}
	var validations int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM validation_attempts WHERE asset_version_id = $1::uuid`, request.AssetVersionID).Scan(&validations); err != nil {
		t.Fatalf("counting validation attempts: %v", err)
	}
	if validations != 1 {
		t.Fatalf("validation attempts after replay = %d, want exactly 1", validations)
	}

	// The same provider event carrying different evidence is a conflict, not a
	// second pass.
	tampered := request
	tampered.SHA256Hex = strings.Repeat("b", 64)
	if _, err := f.trusted.CompleteUpload(f.ctx, tampered); !errors.Is(err, ErrConflict) {
		t.Fatalf("replay with different evidence = %v, want ErrConflict", err)
	}
}

func TestD088LessonResourceSizeBoundsAreEnforced(t *testing.T) {
	f := newTrustedInstructorFixture(t)
	pdf := trustedPDF()
	// Bounds tightened to the size of the fixture bytes so the rule can be
	// proven without materialising 50 MB objects.
	bounded := trustedServiceOver(t, f.mediaFixture, ServiceOptions{
		ResourceMaxBytes:       int64(len(pdf)),
		ResourceLessonMaxBytes: int64(len(pdf)) * 2,
	})

	t.Run("per-file cap refuses an oversized resource", func(t *testing.T) {
		_, err := bounded.BeginUpload(f.ctx, UploadRequest{
			OwnerAccountID: f.instructorID, CourseID: f.courseID, LessonID: uuid.NewString(),
			Kind: KindResource, ContentType: "application/pdf", SizeBytes: int64(len(pdf)) + 1,
		})
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("oversized resource = %v, want a validation refusal", err)
		}
	})

	t.Run("per-lesson aggregate refuses the file that would exceed it", func(t *testing.T) {
		// The bucket holds exactly two files of this size, so the third is the
		// first request that must be refused.
		lessonID := uuid.NewString()
		for attempt := 1; attempt <= 2; attempt++ {
			if _, err := bounded.BeginUpload(f.ctx, UploadRequest{
				OwnerAccountID: f.instructorID, CourseID: f.courseID, LessonID: lessonID,
				Kind: KindResource, ContentType: "application/pdf", SizeBytes: int64(len(pdf)),
			}); err != nil {
				t.Fatalf("resource %d in the same Lesson was refused: %v", attempt, err)
			}
		}
		_, err := bounded.BeginUpload(f.ctx, UploadRequest{
			OwnerAccountID: f.instructorID, CourseID: f.courseID, LessonID: lessonID,
			Kind: KindResource, ContentType: "application/pdf", SizeBytes: int64(len(pdf)),
		})
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("third resource in the Lesson = %v, want an aggregate refusal", err)
		}
		if !strings.Contains(err.Error(), "per-Lesson limit") {
			t.Fatalf("aggregate refusal = %v, want it to name the per-Lesson limit", err)
		}
		// A different Lesson has its own bucket and is unaffected.
		if _, err := bounded.BeginUpload(f.ctx, UploadRequest{
			OwnerAccountID: f.instructorID, CourseID: f.courseID, LessonID: uuid.NewString(),
			Kind: KindResource, ContentType: "application/pdf", SizeBytes: int64(len(pdf)),
		}); err != nil {
			t.Fatalf("a different Lesson's bucket was affected: %v", err)
		}
	})

	t.Run("the intent records the bucket bound, not the deployment ceiling", func(t *testing.T) {
		ticket, err := bounded.BeginUpload(f.ctx, UploadRequest{
			OwnerAccountID: f.instructorID, CourseID: f.courseID, LessonID: uuid.NewString(),
			Kind: KindResource, ContentType: "application/pdf", SizeBytes: int64(len(pdf)),
		})
		if err != nil {
			t.Fatalf("beginning a bounded resource upload: %v", err)
		}
		var maxSize int64
		if err := f.pool.QueryRow(f.ctx, `SELECT max_size_bytes FROM upload_intents WHERE asset_version_id = $1::uuid`, ticket.AssetVersionID).Scan(&maxSize); err != nil {
			t.Fatalf("reading the intent bound: %v", err)
		}
		if maxSize != int64(len(pdf)) {
			t.Fatalf("intent max_size_bytes = %d, want the resource bucket bound %d", maxSize, len(pdf))
		}
	})
}

func TestD088TrustedRetryRevalidatesAndNeverSchedulesAScan(t *testing.T) {
	f := newTrustedInstructorFixture(t)
	request := f.mustStage(t, KindVideo, "video/mp4", trustedMP4(), "object-v1")
	if _, err := f.trusted.CompleteUpload(f.ctx, request); err != nil {
		t.Fatalf("completing a trusted video upload: %v", err)
	}
	var operationID string
	if err := f.pool.QueryRow(f.ctx, `
		SELECT safe_payload->>'operation_id' FROM outbox_events
		WHERE event_type = 'media.transcode_requested' AND aggregate_id = $1::uuid
	`, request.AssetVersionID).Scan(&operationID); err != nil {
		t.Fatalf("reading the transcode operation ID: %v", err)
	}

	failing := trustedWorker(t, f, func(context.Context, ObjectVersion) (TranscodeResult, error) {
		return TranscodeResult{}, errors.New("ffmpeg exited non-zero")
	})
	if err := failing.Transcode(f.ctx, request.AssetVersionID, operationID); err == nil {
		t.Fatal("a failing processor reported success")
	}
	if got := mediaState(t, f.pool, request.AssetVersionID); got != StateProcessFailed {
		t.Fatalf("state after a processing failure = %q, want PROCESS_FAILED", got)
	}

	if err := f.trusted.Retry(f.ctx, RetryRequest{
		AssetVersionID: request.AssetVersionID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID,
	}); err != nil {
		t.Fatalf("retrying a trusted video: %v", err)
	}
	if got := mediaState(t, f.pool, request.AssetVersionID); got != StateValidated {
		t.Fatalf("state after a trusted retry = %q, want VALIDATED", got)
	}
	assertNoScanEvidence(t, f, request.AssetVersionID)
	if transcodes := countEvents(t, f, "media.transcode_requested", request.AssetVersionID); transcodes != 2 {
		t.Fatalf("media.transcode_requested events = %d, want 2 after one retry", transcodes)
	}
	// The retry re-proved the exact object version rather than inheriting the
	// earlier pass, so a second immutable attempt exists for the same bytes.
	var attempts int
	if err := f.pool.QueryRow(f.ctx, `
		SELECT count(*) FROM validation_attempts
		WHERE asset_version_id = $1::uuid AND storage_object_version = $2 AND outcome = 'PASSED'
	`, request.AssetVersionID, request.StorageObjectVersion).Scan(&attempts); err != nil {
		t.Fatalf("counting validation attempts: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("validation attempts after a retry = %d, want 2", attempts)
	}
}

func TestD088TrustedRetryIsRefusedWhenTheDeploymentLeavesTheProfile(t *testing.T) {
	f := newTrustedInstructorFixture(t)
	request := f.mustStage(t, KindVideo, "video/mp4", trustedMP4(), "object-v1")
	if _, err := f.trusted.CompleteUpload(f.ctx, request); err != nil {
		t.Fatalf("completing a trusted video upload: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE media_asset_versions SET state = 'PROCESS_FAILED' WHERE id = $1::uuid`, request.AssetVersionID); err != nil {
		t.Fatalf("forcing PROCESS_FAILED: %v", err)
	}

	// f.service runs in SCANNER mode. Routing this asset into a scan it never
	// had would be a lie about what happened, so the retry is refused instead.
	err := f.service.Retry(f.ctx, RetryRequest{
		AssetVersionID: request.AssetVersionID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID,
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("scanner-mode retry of a trusted asset = %v, want ErrConflict", err)
	}
	if scans := countEvents(t, f, "media.scan_requested", request.AssetVersionID); scans != 0 {
		t.Fatalf("media.scan_requested events = %d, want 0: a trusted asset was routed to a scanner", scans)
	}
}

func TestD088WorkerRefusesToProcessAnAssetWithoutProvenance(t *testing.T) {
	f := newTrustedInstructorFixture(t)
	request := f.mustStage(t, KindVideo, "video/mp4", trustedMP4(), "object-v1")

	worker := trustedWorker(t, f, func(context.Context, ObjectVersion) (TranscodeResult, error) {
		t.Fatal("the processor ran for an asset with no safety provenance")
		return TranscodeResult{}, nil
	})
	// UPLOADED: no completion, no quarantine, no evidence of any kind.
	if err := worker.Transcode(f.ctx, request.AssetVersionID, uuid.NewString()); err != nil {
		t.Fatalf("transcoding an unprepared asset should be a no-op, got: %v", err)
	}
	if got := mediaState(t, f.pool, request.AssetVersionID); got != StateUploaded {
		t.Fatalf("state = %q, want UPLOADED to be left untouched", got)
	}

	if _, err := f.trusted.CompleteUpload(f.ctx, request); err != nil {
		t.Fatalf("completing a trusted video upload: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE media_asset_versions SET state = 'PROCESS_FAILED' WHERE id = $1::uuid`, request.AssetVersionID); err != nil {
		t.Fatalf("forcing PROCESS_FAILED: %v", err)
	}
	// A failed asset is not claimable either, so a stale queue item cannot
	// resurrect it.
	if err := worker.Transcode(f.ctx, request.AssetVersionID, uuid.NewString()); err != nil {
		t.Fatalf("transcoding a failed asset should be a no-op, got: %v", err)
	}
	if got := mediaState(t, f.pool, request.AssetVersionID); got != StateProcessFailed {
		t.Fatalf("state = %q, want PROCESS_FAILED to be left untouched", got)
	}
}

func trustedWorker(t *testing.T, f *trustedInstructorFixture, process integrationProcessorFunc) *Worker {
	t.Helper()
	unavailable, err := NewUnavailableScanner("no scanner is configured in the D-088 launch profile")
	if err != nil {
		t.Fatalf("constructing the unavailable scanner: %v", err)
	}
	adapter, err := NewScannerAdapter(unavailable)
	if err != nil {
		t.Fatalf("constructing the scanner adapter: %v", err)
	}
	worker, err := NewWorker(WorkerOptions{DB: f.pool, Scanner: adapter, Process: process, Outbox: f.writer})
	if err != nil {
		t.Fatalf("constructing the media worker: %v", err)
	}
	return worker
}

func seedAccount(t *testing.T, f *trustedInstructorFixture, role, status string) string {
	t.Helper()
	var id string
	if err := f.pool.QueryRow(f.ctx, `
		INSERT INTO accounts (normalized_email, email, role, status, display_name, locale, email_verified_at)
		VALUES ($1, $1, $2, $3, 'D-088 Seeded', 'en', now()) RETURNING id::text
	`, "d088-"+uuid.NewString()+"@example.test", role, status).Scan(&id); err != nil {
		t.Fatalf("seeding a %s/%s account: %v", role, status, err)
	}
	return id
}

func arbitraryZip(t *testing.T) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	w, err := writer.Create("notes.txt")
	if err != nil {
		t.Fatalf("creating a ZIP entry: %v", err)
	}
	if _, err := w.Write([]byte("this is not an Office package")); err != nil {
		t.Fatalf("writing a ZIP entry: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("closing the ZIP: %v", err)
	}
	return buffer.Bytes()
}

func macroEnabledDOCX(t *testing.T) []byte {
	t.Helper()
	return officePackage(t, map[string]string{
		"[Content_Types].xml": `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Override PartName="/word/document.xml" ContentType="` + ContentTypeDOCX + `.main+xml"/></Types>`,
		"word/document.xml":   "<w:document/>",
		"word/vbaProject.bin": "\x00\x01macro payload",
	})
}

func traversalDOCX(t *testing.T) []byte {
	t.Helper()
	return officePackage(t, map[string]string{
		"[Content_Types].xml": `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Override PartName="/word/document.xml" ContentType="` + ContentTypeDOCX + `.main+xml"/></Types>`,
		"word/document.xml": "<w:document/>",
		"../../etc/crontab": "* * * * * root sh",
	})
}

func officePackage(t *testing.T, parts map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, body := range parts {
		w, err := writer.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
		if err != nil {
			t.Fatalf("creating package part %q: %v", name, err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatalf("writing package part %q: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("closing the package: %v", err)
	}
	return buffer.Bytes()
}

// D-096, end to end. An MP4 public preview from a vetted Instructor is admitted
// to the trusted profile, validates against the exact stored object version,
// and then owes exactly what a Lesson video owes: real FFmpeg evidence before
// READY. Nothing on the path records or implies malware scanning.
func TestD096TrustedPublicPreviewValidatesThenProcessesToReady(t *testing.T) {
	f := newTrustedInstructorFixture(t)
	revisionID := f.draftRevision(t)
	request := f.mustStagePreview(t, revisionID, "preview-v1")

	completed, err := f.trusted.CompleteUpload(f.ctx, request)
	if err != nil {
		t.Fatalf("completing a trusted public-preview upload: %v", err)
	}
	if completed.State != StateValidated {
		t.Fatalf("state after completion = %q, want VALIDATED", completed.State)
	}
	if got := mediaState(t, f.pool, request.AssetVersionID); got != StateValidated {
		t.Fatalf("persisted state = %q, want VALIDATED; a preview must never be READY on validation alone", got)
	}
	assertNoScanEvidence(t, f, request.AssetVersionID)
	assertValidationEvidence(t, f, request)
	if transcodes := countEvents(t, f, "media.transcode_requested", request.AssetVersionID); transcodes != 1 {
		t.Fatalf("media.transcode_requested events = %d, want exactly 1", transcodes)
	}

	// The preview stays bound to the revision it was uploaded for, and public
	// rather than protected.
	var visibility, originRevision string
	var lessonID *string
	if err := f.pool.QueryRow(f.ctx, `
		SELECT ma.visibility::text, ma.preview_origin_revision_id::text, ma.lesson_id::text
		FROM media_asset_versions mav
		JOIN media_assets ma ON ma.id = mav.logical_asset_id
		WHERE mav.id = $1::uuid
	`, request.AssetVersionID).Scan(&visibility, &originRevision, &lessonID); err != nil {
		t.Fatalf("reading the preview binding: %v", err)
	}
	if visibility != "PUBLIC_PREVIEW" || originRevision != revisionID || lessonID != nil {
		t.Fatalf("preview binding visibility=%q origin=%q lesson=%v, want PUBLIC_PREVIEW bound to %s with no Lesson",
			visibility, originRevision, lessonID, revisionID)
	}

	operationID := previewTranscodeOperation(t, f, request.AssetVersionID)
	worker := trustedWorker(t, f, func(_ context.Context, object ObjectVersion) (TranscodeResult, error) {
		return TranscodeResult{
			TrustedDurationMS: 45000,
			OutputPrefix:      "media/" + object.AssetVersionID + "/hls",
			Renditions: []Rendition{{
				Name: "720p", StorageObjectKey: "media/" + object.AssetVersionID + "/hls/720p/playlist.m3u8",
				Width: 1280, Height: 720, BitrateKbps: 2800, DurationMS: 45000,
			}},
		}, nil
	})
	if err := worker.Transcode(f.ctx, request.AssetVersionID, operationID); err != nil {
		t.Fatalf("transcoding a validated public preview: %v", err)
	}
	if got := mediaState(t, f.pool, request.AssetVersionID); got != StateReady {
		t.Fatalf("state after processing = %q, want READY", got)
	}
	assertNoScanEvidence(t, f, request.AssetVersionID)

	var processingAttempt *string
	var duration *int64
	if err := f.pool.QueryRow(f.ctx, `
		SELECT successful_processing_attempt_id::text, trusted_duration_ms
		FROM media_asset_versions WHERE id = $1::uuid
	`, request.AssetVersionID).Scan(&processingAttempt, &duration); err != nil {
		t.Fatalf("reading preview processing provenance: %v", err)
	}
	if processingAttempt == nil || duration == nil || *duration != 45000 {
		t.Fatal("a READY trusted preview lacks successful processing evidence")
	}
}

// The readiness gate, proved by failure rather than by inspection: a preview
// whose processing fails must never become deliverable.
func TestD096TrustedPublicPreviewCannotBecomeReadyWithoutProcessing(t *testing.T) {
	f := newTrustedInstructorFixture(t)
	revisionID := f.draftRevision(t)
	request := f.mustStagePreview(t, revisionID, "preview-failed-v1")
	if _, err := f.trusted.CompleteUpload(f.ctx, request); err != nil {
		t.Fatalf("completing a trusted public-preview upload: %v", err)
	}
	operationID := previewTranscodeOperation(t, f, request.AssetVersionID)

	worker := trustedWorker(t, f, func(_ context.Context, _ ObjectVersion) (TranscodeResult, error) {
		return TranscodeResult{}, errors.New("ffmpeg refused the source")
	})
	// The worker surfaces the processor's own failure after recording it.
	if err := worker.Transcode(f.ctx, request.AssetVersionID, operationID); err == nil {
		t.Fatal("a failed preview transcode reported success")
	}
	if got := mediaState(t, f.pool, request.AssetVersionID); got != StateProcessFailed {
		t.Fatalf("state after failed processing = %q, want PROCESS_FAILED", got)
	}

	// The database refuses the shortcut as well, so a direct write cannot make
	// an unprocessed preview deliverable.
	if _, err := f.pool.Exec(f.ctx, `
		UPDATE media_asset_versions SET state = 'READY' WHERE id = $1::uuid
	`, request.AssetVersionID); err == nil {
		t.Fatal("an unprocessed public preview was forced to READY")
	}
}

// The profile stays narrow. A non-MP4 preview is refused at the boundary, and
// the database refuses to attach validation provenance to one even if the
// service were bypassed.
func TestD096TrustedProfileAdmitsOnlyMP4PublicPreviews(t *testing.T) {
	f := newTrustedInstructorFixture(t)
	revisionID := f.draftRevision(t)
	for name, contentType := range map[string]string{
		"pdf":       "application/pdf",
		"quicktime": "video/quicktime",
		"png":       "image/png",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := f.stagePreview(t, f.trusted, revisionID, contentType, trustedPDF(), "preview-"+name)
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("BeginUpload(PREVIEW, %s) = %v, want a validation refusal", contentType, err)
			}
		})
	}
}

// Scanner mode is untouched by D-096: the same preview upload still enters
// quarantine behind a scan-work intent, records no validation evidence, and
// reaches READY through the scanner path with no FFmpeg requirement.
func TestD096ScannerModePublicPreviewIsUnchanged(t *testing.T) {
	f := newTrustedInstructorFixture(t)
	revisionID := f.draftRevision(t)
	request, err := f.stagePreview(t, f.service, revisionID, "video/mp4", trustedMP4(), "scanner-preview-v1")
	if err != nil {
		t.Fatalf("staging a scanner-mode public preview: %v", err)
	}
	completed, err := f.service.CompleteUpload(f.ctx, request)
	if err != nil {
		t.Fatalf("completing a scanner-mode public-preview upload: %v", err)
	}
	if completed.State != StateQuarantined {
		t.Fatalf("scanner-mode state after completion = %q, want QUARANTINED", completed.State)
	}
	if scans := countEvents(t, f, "media.scan_requested", request.AssetVersionID); scans != 1 {
		t.Fatalf("media.scan_requested events = %d, want exactly 1", scans)
	}
	var validationAttempts int
	if err := f.pool.QueryRow(f.ctx, `
		SELECT count(*) FROM validation_attempts WHERE asset_version_id = $1::uuid
	`, request.AssetVersionID).Scan(&validationAttempts); err != nil {
		t.Fatalf("counting validation attempts: %v", err)
	}
	if validationAttempts != 0 {
		t.Fatalf("validation attempts = %d, want 0 in scanner mode", validationAttempts)
	}
}
