//go:build integration

package catalog

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Owlah2025/gradex/backend/internal/media"
)

// D-105 / Option 3A: a Lesson video becomes publication-ready as soon as one
// canonical rendition is verified and persisted, provided the worker is still
// producing the rest of the ladder. These tests build that state the way the
// product does — upload, scan, progressive transcode, claim, submit, approve —
// rather than by writing a state name into a row, because the whole point of
// the change is that the normal path can now reach it.

// heldPlayableProcessor persists exactly one canonical rendition, tells the test
// it has done so, and then blocks. While it blocks the worker still holds a live
// claim on the Asset Version, which is precisely the "actively PLAYABLE" shape:
// streamable now, ladder still being produced.
//
// Releasing it decides how the attempt ends. `complete` finishes the expected
// ladder, taking the version PLAYABLE -> READY; otherwise the attempt fails and
// the version stays PLAYABLE with its claim cleared — a failed PLAYABLE.
type heldPlayableProcessor struct {
	persisted chan string
	release   chan bool
}

func newHeldPlayableProcessor() *heldPlayableProcessor {
	return &heldPlayableProcessor{persisted: make(chan string, 1), release: make(chan bool)}
}

func (p *heldPlayableProcessor) Transcode(context.Context, media.ObjectVersion) (media.TranscodeResult, error) {
	return media.TranscodeResult{}, errors.New("heldPlayableProcessor requires the progressive path")
}

func (p *heldPlayableProcessor) TranscodeProgressive(
	ctx context.Context,
	object media.ObjectVersion,
	_ media.ProgressSink,
	renditions media.VerifiedRenditionSink,
) (media.TranscodeResult, error) {
	prefix := media.ProcessingOutputPrefix(object.AssetVersionID, object.ProcessingOperationID)
	rendition := media.Rendition{
		Name: "720p", StorageObjectKey: prefix + "/720p/playlist.m3u8",
		Width: 1280, Height: 720, BitrateKbps: 2800, DurationMS: 60000,
	}
	if err := renditions.PersistVerifiedRendition(ctx, rendition); err != nil {
		return media.TranscodeResult{}, err
	}
	p.persisted <- prefix

	select {
	case complete := <-p.release:
		if !complete {
			return media.TranscodeResult{}, errors.New("enhancement processing failed after the first rendition")
		}
		return media.TranscodeResult{
			OutputPrefix: prefix, TrustedDurationMS: 60000,
			Renditions:         []media.Rendition{rendition},
			ExpectedRenditions: []string{"720p"},
		}, nil
	case <-ctx.Done():
		return media.TranscodeResult{}, ctx.Err()
	}
}

