//go:build integration

package catalog

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Owlah2025/gradex/backend/internal/catalogpublic"
	"github.com/Owlah2025/gradex/backend/internal/media"
)

// The public-preview twin of the Lesson-video durability tests.
//
// D-096 put a trusted MP4 preview on the FFmpeg path, so the browser can no
// longer be the thing that attaches the preview once processing finishes.
// These tests cover what has to be true instead: the selection is durable and
// ordered, it survives the tab, and selecting early never loosens a gate.

// seedPublicPreviewUpload creates one public-preview upload for the candidate
// revision, with the D-096 trusted provenance a production TRUSTED_INSTRUCTOR
// deployment writes. An incomplete upload stops at UPLOADED with an open
// intent; a completed one is VALIDATED with a closed intent, which is exactly
// the state the combined completion operation hands to the claim.
func seedPublicPreviewUpload(
	t *testing.T,
	f *d5Fixture,
	revisionID string,
	createdAt time.Time,
	completed bool,
) string {
	t.Helper()
	assetID, versionID := uuid.NewString(), uuid.NewString()
	key := "quarantine/" + f.courseID + "/" + versionID + "/source"
	checksum := strings.Repeat("a", 64)
	if _, err := f.p.Exec(f.ctx, `
		INSERT INTO media_assets (
			id, kind, owner_account_id, course_id, preview_origin_revision_id, visibility, created_at
		) VALUES ($1::uuid, 'PREVIEW', $2::uuid, $3::uuid, $4::uuid, 'PUBLIC_PREVIEW', $5)
	`, assetID, f.ownerID, f.courseID, revisionID, createdAt); err != nil {
		t.Fatalf("seeding public preview asset: %v", err)
	}
	if _, err := f.p.Exec(f.ctx, `
		INSERT INTO media_asset_versions (
			id, logical_asset_id, kind, state, storage_object_key, storage_object_version,
			content_type, size_bytes, sha256_hex, created_at
		) VALUES ($1::uuid, $2::uuid, 'PREVIEW', 'UPLOADED', $3, 'fixture-v1',
			'video/mp4', 1024, $4, $5)
	`, versionID, assetID, key, func() any {
		if completed {
			return checksum
		}
		return nil
	}(), createdAt); err != nil {
		t.Fatalf("seeding public preview version: %v", err)
	}

	var completedAt, fingerprint any
	if completed {
		completedAt = createdAt.Add(time.Minute)
		fingerprint = "completed:" + versionID
	}
	if _, err := f.p.Exec(f.ctx, `
		INSERT INTO upload_intents (
			asset_version_id, expected_object_key, expected_content_type, expected_size_bytes,
			max_size_bytes, expires_at, completed_at, completion_fingerprint, created_at
		) VALUES ($1::uuid, $2, 'video/mp4', 1024, 2048, $3, $4, $5, $6)
	`, versionID, key, createdAt.Add(time.Hour), completedAt, fingerprint, createdAt); err != nil {
		t.Fatalf("seeding public preview upload intent: %v", err)
	}
	if !completed {
		return versionID
	}

	if _, err := f.p.Exec(f.ctx, `
		UPDATE media_asset_versions SET state = 'QUARANTINED' WHERE id = $1::uuid
	`, versionID); err != nil {
		t.Fatalf("quarantining the public preview: %v", err)
	}
	var attemptID string
	if err := f.p.QueryRow(f.ctx, `
		INSERT INTO validation_attempts (
			asset_version_id, attempt_number, work_id, storage_object_version, outcome,
			validator_identity, profile, declared_content_type, verified_size_bytes,
			max_size_bytes, sha256_hex
		) VALUES ($1::uuid, 1, $2, 'fixture-v1', 'PASSED', 'gradex-media-exact-version-validator',
			$3, 'video/mp4', 1024, 2048, $4)
		RETURNING id::text
	`, versionID, "validation:"+versionID, media.TrustedValidationProfile, checksum).Scan(&attemptID); err != nil {
		t.Fatalf("seeding public preview validation evidence: %v", err)
	}
	if _, err := f.p.Exec(f.ctx, `
		UPDATE media_asset_versions
		SET state = 'VALIDATED', successful_validation_attempt_id = $1::uuid
		WHERE id = $2::uuid
	`, attemptID, versionID); err != nil {
		t.Fatalf("validating the public preview: %v", err)
	}
	return versionID
}

