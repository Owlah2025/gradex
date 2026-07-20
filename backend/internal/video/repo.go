package video

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// repo is the video package's only path to Postgres. Keeping raw SQL here
// (rather than scattered across service/worker files) keeps those files
// unit-testable against a fake repo.
type repo interface {
	getLesson(ctx context.Context, lessonID string) (Lesson, error)
	getVideoByLesson(ctx context.Context, lessonID string) (Video, error)
	getVideoByID(ctx context.Context, videoID string) (Video, error)
	insertVideo(ctx context.Context, lessonID, rawKey string, status Status) (Video, error)
	updateVideoForReupload(ctx context.Context, videoID, rawKey string) error
	transitionVideoStatus(ctx context.Context, videoID string, from, to Status) (bool, error)
	setVideoFailed(ctx context.Context, videoID, reason string) (bool, error)
	updateVideoMetadata(ctx context.Context, videoID string, resolution string, bitrate int, codec string, fps float64, fileSize int64) error
	updateLessonDuration(ctx context.Context, lessonID string, durationSeconds float64) error
	updateVideoHLSMasterKey(ctx context.Context, videoID, hlsMasterKey string) error
	getProgress(ctx context.Context, userID, lessonID string) (Progress, error)
	upsertProgress(ctx context.Context, userID, lessonID string, maxPos, lastPos float64, completed bool) (Progress, error)
	resetStaleUploads(ctx context.Context) (int64, error)
}

type pgRepo struct {
	db *pgxpool.Pool
}

func newRepo(db *pgxpool.Pool) *pgRepo {
	return &pgRepo{db: db}
}

func (r *pgRepo) getLesson(ctx context.Context, lessonID string) (Lesson, error) {
	var l Lesson
	err := r.db.QueryRow(ctx, `
		SELECT lessons.id, sections.course_id, lessons.status, lessons.duration_seconds
		FROM lessons
		JOIN sections ON sections.id = lessons.section_id
		WHERE lessons.id = $1::uuid
	`, lessonID).Scan(&l.ID, &l.CourseID, &l.Status, &l.DurationSeconds)
	if errors.Is(err, pgx.ErrNoRows) {
		return Lesson{}, ErrNotFound
	}
	if err != nil {
		return Lesson{}, fmt.Errorf("getLesson: %w", err)
	}
	return l, nil
}

func (r *pgRepo) getVideoByLesson(ctx context.Context, lessonID string) (Video, error) {
	return r.scanVideo(r.db.QueryRow(ctx, videoSelectCols+` WHERE lesson_id = $1::uuid`, lessonID))
}

func (r *pgRepo) getVideoByID(ctx context.Context, videoID string) (Video, error) {
	return r.scanVideo(r.db.QueryRow(ctx, videoSelectCols+` WHERE id = $1::uuid`, videoID))
}

const videoSelectCols = `
	SELECT id, lesson_id, status, failed_reason, retry_count, raw_key, hls_master_key,
	       resolution, bitrate, codec, fps, file_size_bytes, provider, provider_asset_id,
	       provider_status, sync_version
	FROM videos`

func (r *pgRepo) scanVideo(row pgx.Row) (Video, error) {
	var v Video
	err := row.Scan(&v.ID, &v.LessonID, &v.Status, &v.FailedReason, &v.RetryCount, &v.RawKey, &v.HLSMasterKey,
		&v.Resolution, &v.Bitrate, &v.Codec, &v.FPS, &v.FileSizeBytes, &v.Provider, &v.ProviderAssetID,
		&v.ProviderStatus, &v.SyncVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return Video{}, ErrNotFound
	}
	if err != nil {
		return Video{}, fmt.Errorf("scanVideo: %w", err)
	}
	return v, nil
}