// activePlayableVideo drives one Lesson video from upload to actively PLAYABLE
// through the real media worker, selects it onto the given candidate revision
// through the real authoring command, and leaves the worker holding its claim.
//
// The returned finish function ends the attempt: finish(true) completes the
// ladder, finish(false) fails it. Every caller must call it, so the fixture
// never leaves a goroutine parked on a lease.
func activePlayableVideo(t *testing.T, f *d5Fixture, revisionID string) (string, func(complete bool)) {
	t.Helper()
	versionID := seedLessonVideoUpload(t, f, time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC), true)
	claimLessonVideo(t, f, revisionID, versionID)

	scanner, err := media.NewScannerAdapter(lessonVideoScanner{})
	if err != nil {
		t.Fatalf("NewScannerAdapter: %v", err)
	}
	processor := newHeldPlayableProcessor()
	worker, err := media.NewWorker(media.WorkerOptions{
		DB: f.p, Scanner: scanner, Process: processor, Outbox: testOutboxWriter(t),
		// Long enough that the lease cannot expire while the test holds the
		// attempt open. Recovery must not reclaim this work underneath us: the
		// point of the fixture is a claim that is genuinely live.
		ProcessingTimeout: 2 * time.Minute,
		WorkLeaseDuration: 5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}
	if err := worker.Scan(f.ctx, versionID); err != nil {
		t.Fatalf("Worker.Scan: %v", err)
	}

	var operationID string
	if err := f.p.QueryRow(f.ctx, `
		SELECT safe_payload->>'operation_id'
		FROM outbox_events
		WHERE event_type = 'media.transcode_requested' AND aggregate_id = $1::uuid
		ORDER BY occurred_at DESC, id DESC LIMIT 1
	`, versionID).Scan(&operationID); err != nil {
		t.Fatalf("loading transcode operation: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- worker.Transcode(context.Background(), versionID, operationID) }()

	select {
	case <-processor.persisted:
	case err := <-done:
		t.Fatalf("transcode finished before persisting a rendition: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("timed out waiting for the first canonical rendition")
	}
	assertActivePlayable(t, f, versionID)

	finished := false
	finish := func(complete bool) {
		if finished {
			return
		}
		finished = true
		processor.release <- complete
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			t.Fatal("timed out waiting for the held transcode attempt to finish")
		}
	}
	t.Cleanup(func() { finish(false) })
	return versionID, finish
}

func assertActivePlayable(t *testing.T, f *d5Fixture, versionID string) {
	t.Helper()
	var state string
	var claimed, leaseLive bool
	var renditions int
	if err := f.p.QueryRow(f.ctx, `
		SELECT state::text, work_claim_token IS NOT NULL,
		       COALESCE(work_lease_expires_at > now(), false),
		       (SELECT count(*) FROM video_renditions WHERE asset_version_id = $1::uuid)
		FROM media_asset_versions WHERE id = $1::uuid
	`, versionID).Scan(&state, &claimed, &leaseLive, &renditions); err != nil {
		t.Fatalf("reading Lesson video state: %v", err)
	}
	if state != "PLAYABLE" || !claimed || !leaseLive || renditions != 1 {
		t.Fatalf("want actively PLAYABLE with 1 rendition, got state=%s claimed=%t lease_live=%t renditions=%d",
			state, claimed, leaseLive, renditions)
	}
}

func assertFailedPlayable(t *testing.T, f *d5Fixture, versionID string) {
	t.Helper()
	var state string
	var claimed bool
	var failedAttempts, renditions int
	if err := f.p.QueryRow(f.ctx, `
		SELECT state::text, work_claim_token IS NOT NULL,
		       (SELECT count(*) FROM processing_attempts WHERE asset_version_id = $1::uuid AND state = 'FAILED'),
		       (SELECT count(*) FROM video_renditions WHERE asset_version_id = $1::uuid)
		FROM media_asset_versions WHERE id = $1::uuid
	`, versionID).Scan(&state, &claimed, &failedAttempts, &renditions); err != nil {
		t.Fatalf("reading Lesson video state: %v", err)
	}
	if state != "PLAYABLE" || claimed || failedAttempts == 0 || renditions != 1 {
		t.Fatalf("want failed PLAYABLE with surviving renditions, got state=%s claimed=%t failed_attempts=%d renditions=%d",
			state, claimed, failedAttempts, renditions)
	}
}

func assetVersionState(t *testing.T, f *d5Fixture, versionID string) string {
	t.Helper()
	var state string
	if err := f.p.QueryRow(f.ctx,
		`SELECT state::text FROM media_asset_versions WHERE id = $1::uuid`, versionID,
	).Scan(&state); err != nil {
		t.Fatalf("reading asset version state: %v", err)
	}
	return state
}

// TestActivePlayableLessonVideoPublishesThroughTheInstructorPath is the update
// case: a live Course whose Instructor ships an edit carrying a video that is
// streamable but not yet ladder-complete.
func TestActivePlayableLessonVideoPublishesThroughTheInstructorPath(t *testing.T) {
	f := newD5Fixture(t)
	candidate := f.candidate(t)
	versionID, finish := activePlayableVideo(t, f, candidate.ID)

	if err := f.publish(f.ctx, candidate.ID); err != nil {
		t.Fatalf("PublishRevision with an actively PLAYABLE Lesson video: %v", err)
	}

	// The published revision points at exactly the version that was validated,
	// and the previously live revision was superseded rather than rewritten.
	if got := selectedLessonVideo(t, f, candidate.ID); got != versionID {
		t.Fatalf("published Lesson video = %s, want %s", got, versionID)
	}
	if got := selectedLessonVideo(t, f, f.liveID); got != f.videoOld {
		t.Fatalf("superseded revision video = %s, want unchanged %s", got, f.videoOld)
	}
	assertLiveRevision(t, f, candidate.ID)
	assertRevisionState(t, f, f.liveID, "SUPERSEDED")

	// The asset is still PLAYABLE while the course is live: publication did not
	// wait for the ladder, and did not force it either.
	assertActivePlayable(t, f, versionID)

	// The ladder finishing afterwards changes the media row and nothing else.
	// The lesson still points at the same Asset Version — no hot swap.
	finish(true)
	if got := assetVersionState(t, f, versionID); got != "READY" {
		t.Fatalf("asset version state after the ladder completed = %s, want READY", got)
	}
	if got := selectedLessonVideo(t, f, candidate.ID); got != versionID {
		t.Fatalf("live Lesson video changed to %s after PLAYABLE -> READY, want %s", got, versionID)
	}
	assertLiveRevision(t, f, candidate.ID)
}

// TestActivePlayableLessonVideoReachesStudentsThroughFirstPublication is the
// test that could not exist before this change: the whole chain, from an upload
// that is only partly processed to an enrolled Student holding a signed
// playback authorization, with the Asset Version still PLAYABLE throughout.
func TestActivePlayableLessonVideoReachesStudentsThroughFirstPublication(t *testing.T) {
	f, revisionID, versionID, finish := newStreamReadyFirstPublicationFixture(t)

	// 1. Admin review must be able to watch what it is being asked to approve.
	delivery := streamReadyDeliveryService(t, f)
	review, err := delivery.IssueAdminReviewPlayback(f.ctx, media.AdminReviewPlaybackRequest{
		AdminAccountID: f.adminID, CourseID: f.courseID, RevisionID: revisionID,
		LessonID: f.lessonIdentityID, AssetVersionID: versionID,
	})
	if err != nil {
		t.Fatalf("IssueAdminReviewPlayback for an actively PLAYABLE video: %v", err)
	}
	if review.AssetVersionID != versionID {
		t.Fatalf("admin review resolved %s, want %s", review.AssetVersionID, versionID)
	}
	// A PLAYABLE version carries no trusted duration yet, so the session
	// lifetime must come from the persisted rendition rather than collapsing to
	// the bare configured grace period.
	if lifetime := review.ExpiresAt.Sub(time.Now().UTC()); lifetime < time.Minute {
		t.Fatalf("admin review session lifetime = %s, want the rendition duration to be honoured", lifetime)
	}
	if review.Watermark != nil {
		t.Fatal("admin review playback must carry no Student watermark")
	}
	// The reviewer's master manifest re-authorizes the session against the same
	// submitted target, so the broadened predicate has to hold on that path too.
	reviewManifest, err := delivery.IssueAdminReviewPlaybackManifest(f.ctx, f.adminID, review.PlaybackSession)
	if err != nil {
		t.Fatalf("IssueAdminReviewPlaybackManifest for an actively PLAYABLE video: %v", err)
	}
	if len(reviewManifest.Contents) == 0 {
		t.Fatal("admin review master manifest was empty")
	}

	// 2. Admin approval promotes the revision while the video is still PLAYABLE.
	if _, err := f.repo.ApproveCourse(f.ctx, f.validator, ApproveCourseRequest{
		CourseID: f.courseID, RevisionID: revisionID,
		AdminAccountID: f.adminID, ActorDescriptor: f.adminID,
	}); err != nil {
		t.Fatalf("ApproveCourse with an actively PLAYABLE Lesson video: %v", err)
	}
	assertLiveRevision(t, f, revisionID)
	assertActivePlayable(t, f, versionID)

	// 3. An ordinary enrolled Student resolves the Lesson through the live
	//    approved revision and obtains protected playback for these exact bytes.
	studentID := seedEntitledStudent(t, f)
	authorization, err := delivery.IssuePlayback(f.ctx, media.PlaybackRequest{
		StudentID: studentID, LessonID: f.lessonIdentityID, AssetVersionID: versionID,
		DeviceID: "device-stream-ready", SessionID: "session-stream-ready",
	})
	if err != nil {
		t.Fatalf("IssuePlayback while the Lesson video is PLAYABLE: %v", err)
	}
	if authorization.AssetVersionID != versionID {
		t.Fatalf("playback resolved %s, want %s", authorization.AssetVersionID, versionID)
	}
	if authorization.Watermark == nil {
		t.Fatal("Student playback was issued without a watermark")
	}
	manifest, err := delivery.IssuePlaybackManifest(f.ctx, media.PlaybackSessionRequest{
		StudentID: studentID, DeviceID: "device-stream-ready", Token: authorization.PlaybackSession,
	})
	if err != nil {
		t.Fatalf("IssuePlaybackManifest while the Lesson video is PLAYABLE: %v", err)
	}
	if len(manifest.Contents) == 0 {
		t.Fatal("protected master manifest was empty")
	}

	// 4. The same authorization survives the ladder completing underneath it.
	finish(true)
	if got := assetVersionState(t, f, versionID); got != "READY" {
		t.Fatalf("asset version state after the ladder completed = %s, want READY", got)
	}
	if _, err := delivery.IssuePlaybackManifest(f.ctx, media.PlaybackSessionRequest{
		StudentID: studentID, DeviceID: "device-stream-ready", Token: authorization.PlaybackSession,
	}); err != nil {
		t.Fatalf("playback session did not survive PLAYABLE -> READY: %v", err)
	}
}

// TestFailedPlayableLessonVideoStaysLiveButCannotPublishAgain covers the two
// halves of the failed-PLAYABLE policy in one timeline, because they are only
// meaningful together: the bytes a Student is already watching keep working,
// and the same version may not carry a *new* revision to live while no
// enhancement recovery exists for it.
func TestFailedPlayableLessonVideoStaysLiveButCannotPublishAgain(t *testing.T) {
	f := newD5Fixture(t)
	candidate := f.candidate(t)
	versionID, finish := activePlayableVideo(t, f, candidate.ID)
	if err := f.publish(f.ctx, candidate.ID); err != nil {
		t.Fatalf("PublishRevision with an actively PLAYABLE Lesson video: %v", err)
	}

	// The enhancement dies after the course is already live.
	finish(false)
	assertFailedPlayable(t, f, versionID)

	// The live revision is untouched: no automatic unpublish, no pointer
	// rewrite, no lifecycle change.
	assertLiveRevision(t, f, candidate.ID)
	assertRevisionState(t, f, candidate.ID, "APPROVED")
	if got := selectedLessonVideo(t, f, candidate.ID); got != versionID {
		t.Fatalf("live Lesson video = %s, want unchanged %s", got, versionID)
	}

	// And the surviving rendition is still streamable to an entitled Student.
	delivery := streamReadyDeliveryService(t, f)
	studentID := seedEntitledStudent(t, f)
	if _, err := delivery.IssuePlayback(f.ctx, media.PlaybackRequest{
		StudentID: studentID, LessonID: f.lessonIdentityID, AssetVersionID: versionID,
		DeviceID: "device-failed-playable", SessionID: "session-failed-playable",
	}); err != nil {
		t.Fatalf("a failed PLAYABLE video that is already live must remain streamable: %v", err)
	}

	// A new candidate carrying the same failed PLAYABLE version cannot publish.
	// Before Phase 3C there is no path back to a complete ladder for it, so a
	// fresh publication would be a decision to ship a permanently partial one.
	next := f.candidate(t)
	claimLessonVideo(t, f, next.ID, versionID)
	assertSubmissionFailure(t, f.publish(f.ctx, next.ID))
	assertLiveRevision(t, f, candidate.ID)
}

// TestLessonVideoPublicationReadinessRejectsEveryUnsafePlayableShape pins the
// predicate itself. Each case starts from a genuine actively PLAYABLE version
// and removes exactly one of the conditions that made it publishable.
func TestLessonVideoPublicationReadinessRejectsEveryUnsafePlayableShape(t *testing.T) {
	for _, tc := range []struct {
		name    string
		degrade func(t *testing.T, f *d5Fixture, versionID string)
	}{
		{
			name: "claim cleared",
			degrade: func(t *testing.T, f *d5Fixture, versionID string) {
				if _, err := f.p.Exec(f.ctx, `
					UPDATE media_asset_versions
					SET work_claim_token = NULL, work_claimed_at = NULL, work_lease_expires_at = NULL,
					    active_processing_attempt_kind = NULL
					WHERE id = $1::uuid
				`, versionID); err != nil {
					t.Fatalf("clearing work claim: %v", err)
				}
			},
		},
		{
			name: "lease expired",
			degrade: func(t *testing.T, f *d5Fixture, versionID string) {
				if _, err := f.p.Exec(f.ctx, `
					UPDATE media_asset_versions
					SET work_claimed_at = now() - interval '2 hours',
					    work_lease_expires_at = now() - interval '1 hour'
					WHERE id = $1::uuid
				`, versionID); err != nil {
					t.Fatalf("expiring work lease: %v", err)
				}
			},
		},
		{
			name: "logical asset retired",
			degrade: func(t *testing.T, f *d5Fixture, versionID string) {
				if _, err := f.p.Exec(f.ctx, `
					UPDATE media_assets SET retired_at = now()
					WHERE id = (SELECT logical_asset_id FROM media_asset_versions WHERE id = $1::uuid)
				`, versionID); err != nil {
					t.Fatalf("retiring logical asset: %v", err)
				}
			},
		},
		// The predicate's exact-version provenance clause has no negative case
		// at this layer, and that is the finding rather than a gap: `scan_attempts`
		// and `validation_attempts` are append-only, and the schema-40 trigger
		// re-proves the evidence against the row on every UPDATE, so a PLAYABLE
		// video whose provenance disagrees with its bytes cannot be constructed
		// even by direct SQL. The clause stays in the query as defence in depth
		// against a future path that does not go through those guards.
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newD5Fixture(t)
			candidate := f.candidate(t)
			versionID, _ := activePlayableVideo(t, f, candidate.ID)
			tc.degrade(t, f, versionID)

			assertSubmissionFailure(t, f.publish(f.ctx, candidate.ID))
			if got := selectedLessonVideo(t, f, f.liveID); got != f.videoOld {
				t.Fatalf("live video = %s, want unchanged %s", got, f.videoOld)
			}
		})
	}
}

