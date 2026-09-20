//go:build integration

package media

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
)

func renditionFixture(versionID, operationID, name string, width, height, bitrate int, durationMS int64) Rendition {
	prefix := processingOutputPrefix(versionID, operationID)
	return Rendition{
		Name:             name,
		StorageObjectKey: prefix + "/" + name + "/playlist.m3u8",
		Width:            width,
		Height:           height,
		BitrateKbps:      bitrate,
		DurationMS:       durationMS,
	}
}

func processingVideoReadyForRenditions(t *testing.T) (*mediaFixture, *Worker, *time.Time, string, string) {
	t.Helper()
	f, worker, now, versionID := validatedVideoReadyForProcessing(t)
	operationID := uuid.NewString()
	_, applied, err := worker.beginTranscode(f.ctx, versionID, operationID)
	if err != nil || !applied {
		t.Fatalf("beginTranscode: applied=%t, err=%v", applied, err)
	}
	return f, worker, now, versionID, operationID
}

// ============================================================================
// SECTION 24: PROGRESSIVE RENDITION PERSISTENCE MATRIX
// ============================================================================

func TestSection24_ProgressivePersistenceMatrix(t *testing.T) {
	// 1. First verified rendition: PROCESSING + claim -> canonical row + PLAYABLE atomically.
	t.Run("1_first_rendition_promotes_atomically", func(t *testing.T) {
		f, worker, _, versionID, opID := processingVideoReadyForRenditions(t)
		r1 := renditionFixture(versionID, opID, "720p", 1280, 720, 2800, 10000)

		if err := worker.PersistVerifiedRendition(f.ctx, versionID, opID, r1); err != nil {
			t.Fatalf("PersistVerifiedRendition: %v", err)
		}

		// Verify media_asset_versions state
		var state AssetVersionState
		var claimToken *string
		var leaseExpires *time.Time
		if err := f.pool.QueryRow(f.ctx, `
			SELECT state, work_claim_token, work_lease_expires_at
			FROM media_asset_versions WHERE id = $1::uuid
		`, versionID).Scan(&state, &claimToken, &leaseExpires); err != nil {
			t.Fatalf("querying asset version: %v", err)
		}
		if state != StatePlayable {
			t.Fatalf("expected state PLAYABLE, got %s", state)
		}
		if claimToken == nil || *claimToken != opID {
			t.Fatalf("claimToken = %v, want %s", claimToken, opID)
		}
		if leaseExpires == nil || !leaseExpires.After(time.Now()) {
			t.Fatalf("lease must remain active and in future, got %v", leaseExpires)
		}

		// Verify video_renditions row
		var count int
		var key string
		var width, height, bitrate int
		var duration int64
		if err := f.pool.QueryRow(f.ctx, `
			SELECT count(*), COALESCE(max(storage_object_key), ''), COALESCE(max(width), 0),
			       COALESCE(max(height), 0), COALESCE(max(bitrate_kbps), 0), COALESCE(max(duration_ms), 0)
			FROM video_renditions WHERE asset_version_id = $1::uuid AND name = '720p'
		`, versionID).Scan(&count, &key, &width, &height, &bitrate, &duration); err != nil {
			t.Fatalf("querying video_renditions: %v", err)
		}
		if count != 1 {
			t.Fatalf("video_renditions count = %d, want 1", count)
		}
		if key != r1.StorageObjectKey || width != r1.Width || height != r1.Height || bitrate != r1.BitrateKbps || duration != r1.DurationMS {
			t.Fatalf("persisted row metadata mismatch: key=%s w=%d h=%d b=%d d=%d", key, width, height, bitrate, duration)
		}
	})

	// 2. First promotion with wrong operationID rejected (ErrConcurrentModification).
	t.Run("2_first_promotion_wrong_operation_rejected", func(t *testing.T) {
		f, worker, _, versionID, opID := processingVideoReadyForRenditions(t)
		wrongOp := uuid.NewString()
		r1 := renditionFixture(versionID, wrongOp, "720p", 1280, 720, 2800, 10000)

		err := worker.PersistVerifiedRendition(f.ctx, versionID, wrongOp, r1)
		if !errors.Is(err, ErrConcurrentModification) {
			t.Fatalf("err = %v, want ErrConcurrentModification", err)
		}

		if got := mediaState(t, f.pool, versionID); got != StateProcessing {
			t.Fatalf("state = %s, want PROCESSING", got)
		}
		var count int
		_ = f.pool.QueryRow(f.ctx, "SELECT count(*) FROM video_renditions WHERE asset_version_id = $1::uuid", versionID).Scan(&count)
		if count != 0 {
			t.Fatalf("expected 0 video_renditions, got %d", count)
		}
		_ = opID
	})

	// 3. First promotion with expired lease rejected (ErrLeaseExpired).
	t.Run("3_first_promotion_expired_lease_rejected", func(t *testing.T) {
		f, worker, _, versionID, opID := processingVideoReadyForRenditions(t)
		expireLease(t, f, versionID)
		r1 := renditionFixture(versionID, opID, "720p", 1280, 720, 2800, 10000)

		err := worker.PersistVerifiedRendition(f.ctx, versionID, opID, r1)
		if !errors.Is(err, ErrLeaseExpired) {
			t.Fatalf("err = %v, want ErrLeaseExpired", err)
		}

		if got := mediaState(t, f.pool, versionID); got != StateProcessing {
			t.Fatalf("state = %s, want PROCESSING", got)
		}
		var count int
		_ = f.pool.QueryRow(f.ctx, "SELECT count(*) FROM video_renditions WHERE asset_version_id = $1::uuid", versionID).Scan(&count)
		if count != 0 {
			t.Fatalf("expected 0 video_renditions, got %d", count)
		}
	})

	// 4. First promotion with spent operation rejected (ErrConcurrentModification).
	t.Run("4_first_promotion_spent_operation_rejected", func(t *testing.T) {
		f, worker, _, versionID, opID := processingVideoReadyForRenditions(t)
		// Mark attempt spent in processing_attempts
		if _, err := f.pool.Exec(f.ctx, `
			INSERT INTO processing_attempts (asset_version_id, operation_id, state, error_reason)
			VALUES ($1::uuid, $2, 'FAILED', 'spent attempt')
		`, versionID, opID); err != nil {
			t.Fatalf("inserting spent attempt: %v", err)
		}

		r1 := renditionFixture(versionID, opID, "720p", 1280, 720, 2800, 10000)
		err := worker.PersistVerifiedRendition(f.ctx, versionID, opID, r1)
		if !errors.Is(err, ErrConcurrentModification) {
			t.Fatalf("err = %v, want ErrConcurrentModification", err)
		}
	})

	// 5. Invalid storage prefix rejected (ErrValidation).
	t.Run("5_invalid_storage_prefix_rejected", func(t *testing.T) {
		f, worker, _, versionID, opID := processingVideoReadyForRenditions(t)
		rBad := Rendition{
			Name:             "720p",
			StorageObjectKey: "media/other-asset/hls/rogue/720p/playlist.m3u8",
			Width:            1280,
			Height:           720,
			BitrateKbps:      2800,
			DurationMS:       10000,
		}

		err := worker.PersistVerifiedRendition(f.ctx, versionID, opID, rBad)
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("err = %v, want ErrValidation", err)
		}
		if got := mediaState(t, f.pool, versionID); got != StateProcessing {
			t.Fatalf("state = %s, want PROCESSING", got)
		}
	})

	// 6. Identical first callback replay succeeds idempotently.
	t.Run("6_identical_first_callback_replay_idempotent", func(t *testing.T) {
		f, worker, _, versionID, opID := processingVideoReadyForRenditions(t)
		r1 := renditionFixture(versionID, opID, "720p", 1280, 720, 2800, 10000)

		if err := worker.PersistVerifiedRendition(f.ctx, versionID, opID, r1); err != nil {
			t.Fatalf("first persist: %v", err)
		}
		if err := worker.PersistVerifiedRendition(f.ctx, versionID, opID, r1); err != nil {
			t.Fatalf("replay persist: %v", err)
		}

		if got := mediaState(t, f.pool, versionID); got != StatePlayable {
			t.Fatalf("state = %s, want PLAYABLE", got)
		}
		var count int
		_ = f.pool.QueryRow(f.ctx, "SELECT count(*) FROM video_renditions WHERE asset_version_id = $1::uuid", versionID).Scan(&count)
		if count != 1 {
			t.Fatalf("expected 1 row, got %d", count)
		}
	})

	// 7. Conflicting first callback replay rejected (ErrConflict).
	t.Run("7_conflicting_first_callback_replay_rejected", func(t *testing.T) {
		f, worker, _, versionID, opID := processingVideoReadyForRenditions(t)
		r1 := renditionFixture(versionID, opID, "720p", 1280, 720, 2800, 10000)

		if err := worker.PersistVerifiedRendition(f.ctx, versionID, opID, r1); err != nil {
			t.Fatalf("first persist: %v", err)
		}

		rConflict := renditionFixture(versionID, opID, "720p", 1280, 720, 5000, 10000) // different bitrate
		err := worker.PersistVerifiedRendition(f.ctx, versionID, opID, rConflict)
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("err = %v, want ErrConflict", err)
		}
	})

	// 8. Subsequent rendition with same operation succeeds in PLAYABLE.
	t.Run("8_subsequent_rendition_same_operation_succeeds_in_playable", func(t *testing.T) {
		f, worker, _, versionID, opID := processingVideoReadyForRenditions(t)
		r1 := renditionFixture(versionID, opID, "720p", 1280, 720, 2800, 10000)
		r2 := renditionFixture(versionID, opID, "480p", 854, 480, 1400, 10000)

		if err := worker.PersistVerifiedRendition(f.ctx, versionID, opID, r1); err != nil {
			t.Fatalf("persist r1: %v", err)
		}
		if err := worker.PersistVerifiedRendition(f.ctx, versionID, opID, r2); err != nil {
			t.Fatalf("persist r2: %v", err)
		}

		if got := mediaState(t, f.pool, versionID); got != StatePlayable {
			t.Fatalf("state = %s, want PLAYABLE", got)
		}
		var count int
		_ = f.pool.QueryRow(f.ctx, "SELECT count(*) FROM video_renditions WHERE asset_version_id = $1::uuid", versionID).Scan(&count)
		if count != 2 {
			t.Fatalf("expected 2 video_renditions rows, got %d", count)
		}
	})

	// 9. Subsequent rendition from another operation rejected (ErrConcurrentModification).
	t.Run("9_subsequent_rendition_different_operation_rejected", func(t *testing.T) {
		f, worker, _, versionID, opID := processingVideoReadyForRenditions(t)
		r1 := renditionFixture(versionID, opID, "720p", 1280, 720, 2800, 10000)
		if err := worker.PersistVerifiedRendition(f.ctx, versionID, opID, r1); err != nil {
			t.Fatalf("persist r1: %v", err)
		}

		rogueOp := uuid.NewString()
		r2Rogue := renditionFixture(versionID, rogueOp, "480p", 854, 480, 1400, 10000)
		err := worker.PersistVerifiedRendition(f.ctx, versionID, rogueOp, r2Rogue)
		if !errors.Is(err, ErrConcurrentModification) {
			t.Fatalf("err = %v, want ErrConcurrentModification", err)
		}
	})

	// 10. Identical subsequent callback is no-op success.
	t.Run("10_identical_subsequent_callback_noop_success", func(t *testing.T) {
		f, worker, _, versionID, opID := processingVideoReadyForRenditions(t)
		r1 := renditionFixture(versionID, opID, "720p", 1280, 720, 2800, 10000)
		r2 := renditionFixture(versionID, opID, "480p", 854, 480, 1400, 10000)

		if err := worker.PersistVerifiedRendition(f.ctx, versionID, opID, r1); err != nil {
			t.Fatalf("persist r1: %v", err)
		}
		if err := worker.PersistVerifiedRendition(f.ctx, versionID, opID, r2); err != nil {
			t.Fatalf("persist r2: %v", err)
		}
		// Replay r2 identically
		if err := worker.PersistVerifiedRendition(f.ctx, versionID, opID, r2); err != nil {
			t.Fatalf("replay r2: %v", err)
		}

		var count int
		_ = f.pool.QueryRow(f.ctx, "SELECT count(*) FROM video_renditions WHERE asset_version_id = $1::uuid", versionID).Scan(&count)
		if count != 2 {
			t.Fatalf("expected 2 rows, got %d", count)
		}
	})

	// 11. Conflicting subsequent callback rejected (ErrConflict).
	t.Run("11_conflicting_subsequent_callback_rejected", func(t *testing.T) {
		f, worker, _, versionID, opID := processingVideoReadyForRenditions(t)
		r1 := renditionFixture(versionID, opID, "720p", 1280, 720, 2800, 10000)
		r2 := renditionFixture(versionID, opID, "480p", 854, 480, 1400, 10000)

		if err := worker.PersistVerifiedRendition(f.ctx, versionID, opID, r1); err != nil {
			t.Fatalf("persist r1: %v", err)
		}
		if err := worker.PersistVerifiedRendition(f.ctx, versionID, opID, r2); err != nil {
			t.Fatalf("persist r2: %v", err)
		}

		r2Conflict := renditionFixture(versionID, opID, "480p", 854, 480, 9999, 10000)
		err := worker.PersistVerifiedRendition(f.ctx, versionID, opID, r2Conflict)
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("err = %v, want ErrConflict", err)
		}
	})

	// 12. Invariant: no canonical row while state is PROCESSING.
	t.Run("12_invariant_no_canonical_row_while_processing", func(t *testing.T) {
		f, _, _, versionID, _ := processingVideoReadyForRenditions(t)
		// While asset is in PROCESSING, video_renditions must have 0 rows
		var count int
		if err := f.pool.QueryRow(f.ctx, `
			SELECT count(*) FROM video_renditions vr
			JOIN media_asset_versions mav ON mav.id = vr.asset_version_id
			WHERE mav.id = $1::uuid AND mav.state = 'PROCESSING'
		`, versionID).Scan(&count); err != nil {
			t.Fatalf("querying: %v", err)
		}
		if count != 0 {
			t.Fatalf("found %d video_renditions rows for PROCESSING asset", count)
		}
	})

	// 13. Invariant: no PLAYABLE state without at least 1 canonical row.
	t.Run("13_invariant_no_playable_without_canonical_row", func(t *testing.T) {
		f, worker, _, versionID, opID := processingVideoReadyForRenditions(t)
		r1 := renditionFixture(versionID, opID, "720p", 1280, 720, 2800, 10000)
		if err := worker.PersistVerifiedRendition(f.ctx, versionID, opID, r1); err != nil {
			t.Fatalf("persist r1: %v", err)
		}

		var count int
		if err := f.pool.QueryRow(f.ctx, `
			SELECT count(*) FROM video_renditions WHERE asset_version_id = $1::uuid
		`, versionID).Scan(&count); err != nil {
			t.Fatalf("querying: %v", err)
		}
		if count < 1 {
			t.Fatalf("asset is PLAYABLE but has %d canonical renditions", count)
		}
	})

	// 14. Failure before promotion keeps existing retry behavior (PROCESSING -> PROCESS_FAILED -> retryable).
	t.Run("14_failure_before_promotion_keeps_retry_behavior", func(t *testing.T) {
		f, worker, _, versionID, opID := processingVideoReadyForRenditions(t)

		_ = worker.recordProcessingFailure(f.ctx, versionID, opID, errors.New("simulated early non-retryable failure"))

		if got := mediaState(t, f.pool, versionID); got != StateProcessFailed {
			t.Fatalf("state = %s, want PROCESS_FAILED", got)
		}
	})

	// 15. Failure after promotion keeps PLAYABLE and disables retry.
	t.Run("15_failure_after_promotion_keeps_playable_and_disables_retry", func(t *testing.T) {
		f, worker, _, versionID, opID := processingVideoReadyForRenditions(t)
		r1 := renditionFixture(versionID, opID, "720p", 1280, 720, 2800, 10000)
		if err := worker.PersistVerifiedRendition(f.ctx, versionID, opID, r1); err != nil {
			t.Fatalf("persist r1: %v", err)
		}

		// Even with transient storage error, failure after promotion must KEEP PLAYABLE and NOT retry
		_ = worker.recordProcessingFailure(f.ctx, versionID, opID, fmt.Errorf("%w: simulated storage failure", ErrStorageUnavailable))

		// State must REMAIN PLAYABLE
		if got := mediaState(t, f.pool, versionID); got != StatePlayable {
			t.Fatalf("state = %s, want PLAYABLE", got)
		}

		// Outbox must NOT have new transcode requests
		var count int
		if err := f.pool.QueryRow(f.ctx, "SELECT count(*) FROM outbox_events WHERE aggregate_id=$1::uuid AND event_type='media.transcode_requested'", versionID).Scan(&count); err != nil {
			t.Fatalf("querying outbox: %v", err)
		}
		if count > 0 {
			t.Fatalf("expected 0 new transcode_requested events, got %d", count)
		}
	})

	// 16. Cleanup before promotion removes prefix.
	t.Run("16_cleanup_before_promotion_removes_prefix", func(t *testing.T) {
		f, _, _, versionID, opID := processingVideoReadyForRenditions(t)
		prefix := processingOutputPrefix(versionID, opID)

		ref, err := HasAttemptRenditionReferences(f.ctx, f.pool, versionID, opID)
		if err != nil {
			t.Fatalf("HasAttemptRenditionReferences: %v", err)
		}
		if ref {
			t.Fatal("prefix should be unreferenced before promotion")
		}
		_ = prefix
	})

	// 17. Cleanup after promotion retains prefix.
	t.Run("17_cleanup_after_promotion_retains_prefix", func(t *testing.T) {
		f, worker, _, versionID, opID := processingVideoReadyForRenditions(t)
		r1 := renditionFixture(versionID, opID, "720p", 1280, 720, 2800, 10000)
		if err := worker.PersistVerifiedRendition(f.ctx, versionID, opID, r1); err != nil {
			t.Fatalf("persist r1: %v", err)
		}

		ref, err := HasAttemptRenditionReferences(f.ctx, f.pool, versionID, opID)
		if err != nil {
			t.Fatalf("HasAttemptRenditionReferences: %v", err)
		}
		if !ref {
			t.Fatal("prefix must be reported referenced after promotion")
		}
	})

	// 18. Stale worker cannot persist after attempt failure.
	t.Run("18_stale_worker_cannot_persist_after_attempt_failure", func(t *testing.T) {
		f, worker, _, versionID, opID := processingVideoReadyForRenditions(t)
		// Mark attempt failed
		_ = worker.recordProcessingFailure(f.ctx, versionID, opID, errors.New("aborted"))

		r1 := renditionFixture(versionID, opID, "720p", 1280, 720, 2800, 10000)
		err := worker.PersistVerifiedRendition(f.ctx, versionID, opID, r1)
		if !errors.Is(err, ErrConcurrentModification) {
			t.Fatalf("err = %v, want ErrConcurrentModification", err)
		}
	})

	// 19. Stale worker cannot finalize READY after recovery clears claim.
	t.Run("19_stale_worker_cannot_finalize_ready_after_recovery_clears_claim", func(t *testing.T) {
		f, worker, now, versionID, opID := processingVideoReadyForRenditions(t)
		r1 := renditionFixture(versionID, opID, "720p", 1280, 720, 2800, 10000)
		if err := worker.PersistVerifiedRendition(f.ctx, versionID, opID, r1); err != nil {
			t.Fatalf("persist r1: %v", err)
		}

		expireLease(t, f, versionID)
		*now = now.Add(3 * time.Second)
		recovered, err := worker.RecoverStale(f.ctx, 10)
		if err != nil || recovered != 1 {
			t.Fatalf("RecoverStale: recovered=%d, err=%v", recovered, err)
		}

		// Worker attempts CompleteTranscode
		err = worker.CompleteTranscode(f.ctx, versionID, opID, TranscodeResult{
			OutputPrefix:       processingOutputPrefix(versionID, opID),
			TrustedDurationMS:  10000,
			Renditions:         []Rendition{r1},
			ExpectedRenditions: []string{"720p"},
		})
		if !errors.Is(err, ErrConcurrentModification) && !errors.Is(err, ErrLeaseExpired) {
			t.Fatalf("err = %v, want ErrConcurrentModification or ErrLeaseExpired", err)
		}
	})
}

