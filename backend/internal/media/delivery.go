package media

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/entitlement"
)

var ErrProtectedUnavailable = errors.New("protected media is unavailable")

// protectedDenial retains the evaluator's typed internal reason for the
// audit/diagnostic boundary while preserving one external refusal. It is not a
// response model and callers must never serialize it.
type protectedDenial struct{ reason entitlement.Reason }

func (e protectedDenial) Error() string { return ErrProtectedUnavailable.Error() }
func (e protectedDenial) Is(target error) bool {
	return target == ErrProtectedUnavailable
}

// ProtectedDenialReason exposes only the closed, non-PII diagnostic reason to
// the HTTP telemetry boundary. Its absence means an operational target/store
// failure, which is still externally indistinguishable from every denial.
func ProtectedDenialReason(err error) (entitlement.Reason, bool) {
	var denied protectedDenial
	if errors.As(err, &denied) {
		return denied.reason, true
	}
	return "", false
}

func denyProtected(reason entitlement.Reason) error { return protectedDenial{reason: reason} }

// EntitlementEvaluator is the one S4 entitlement decision point. Delivery
// accepts no callback that performs a local expiry, scope, or suspension test.
type EntitlementEvaluator interface {
	Evaluate(context.Context, string, string, time.Time) entitlement.Decision
	EvaluateTarget(context.Context, string, string, *time.Time, time.Time) entitlement.Decision
}

type DeliveryOptions struct {
	DB                *pgxpool.Pool
	Store             DeliveryStore
	Evaluator         EntitlementEvaluator
	SignatureLifetime time.Duration
	BuyerTagKey       []byte
	Now               func() time.Time
}

type DeliveryService struct {
	db                *pgxpool.Pool
	store             DeliveryStore
	evaluator         EntitlementEvaluator
	signatureLifetime time.Duration
	buyerTagKey       []byte
	now               func() time.Time
}