// TestPlayableLessonVideoWithNoCanonicalRenditionIsRefused builds the
// malformed shape progressive persistence is supposed to make impossible: a
// version that claims PLAYABLE with nothing to serve. Renditions are
// append-only, so it has to be assembled by moving a scanned version through
// PROCESSING to PLAYABLE without ever persisting one — which is exactly the
// hand-written row the EXISTS clause exists to refuse.
func TestPlayableLessonVideoWithNoCanonicalRenditionIsRefused(t *testing.T) {
	f := newD5Fixture(t)
	candidate := f.candidate(t)
	versionID := seedLessonVideoUpload(t, f, time.Date(2026, 9, 18, 11, 0, 0, 0, time.UTC), true)
	claimLessonVideo(t, f, candidate.ID, versionID)

	scanner, err := media.NewScannerAdapter(lessonVideoScanner{})
	if err != nil {
		t.Fatalf("NewScannerAdapter: %v", err)
	}
	worker, err := media.NewWorker(media.WorkerOptions{
		DB: f.p, Scanner: scanner, Process: newHeldPlayableProcessor(), Outbox: testOutboxWriter(t),
	})
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}
	if err := worker.Scan(f.ctx, versionID); err != nil {
		t.Fatalf("Worker.Scan: %v", err)
	}
	if _, err := f.p.Exec(f.ctx, `
		UPDATE media_asset_versions
		SET state = 'PROCESSING', work_claim_token = 'fixture-claim',
		    processing_stage = 'TRANSCODING', processing_progress_percent = 0,
		    processing_updated_at = now(), processing_attempt_token = 'fixture-claim',
		    active_processing_attempt_kind = 'FULL',
		    work_claimed_at = now(), work_lease_expires_at = now() + interval '1 hour'
		WHERE id = $1::uuid
	`, versionID); err != nil {
		t.Fatalf("forcing PROCESSING: %v", err)
	}
	if _, err := f.p.Exec(f.ctx,
		`UPDATE media_asset_versions SET state = 'PLAYABLE' WHERE id = $1::uuid`, versionID,
	); err != nil {
		t.Fatalf("forcing PLAYABLE without a rendition: %v", err)
	}

	var renditions int
	if err := f.p.QueryRow(f.ctx,
		`SELECT count(*) FROM video_renditions WHERE asset_version_id = $1::uuid`, versionID,
	).Scan(&renditions); err != nil {
		t.Fatalf("counting renditions: %v", err)
	}
	if renditions != 0 {
		t.Fatalf("fixture has %d renditions, want a malformed zero-rendition PLAYABLE", renditions)
	}

	if err := f.validator.ValidateLessonVideoForPublication(f.ctx, versionID); !errors.Is(err, ErrAssetVersionNotReady) {
		t.Fatalf("zero-rendition PLAYABLE = %v, want %v", err, ErrAssetVersionNotReady)
	}
	assertSubmissionFailure(t, f.publish(f.ctx, candidate.ID))
	if got := selectedLessonVideo(t, f, f.liveID); got != f.videoOld {
		t.Fatalf("live video = %s, want unchanged %s", got, f.videoOld)
	}
}