func claimPublicPreview(t *testing.T, f *d5Fixture, revisionID, versionID string) *PublicPreviewUploadClaim {
	t.Helper()
	claim, err := f.repo.ClaimPublicPreviewUpload(f.ctx, ClaimPublicPreviewUploadRequest{
		CourseID: f.courseID, RevisionID: revisionID,
		PreviewAssetVersionID: versionID, OwnerAccountID: f.ownerID,
	}, f.ownerID)
	if err != nil {
		t.Fatalf("ClaimPublicPreviewUpload: %v", err)
	}
	return claim
}

func selectedPublicPreview(t *testing.T, f *d5Fixture, revisionID string) *string {
	t.Helper()
	var selected *string
	if err := f.p.QueryRow(f.ctx, `
		SELECT preview_asset_version_id::text FROM course_revisions WHERE id = $1::uuid
	`, revisionID).Scan(&selected); err != nil {
		t.Fatalf("reading selected public preview: %v", err)
	}
	return selected
}

// previewFromEditableReadModel is what a reloading browser actually sees.
func previewFromEditableReadModel(t *testing.T, f *d5Fixture) *CourseRevision {
	t.Helper()
	course, err := f.repo.GetOwnedCourse(f.ctx, f.courseID, f.ownerID)
	if err != nil {
		t.Fatalf("GetOwnedCourse: %v", err)
	}
	if course.EditableRevision == nil {
		t.Fatal("editable Course revision is missing")
	}
	return course.EditableRevision
}

// transcodePublicPreview runs the worker over a claimed preview, which is the
// only way a trusted preview can reach READY.
func transcodePublicPreview(t *testing.T, f *d5Fixture, versionID string, processor lessonVideoProcessor) error {
	t.Helper()
	worker := lessonVideoWorker(t, f, processor)
	return worker.Transcode(f.ctx, versionID, "preview-operation:"+versionID)
}