func NewDeliveryService(options DeliveryOptions) (*DeliveryService, error) {
	if options.DB == nil {
		return nil, errors.New("delivery database is required")
	}
	if options.Store == nil {
		return nil, errors.New("delivery object store is required")
	}
	if options.Evaluator == nil {
		return nil, errors.New("entitlement evaluator is required")
	}
	if options.SignatureLifetime <= 0 {
		return nil, errors.New("delivery signature lifetime must be positive")
	}
	if len(options.BuyerTagKey) == 0 {
		return nil, errors.New("delivery buyer-tag key is required")
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &DeliveryService{
		db: options.DB, store: options.Store, evaluator: options.Evaluator,
		signatureLifetime: options.SignatureLifetime,
		buyerTagKey:       append([]byte(nil), options.BuyerTagKey...), now: now,
	}, nil
}

type PlaybackRequest struct {
	StudentID      string
	LessonID       string
	AssetVersionID string
}

type DownloadRequest struct {
	StudentID      string
	LessonID       string
	AssetVersionID string
	Kind           AssetKind
}

// MaterialKind is the presentation-only kind exposed to protected learning
// read models. It deliberately carries no Asset Version or storage details.
type MaterialKind string

const (
	MaterialResource    MaterialKind = "resource"
	MaterialLabMaterial MaterialKind = "lab_material"
)

// DownloadEntryRequest selects a stable Lesson entry point. The current
// Asset Version is resolved by S4; callers cannot supply one.
type DownloadEntryRequest struct {
	StudentID string
	LessonID  string
	Kind      AssetKind
}

type PlaybackAuthorization struct {
	PlaybackSession string    `json:"playback_session"`
	ManifestURL     string    `json:"manifest_url"`
	AssetVersionID  string    `json:"asset_version_id"`
	ExpiresAt       time.Time `json:"expires_at"`
}

type DownloadAuthorization struct {
	URL            string    `json:"url"`
	AssetVersionID string    `json:"asset_version_id"`
	ExpiresAt      time.Time `json:"expires_at"`
	BuyerTag       string    `json:"buyer_tag,omitempty"`
}

type PreviewAuthorization struct {
	URL            string    `json:"url"`
	AssetVersionID string    `json:"asset_version_id"`
	ExpiresAt      time.Time `json:"expires_at"`
}

type deliveryTarget struct {
	lessonID       string
	assetVersionID string
	kind           AssetKind
	state          AssetVersionState
	storageKey     string
	durationMS     int64
	retiredAt      *time.Time
}

// TrustedVideoDuration returns S4-owned READY exact-version metadata for an
// already-authorized S5 progress write. It does not evaluate access or sign a
// URL; callers must use the Entitlement evaluator first.
func (s *DeliveryService) TrustedVideoDuration(ctx context.Context, lessonID, assetVersionID string) (time.Duration, error) {
	target, err := s.loadApprovedTarget(ctx, lessonID, assetVersionID, KindVideo)
	if err != nil || target.durationMS <= 0 {
		return 0, ErrProtectedUnavailable
	}
	return time.Duration(target.durationMS) * time.Millisecond, nil
}

// IssuePlayback evaluates the Student immediately before signing. The route
// selects an exact S2-approved version; it never substitutes a logical
// Asset's latest READY version.
func (s *DeliveryService) IssuePlayback(ctx context.Context, request PlaybackRequest) (PlaybackAuthorization, error) {
	if request.StudentID == "" || request.LessonID == "" || request.AssetVersionID == "" {
		return PlaybackAuthorization{}, ErrProtectedUnavailable
	}
	target, err := s.loadApprovedTarget(ctx, request.LessonID, request.AssetVersionID, KindVideo)
	if err != nil || target.state != StateReady || target.storageKey == "" {
		return PlaybackAuthorization{}, ErrProtectedUnavailable
	}
	decision := s.evaluator.EvaluateTarget(ctx, request.StudentID, request.LessonID, target.retiredAt, s.now().UTC())
	if !decision.Allowed {
		return PlaybackAuthorization{}, denyProtected(decision.Reason)
	}
	url, err := s.store.PresignGetURL(ctx, target.storageKey, s.signatureLifetime)
	if err != nil {
		return PlaybackAuthorization{}, ErrProtectedUnavailable
	}
	now := s.now().UTC()
	return PlaybackAuthorization{
		PlaybackSession: s.playbackSession(request.StudentID, target.assetVersionID, now.Add(s.signatureLifetime)),
		ManifestURL:     url, AssetVersionID: target.assetVersionID, ExpiresAt: now.Add(s.signatureLifetime),
	}, nil
}

// IssueDownload protects Resource and Lab Material bytes independently on each
// request. Buyer tags are generated only after authorization and only for Lab
// Material; the tag is evidence, never an authorization credential.
func (s *DeliveryService) IssueDownload(ctx context.Context, request DownloadRequest) (DownloadAuthorization, error) {
	if request.StudentID == "" || request.LessonID == "" || request.AssetVersionID == "" ||
		(request.Kind != KindResource && request.Kind != KindLabMaterial) {
		return DownloadAuthorization{}, ErrProtectedUnavailable
	}
	target, err := s.loadApprovedTarget(ctx, request.LessonID, request.AssetVersionID, request.Kind)
	if err != nil || target.state != StateReady || target.storageKey == "" {
		return DownloadAuthorization{}, ErrProtectedUnavailable
	}
	return s.issueDownloadTarget(ctx, request.StudentID, request.Kind, target)
}

// IssueDownloadEntry resolves the current live Lesson material and then uses
// the same authorization and signing path as the existing POST contract.
// Stable GET entry routes never accept an Asset Version from the client.
func (s *DeliveryService) IssueDownloadEntry(ctx context.Context, request DownloadEntryRequest) (DownloadAuthorization, error) {
	if request.StudentID == "" || request.LessonID == "" ||
		(request.Kind != KindResource && request.Kind != KindLabMaterial) {
		return DownloadAuthorization{}, ErrProtectedUnavailable
	}
	target, err := s.loadCurrentMaterialTarget(ctx, request.LessonID, request.Kind)
	if err != nil || target.state != StateReady || target.storageKey == "" {
		return DownloadAuthorization{}, ErrProtectedUnavailable
	}
	return s.issueDownloadTarget(ctx, request.StudentID, request.Kind, target)
}

func (s *DeliveryService) issueDownloadTarget(ctx context.Context, studentID string, kind AssetKind, target deliveryTarget) (DownloadAuthorization, error) {
	decision := s.evaluator.EvaluateTarget(ctx, studentID, target.lessonID, target.retiredAt, s.now().UTC())
	if !decision.Allowed {
		return DownloadAuthorization{}, denyProtected(decision.Reason)
	}
	url, err := s.store.PresignGetURL(ctx, target.storageKey, s.signatureLifetime)
	if err != nil {
		return DownloadAuthorization{}, ErrProtectedUnavailable
	}
	now := s.now().UTC()
	result := DownloadAuthorization{URL: url, AssetVersionID: target.assetVersionID, ExpiresAt: now.Add(s.signatureLifetime)}
	if kind == KindLabMaterial {
		result.BuyerTag = s.buyerTag(decision.EntitlementID, target.assetVersionID)
	}
	return result, nil
}

// Material is one current, ready material attached to a stable Lesson identity.
//
// AssetVersionID is internal: it names the exact version this read resolved, which S5 needs to
// bind a report context to the instance actually rendered (D-065). It carries no JSON tag and is
// never serialized — the public material contract exposes only the kind.
type Material struct {
	Kind           MaterialKind
	AssetVersionID string
}

// MaterialKinds returns only current, ready materials for stable Lesson
// identities in the approved live graph. It is a bounded bulk read and never
// signs a target or exposes storage detail.
func (s *DeliveryService) MaterialKinds(ctx context.Context, lessonIDs []string) (map[string][]Material, error) {
	result := make(map[string][]Material, len(lessonIDs))
	if len(lessonIDs) == 0 {
		return result, nil
	}
	// One row per (Lesson, kind), selecting the same version D-064's stable entry route resolves,
	// so a report context and a download resolve the identical instance. Still one query.
	rows, err := s.db.Query(ctx, `
		SELECT lesson_identity_id, kind, asset_version_id
		FROM (
			SELECT DISTINCT ON (cl.lesson_identity_id, lf.kind)
				cl.lesson_identity_id::text AS lesson_identity_id,
				lf.kind::text AS kind,
				mav.id::text AS asset_version_id,
				lf.kind AS ordering_kind
			FROM courses c
			JOIN course_revisions cr ON cr.id = c.live_revision_id
				AND cr.course_id = c.id AND cr.state = 'APPROVED'
			JOIN course_sections cs ON cs.revision_id = cr.id AND cs.course_id = c.id
			JOIN course_lessons cl ON cl.section_id = cs.id AND cl.course_id = c.id
			JOIN lesson_files lf ON lf.lesson_id = cl.id
			JOIN media_asset_versions mav ON mav.id = lf.asset_version_id
				AND mav.kind::text = lf.kind::text AND mav.state = 'READY'
			JOIN scan_attempts scan ON scan.id = mav.successful_scan_attempt_id
				AND scan.asset_version_id = mav.id
				AND scan.storage_object_version = mav.storage_object_version
				AND scan.outcome = 'PASSED'
			WHERE cl.lesson_identity_id = ANY($1::uuid[])
			ORDER BY cl.lesson_identity_id, lf.kind, lf.position ASC, lf.id ASC, mav.created_at DESC
		) selected
		ORDER BY lesson_identity_id ASC,
			CASE ordering_kind WHEN 'RESOURCE' THEN 0 WHEN 'LAB_MATERIAL' THEN 1 ELSE 2 END ASC
	`, lessonIDs)
	if err != nil {
		return nil, fmt.Errorf("reading current lesson material kinds: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var lessonID, kind, assetVersionID string
		if err := rows.Scan(&lessonID, &kind, &assetVersionID); err != nil {
			return nil, fmt.Errorf("scanning current lesson material kinds: %w", err)
		}
		var materialKind MaterialKind
		switch AssetKind(kind) {
		case KindResource:
			materialKind = MaterialResource
		case KindLabMaterial:
			materialKind = MaterialLabMaterial
		default:
			continue
		}
		duplicate := false
		for _, existing := range result[lessonID] {
			if existing.Kind == materialKind {
				duplicate = true
				break
			}
		}
		if !duplicate {
			result[lessonID] = append(result[lessonID], Material{Kind: materialKind, AssetVersionID: assetVersionID})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating current lesson material kinds: %w", err)
	}
	return result, nil
}

// IssuePreview is anonymous but has a strictly narrower S2 publication query:
// only the exact live, published preview reference can be signed.
func (s *DeliveryService) IssuePreview(ctx context.Context, assetVersionID string) (PreviewAuthorization, error) {
	if assetVersionID == "" {
		return PreviewAuthorization{}, ErrProtectedUnavailable
	}
	var target deliveryTarget
	err := s.db.QueryRow(ctx, `
		SELECT cr.preview_asset_version_id::text, mav.kind, mav.state, mav.storage_object_key, ma.retired_at
		FROM courses c
		JOIN course_revisions cr ON cr.id = c.live_revision_id AND cr.course_id = c.id
		JOIN media_asset_versions mav ON mav.id = cr.preview_asset_version_id
		JOIN media_assets ma ON ma.id = mav.logical_asset_id
		JOIN scan_attempts scan ON scan.id = mav.successful_scan_attempt_id
			AND scan.asset_version_id = mav.id
			AND scan.storage_object_version = mav.storage_object_version
			AND scan.outcome = 'PASSED'
		WHERE cr.preview_asset_version_id = $1::uuid
		  AND c.lifecycle = 'PUBLISHED'
		  AND cr.state = 'APPROVED'
		  AND mav.kind = 'PREVIEW'
		  AND ma.visibility = 'PUBLIC_PREVIEW'
	`, assetVersionID).Scan(&target.assetVersionID, &target.kind, &target.state, &target.storageKey, &target.retiredAt)
	if err != nil || target.state != StateReady || target.retiredAt != nil {
		return PreviewAuthorization{}, ErrProtectedUnavailable
	}
	url, err := s.store.PresignGetURL(ctx, target.storageKey, s.signatureLifetime)
	if err != nil {
		return PreviewAuthorization{}, ErrProtectedUnavailable
	}
	now := s.now().UTC()
	return PreviewAuthorization{URL: url, AssetVersionID: target.assetVersionID, ExpiresAt: now.Add(s.signatureLifetime)}, nil
}

func (s *DeliveryService) loadApprovedTarget(ctx context.Context, lessonID, assetVersionID string, kind AssetKind) (deliveryTarget, error) {
	var target deliveryTarget
	var assetRetiredAt *time.Time
	err := s.db.QueryRow(ctx, `
		SELECT cl.lesson_identity_id::text, mav.id::text, mav.kind, mav.state,
		       CASE WHEN mav.kind = 'VIDEO' THEN vr.storage_object_key ELSE mav.storage_object_key END,
		       COALESCE(mav.trusted_duration_ms, 0), ma.retired_at
		FROM course_lessons cl
		JOIN course_sections cs ON cs.id = cl.section_id AND cs.course_id = cl.course_id
		JOIN course_revisions cr ON cr.id = cs.revision_id AND cr.course_id = cl.course_id
		JOIN courses c ON c.id = cl.course_id
		JOIN media_asset_versions mav ON mav.id = $2::uuid
		JOIN media_assets ma ON ma.id = mav.logical_asset_id AND ma.course_id = cl.course_id
		JOIN scan_attempts scan ON scan.id = mav.successful_scan_attempt_id
			AND scan.asset_version_id = mav.id
			AND scan.storage_object_version = mav.storage_object_version
			AND scan.outcome = 'PASSED'
		LEFT JOIN LATERAL (
			SELECT storage_object_key FROM video_renditions WHERE asset_version_id = mav.id ORDER BY name LIMIT 1
		) vr ON mav.kind = 'VIDEO'
		WHERE cl.lesson_identity_id = $1::uuid
		  AND mav.kind = $3::media_asset_kind
		  AND mav.state = 'READY'
		  AND (mav.kind <> 'VIDEO' OR (
			mav.successful_processing_attempt_id IS NOT NULL
			AND mav.trusted_duration_ms IS NOT NULL
			AND vr.storage_object_key IS NOT NULL
		  ))
		  AND (
				(cl.video_asset_version_id = mav.id AND c.live_revision_id = cr.id AND cr.state = 'APPROVED')
				OR (cl.video_asset_version_id = mav.id AND cr.state = 'SUPERSEDED')
				OR (EXISTS (SELECT 1 FROM lesson_files lf WHERE lf.lesson_id = cl.id AND lf.asset_version_id = mav.id
					AND (($3::media_asset_kind = 'RESOURCE' AND lf.kind = 'RESOURCE')
						OR ($3::media_asset_kind = 'LAB_MATERIAL' AND lf.kind = 'LAB_MATERIAL')))
					AND (c.live_revision_id = cr.id AND cr.state = 'APPROVED' OR cr.state = 'SUPERSEDED'))
		  )
	`, lessonID, assetVersionID, kind).Scan(&target.lessonID, &target.assetVersionID, &target.kind, &target.state, &target.storageKey, &target.durationMS, &assetRetiredAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return deliveryTarget{}, ErrProtectedUnavailable
	}
	if err != nil {
		return deliveryTarget{}, fmt.Errorf("loading exact approved delivery target: %w", err)
	}
	target.retiredAt = assetRetiredAt
	return target, nil
}

func (s *DeliveryService) loadCurrentMaterialTarget(ctx context.Context, lessonID string, kind AssetKind) (deliveryTarget, error) {
	var target deliveryTarget
	var assetRetiredAt *time.Time
	err := s.db.QueryRow(ctx, `
		SELECT cl.lesson_identity_id::text, mav.id::text, mav.kind, mav.state, mav.storage_object_key,
		       0, ma.retired_at
		FROM course_lessons cl
		JOIN course_sections cs ON cs.id = cl.section_id AND cs.course_id = cl.course_id
		JOIN courses c ON c.id = cl.course_id AND c.live_revision_id = cs.revision_id
		JOIN course_revisions cr ON cr.id = cs.revision_id AND cr.course_id = c.id AND cr.state = 'APPROVED'
		JOIN lesson_files lf ON lf.lesson_id = cl.id AND lf.kind = $2::lesson_file_kind
		JOIN media_asset_versions mav ON mav.id = lf.asset_version_id
			AND mav.kind = $3::media_asset_kind AND mav.state = 'READY'
		JOIN media_assets ma ON ma.id = mav.logical_asset_id AND ma.course_id = cl.course_id
		JOIN scan_attempts scan ON scan.id = mav.successful_scan_attempt_id
			AND scan.asset_version_id = mav.id
			AND scan.storage_object_version = mav.storage_object_version
			AND scan.outcome = 'PASSED'
		WHERE cl.lesson_identity_id = $1::uuid
		ORDER BY lf.position ASC, lf.id ASC, mav.created_at DESC
		LIMIT 1
	`, lessonID, kind, kind).Scan(&target.lessonID, &target.assetVersionID, &target.kind, &target.state, &target.storageKey, &target.durationMS, &assetRetiredAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return deliveryTarget{}, ErrProtectedUnavailable
	}
	if err != nil {
		return deliveryTarget{}, fmt.Errorf("loading current lesson material target: %w", err)
	}
	target.retiredAt = assetRetiredAt
	return target, nil
}

func (s *DeliveryService) playbackSession(studentID, assetVersionID string, expiresAt time.Time) string {
	payload, _ := json.Marshal(struct {
		StudentID      string `json:"student_id"`
		AssetVersionID string `json:"asset_version_id"`
		ExpiresAt      int64  `json:"expires_at"`
	}{studentID, assetVersionID, expiresAt.Unix()})
	mac := hmac.New(sha256.New, s.buyerTagKey)
	_, _ = mac.Write([]byte("gradex:s4:playback-session:v1\x00"))
	_, _ = mac.Write(payload)
	signature := mac.Sum(nil)
	return base64.RawURLEncoding.EncodeToString(append(payload, signature...))
}

func (s *DeliveryService) buyerTag(entitlementID, assetVersionID string) string {
	mac := hmac.New(sha256.New, s.buyerTagKey)
	_, _ = mac.Write([]byte("gradex:s4:lab-material-buyer-tag:v1\x00"))
	_, _ = mac.Write([]byte(entitlementID))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(assetVersionID))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