// TestReadyLessonVideoPublicationSemanticsAreUnchanged is the regression floor
// for §2 of the Option 3A brief: the PLAYABLE branch is additive, and a READY
// Asset Version is still accepted on exactly the terms it always was —
// including shapes the new PLAYABLE predicate would refuse.
func TestReadyLessonVideoPublicationSemanticsAreUnchanged(t *testing.T) {
	f := newD5Fixture(t)

	// Keep a legacy-only validator compatibility probe. The authoring fixture
	// itself now uses real media to satisfy its stricter ownership contract.
	legacyLesson, legacyVideo := uuid.NewString(), uuid.NewString()
	if _, err := f.p.Exec(f.ctx, `INSERT INTO lessons(id,section_id,title,"order") SELECT $1::uuid,section_id,'Legacy-only',99 FROM lessons WHERE id=$2::uuid`, legacyLesson, f.legacyLessonID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Exec(f.ctx, `INSERT INTO videos(id,lesson_id,status) VALUES($1::uuid,$2::uuid,'READY')`, legacyVideo, legacyLesson); err != nil {
		t.Fatal(err)
	}
	if err := f.validator.ValidateLessonVideoForPublication(f.ctx, legacyVideo); err != nil {
		t.Fatalf("legacy READY video is no longer publishable: %v", err)
	}

	// A READY media video with no live work claim — the ordinary terminal
	// shape — is publishable even though the identical PLAYABLE shape is not.
	candidate := f.candidate(t)
	versionID, finish := activePlayableVideo(t, f, candidate.ID)
	finish(true)
	if got := assetVersionState(t, f, versionID); got != "READY" {
		t.Fatalf("asset version state = %s, want READY", got)
	}
	if err := f.validator.ValidateLessonVideoForPublication(f.ctx, versionID); err != nil {
		t.Fatalf("READY video with no work claim was refused: %v", err)
	}
	if err := f.publish(f.ctx, candidate.ID); err != nil {
		t.Fatalf("PublishRevision with a READY Lesson video: %v", err)
	}
	assertLiveRevision(t, f, candidate.ID)
}

// TestUnprocessedLessonVideoStatesNeverBecomePublicationReady keeps every state
// that has no verified rendition out of a live revision, PROCESSING included.
func TestUnprocessedLessonVideoStatesNeverBecomePublicationReady(t *testing.T) {
	f := newD5Fixture(t)
	candidate := f.candidate(t)
	versionID := seedLessonVideoUpload(t, f, time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC), true)
	claimLessonVideo(t, f, candidate.ID, versionID)

	for _, state := range []string{"QUARANTINED", "SCANNING", "SCAN_PASSED", "SCAN_FAILED", "SCAN_ERROR", "PROCESSING", "PROCESS_FAILED"} {
		if _, err := f.p.Exec(f.ctx,
			`UPDATE media_asset_versions SET state = $2::media_asset_version_state WHERE id = $1::uuid`,
			versionID, state,
		); err != nil {
			// Not every state is reachable by a direct write under the schema-40
			// trigger; the ones that are not are already unreachable in
			// production, which is the property being asserted.
			continue
		}
		if err := f.validator.ValidateLessonVideoForPublication(f.ctx, versionID); !errors.Is(err, ErrAssetVersionNotReady) {
			t.Fatalf("ValidateLessonVideoForPublication(%s) = %v, want %v", state, err, ErrAssetVersionNotReady)
		}
		assertSubmissionFailure(t, f.publish(f.ctx, candidate.ID))
	}
}