func (r *pgRepo) insertVideo(ctx context.Context, lessonID, rawKey string, status Status) (Video, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Video{}, fmt.Errorf("insertVideo begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var videoID string
	err = tx.QueryRow(ctx, `
		INSERT INTO videos (lesson_id, raw_key, status, provider_asset_id)
		VALUES ($1::uuid, $2, $3, $2)
		RETURNING id
	`, lessonID, rawKey, status).Scan(&videoID)
	if err != nil {
		return Video{}, fmt.Errorf("insertVideo: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE lessons SET status = $1, updated_at = now() WHERE id = $2::uuid`, status, lessonID); err != nil {
		return Video{}, fmt.Errorf("insertVideo lesson update: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Video{}, fmt.Errorf("insertVideo commit: %w", err)
	}

	return r.getVideoByID(ctx, videoID)
}

// updateVideoForReupload sets a fresh raw_key and moves to UPLOADING. Called
// only after the caller has already validated no in-flight video is present,
// so this is an unconditional write rather than a CAS.
func (r *pgRepo) updateVideoForReupload(ctx context.Context, videoID, rawKey string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("updateVideoForReupload begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var lessonID string
	err = tx.QueryRow(ctx, `
		UPDATE videos SET raw_key = $1, provider_asset_id = $1, status = $2, failed_reason = NULL,
		       hls_master_key = NULL, updated_at = now()
		WHERE id = $3::uuid
		RETURNING lesson_id
	`, rawKey, StatusUploading, videoID).Scan(&lessonID)
	if err != nil {
		return fmt.Errorf("updateVideoForReupload: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE lessons SET status = $1, updated_at = now() WHERE id = $2::uuid`, StatusUploading, lessonID); err != nil {
		return fmt.Errorf("updateVideoForReupload lesson update: %w", err)
	}
	return tx.Commit(ctx)
}

// transitionVideoStatus is the idempotency guard every status change goes
// through: the WHERE status=$from clause is a compare-and-swap. Zero rows
// affected means someone already applied this transition (asynq redelivery,
// concurrent request) — the caller treats that as a safe no-op, not an error.
// lessons.status is updated in the same transaction to stay in lockstep.
func (r *pgRepo) transitionVideoStatus(ctx context.Context, videoID string, from, to Status) (bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("transitionVideoStatus begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var lessonID string
	err = tx.QueryRow(ctx, `
		UPDATE videos SET status = $1, sync_version = sync_version + 1, updated_at = now()
		WHERE id = $2::uuid AND status = $3
		RETURNING lesson_id
	`, to, videoID, from).Scan(&lessonID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil // CAS didn't apply — already handled elsewhere
	}
	if err != nil {
		return false, fmt.Errorf("transitionVideoStatus: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE lessons SET status = $1, updated_at = now() WHERE id = $2::uuid`, to, lessonID); err != nil {
		return false, fmt.Errorf("transitionVideoStatus lesson update: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("transitionVideoStatus commit: %w", err)
	}
	return true, nil
}

// setVideoFailed only applies from an in-flight state (QUEUED/PROCESSING) —
// guarded the same way as transitionVideoStatus, so a stale/redelivered
// failure can never regress a video that another delivery of the same job
// already pushed to READY/PUBLISHED. Returns false (not an error) if the
// guard didn't apply.
func (r *pgRepo) setVideoFailed(ctx context.Context, videoID, reason string) (bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("setVideoFailed begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var lessonID string
	err = tx.QueryRow(ctx, `
		UPDATE videos SET status = $1, failed_reason = $2, retry_count = retry_count + 1,
		       sync_version = sync_version + 1, updated_at = now()
		WHERE id = $3::uuid AND status IN ($4, $5)
		RETURNING lesson_id
	`, StatusFailed, reason, videoID, StatusQueued, StatusProcessing).Scan(&lessonID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil // already moved past QUEUED/PROCESSING — don't regress it
	}
	if err != nil {
		return false, fmt.Errorf("setVideoFailed: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE lessons SET status = $1, updated_at = now() WHERE id = $2::uuid`, StatusFailed, lessonID); err != nil {
		return false, fmt.Errorf("setVideoFailed lesson update: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("setVideoFailed commit: %w", err)
	}
	return true, nil
}

func (r *pgRepo) updateVideoMetadata(ctx context.Context, videoID string, resolution string, bitrate int, codec string, fps float64, fileSize int64) error {
	_, err := r.db.Exec(ctx, `
		UPDATE videos SET resolution = $1, bitrate = $2, codec = $3, fps = $4, file_size_bytes = $5,
		       last_sync_at = now(), sync_version = sync_version + 1, updated_at = now()
		WHERE id = $6::uuid
	`, resolution, bitrate, codec, fps, fileSize, videoID)
	if err != nil {
		return fmt.Errorf("updateVideoMetadata: %w", err)
	}
	return nil
}

// updateLessonDuration persists the ffprobe-derived duration onto lessons —
// UpdateProgress's 90%-completion threshold reads this. This is the only
// writer of lessons.duration_seconds in the pipeline.
func (r *pgRepo) updateLessonDuration(ctx context.Context, lessonID string, durationSeconds float64) error {
	_, err := r.db.Exec(ctx, `UPDATE lessons SET duration_seconds = $1, updated_at = now() WHERE id = $2::uuid`, durationSeconds, lessonID)
	if err != nil {
		return fmt.Errorf("updateLessonDuration: %w", err)
	}
	return nil
}

func (r *pgRepo) updateVideoHLSMasterKey(ctx context.Context, videoID, hlsMasterKey string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE videos SET hls_master_key = $1, last_sync_at = now(), sync_version = sync_version + 1, updated_at = now()
		WHERE id = $2::uuid
	`, hlsMasterKey, videoID)
	if err != nil {
		return fmt.Errorf("updateVideoHLSMasterKey: %w", err)
	}
	return nil
}

func (r *pgRepo) getProgress(ctx context.Context, userID, lessonID string) (Progress, error) {
	var p Progress
	err := r.db.QueryRow(ctx, `
		SELECT user_id, lesson_id, max_position_seconds, last_position_seconds, completed
		FROM progress WHERE user_id = $1::uuid AND lesson_id = $2::uuid
	`, userID, lessonID).Scan(&p.UserID, &p.LessonID, &p.MaxPositionSeconds, &p.LastPositionSeconds, &p.Completed)
	if errors.Is(err, pgx.ErrNoRows) {
		return Progress{}, ErrNotFound
	}
	if err != nil {
		return Progress{}, fmt.Errorf("getProgress: %w", err)
	}
	return p, nil
}

func (r *pgRepo) upsertProgress(ctx context.Context, userID, lessonID string, maxPos, lastPos float64, completed bool) (Progress, error) {
	var p Progress
	err := r.db.QueryRow(ctx, `
		INSERT INTO progress (user_id, lesson_id, max_position_seconds, last_position_seconds, completed, completed_at, last_watched_at, updated_at)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, CASE WHEN $5 THEN now() ELSE NULL END, now(), now())
		ON CONFLICT (user_id, lesson_id) DO UPDATE SET
			max_position_seconds = GREATEST(progress.max_position_seconds, EXCLUDED.max_position_seconds),
			last_position_seconds = EXCLUDED.last_position_seconds,
			completed = progress.completed OR EXCLUDED.completed,
			completed_at = COALESCE(progress.completed_at, CASE WHEN EXCLUDED.completed THEN now() ELSE NULL END),
			last_watched_at = now(),
			updated_at = now()
		RETURNING user_id, lesson_id, max_position_seconds, last_position_seconds, completed
	`, userID, lessonID, maxPos, lastPos, completed).Scan(&p.UserID, &p.LessonID, &p.MaxPositionSeconds, &p.LastPositionSeconds, &p.Completed)
	if err != nil {
		return Progress{}, fmt.Errorf("upsertProgress: %w", err)
	}
	return p, nil
}

func (r *pgRepo) resetStaleUploads(ctx context.Context) (int64, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("resetStaleUploads begin: %w", err)
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		UPDATE videos SET status = $1, updated_at = now()
		WHERE status = $2 AND updated_at < now() - interval '24 hours'
		RETURNING lesson_id
	`, StatusDraft, StatusUploading)
	if err != nil {
		return 0, fmt.Errorf("resetStaleUploads videos: %w", err)
	}
	var lessonIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, fmt.Errorf("resetStaleUploads scan: %w", err)
		}
		lessonIDs = append(lessonIDs, id)
	}
	rows.Close()

	for _, id := range lessonIDs {
		if _, err := tx.Exec(ctx, `UPDATE lessons SET status = $1, updated_at = now() WHERE id = $2::uuid`, StatusDraft, id); err != nil {
			return 0, fmt.Errorf("resetStaleUploads lesson update: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("resetStaleUploads commit: %w", err)
	}
	return int64(len(lessonIDs)), nil
}
