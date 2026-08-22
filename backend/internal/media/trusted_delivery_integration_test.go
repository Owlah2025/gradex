//go:build integration

package media

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// D-088 §7 keeps a trusted Lesson Resource protected content: it is delivered
// only through the existing authenticated, Entitlement-checked signed-download
// boundary. Skipping malware scanning must not skip entitlement.

// validatedAsset seeds a READY Asset Version whose only provenance is D-088
// trusted validation — no scan attempt exists for it anywhere.
func (f *deliveryFixture) validatedAsset(t *testing.T, kind AssetKind, contentType string) string {
	t.Helper()
	assetID, versionID := uuid.NewString(), uuid.NewString()
	key := "quarantine/" + f.courseID + "/" + versionID + "/source"
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO media_assets (id, kind, owner_account_id, course_id, lesson_id, visibility)
		VALUES ($1::uuid, $2::media_asset_kind, $3::uuid, $4::uuid, $5::uuid, $6::media_asset_visibility)
	`, assetID, kind, f.instructorID, f.courseID, f.lesson, visibilityForKind(kind)); err != nil {
		t.Fatalf("seeding a validated logical asset: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO media_asset_versions (
			id, logical_asset_id, kind, state, storage_object_key, storage_object_version,
			content_type, size_bytes, sha256_hex
		) VALUES ($1::uuid, $2::uuid, $3::media_asset_kind, 'QUARANTINED', $4, 'v1', $5, 12, repeat('a', 64))
	`, versionID, assetID, kind, key, contentType); err != nil {
		t.Fatalf("seeding a validated asset version: %v", err)
	}
	// The upload intent carries the configured bound this upload was admitted
	// against. Validation provenance is bound to it, so evidence without an
	// intent describes an upload that was never admitted.
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO upload_intents (
			asset_version_id, expected_object_key, expected_content_type,
			expected_size_bytes, max_size_bytes, expires_at
		) VALUES ($1::uuid, $2, $3, 12, 52428800, now() + interval '15 minutes')
	`, versionID, key, contentType); err != nil {
		t.Fatalf("seeding the upload intent: %v", err)
	}
	var attemptID string
	if err := f.pool.QueryRow(f.ctx, `
		INSERT INTO validation_attempts (
			asset_version_id, attempt_number, work_id, storage_object_version, outcome,
			validator_identity, profile, declared_content_type, verified_size_bytes,
			max_size_bytes, sha256_hex
		) VALUES ($1::uuid, 1, $2, 'v1', 'PASSED', 'gradex-media-exact-version-validator',
			$3, $4, 12, 52428800, repeat('a', 64))
		RETURNING id::text
	`, versionID, "validation:"+versionID, TrustedValidationProfile, contentType).Scan(&attemptID); err != nil {
		t.Fatalf("seeding validation evidence: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `
		UPDATE media_asset_versions SET state = 'VALIDATED', successful_validation_attempt_id = $1::uuid
		WHERE id = $2::uuid
	`, attemptID, versionID); err != nil {
		t.Fatalf("validating the asset version: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE media_asset_versions SET state = 'READY' WHERE id = $1::uuid`, versionID); err != nil {
		t.Fatalf("making the validated asset READY: %v", err)
	}
	return versionID
}