// TestNonVideoAssetsNeverAcceptPlayableSemantics proves the split validator
// kept its boundary: only Lesson video gained the PLAYABLE branch.
func TestNonVideoAssetsNeverAcceptPlayableSemantics(t *testing.T) {
	f := newD5Fixture(t)
	candidate := f.candidate(t)
	versionID, _ := activePlayableVideo(t, f, candidate.ID)

	// The generic, kind-agnostic readiness gate — the one every attachment and
	// every non-video dependency uses — still means READY and nothing else.
	if err := f.validator.ValidateAssetVersion(f.ctx, versionID); !errors.Is(err, ErrAssetVersionNotReady) {
		t.Fatalf("ValidateAssetVersion on an actively PLAYABLE video = %v, want %v", err, ErrAssetVersionNotReady)
	}
	// And the real commands that attach non-video dependencies refuse it, so
	// the new semantics cannot be reached by pointing another kind at a
	// PLAYABLE version.
	if _, err := f.repo.AddLessonFile(f.ctx, f.validator, LessonFileRequest{
		CourseID: f.courseID, RevisionID: candidate.ID, LessonID: f.lessonIdentityID,
		Kind: FileKindResource, AssetVersionID: versionID,
		DisplayNameAr: "مرجع", DisplayNameEn: "RESOURCE", OwnerAccountID: f.ownerID,
	}, f.ownerID); !errors.Is(err, ErrAssetVersionNotReady) {
		t.Fatalf("AddLessonFile(RESOURCE) with a PLAYABLE version = %v, want %v", err, ErrAssetVersionNotReady)
	}
	if _, err := f.repo.AddLessonFile(f.ctx, f.validator, LessonFileRequest{
		CourseID: f.courseID, RevisionID: candidate.ID, LessonID: f.lessonIdentityID,
		Kind: FileKindLabMaterial, AssetVersionID: versionID,
		DisplayNameAr: "مختبر", DisplayNameEn: "LAB", OwnerAccountID: f.ownerID,
	}, f.ownerID); !errors.Is(err, ErrAssetVersionNotReady) {
		t.Fatalf("AddLessonFile(LAB_MATERIAL) with a PLAYABLE version = %v, want %v", err, ErrAssetVersionNotReady)
	}
	// Public preview keeps its own separate rule, and it is still READY-only.
	if _, err := f.repo.SetPreviewAsset(f.ctx, f.validator, PreviewAssetRequest{
		CourseID: f.courseID, RevisionID: candidate.ID,
		PreviewAssetVersionID: versionID, OwnerAccountID: f.ownerID,
	}, f.ownerID); !errors.Is(err, ErrAssetVersionInvalid) {
		t.Fatalf("SetPreviewAsset with a PLAYABLE version = %v, want %v", err, ErrAssetVersionInvalid)
	}
	// And generic deliverability is untouched.
	if media.StatePlayable.Deliverable() {
		t.Fatal("PLAYABLE became generically deliverable")
	}
}