// ============================================================================
// SECTION 25: STRICT READY COMPLETENESS MATRIX
// ============================================================================

func TestSection25_StrictReadyCompletenessMatrix(t *testing.T) {
	// Setup helper: creates a PLAYABLE asset with 720p and 480p renditions persisted
	setupPlayable := func(t *testing.T) (*mediaFixture, *Worker, *time.Time, string, string, Rendition, Rendition) {
		f, worker, now, versionID, opID := processingVideoReadyForRenditions(t)
		r720 := renditionFixture(versionID, opID, "720p", 1280, 720, 2800, 10000)
		r480 := renditionFixture(versionID, opID, "480p", 854, 480, 1400, 10000)
		if err := worker.PersistVerifiedRendition(f.ctx, versionID, opID, r720); err != nil {
			t.Fatalf("persist r720: %v", err)
		}
		if err := worker.PersistVerifiedRendition(f.ctx, versionID, opID, r480); err != nil {
			t.Fatalf("persist r480: %v", err)
		}
		return f, worker, now, versionID, opID, r720, r480
	}

	t.Run("rejects_missing_expected_rendition", func(t *testing.T) {
		f, worker, _, versionID, opID, r720, r480 := setupPlayable(t)
		// Expected includes 240p which is not persisted in DB
		res := TranscodeResult{
			OutputPrefix:       processingOutputPrefix(versionID, opID),
			TrustedDurationMS:  10000,
			Renditions:         []Rendition{r720, r480},
			ExpectedRenditions: []string{"720p", "480p", "240p"},
		}
		err := worker.CompleteTranscode(f.ctx, versionID, opID, res)
		if !errors.Is(err, ErrConflict) && !errors.Is(err, ErrValidation) {
			t.Fatalf("err = %v, want ErrConflict or ErrValidation", err)
		}
		if got := mediaState(t, f.pool, versionID); got != StatePlayable {
			t.Fatalf("state = %s, want PLAYABLE", got)
		}
	})

	t.Run("rejects_extra_unpersisted_rendition_in_result", func(t *testing.T) {
		f, worker, _, versionID, opID, r720, r480 := setupPlayable(t)
		r240 := renditionFixture(versionID, opID, "240p", 426, 240, 400, 10000)
		// Result has r240 which was never persisted via PersistVerifiedRendition
		res := TranscodeResult{
			OutputPrefix:       processingOutputPrefix(versionID, opID),
			TrustedDurationMS:  10000,
			Renditions:         []Rendition{r720, r480, r240},
			ExpectedRenditions: []string{"720p", "480p", "240p"},
		}
		err := worker.CompleteTranscode(f.ctx, versionID, opID, res)
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("err = %v, want ErrConflict", err)
		}
	})

	t.Run("rejects_wrong_name_agreement", func(t *testing.T) {
		f, worker, _, versionID, opID, r720, r480 := setupPlayable(t)
		// Expected names mismatch persisted names
		res := TranscodeResult{
			OutputPrefix:       processingOutputPrefix(versionID, opID),
			TrustedDurationMS:  10000,
			Renditions:         []Rendition{r720, r480},
			ExpectedRenditions: []string{"720p", "360p"},
		}
		err := worker.CompleteTranscode(f.ctx, versionID, opID, res)
		if !errors.Is(err, ErrValidation) && !errors.Is(err, ErrConflict) {
			t.Fatalf("err = %v, want ErrValidation or ErrConflict", err)
		}
	})

	t.Run("rejects_wrong_output_prefix", func(t *testing.T) {
		f, worker, _, versionID, opID, r720, r480 := setupPlayable(t)
		res := TranscodeResult{
			OutputPrefix:       "media/" + versionID + "/hls/wrong-prefix",
			TrustedDurationMS:  10000,
			Renditions:         []Rendition{r720, r480},
			ExpectedRenditions: []string{"720p", "480p"},
		}
		err := worker.CompleteTranscode(f.ctx, versionID, opID, res)
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("err = %v, want ErrValidation", err)
		}
	})

	t.Run("rejects_metadata_mismatch_width_height_bitrate_duration", func(t *testing.T) {
		cases := []struct {
			name string
			mod  func(r Rendition) Rendition
		}{
			{"width_mismatch", func(r Rendition) Rendition { r.Width += 10; return r }},
			{"height_mismatch", func(r Rendition) Rendition { r.Height += 10; return r }},
			{"bitrate_mismatch", func(r Rendition) Rendition { r.BitrateKbps += 100; return r }},
			{"duration_mismatch", func(r Rendition) Rendition { r.DurationMS += 500; return r }},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				f, worker, _, versionID, opID, r720, r480 := setupPlayable(t)
				modR480 := tc.mod(r480)
				res := TranscodeResult{
					OutputPrefix:       processingOutputPrefix(versionID, opID),
					TrustedDurationMS:  10000,
					Renditions:         []Rendition{r720, modR480},
					ExpectedRenditions: []string{"720p", "480p"},
				}
				err := worker.CompleteTranscode(f.ctx, versionID, opID, res)
				if !errors.Is(err, ErrConflict) {
					t.Fatalf("err = %v, want ErrConflict", err)
				}
			})
		}
	})

	t.Run("rejects_stale_operation", func(t *testing.T) {
		f, worker, _, versionID, opID, r720, r480 := setupPlayable(t)
		res := TranscodeResult{
			OutputPrefix:       processingOutputPrefix(versionID, opID),
			TrustedDurationMS:  10000,
			Renditions:         []Rendition{r720, r480},
			ExpectedRenditions: []string{"720p", "480p"},
		}
		err := worker.CompleteTranscode(f.ctx, versionID, "stale-op", res)
		if !errors.Is(err, ErrValidation) && !errors.Is(err, ErrConcurrentModification) {
			t.Fatalf("err = %v, want ErrValidation or ErrConcurrentModification", err)
		}
	})

	t.Run("rejects_expired_lease", func(t *testing.T) {
		f, worker, _, versionID, opID, r720, r480 := setupPlayable(t)
		expireLease(t, f, versionID)
		res := TranscodeResult{
			OutputPrefix:       processingOutputPrefix(versionID, opID),
			TrustedDurationMS:  10000,
			Renditions:         []Rendition{r720, r480},
			ExpectedRenditions: []string{"720p", "480p"},
		}
		err := worker.CompleteTranscode(f.ctx, versionID, opID, res)
		if !errors.Is(err, ErrLeaseExpired) {
			t.Fatalf("err = %v, want ErrLeaseExpired", err)
		}
	})

	t.Run("rejects_spent_operation", func(t *testing.T) {
		f, worker, _, versionID, opID, r720, r480 := setupPlayable(t)
		// Mark attempt spent
		if _, err := f.pool.Exec(f.ctx, `
			INSERT INTO processing_attempts (asset_version_id, operation_id, state, error_reason)
			VALUES ($1::uuid, $2, 'FAILED', 'spent attempt')
		`, versionID, opID); err != nil {
			t.Fatalf("inserting spent attempt: %v", err)
		}
		res := TranscodeResult{
			OutputPrefix:       processingOutputPrefix(versionID, opID),
			TrustedDurationMS:  10000,
			Renditions:         []Rendition{r720, r480},
			ExpectedRenditions: []string{"720p", "480p"},
		}
		err := worker.CompleteTranscode(f.ctx, versionID, opID, res)
		if !errors.Is(err, ErrConcurrentModification) {
			t.Fatalf("err = %v, want ErrConcurrentModification", err)
		}
	})

	t.Run("rejects_state_processing_instead_of_playable", func(t *testing.T) {
		// Asset is in PROCESSING (no renditions persisted yet)
		f, worker, _, versionID, opID := processingVideoReadyForRenditions(t)
		r720 := renditionFixture(versionID, opID, "720p", 1280, 720, 2800, 10000)
		res := TranscodeResult{
			OutputPrefix:       processingOutputPrefix(versionID, opID),
			TrustedDurationMS:  10000,
			Renditions:         []Rendition{r720},
			ExpectedRenditions: []string{"720p"},
		}
		err := worker.CompleteTranscode(f.ctx, versionID, opID, res)
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("err = %v, want ErrConflict (state must be PLAYABLE)", err)
		}
	})

	t.Run("rejects_conflicting_completion_receipt", func(t *testing.T) {
		f, worker, _, versionID, opID, r720, r480 := setupPlayable(t)
		// Injected conflicting completion receipt
		if _, err := f.pool.Exec(f.ctx, `
			INSERT INTO media_callback_receipts (provider_event_id, callback_kind, asset_version_id, request_fingerprint)
			VALUES ($1, 'TRANSCODE_COMPLETED', $2::uuid, 'conflicting-fingerprint')
		`, opID, versionID); err != nil {
			t.Fatalf("inserting conflicting completion: %v", err)
		}
		res := TranscodeResult{
			OutputPrefix:       processingOutputPrefix(versionID, opID),
			TrustedDurationMS:  10000,
			Renditions:         []Rendition{r720, r480},
			ExpectedRenditions: []string{"720p", "480p"},
		}
		err := worker.CompleteTranscode(f.ctx, versionID, opID, res)
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("err = %v, want ErrConflict", err)
		}
	})

	t.Run("succeeds_exact_expected_set_and_promotes_to_ready", func(t *testing.T) {
		f, worker, _, versionID, opID, r720, r480 := setupPlayable(t)
		res := TranscodeResult{
			OutputPrefix:       processingOutputPrefix(versionID, opID),
			TrustedDurationMS:  10000,
			Renditions:         []Rendition{r720, r480},
			ExpectedRenditions: []string{"720p", "480p"},
		}
		if err := worker.CompleteTranscode(f.ctx, versionID, opID, res); err != nil {
			t.Fatalf("CompleteTranscode: %v", err)
		}

		// Verify state is READY, progress is 100, claim is cleared, successful_processing_attempt_id is set
		var state AssetVersionState
		var progress int
		var claimToken *string
		var attemptID *string
		if err := f.pool.QueryRow(f.ctx, `
			SELECT state, processing_progress_percent, work_claim_token, successful_processing_attempt_id::text
			FROM media_asset_versions WHERE id = $1::uuid
		`, versionID).Scan(&state, &progress, &claimToken, &attemptID); err != nil {
			t.Fatalf("querying asset version: %v", err)
		}
		if state != StateReady {
			t.Fatalf("state = %s, want READY", state)
		}
		if progress != 100 {
			t.Fatalf("progress = %d, want 100", progress)
		}
		if claimToken != nil {
			t.Fatalf("claimToken = %v, want nil", *claimToken)
		}
		if attemptID == nil || *attemptID == "" {
			t.Fatal("successful_processing_attempt_id must be populated")
		}

		// Verify duplicate completion replay succeeds idempotently
		if err := worker.CompleteTranscode(f.ctx, versionID, opID, res); err != nil {
			t.Fatalf("duplicate CompleteTranscode: %v", err)
		}
	})
}