func TestPublicPreviewUploadClaimIsDurableAndOrdered(t *testing.T) {
	t.Run("a completed upload is selected while it is still processing", func(t *testing.T) {
		f := newD5Fixture(t)
		candidate := f.candidate(t)
		versionID := seedPublicPreviewUpload(t, f, candidate.ID, time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC), true)

		claim := claimPublicPreview(t, f, candidate.ID, versionID)
		if !claim.Selected || claim.Revision == nil {
			t.Fatalf("claim = %+v, want a selected preview and a revision projection", claim)
		}
		if selected := selectedPublicPreview(t, f, candidate.ID); selected == nil || *selected != versionID {
			t.Fatalf("selected preview = %v, want %s", selected, versionID)
		}
		// Selected, but not yet processed — that is the whole point.
		if claim.Revision.PreviewAssetState == nil || *claim.Revision.PreviewAssetState != "VALIDATED" {
			t.Fatalf("projected preview state = %v, want VALIDATED", claim.Revision.PreviewAssetState)
		}
		// A replay of the same claim converges instead of erroring or double-writing.
		again := claimPublicPreview(t, f, candidate.ID, versionID)
		if !again.Selected {
			t.Fatal("re-claiming the already-selected preview did not converge on success")
		}
		var audits int
		if err := f.p.QueryRow(f.ctx, `
			SELECT count(*) FROM audit_events
			WHERE action = 'PREVIEW_UPLOAD_SELECTED' AND target_id = $1
		`, candidate.ID).Scan(&audits); err != nil {
			t.Fatalf("counting preview selection audits: %v", err)
		}
		if audits != 1 {
			t.Fatalf("preview selection audits = %d, want exactly 1 for a converged retry", audits)
		}
	})

	t.Run("an unfinished replacement upload leaves the current preview selected", func(t *testing.T) {
		f := newD5Fixture(t)
		candidate := f.candidate(t)
		current := seedPublicPreviewUpload(t, f, candidate.ID, time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC), true)
		claimPublicPreview(t, f, candidate.ID, current)

		incomplete := seedPublicPreviewUpload(t, f, candidate.ID, time.Date(2026, 9, 2, 10, 5, 0, 0, time.UTC), false)
		_, err := f.repo.ClaimPublicPreviewUpload(f.ctx, ClaimPublicPreviewUploadRequest{
			CourseID: f.courseID, RevisionID: candidate.ID,
			PreviewAssetVersionID: incomplete, OwnerAccountID: f.ownerID,
		}, f.ownerID)
		if !errors.Is(err, ErrAssetVersionInvalid) {
			t.Fatalf("incomplete claim error = %v, want %v", err, ErrAssetVersionInvalid)
		}
		if selected := selectedPublicPreview(t, f, candidate.ID); selected == nil || *selected != current {
			t.Fatalf("selected preview = %v, want the existing %s", selected, current)
		}
	})

	t.Run("a preview uploaded for another revision cannot be claimed", func(t *testing.T) {
		f := newD5Fixture(t)
		candidate := f.candidate(t)
		// Uploaded for the live revision rather than this candidate. Inheriting
		// such a preview is the semantic set command's business, not this
		// command's: a claim only ever selects bytes uploaded for this revision.
		foreign := seedPublicPreviewUpload(t, f, f.liveID, time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC), true)

		_, err := f.repo.ClaimPublicPreviewUpload(f.ctx, ClaimPublicPreviewUploadRequest{
			CourseID: f.courseID, RevisionID: candidate.ID,
			PreviewAssetVersionID: foreign, OwnerAccountID: f.ownerID,
		}, f.ownerID)
		if !errors.Is(err, ErrAssetVersionInvalid) {
			t.Fatalf("cross-revision claim error = %v, want %v", err, ErrAssetVersionInvalid)
		}
	})

	t.Run("a non-owner cannot claim a preview for the revision", func(t *testing.T) {
		f := newD5Fixture(t)
		candidate := f.candidate(t)
		before := selectedPublicPreview(t, f, candidate.ID)
		versionID := seedPublicPreviewUpload(t, f, candidate.ID, time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC), true)

		_, err := f.repo.ClaimPublicPreviewUpload(f.ctx, ClaimPublicPreviewUploadRequest{
			CourseID: f.courseID, RevisionID: candidate.ID,
			PreviewAssetVersionID: versionID, OwnerAccountID: f.adminID,
		}, f.adminID)
		if !errors.Is(err, ErrCourseNotFound) {
			t.Fatalf("non-owner claim error = %v, want %v", err, ErrCourseNotFound)
		}
		after := selectedPublicPreview(t, f, candidate.ID)
		if !sameOptionalPreview(before, after) {
			t.Fatalf("a non-owner claim changed the selected preview from %v to %v",
				optionalPreview(before), optionalPreview(after))
		}
		if after != nil && *after == versionID {
			t.Fatal("a non-owner claim selected the uploaded preview")
		}
	})

	t.Run("an older completion cannot replace a newer completed preview", func(t *testing.T) {
		f := newD5Fixture(t)
		candidate := f.candidate(t)
		older := seedPublicPreviewUpload(t, f, candidate.ID, time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC), true)
		newer := seedPublicPreviewUpload(t, f, candidate.ID, time.Date(2026, 9, 2, 10, 1, 0, 0, time.UTC), true)

		if claim := claimPublicPreview(t, f, candidate.ID, newer); !claim.Selected {
			t.Fatal("the newer completed upload was not selected")
		}
		// The older upload's completion arrives late — it must lose, and say so
		// rather than fail.
		claim := claimPublicPreview(t, f, candidate.ID, older)
		if claim.Selected {
			t.Fatal("an older late completion replaced the newer preview")
		}
		if selected := selectedPublicPreview(t, f, candidate.ID); selected == nil || *selected != newer {
			t.Fatalf("selected preview = %v, want the newer %s", selected, newer)
		}
	})
}