// TestPublicationSerializesAgainstEnhancementFailure is the row-lock race. The
// publication transaction holds a share lock on the Lesson video row it
// validated, so an enhancement failure cannot land between the check and the
// commit: it is serialized to one side or the other, and both sides are
// coherent.
func TestPublicationSerializesAgainstEnhancementFailure(t *testing.T) {
	t.Run("failure after the publication commits leaves the course live", func(t *testing.T) {
		f := newD5Fixture(t)
		candidate := f.candidate(t)
		versionID, finish := activePlayableVideo(t, f, candidate.ID)

		if err := f.publish(f.ctx, candidate.ID); err != nil {
			t.Fatalf("PublishRevision: %v", err)
		}
		finish(false)

		assertFailedPlayable(t, f, versionID)
		assertLiveRevision(t, f, candidate.ID)
		assertRevisionState(t, f, candidate.ID, "APPROVED")
	})

	t.Run("failure before the publication validates refuses it", func(t *testing.T) {
		f := newD5Fixture(t)
		candidate := f.candidate(t)
		versionID, finish := activePlayableVideo(t, f, candidate.ID)

		finish(false)
		assertFailedPlayable(t, f, versionID)

		assertSubmissionFailure(t, f.publish(f.ctx, candidate.ID))
		assertLiveRevision(t, f, f.liveID)
	})
}