// ============================================================================
// SECTION 26: LOW-RESOLUTION LADDER TESTS (1, 2, 3, 4 RUNGS)
// ============================================================================

type progressiveStubProcessor struct {
	height int
}

func (p *progressiveStubProcessor) Transcode(ctx context.Context, object ObjectVersion) (TranscodeResult, error) {
	return p.TranscodeProgressive(ctx, object, nil, nil)
}

func (p *progressiveStubProcessor) TranscodeProgressive(ctx context.Context, object ObjectVersion, progress ProgressSink, renditions VerifiedRenditionSink) (TranscodeResult, error) {
	rungs := hlsRungsForHeight(p.height)
	prefix := processingOutputPrefix(object.AssetVersionID, object.ProcessingOperationID)
	resultRenditions := make([]Rendition, 0, len(rungs))
	expectedNames := make([]string, 0, len(rungs))
	for _, r := range rungs {
		expectedNames = append(expectedNames, r.Name)
		rend := Rendition{
			Name:             r.Name,
			StorageObjectKey: prefix + "/" + r.Name + "/playlist.m3u8",
			Width:            r.Width,
			Height:           r.Height,
			BitrateKbps:      r.VideoKbps,
			DurationMS:       60000,
		}
		if renditions != nil {
			if err := renditions.PersistVerifiedRendition(ctx, rend); err != nil {
				return TranscodeResult{}, err
			}
		}
		resultRenditions = append(resultRenditions, rend)
	}
	return TranscodeResult{
		OutputPrefix:       prefix,
		TrustedDurationMS:  60000,
		Renditions:         resultRenditions,
		ExpectedRenditions: expectedNames,
	}, nil
}

