//go:build integration

package db

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The D-088 database boundary.
//
// D-088 §5 forbids fabricating scan evidence, and the launch profile is only
// as safe as its narrowest enforcement point. These tests assert that the
// *database* refuses a deliverable Asset Version with no legitimate provenance,
// refuses to let validation provenance stand in for a scan, and refuses to let
// the trusted profile reach a kind or content type it never covered — so an
// application bug, a mode switch, or a direct UPDATE cannot widen it.

// The one canonical D-088 profile identifier. It is the value
// `media.TrustedValidationProfile` writes and the value migration 0020 requires;
// TestTrustedValidationProfileIdentifierIsCanonical in the media package pins
// the Go constant to this same literal so the two cannot drift apart.
const d088Profile = "D-088-TRUSTED-INSTRUCTOR"

// The seeded Asset Version's own bytes. Evidence that claims anything different
// is, by definition, evidence about some other bytes.
const (
	seededSizeBytes    int64 = 1024
	seededSHA256             = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	seededMaxSizeBytes int64 = 52428800
)

type trustedFixture struct {
	pool           *pgxpool.Pool
	instructorID   string
	courseID       string
	logicalAssetID string
	versionID      string
	objectVersion  string
	contentType    string
}

func seedTrustedAsset(t *testing.T, kind, contentType string) trustedFixture {
	t.Helper()
	pool := openPool(t)
	ctx := context.Background()

	var instructorID, courseID, logicalAssetID, versionID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO accounts (normalized_email, email, role, status, display_name, locale)
		VALUES ($1, $1, 'INSTRUCTOR', 'ACTIVE', 'D-088 Instructor', 'en') RETURNING id::text
	`, "d088-"+uuid.NewString()+"@example.test").Scan(&instructorID); err != nil {
		t.Fatalf("seeding instructor: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO courses (owner_account_id, lifecycle) VALUES ($1::uuid, 'DRAFT') RETURNING id::text
	`, instructorID).Scan(&courseID); err != nil {
		t.Fatalf("seeding course: %v", err)
	}
	visibility := "PROTECTED"
	if kind == "PREVIEW" {
		visibility = "PUBLIC_PREVIEW"
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO media_assets (kind, owner_account_id, course_id, visibility)
		VALUES ($1::media_asset_kind, $2::uuid, $3::uuid, $4::media_asset_visibility) RETURNING id::text
	`, kind, instructorID, courseID, visibility).Scan(&logicalAssetID); err != nil {
		t.Fatalf("seeding logical asset: %v", err)
	}
	objectVersion := "object-" + kind + "-v1"
	objectKey := "quarantine/" + courseID + "/source"
	if err := pool.QueryRow(ctx, `
		INSERT INTO media_asset_versions (
			logical_asset_id, kind, state, storage_object_key, storage_object_version,
			content_type, size_bytes, sha256_hex
		) VALUES ($1::uuid, $2::media_asset_kind, 'QUARANTINED', $3, $4, $5, $6, $7)
		RETURNING id::text
	`, logicalAssetID, kind, objectKey, objectVersion, contentType, seededSizeBytes, seededSHA256).Scan(&versionID); err != nil {
		t.Fatalf("seeding asset version: %v", err)
	}
	// The upload intent carries the configured size bound this upload was
	// admitted against. It is the only durable record of that bound, so
	// validation evidence must agree with it rather than assert its own.
	if _, err := pool.Exec(ctx, `
		INSERT INTO upload_intents (
			asset_version_id, expected_object_key, expected_content_type,
			expected_size_bytes, max_size_bytes, expires_at
		) VALUES ($1::uuid, $2, $3, $4, $5, now() + interval '15 minutes')
	`, versionID, objectKey, contentType, seededSizeBytes, seededMaxSizeBytes); err != nil {
		t.Fatalf("seeding upload intent: %v", err)
	}
	return trustedFixture{
		pool: pool, instructorID: instructorID, courseID: courseID,
		logicalAssetID: logicalAssetID, versionID: versionID,
		objectVersion: objectVersion, contentType: contentType,
	}
}

// validationEvidence is one row of D-088 exact-version evidence. Each field is
// separately settable so a test can make exactly one of them disagree with the
// Asset Version it claims to describe.
type validationEvidence struct {
	outcome       string
	objectVersion string
	profile       string
	contentType   string
	sizeBytes     int64
	maxSizeBytes  int64
	sha256Hex     string
}

func failedEvidence(evidence validationEvidence) validationEvidence {
	evidence.outcome = "FAILED"
	return evidence
}

// truthfulEvidence describes the Asset Version's real bytes. A test mutates one
// field to build the inconsistent case it is about.
func (f trustedFixture) truthfulEvidence() validationEvidence {
	return validationEvidence{
		outcome:       "PASSED",
		objectVersion: f.objectVersion,
		profile:       d088Profile,
		contentType:   f.contentType,
		sizeBytes:     seededSizeBytes,
		maxSizeBytes:  seededMaxSizeBytes,
		sha256Hex:     seededSHA256,
	}
}

func (f trustedFixture) recordValidationWith(t *testing.T, evidence validationEvidence) string {
	t.Helper()
	reason := "exact-version validation failed"
	if evidence.outcome == "PASSED" {
		reason = ""
	}
	var attemptID string
	if err := f.pool.QueryRow(context.Background(), `
		INSERT INTO validation_attempts (
			asset_version_id, attempt_number, work_id, storage_object_version, outcome,
			validator_identity, profile, declared_content_type, verified_size_bytes,
			max_size_bytes, sha256_hex, reason
		) VALUES (
			$1::uuid,
			(SELECT COALESCE(MAX(attempt_number), 0) + 1 FROM validation_attempts WHERE asset_version_id = $1::uuid),
			$2, $3, $4::media_validation_outcome,
			'gradex-media-exact-version-validator', $5, $6, $7, $8, $9, NULLIF($10, '')
		) RETURNING id::text
	`, f.versionID, "validation-"+uuid.NewString(), evidence.objectVersion, evidence.outcome,
		evidence.profile, evidence.contentType, evidence.sizeBytes, evidence.maxSizeBytes,
		evidence.sha256Hex, reason).Scan(&attemptID); err != nil {
		t.Fatalf("recording validation attempt: %v", err)
	}
	return attemptID
}

func TestTrustedValidationDatabaseGuardsProvenance(t *testing.T) {
	freshDatabase(t)
	if err := openMigrator(t).Up(); err != nil {
		t.Fatalf("applying the migration chain: %v", err)
	}
	ctx := context.Background()

	t.Run("READY refuses an asset with no provenance at all", func(t *testing.T) {
		f := seedTrustedAsset(t, "RESOURCE", "application/pdf")
		_, err := f.pool.Exec(ctx, `UPDATE media_asset_versions SET state = 'READY' WHERE id = $1::uuid`, f.versionID)
		if err == nil {
			t.Fatal("an asset was forced to READY with no scan or validation evidence")
		}
		// QUARANTINED -> READY is not even a transition, so the machine refuses
		// it before provenance is considered.
		if !strings.Contains(err.Error(), "invalid media asset version state transition") {
			t.Fatalf("unexpected refusal: %v", err)
		}
	})

	t.Run("READY refuses an asset whose provenance is cleared on the way", func(t *testing.T) {
		f := seedTrustedAsset(t, "VIDEO", "video/mp4")
		attemptID := f.recordValidationWith(t, f.truthfulEvidence())
		if _, err := f.pool.Exec(ctx, `
			UPDATE media_asset_versions SET state = 'VALIDATED', successful_validation_attempt_id = $1::uuid
			WHERE id = $2::uuid
		`, attemptID, f.versionID); err != nil {
			t.Fatalf("validating a video: %v", err)
		}
		if _, err := f.pool.Exec(ctx, `UPDATE media_asset_versions SET state = 'PROCESSING' WHERE id = $1::uuid`, f.versionID); err != nil {
			t.Fatalf("claiming a validated video for processing: %v", err)
		}
		_, err := f.pool.Exec(ctx, `
			UPDATE media_asset_versions
			SET state = 'READY', successful_validation_attempt_id = NULL, successful_scan_attempt_id = NULL
			WHERE id = $1::uuid
		`, f.versionID)
		if err == nil || !strings.Contains(err.Error(), "lacks successful exact-version scan or validation evidence") {
			t.Fatalf("READY after clearing provenance = %v, want a provenance refusal", err)
		}
	})

	t.Run("VALIDATED refuses an asset with no validation evidence", func(t *testing.T) {
		f := seedTrustedAsset(t, "RESOURCE", "application/pdf")
		_, err := f.pool.Exec(ctx, `UPDATE media_asset_versions SET state = 'VALIDATED' WHERE id = $1::uuid`, f.versionID)
		if err == nil || !strings.Contains(err.Error(), "lacks successful exact-version validation evidence") {
			t.Fatalf("VALIDATED without evidence = %v, want a validation-evidence refusal", err)
		}
	})

	t.Run("VALIDATED refuses a failed validation attempt", func(t *testing.T) {
		f := seedTrustedAsset(t, "RESOURCE", "application/pdf")
		attemptID := f.recordValidationWith(t, failedEvidence(f.truthfulEvidence()))
		_, err := f.pool.Exec(ctx, `
			UPDATE media_asset_versions SET state = 'VALIDATED', successful_validation_attempt_id = $1::uuid
			WHERE id = $2::uuid
		`, attemptID, f.versionID)
		if err == nil || !strings.Contains(err.Error(), "matching successful exact-version validation attempt") {
			t.Fatalf("VALIDATED on a FAILED attempt = %v, want a matching-attempt refusal", err)
		}
	})

	// A PASSED attempt is not automatically truthful evidence. It carries its own
	// account of the bytes it inspected — checksum, actual size, declared type,
	// profile, and the configured bound it was checked against. If any of that
	// disagrees with the Asset Version it is attached to, the attempt describes
	// some other bytes, and the database must refuse to treat it as provenance
	// for these ones. Otherwise a forged or stale row could make an unvalidated
	// object deliverable while every column still read as a legitimate pass.
	t.Run("VALIDATED refuses a PASSED attempt that contradicts the exact bytes", func(t *testing.T) {
		inconsistent := map[string]struct {
			mutate func(validationEvidence) validationEvidence
			// what the seeded Asset Version actually is, for the failure message
			describe string
		}{
			"checksum mismatch": {
				mutate: func(e validationEvidence) validationEvidence {
					e.sha256Hex = strings.Repeat("b", 64)
					return e
				},
				describe: "a SHA-256 that is not the Asset Version's checksum",
			},
			"actual size mismatch": {
				mutate: func(e validationEvidence) validationEvidence {
					e.sizeBytes = seededSizeBytes + 1
					return e
				},
				describe: "a verified size that is not the Asset Version's size",
			},
			"declared content type mismatch": {
				mutate: func(e validationEvidence) validationEvidence {
					e.contentType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
					return e
				},
				describe: "a declared type that is not the Asset Version's content type",
			},
			"arbitrary non-D-088 profile": {
				mutate: func(e validationEvidence) validationEvidence {
					e.profile = "anything-non-empty"
					return e
				},
				describe: "a profile that is not the canonical D-088 identifier",
			},
			"lowercase D-088 profile": {
				mutate: func(e validationEvidence) validationEvidence {
					e.profile = strings.ToLower(d088Profile)
					return e
				},
				describe: "a near-miss of the canonical D-088 identifier",
			},
			"configured bound not the one this upload was admitted against": {
				mutate: func(e validationEvidence) validationEvidence {
					e.maxSizeBytes = seededMaxSizeBytes * 4
					return e
				},
				describe: "a max size that is not the upload intent's recorded bound",
			},
		}
		for name, tc := range inconsistent {
			t.Run(name, func(t *testing.T) {
				f := seedTrustedAsset(t, "RESOURCE", "application/pdf")
				attemptID := f.recordValidationWith(t, tc.mutate(f.truthfulEvidence()))
				_, err := f.pool.Exec(ctx, `
					UPDATE media_asset_versions SET state = 'VALIDATED', successful_validation_attempt_id = $1::uuid
					WHERE id = $2::uuid
				`, attemptID, f.versionID)
				if err == nil {
					t.Fatalf("a PASSED attempt with %s was accepted as provenance", tc.describe)
				}
				if !strings.Contains(err.Error(), "matching successful exact-version validation attempt") {
					t.Fatalf("refusal = %v, want a matching-attempt refusal", err)
				}

				// It must also be unusable further along the lifecycle: the same
				// inconsistent attempt cannot carry the asset to PROCESSING or
				// READY by attaching it during a later transition either.
				truthful := f.recordValidationWith(t, f.truthfulEvidence())
				if _, err := f.pool.Exec(ctx, `
					UPDATE media_asset_versions SET state = 'VALIDATED', successful_validation_attempt_id = $1::uuid
					WHERE id = $2::uuid
				`, truthful, f.versionID); err != nil {
					t.Fatalf("truthful evidence was refused: %v", err)
				}
				_, err = f.pool.Exec(ctx, `
					UPDATE media_asset_versions SET state = 'READY', successful_validation_attempt_id = $1::uuid
					WHERE id = $2::uuid
				`, attemptID, f.versionID)
				if err == nil {
					t.Fatalf("READY accepted a PASSED attempt with %s", tc.describe)
				}
			})
		}
	})

	// The bound is only meaningful if it is the bound this upload was actually
	// admitted against, which lives in the upload intent. Evidence that asserts
	// its own bound proves nothing.
	t.Run("validation evidence must agree with the upload intent's configured bound", func(t *testing.T) {
		f := seedTrustedAsset(t, "RESOURCE", "application/pdf")
		attemptID := f.recordValidationWith(t, f.truthfulEvidence())
		if _, err := f.pool.Exec(ctx, `
			UPDATE media_asset_versions SET state = 'VALIDATED', successful_validation_attempt_id = $1::uuid
			WHERE id = $2::uuid
		`, attemptID, f.versionID); err != nil {
			t.Fatalf("evidence agreeing with the intent bound was refused: %v", err)
		}
		// The intent's bound is itself immutable, so the agreement cannot be
		// manufactured after the fact by editing the intent to match.
		_, err := f.pool.Exec(ctx, `
			UPDATE upload_intents SET max_size_bytes = $1 WHERE asset_version_id = $2::uuid
		`, seededMaxSizeBytes*4, f.versionID)
		if err == nil {
			t.Fatal("the upload intent's configured bound was rewritten after completion")
		}
		if !strings.Contains(err.Error(), "upload intent") {
			t.Fatalf("refusal = %v, want an upload-intent immutability refusal", err)
		}
	})

	// Completion is the one intent field that legitimately moves, and only once.
	t.Run("the upload intent records completion exactly once", func(t *testing.T) {
		f := seedTrustedAsset(t, "RESOURCE", "application/pdf")
		if _, err := f.pool.Exec(ctx, `
			UPDATE upload_intents SET completed_at = now(), completion_fingerprint = 'fingerprint-1'
			WHERE asset_version_id = $1::uuid AND completed_at IS NULL
		`, f.versionID); err != nil {
			t.Fatalf("recording completion: %v", err)
		}
		_, err := f.pool.Exec(ctx, `
			UPDATE upload_intents SET completion_fingerprint = 'fingerprint-2' WHERE asset_version_id = $1::uuid
		`, f.versionID)
		if err == nil {
			t.Fatal("a completed upload intent was rewritten with different evidence")
		}
	})

	t.Run("VALIDATED refuses evidence bound to another asset version", func(t *testing.T) {
		mine := seedTrustedAsset(t, "RESOURCE", "application/pdf")
		other := seedTrustedAsset(t, "RESOURCE", "application/pdf")
		otherAttempt := other.recordValidationWith(t, other.truthfulEvidence())
		_, err := mine.pool.Exec(ctx, `
			UPDATE media_asset_versions SET state = 'VALIDATED', successful_validation_attempt_id = $1::uuid
			WHERE id = $2::uuid
		`, otherAttempt, mine.versionID)
		if err == nil {
			t.Fatal("another asset version's validation evidence was accepted")
		}
	})

	t.Run("validation evidence never satisfies SCAN_PASSED", func(t *testing.T) {
		f := seedTrustedAsset(t, "RESOURCE", "application/pdf")
		attemptID := f.recordValidationWith(t, f.truthfulEvidence())
		if _, err := f.pool.Exec(ctx, `
			UPDATE media_asset_versions SET state = 'SCANNING' WHERE id = $1::uuid
		`, f.versionID); err != nil {
			t.Fatalf("entering SCANNING: %v", err)
		}
		_, err := f.pool.Exec(ctx, `
			UPDATE media_asset_versions SET state = 'SCAN_PASSED', successful_validation_attempt_id = $1::uuid
			WHERE id = $2::uuid
		`, attemptID, f.versionID)
		if err == nil || !strings.Contains(err.Error(), "lacks successful exact-version scan evidence") {
			t.Fatalf("SCAN_PASSED on validation evidence = %v, want a scan-evidence refusal", err)
		}
	})

	// D-096 admits the MP4 public preview to the profile, and immediately puts
	// it under the Lesson-video processing requirement: it may be VALIDATED, but
	// the direct VALIDATED -> READY edge is closed to it.
	t.Run("MP4 public preview carries validation provenance but cannot skip processing", func(t *testing.T) {
		f := seedTrustedAsset(t, "PREVIEW", "video/mp4")
		attemptID := f.recordValidationWith(t, f.truthfulEvidence())
		if _, err := f.pool.Exec(ctx, `
			UPDATE media_asset_versions SET state = 'VALIDATED', successful_validation_attempt_id = $1::uuid
			WHERE id = $2::uuid
		`, attemptID, f.versionID); err != nil {
			t.Fatalf("MP4 PREVIEW validation provenance = %v, want it accepted under D-096", err)
		}
		_, err := f.pool.Exec(ctx, `
			UPDATE media_asset_versions SET state = 'READY' WHERE id = $1::uuid
		`, f.versionID)
		if err == nil || !strings.Contains(err.Error(), "invalid media asset version state transition") {
			t.Fatalf("VALIDATED -> READY on a preview = %v, want a transition refusal", err)
		}
	})

	t.Run("non-MP4 public preview cannot carry validation provenance", func(t *testing.T) {
		f := seedTrustedAsset(t, "PREVIEW", "application/pdf")
		attemptID := f.recordValidationWith(t, f.truthfulEvidence())
		_, err := f.pool.Exec(ctx, `
			UPDATE media_asset_versions SET state = 'VALIDATED', successful_validation_attempt_id = $1::uuid
			WHERE id = $2::uuid
		`, attemptID, f.versionID)
		if err == nil || !strings.Contains(err.Error(), "outside the D-088 trusted-validation profile") {
			t.Fatalf("non-MP4 PREVIEW validation provenance = %v, want a profile refusal", err)
		}
	})

	t.Run("lab material cannot carry validation provenance", func(t *testing.T) {
		f := seedTrustedAsset(t, "LAB_MATERIAL", "application/pdf")
		attemptID := f.recordValidationWith(t, f.truthfulEvidence())
		_, err := f.pool.Exec(ctx, `
			UPDATE media_asset_versions SET state = 'VALIDATED', successful_validation_attempt_id = $1::uuid
			WHERE id = $2::uuid
		`, attemptID, f.versionID)
		if err == nil || !strings.Contains(err.Error(), "outside the D-088 trusted-validation profile") {
			t.Fatalf("LAB_MATERIAL validation provenance = %v, want a profile refusal", err)
		}
	})

	t.Run("an unapproved content type cannot carry validation provenance", func(t *testing.T) {
		f := seedTrustedAsset(t, "RESOURCE", "image/png")
		attemptID := f.recordValidationWith(t, f.truthfulEvidence())
		_, err := f.pool.Exec(ctx, `
			UPDATE media_asset_versions SET state = 'VALIDATED', successful_validation_attempt_id = $1::uuid
			WHERE id = $2::uuid
		`, attemptID, f.versionID)
		if err == nil || !strings.Contains(err.Error(), "outside the D-088 trusted-validation profile") {
			t.Fatalf("image/png validation provenance = %v, want a profile refusal", err)
		}
	})

	t.Run("a validated DOCX resource reaches READY", func(t *testing.T) {
		f := seedTrustedAsset(t, "RESOURCE",
			"application/vnd.openxmlformats-officedocument.wordprocessingml.document")
		attemptID := f.recordValidationWith(t, f.truthfulEvidence())
		if _, err := f.pool.Exec(ctx, `
			UPDATE media_asset_versions SET state = 'VALIDATED', successful_validation_attempt_id = $1::uuid
			WHERE id = $2::uuid
		`, attemptID, f.versionID); err != nil {
			t.Fatalf("validating a DOCX resource: %v", err)
		}
		if _, err := f.pool.Exec(ctx, `
			UPDATE media_asset_versions SET state = 'READY' WHERE id = $1::uuid
		`, f.versionID); err != nil {
			t.Fatalf("making a validated DOCX resource READY: %v", err)
		}
		var scanEvidence *string
		if err := f.pool.QueryRow(ctx, `
			SELECT successful_scan_attempt_id::text FROM media_asset_versions WHERE id = $1::uuid
		`, f.versionID).Scan(&scanEvidence); err != nil {
			t.Fatalf("reading provenance: %v", err)
		}
		if scanEvidence != nil {
			t.Fatal("a trusted resource carries fabricated scan provenance")
		}
		var scans int
		if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM scan_attempts WHERE asset_version_id = $1::uuid`, f.versionID).Scan(&scans); err != nil {
			t.Fatalf("counting scan attempts: %v", err)
		}
		if scans != 0 {
			t.Fatalf("scan attempts = %d, want 0 for a trusted validation", scans)
		}
	})

	t.Run("a validated video cannot skip processing evidence", func(t *testing.T) {
		f := seedTrustedAsset(t, "VIDEO", "video/mp4")
		attemptID := f.recordValidationWith(t, f.truthfulEvidence())
		if _, err := f.pool.Exec(ctx, `
			UPDATE media_asset_versions SET state = 'VALIDATED', successful_validation_attempt_id = $1::uuid
			WHERE id = $2::uuid
		`, attemptID, f.versionID); err != nil {
			t.Fatalf("validating a video: %v", err)
		}
		_, err := f.pool.Exec(ctx, `UPDATE media_asset_versions SET state = 'READY' WHERE id = $1::uuid`, f.versionID)
		if err == nil {
			t.Fatal("a validated video reached READY without processing evidence")
		}
		if _, err := f.pool.Exec(ctx, `UPDATE media_asset_versions SET state = 'PROCESSING' WHERE id = $1::uuid`, f.versionID); err != nil {
			t.Fatalf("claiming a validated video for processing: %v", err)
		}
		_, err = f.pool.Exec(ctx, `UPDATE media_asset_versions SET state = 'READY' WHERE id = $1::uuid`, f.versionID)
		if err == nil || !strings.Contains(err.Error(), "lacks successful trusted processing evidence") {
			t.Fatalf("READY without FFmpeg evidence = %v, want a processing-evidence refusal", err)
		}
	})

	t.Run("validation evidence is append-only", func(t *testing.T) {
		f := seedTrustedAsset(t, "RESOURCE", "application/pdf")
		attemptID := f.recordValidationWith(t, f.truthfulEvidence())
		if _, err := f.pool.Exec(ctx, `UPDATE validation_attempts SET outcome = 'FAILED' WHERE id = $1::uuid`, attemptID); err == nil {
			t.Fatal("a validation attempt was rewritten")
		}
		if _, err := f.pool.Exec(ctx, `DELETE FROM validation_attempts WHERE id = $1::uuid`, attemptID); err == nil {
			t.Fatal("a validation attempt was deleted")
		}
	})

	t.Run("the scanner path still requires successful scan evidence", func(t *testing.T) {
		f := seedTrustedAsset(t, "RESOURCE", "image/png")
		if _, err := f.pool.Exec(ctx, `UPDATE media_asset_versions SET state = 'SCANNING' WHERE id = $1::uuid`, f.versionID); err != nil {
			t.Fatalf("entering SCANNING: %v", err)
		}
		_, err := f.pool.Exec(ctx, `UPDATE media_asset_versions SET state = 'SCAN_PASSED' WHERE id = $1::uuid`, f.versionID)
		if err == nil || !strings.Contains(err.Error(), "lacks successful exact-version scan evidence") {
			t.Fatalf("SCAN_PASSED without evidence = %v, want a scan-evidence refusal", err)
		}
	})
}