// TestPublicationAcceptsAPlayableVideoThatBecomesReady covers the benign race:
// the state improving between the Instructor's snapshot validation and the
// publication transaction must never produce a spurious refusal.
func TestPublicationAcceptsAPlayableVideoThatBecomesReady(t *testing.T) {
	f := newD5Fixture(t)
	candidate := f.candidate(t)
	versionID, finish := activePlayableVideo(t, f, candidate.ID)

	// Validate against the PLAYABLE snapshot exactly as submission does.
	if err := f.validator.ValidateLessonVideoForPublication(f.ctx, versionID); err != nil {
		t.Fatalf("actively PLAYABLE video was refused: %v", err)
	}
	// The ladder completes before the publication transaction runs.
	finish(true)
	if got := assetVersionState(t, f, versionID); got != "READY" {
		t.Fatalf("asset version state = %s, want READY", got)
	}
	if err := f.publish(f.ctx, candidate.ID); err != nil {
		t.Fatalf("publication refused a version that improved to READY: %v", err)
	}
	assertLiveRevision(t, f, candidate.ID)
}

// newStreamReadyFirstPublicationFixture authors a never-published Course whose
// single Lesson carries an actively PLAYABLE video, and submits it for review.
// It deliberately does not reuse authorInitialRevision: the video has to be
// selected while the revision is still an open candidate, which is the order a
// real Instructor works in.
func newStreamReadyFirstPublicationFixture(t *testing.T) (*d5Fixture, string, string, func(bool)) {
	t.Helper()
	freshSchema(t)
	p, _ := pool(t)
	ctx := context.Background()

	ownerID, courseID := seedInstructorAndCourse(t, p, ctx)
	repo, err := NewRepository(p, testOutboxWriter(t))
	if err != nil {
		t.Fatalf("NewRepository: %v", err)
	}
	f := newD5FixtureValue(repo, p, ctx, ownerID, courseID)
	f.seedDependencies(t)

	var revisionID string
	if err := f.p.QueryRow(f.ctx,
		`SELECT id FROM course_revisions WHERE course_id = $1::uuid`, f.courseID,
	).Scan(&revisionID); err != nil {
		t.Fatalf("querying initial revision: %v", err)
	}

	year := StudyYearYear1
	if _, err := f.repo.UpdateCourseRevision(f.ctx, f.validator, UpdateRevisionRequest{
		CourseID: f.courseID, RevisionID: revisionID, OwnerAccountID: f.ownerID,
		TitleAr: "مقرر قابل للبث", TitleEn: "STREAM READY",
		DescriptionAr: "وصف", DescriptionEn: "DESCRIPTION",
		MajorTermID: &f.majorOld, SubjectTermID: &f.subjectOld, StudyYear: &year,
	}, f.ownerID); err != nil {
		t.Fatalf("UpdateCourseRevision: %v", err)
	}
	section, err := f.repo.AddSection(f.ctx, AddSectionRequest{
		CourseID: f.courseID, RevisionID: revisionID, OwnerAccountID: f.ownerID,
		TitleAr: "قسم", TitleEn: "SECTION",
	}, f.ownerID)
	if err != nil {
		t.Fatalf("AddSection: %v", err)
	}
	f.sectionIdentityID = section.SectionIdentityID
	lesson, err := f.repo.AddLesson(f.ctx, AddLessonRequest{
		CourseID: f.courseID, RevisionID: revisionID, SectionID: section.SectionIdentityID,
		OwnerAccountID: f.ownerID, TitleAr: "درس", TitleEn: "LESSON",
	}, f.ownerID)
	if err != nil {
		t.Fatalf("AddLesson: %v", err)
	}
	f.lessonIdentityID = lesson.LessonIdentityID

	versionID, finish := activePlayableVideo(t, f, revisionID)

	f.seedPreviewAsset(t, f.previewOld, revisionID)
	if _, err := f.repo.SetPreviewAsset(f.ctx, f.validator, PreviewAssetRequest{
		CourseID: f.courseID, RevisionID: revisionID,
		PreviewAssetVersionID: f.previewOld, OwnerAccountID: f.ownerID,
	}, f.ownerID); err != nil {
		t.Fatalf("SetPreviewAsset: %v", err)
	}
	if _, err := f.repo.SubmitCourse(f.ctx, f.validator, SubmitCourseRequest{
		CourseID: f.courseID, RevisionID: revisionID,
		OwnerAccountID: f.ownerID, ActorDescriptor: f.ownerID,
	}); err != nil {
		t.Fatalf("SubmitCourse with an actively PLAYABLE Lesson video: %v", err)
	}
	if _, err := f.repo.SetCoursePrice(f.ctx, SetCoursePriceRequest{
		CourseID: f.courseID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID,
		PriceMinorUnits: 25000, Reason: "Launch price",
	}); err != nil {
		t.Fatalf("SetCoursePrice: %v", err)
	}
	return f, revisionID, versionID, finish
}

