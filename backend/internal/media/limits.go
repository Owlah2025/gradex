package media

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"

	"github.com/jackc/pgx/v5"
)

// The per-bucket upload caps from D-011, enforced under BR-068.
//
// BR-068 requires that an upload above its bucket's per-file or per-Lesson
// aggregate cap is rejected with a validation error, leaving existing stored
// files unchanged. D-011 sets the values — resources 50 MB/file and
// 200 MB/lesson, labs 250 MB/file and 1 GB/lesson — and records them as tunable
// implementation parameters rather than business invariants, which is why they
// live here beside the enforcement rather than in the rule text.
//
// Video keeps the deployment's own configured `MAX_UPLOAD_SIZE_BYTES` cap:
// D-011 states its cap is distinct from the resource and lab buckets, and the
// existing configuration already owns it.
const (
	DefaultResourceMaxBytes          int64 = 50 * 1024 * 1024
	DefaultResourceLessonMaxBytes    int64 = 200 * 1024 * 1024
	DefaultLabMaterialMaxBytes       int64 = 250 * 1024 * 1024
	DefaultLabMaterialLessonMaxBytes int64 = 1024 * 1024 * 1024
)

// uploadLimits is the resolved cap set for one Service.
type uploadLimits struct {
	uploadMax            int64
	resourceMax          int64
	resourceLessonMax    int64
	labMaterialMax       int64
	labMaterialLessonMax int64
}

func resolveUploadLimits(options ServiceOptions) uploadLimits {
	limits := uploadLimits{
		uploadMax:            options.MaxUploadBytes,
		resourceMax:          options.ResourceMaxBytes,
		resourceLessonMax:    options.ResourceLessonMaxBytes,
		labMaterialMax:       options.LabMaterialMaxBytes,
		labMaterialLessonMax: options.LabMaterialLessonMaxBytes,
	}
	if limits.resourceMax <= 0 {
		limits.resourceMax = DefaultResourceMaxBytes
	}
	if limits.resourceLessonMax <= 0 {
		limits.resourceLessonMax = DefaultResourceLessonMaxBytes
	}
	if limits.labMaterialMax <= 0 {
		limits.labMaterialMax = DefaultLabMaterialMaxBytes
	}
	if limits.labMaterialLessonMax <= 0 {
		limits.labMaterialLessonMax = DefaultLabMaterialLessonMaxBytes
	}
	return limits
}

// perFile is the effective per-file bound for one kind. It never exceeds the
// deployment's configured ceiling: a bucket cap tightens the limit, it cannot
// raise it above what the deployment allows.
func (l uploadLimits) perFile(kind AssetKind) int64 {
	bucket := l.uploadMax
	switch kind {
	case KindResource:
		bucket = l.resourceMax
	case KindLabMaterial:
		bucket = l.labMaterialMax
	}
	if bucket > l.uploadMax {
		return l.uploadMax
	}
	return bucket
}

// perLesson is the aggregate bound for one Lesson bucket, or zero when the kind
// has no aggregate rule. D-011 defines aggregates for resources and labs only;
// Lesson video is one asset per Lesson and public previews are Course-level.
func (l uploadLimits) perLesson(kind AssetKind) int64 {
	switch kind {
	case KindResource:
		return l.resourceLessonMax
	case KindLabMaterial:
		return l.labMaterialLessonMax
	default:
		return 0
	}
}

// enforceLessonAggregate refuses an upload that would push the Lesson's bucket
// past its aggregate cap, leaving every stored file untouched.
//
// "Currently held" is the newest non-failed version of each live logical asset
// in that Lesson and bucket. Counting every immutable version instead would
// charge an Instructor for bytes a replacement already superseded, and counting
// only READY versions would let an unbounded number of in-flight uploads land
// at once.
//
// A transaction-scoped advisory lock on the Lesson serialises concurrent
// uploads into the same bucket, so two simultaneous requests cannot each read a
// stale total and both fit.
func (s *Service) enforceLessonAggregate(ctx context.Context, tx pgx.Tx, request UploadRequest) error {
	aggregate := s.limits.perLesson(request.Kind)
	if aggregate <= 0 || request.LessonID == "" {
		return nil
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1, $2)`,
		lessonAggregateLockClass, lessonAggregateLockKey(request.LessonID, request.Kind)); err != nil {
		return fmt.Errorf("locking the lesson upload bucket: %w", err)
	}
	var held int64
	err := tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(current_version.size_bytes), 0)
		FROM media_assets ma
		JOIN LATERAL (
			SELECT mav.size_bytes
			FROM media_asset_versions mav
			WHERE mav.logical_asset_id = ma.id
			  AND mav.state NOT IN ('SCAN_FAILED', 'SCAN_ERROR', 'PROCESS_FAILED')
			ORDER BY mav.created_at DESC, mav.id DESC
			LIMIT 1
		) current_version ON TRUE
		WHERE ma.lesson_id = $1::uuid
		  AND ma.kind = $2::media_asset_kind
		  AND ma.retired_at IS NULL
	`, request.LessonID, request.Kind).Scan(&held)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("reading the lesson upload bucket total: %w", err)
	}
	if held+request.SizeBytes > aggregate {
		return fmt.Errorf(
			"%w: this Lesson already holds %d bytes of %s; %d more would exceed the %d byte per-Lesson limit",
			ErrValidation, held, request.Kind, request.SizeBytes, aggregate)
	}
	return nil
}

// lessonAggregateLockClass keeps the media aggregate lock in its own advisory
// namespace, so it cannot collide with an unrelated module's lock key.
const lessonAggregateLockClass int32 = 0x6D6564 // "med"

func lessonAggregateLockKey(lessonID string, kind AssetKind) int32 {
	digest := fnv.New32a()
	_, _ = digest.Write([]byte(lessonID))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write([]byte(kind))
	return int32(digest.Sum32())
}