func TestPublicPreviewClaimSurvivesBrowserExitAndProjectsWorkerState(t *testing.T) {
	t.Run("processing success becomes READY in the reloaded authoring graph", func(t *testing.T) {
		f := newD5Fixture(t)
		candidate := f.candidate(t)
		versionID := seedPublicPreviewUpload(t, f, candidate.ID, time.Date(2026, 9, 2, 11, 0, 0, 0, time.UTC), true)
		claimPublicPreview(t, f, candidate.ID, versionID)

		// The browser is gone by now; the worker finishes on its own.
		if err := transcodePublicPreview(t, f, versionID, lessonVideoProcessor{}); err != nil {
			t.Fatalf("Worker.Transcode: %v", err)
		}
		revision := previewFromEditableReadModel(t, f)
		if revision.PreviewAssetVersionID == nil || *revision.PreviewAssetVersionID != versionID {
			t.Fatalf("reloaded preview = %v, want %s", revision.PreviewAssetVersionID, versionID)
		}
		if revision.PreviewAssetState == nil || *revision.PreviewAssetState != "READY" {
			t.Fatalf("reloaded preview state = %v, want READY", revision.PreviewAssetState)
		}
	})

	t.Run("processing failure becomes PROCESS_FAILED in the reloaded authoring graph", func(t *testing.T) {
		f := newD5Fixture(t)
		candidate := f.candidate(t)
		versionID := seedPublicPreviewUpload(t, f, candidate.ID, time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC), true)
		claimPublicPreview(t, f, candidate.ID, versionID)

		processingFailure := errors.New("fixture transcoder failed")
		if err := transcodePublicPreview(t, f, versionID, lessonVideoProcessor{failure: processingFailure}); !errors.Is(err, processingFailure) {
			t.Fatalf("Worker.Transcode error = %v, want the processing failure", err)
		}
		revision := previewFromEditableReadModel(t, f)
		if revision.PreviewAssetState == nil || *revision.PreviewAssetState != "PROCESS_FAILED" {
			t.Fatalf("reloaded preview state = %v, want PROCESS_FAILED", revision.PreviewAssetState)
		}
		// A failed preview stays selected on the draft so the Instructor can see
		// what happened; the gates below are what keep it from being published.
		if selected := selectedPublicPreview(t, f, candidate.ID); selected == nil || *selected != versionID {
			t.Fatalf("selected preview = %v, want the failed %s", selected, versionID)
		}
	})
}