func TestD088ValidatedResourceIsDeliverableButStillEntitlementChecked(t *testing.T) {
	f := newDeliveryFixture(t)
	validated := f.validatedAsset(t, KindResource, "application/pdf")
	var lessonRow string
	if err := f.pool.QueryRow(f.ctx, `SELECT id::text FROM course_lessons WHERE lesson_identity_id = $1::uuid`, f.lesson).Scan(&lessonRow); err != nil {
		t.Fatalf("finding the lesson row: %v", err)
	}
	// Replace the scanner-provenance fixture resource with the validated one, so
	// the Lesson's current RESOURCE resolves to trusted-validation provenance.
	if _, err := f.pool.Exec(f.ctx, `
		UPDATE lesson_files SET asset_version_id = $1::uuid WHERE lesson_id = $2::uuid AND kind = 'RESOURCE'
	`, validated, lessonRow); err != nil {
		t.Fatalf("attaching the validated resource: %v", err)
	}

	t.Run("an entitled Student receives the download", func(t *testing.T) {
		issued, err := f.delivery.IssueDownload(f.ctx, DownloadRequest{
			StudentID: f.student, LessonID: f.lesson, AssetVersionID: validated, Kind: KindResource,
		})
		if err != nil {
			t.Fatalf("issuing a validated resource download: %v", err)
		}
		if issued.AssetVersionID != validated || issued.URL == "" {
			t.Fatalf("download = %+v, want the validated version with a signed URL", issued)
		}
	})

	t.Run("an unentitled account is refused", func(t *testing.T) {
		stranger := uuid.NewString()
		if _, err := f.pool.Exec(f.ctx, `
			INSERT INTO accounts (id, normalized_email, email, role, status, display_name, locale, email_verified_at)
			VALUES ($1::uuid, $2, $2, 'STUDENT', 'ACTIVE', 'D-088 Stranger', 'en', now())
		`, stranger, "d088-stranger-"+stranger+"@example.test"); err != nil {
			t.Fatalf("seeding an unentitled Student: %v", err)
		}
		if _, err := f.delivery.IssueDownload(f.ctx, DownloadRequest{
			StudentID: stranger, LessonID: f.lesson, AssetVersionID: validated, Kind: KindResource,
		}); !errors.Is(err, ErrProtectedUnavailable) {
			t.Fatalf("unentitled download = %v, want ErrProtectedUnavailable", err)
		}
	})

	t.Run("an expired Entitlement is refused", func(t *testing.T) {
		if _, err := f.pool.Exec(f.ctx, `
			UPDATE entitlements SET access_ends_at = $1 WHERE student_account_id = $2::uuid
		`, f.now.Add(-time.Hour), f.student); err != nil {
			t.Fatalf("expiring the Entitlement: %v", err)
		}
		if _, err := f.delivery.IssueDownload(f.ctx, DownloadRequest{
			StudentID: f.student, LessonID: f.lesson, AssetVersionID: validated, Kind: KindResource,
		}); !errors.Is(err, ErrProtectedUnavailable) {
			t.Fatalf("expired download = %v, want ErrProtectedUnavailable", err)
		}
	})
}

// D-088 §7 and the amended BR-104 keep every public preview scanner-gated. A
// preview cannot even hold validation provenance, so this asserts the outcome
// end to end: a preview that only ever passed validation is never public.
func TestD088PublicPreviewStaysScannerGated(t *testing.T) {
	f := newDeliveryFixture(t)
	var attemptID string
	err := f.pool.QueryRow(f.ctx, `
		INSERT INTO validation_attempts (
			asset_version_id, attempt_number, work_id, storage_object_version, outcome,
			validator_identity, profile, declared_content_type, verified_size_bytes,
			max_size_bytes, sha256_hex
		) VALUES ($1::uuid, 1, $2, 'v1', 'PASSED', 'gradex-media-exact-version-validator',
			$3, 'video/mp4', 12, 52428800, repeat('a', 64))
		RETURNING id::text
	`, f.preview, "validation-preview:"+f.preview, TrustedValidationProfile).Scan(&attemptID)
	if err != nil {
		t.Fatalf("seeding preview validation evidence: %v", err)
	}
	// The database refuses to attach it: a PREVIEW is outside the D-088 profile.
	if _, err := f.pool.Exec(f.ctx, `
		UPDATE media_asset_versions SET successful_validation_attempt_id = $1::uuid WHERE id = $2::uuid
	`, attemptID, f.preview); err == nil {
		t.Fatal("a public preview accepted trusted-validation provenance")
	}

	// The fixture preview reached READY through a real scan, so it stays public.
	// Removing that scan provenance must remove the preview from public reach.
	if _, err := f.delivery.IssuePreview(f.ctx, f.preview); err != nil {
		t.Fatalf("a scanned preview was unavailable: %v", err)
	}
	var scanned string
	if err := f.pool.QueryRow(f.ctx, `
		SELECT successful_scan_attempt_id::text FROM media_asset_versions WHERE id = $1::uuid
	`, f.preview).Scan(&scanned); err != nil {
		t.Fatalf("reading preview scan provenance: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `
		UPDATE scan_attempts SET outcome = 'FAILED' WHERE id = $1::uuid
	`, scanned); err == nil {
		t.Fatal("scan evidence was mutable")
	}
}