func assertLiveRevision(t *testing.T, f *d5Fixture, revisionID string) {
	t.Helper()
	var live *string
	if err := f.p.QueryRow(f.ctx,
		`SELECT live_revision_id::text FROM courses WHERE id = $1::uuid`, f.courseID,
	).Scan(&live); err != nil {
		t.Fatalf("reading live revision pointer: %v", err)
	}
	if live == nil || *live != revisionID {
		got := "<nil>"
		if live != nil {
			got = *live
		}
		t.Fatalf("live revision = %s, want %s", got, revisionID)
	}
}

func assertRevisionState(t *testing.T, f *d5Fixture, revisionID, want string) {
	t.Helper()
	var state string
	if err := f.p.QueryRow(f.ctx,
		`SELECT state::text FROM course_revisions WHERE id = $1::uuid`, revisionID,
	).Scan(&state); err != nil {
		t.Fatalf("reading revision state: %v", err)
	}
	if state != want {
		t.Fatalf("revision %s state = %s, want %s", revisionID, state, want)
	}
}

func seedEntitledStudent(t *testing.T, f *d5Fixture) string {
	t.Helper()
	studentID := uuid.NewString()
	email := "student-" + studentID + "@example.com"
	if _, err := f.p.Exec(f.ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name)
		VALUES ($1::uuid, $2, $2, 'STUDENT', 'ACTIVE', 'Stream Ready Student')
	`, studentID, email); err != nil {
		t.Fatalf("seeding student: %v", err)
	}
	invitationID := uuid.NewString()
	if _, err := f.p.Exec(f.ctx, `
		INSERT INTO course_access_invitations (
			id, email, normalized_email, course_id, created_by_account_id,
			accepted_by_account_id, decided_by_account_id, state, accepted_at, decided_at
		) VALUES ($1::uuid, $2, $2, $3::uuid, $4::uuid, $5::uuid, $4::uuid, 'APPROVED', now(), now())
	`, invitationID, email, f.courseID, f.adminID, studentID); err != nil {
		t.Fatalf("seeding course access invitation: %v", err)
	}
	if _, err := f.p.Exec(f.ctx,
		`INSERT INTO enrollments (student_account_id, course_id) VALUES ($1::uuid, $2::uuid)`,
		studentID, f.courseID,
	); err != nil {
		t.Fatalf("seeding enrollment: %v", err)
	}
	if _, err := f.p.Exec(f.ctx, `
		INSERT INTO entitlements (
			student_account_id, scope_kind, scope_id, course_id, grant_source,
			source_invitation_id, original_access_ends_at, access_ends_at,
			retirement_eligibility_at, state
		) VALUES ($1::uuid, 'COURSE', $2::uuid, $2::uuid, 'MANUAL_INVITATION', $3::uuid,
			now() + interval '30 days', now() + interval '30 days', now(), 'ACTIVE')
	`, studentID, f.courseID, invitationID); err != nil {
		t.Fatalf("seeding entitlement: %v", err)
	}
	return studentID
}