func TestTrustedPublicPreviewLifecycleProjectsIntoPublicCatalogue(t *testing.T) {
	f := newD5Fixture(t)
	candidate := f.candidate(t)
	versionID := seedPublicPreviewUpload(
		t, f, candidate.ID, time.Date(2026, 9, 2, 15, 0, 0, 0, time.UTC), true,
	)
	claimPublicPreview(t, f, candidate.ID, versionID)
	if err := transcodePublicPreview(t, f, versionID, lessonVideoProcessor{}); err != nil {
		t.Fatalf("Worker.Transcode: %v", err)
	}
	if _, err := f.repo.SubmitCourse(f.ctx, f.validator, SubmitCourseRequest{
		CourseID: f.courseID, RevisionID: candidate.ID,
		OwnerAccountID: f.ownerID, ActorDescriptor: f.ownerID,
	}); err != nil {
		t.Fatalf("submitting trusted-preview revision: %v", err)
	}
	if _, err := f.repo.ApproveCourse(f.ctx, f.validator, ApproveCourseRequest{
		CourseID: f.courseID, RevisionID: candidate.ID,
		AdminAccountID: f.adminID, ActorDescriptor: f.adminID,
	}); err != nil {
		t.Fatalf("approving trusted-preview revision: %v", err)
	}

	publicRepository, err := catalogpublic.NewRepository(f.p, catalogpublic.PublishedOnly)
	if err != nil {
		t.Fatalf("constructing public catalogue repository: %v", err)
	}
	detail, err := publicRepository.Detail(f.ctx, f.courseID, false)
	if err != nil || detail == nil || !detail.HasPreview {
		t.Fatalf("public catalogue Detail = %#v, %v; want has_preview=true", detail, err)
	}
	list, err := publicRepository.List(f.ctx, false, 1, 10)
	if err != nil {
		t.Fatalf("public catalogue List: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].ID != f.courseID || !list.Items[0].HasPreview {
		t.Fatalf("public catalogue List = %#v; want the trusted-preview Course with has_preview=true", list)
	}
}

// Selecting early must not loosen anything. A preview that is merely selected
// is not a preview that may be published.
func TestNonReadySelectedPublicPreviewCannotPassLifecycleGates(t *testing.T) {
	for name, prepare := range map[string]func(*testing.T, *d5Fixture, string){
		"still processing": func(_ *testing.T, _ *d5Fixture, _ string) {},
		"processing failed": func(t *testing.T, f *d5Fixture, versionID string) {
			failure := errors.New("fixture transcoder failed")
			if err := transcodePublicPreview(t, f, versionID, lessonVideoProcessor{failure: failure}); !errors.Is(err, failure) {
				t.Fatalf("Worker.Transcode error = %v, want the processing failure", err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newD5Fixture(t)
			candidate := f.candidate(t)
			versionID := seedPublicPreviewUpload(t, f, candidate.ID, time.Date(2026, 9, 2, 13, 0, 0, 0, time.UTC), true)
			claimPublicPreview(t, f, candidate.ID, versionID)
			prepare(t, f, versionID)

			_, err := f.repo.SubmitCourse(f.ctx, f.validator, SubmitCourseRequest{
				CourseID: f.courseID, RevisionID: candidate.ID,
				OwnerAccountID: f.ownerID, ActorDescriptor: f.ownerID,
			})
			assertSubmissionFailure(t, err)

			if _, err := f.p.Exec(f.ctx, `
				UPDATE course_revisions SET state = 'PENDING_REVIEW' WHERE id = $1::uuid
			`, candidate.ID); err != nil {
				t.Fatalf("placing candidate under review: %v", err)
			}
			_, err = f.repo.ApproveCourse(f.ctx, f.validator, ApproveCourseRequest{
				CourseID: f.courseID, RevisionID: candidate.ID,
				AdminAccountID: f.adminID, ActorDescriptor: f.adminID,
			})
			assertSubmissionFailure(t, err)
		})
	}
}

// The semantic set command is unchanged and still READY-only, so nothing about
// the new early selection lets an unprocessed preview through that door either.
func TestSetPreviewAssetStillRequiresReadyMedia(t *testing.T) {
	f := newD5Fixture(t)
	candidate := f.candidate(t)
	versionID := seedPublicPreviewUpload(t, f, candidate.ID, time.Date(2026, 9, 2, 14, 0, 0, 0, time.UTC), true)

	if _, err := f.repo.SetPreviewAsset(f.ctx, f.validator, PreviewAssetRequest{
		CourseID: f.courseID, RevisionID: candidate.ID,
		PreviewAssetVersionID: versionID, OwnerAccountID: f.ownerID,
	}, f.ownerID); !errors.Is(err, ErrAssetVersionInvalid) {
		t.Fatalf("SetPreviewAsset on a VALIDATED preview = %v, want %v", err, ErrAssetVersionInvalid)
	}

	claimPublicPreview(t, f, candidate.ID, versionID)
	if err := transcodePublicPreview(t, f, versionID, lessonVideoProcessor{}); err != nil {
		t.Fatalf("Worker.Transcode: %v", err)
	}
	selected, err := f.repo.SetPreviewAsset(f.ctx, f.validator, PreviewAssetRequest{
		CourseID: f.courseID, RevisionID: candidate.ID,
		PreviewAssetVersionID: versionID, OwnerAccountID: f.ownerID,
	}, f.ownerID)
	if err != nil || selected.PreviewAssetVersionID == nil || *selected.PreviewAssetVersionID != versionID {
		t.Fatalf("SetPreviewAsset on the processed preview = %+v err=%v", selected, err)
	}
}

func sameOptionalPreview(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func optionalPreview(value *string) string {
	if value == nil {
		return "<none>"
	}
	return *value
}