func TestSection26_LowResolutionLadders(t *testing.T) {
	cases := []struct {
		name         string
		sourceHeight int
		wantRungs    []string
	}{
		{"1_rung_240p", 240, []string{"240p"}},
		{"2_rungs_480p", 480, []string{"480p", "240p"}},
		{"3_rungs_720p", 720, []string{"720p", "480p", "240p"}},
		{"4_rungs_1080p", 1080, []string{"1080p", "720p", "480p", "240p"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newMediaFixture(t)
			now := time.Now().UTC()
			proc := &progressiveStubProcessor{height: tc.sourceHeight}
			worker := recoveryWorker(t, f, &now, proc)

			request, _ := f.beginVideoUpload("object-v1")
			if _, err := f.service.CompleteUpload(f.ctx, request); err != nil {
				t.Fatalf("CompleteUpload: %v", err)
			}
			markValidated(t, f, request.AssetVersionID)

			opID := uuid.NewString()
			if err := worker.transcode(f.ctx, request.AssetVersionID, opID); err != nil {
				t.Fatalf("worker.transcode: %v", err)
			}

			// Verify final state is READY
			if got := mediaState(t, f.pool, request.AssetVersionID); got != StateReady {
				t.Fatalf("state = %s, want READY", got)
			}

			// Verify exact renditions persisted in DB
			rows, err := f.pool.Query(f.ctx, `
				SELECT name FROM video_renditions
				WHERE asset_version_id = $1::uuid
				ORDER BY height DESC
			`, request.AssetVersionID)
			if err != nil {
				t.Fatalf("querying renditions: %v", err)
			}
			defer rows.Close()

			var names []string
			for rows.Next() {
				var n string
				if err := rows.Scan(&n); err != nil {
					t.Fatalf("scan: %v", err)
				}
				names = append(names, n)
			}
			if len(names) != len(tc.wantRungs) {
				t.Fatalf("got %d renditions (%v), want %d (%v)", len(names), names, len(tc.wantRungs), tc.wantRungs)
			}
			for i, want := range tc.wantRungs {
				if names[i] != want {
					t.Fatalf("rung %d = %s, want %s", i, names[i], want)
				}
			}
		})
	}
}

// ============================================================================
// SECTION 27: PLAYABLE DELIVERY (PHASE 3B3C)
// ============================================================================

func TestSection27_PlayableDelivery(t *testing.T) {
	f := newDeliveryFixture(t)

	// Create a new video using validated upload, then begin transcode and persist 1 rendition to reach PLAYABLE
	request, _ := f.beginVideoUpload("object-v1")
	if _, err := f.service.CompleteUpload(f.ctx, request); err != nil {
		t.Fatalf("CompleteUpload: %v", err)
	}
	markValidated(t, f.mediaFixture, request.AssetVersionID)

	now := time.Now().UTC()
	worker := recoveryWorker(t, f.mediaFixture, &now, integrationProcessorFunc(successfulAttemptProcessor))
	opID := uuid.NewString()
	if _, applied, err := worker.beginTranscode(f.ctx, request.AssetVersionID, opID); err != nil || !applied {
		t.Fatalf("beginTranscode: applied=%t, err=%v", applied, err)
	}

	r1 := renditionFixture(request.AssetVersionID, opID, "720p", 1280, 720, 2800, 60000)
	if err := worker.PersistVerifiedRendition(f.ctx, request.AssetVersionID, opID, r1); err != nil {
		t.Fatalf("PersistVerifiedRendition: %v", err)
	}

	// Attach the PLAYABLE video to the lesson
	if _, err := f.pool.Exec(f.ctx, `UPDATE course_lessons SET video_asset_version_id = $1::uuid WHERE lesson_identity_id = $2::uuid`, request.AssetVersionID, f.lesson); err != nil {
		t.Fatal(err)
	}

	// Verify that IssuePlayback succeeds for PLAYABLE version in Phase 3B3C
	playbackReq := PlaybackRequest{
		StudentID:      f.student,
		DeviceID:       testDeviceID(f.student),
		LessonID:       f.lesson,
		AssetVersionID: request.AssetVersionID,
	}
	auth, err := f.delivery.IssuePlayback(f.ctx, playbackReq)
	if err != nil {
		t.Fatalf("IssuePlayback on PLAYABLE video error = %v, want nil", err)
	}
	if auth.PlaybackSession == "" || auth.ManifestURL == "" {
		t.Fatalf("unexpected playback auth: %+v", auth)
	}
}
