package media

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/catalogpublic"
	"github.com/Owlah2025/gradex/backend/internal/entitlement"
)

var ErrProtectedUnavailable = errors.New("protected media is unavailable")

// ExactVersionProvenanceJoin admits an Asset Version only if these exact bytes
// carry one of the two legitimate safety provenances: successful exact-version
// malware-scan evidence, or D-088 trusted-validation evidence. It is an inner
// join, so an asset with neither is invisible to delivery.
//
// Public preview deliberately does not use it. D-088 keeps every public preview
// scanner-gated, so IssuePreview joins scan_attempts directly and no validation
// evidence can satisfy it.
//
// It is a compile-time constant spliced into queries that already bind their
// own parameters; it carries no caller input.
const ExactVersionProvenanceJoin = `
		JOIN LATERAL (SELECT 1 AS admitted) provenance ON (
			EXISTS (
				SELECT 1 FROM scan_attempts sa
				WHERE sa.id = mav.successful_scan_attempt_id
				  AND sa.asset_version_id = mav.id
				  AND sa.storage_object_version = mav.storage_object_version
				  AND sa.outcome = 'PASSED'
			)
			OR EXISTS (
				SELECT 1 FROM validation_attempts va
				WHERE va.id = mav.successful_validation_attempt_id
				  AND va.asset_version_id = mav.id
				  AND va.storage_object_version = mav.storage_object_version
				  AND va.outcome = 'PASSED'
			)
		)`

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

// AdminReviewPlaybackRequest is the exact submitted Lesson target that an
// already-authorized Admin is reviewing. Capability enforcement stays at the
// HTTP boundary; this service binds the signed playback session to that Admin.
type AdminReviewPlaybackRequest struct {
	AdminAccountID string
	CourseID       string
	RevisionID     string
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

// LessonFileDownloadRequest selects one revision-scoped attachment through a
// stable Lesson identity. FileID is only a selector: the live-graph query and
// entitlement evaluator below independently decide whether it is deliverable.
type LessonFileDownloadRequest struct {
	StudentID string
	CourseID  string
	LessonID  string
	FileID    string
	Locale    string
}

type PlaybackAuthorization struct {
	PlaybackSession string    `json:"playback_session"`
	ManifestURL     string    `json:"manifest_url"`
	AssetVersionID  string    `json:"asset_version_id"`
	ExpiresAt       time.Time `json:"expires_at"`
}

// PlaybackManifest carries only the rewritten HLS manifest text. Video
// segments remain direct private-storage responses through exact presigned
// URLs and never pass through the API process.
type PlaybackManifest struct {
	Contents []byte
}

type adminReviewPlaybackClaims struct {
	AdminAccountID string `json:"admin_account_id"`
	CourseID       string `json:"course_id"`
	RevisionID     string `json:"revision_id"`
	LessonID       string `json:"lesson_id"`
	AssetVersionID string `json:"asset_version_id"`
	ExpiresAt      int64  `json:"expires_at"`
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
	downloadName   string
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
	now := s.now().UTC()
	expiresAt := now.Add(s.signatureLifetime)
	playbackSession := s.playbackSession(request.StudentID, request.LessonID, target.assetVersionID, expiresAt)
	return PlaybackAuthorization{
		PlaybackSession: playbackSession,
		ManifestURL:     "/api/v1/media/playback-manifests/" + playbackSession + "/index.m3u8",
		AssetVersionID:  target.assetVersionID,
		ExpiresAt:       expiresAt,
	}, nil
}

const maxPlaybackManifestBytes = 1024 * 1024

type playbackSessionClaims struct {
	StudentID      string `json:"student_id"`
	LessonID       string `json:"lesson_id"`
	AssetVersionID string `json:"asset_version_id"`
	ExpiresAt      int64  `json:"expires_at"`
}

// IssuePlaybackManifest revalidates the authenticated Student and exact
// approved Asset Version before reading one small rendition playlist. Every
// media URI is replaced with an exact-object private-storage capability whose
// expiry cannot exceed the playback session. Segment bytes remain direct S3
// responses.
func (s *DeliveryService) IssuePlaybackManifest(ctx context.Context, studentID, token string) (PlaybackManifest, error) {
	now := s.now().UTC()
	claims, err := s.verifyPlaybackSession(token, studentID, now)
	if err != nil {
		return PlaybackManifest{}, ErrProtectedUnavailable
	}
	target, err := s.loadApprovedTarget(ctx, claims.LessonID, claims.AssetVersionID, KindVideo)
	if err != nil || target.state != StateReady || target.storageKey == "" {
		return PlaybackManifest{}, ErrProtectedUnavailable
	}
	decision := s.evaluator.EvaluateTarget(ctx, studentID, claims.LessonID, target.retiredAt, now)
	if !decision.Allowed {
		return PlaybackManifest{}, denyProtected(decision.Reason)
	}
	contents, err := s.store.DownloadObject(ctx, target.storageKey)
	if err != nil || len(contents) == 0 || len(contents) > maxPlaybackManifestBytes {
		return PlaybackManifest{}, ErrProtectedUnavailable
	}
	rewritten, err := s.rewritePlaybackManifest(ctx, target.storageKey, contents, time.Unix(claims.ExpiresAt, 0).Sub(now))
	if err != nil {
		return PlaybackManifest{}, ErrProtectedUnavailable
	}
	return PlaybackManifest{Contents: rewritten}, nil
}

// IssueAdminReviewPlayback creates a short-lived manifest session for the
// exact submitted Lesson that an Admin review route has already authorized.
func (s *DeliveryService) IssueAdminReviewPlayback(ctx context.Context, request AdminReviewPlaybackRequest) (PlaybackAuthorization, error) {
	if request.AdminAccountID == "" || request.CourseID == "" || request.RevisionID == "" || request.LessonID == "" || request.AssetVersionID == "" {
		return PlaybackAuthorization{}, ErrProtectedUnavailable
	}
	target, err := s.loadAdminReviewTarget(ctx, request)
	if err != nil || target.state != StateReady || target.storageKey == "" {
		return PlaybackAuthorization{}, ErrProtectedUnavailable
	}
	now := s.now().UTC()
	expiresAt := now.Add(s.signatureLifetime)
	playbackSession := s.adminReviewPlaybackSession(request, expiresAt)
	return PlaybackAuthorization{
		PlaybackSession: playbackSession,
		ManifestURL:     "/api/v1/admin/review/playback-manifests/" + playbackSession + "/index.m3u8",
		AssetVersionID:  target.assetVersionID,
		ExpiresAt:       expiresAt,
	}, nil
}

// IssueAdminReviewPlaybackManifest revalidates the Admin-bound submitted
// revision target before signing the manifest's private storage references.
func (s *DeliveryService) IssueAdminReviewPlaybackManifest(ctx context.Context, adminAccountID, token string) (PlaybackManifest, error) {
	now := s.now().UTC()
	claims, err := s.verifyAdminReviewPlaybackSession(token, adminAccountID, now)
	if err != nil {
		return PlaybackManifest{}, ErrProtectedUnavailable
	}
	target, err := s.loadAdminReviewTarget(ctx, AdminReviewPlaybackRequest{
		AdminAccountID: adminAccountID, CourseID: claims.CourseID, RevisionID: claims.RevisionID,
		LessonID: claims.LessonID, AssetVersionID: claims.AssetVersionID,
	})
	if err != nil || target.state != StateReady || target.storageKey == "" {
		return PlaybackManifest{}, ErrProtectedUnavailable
	}
	contents, err := s.store.DownloadObject(ctx, target.storageKey)
	if err != nil || len(contents) == 0 || len(contents) > maxPlaybackManifestBytes {
		return PlaybackManifest{}, ErrProtectedUnavailable
	}
	rewritten, err := s.rewritePlaybackManifest(ctx, target.storageKey, contents, time.Unix(claims.ExpiresAt, 0).Sub(now))
	if err != nil {
		return PlaybackManifest{}, ErrProtectedUnavailable
	}
	return PlaybackManifest{Contents: rewritten}, nil
}

func (s *DeliveryService) rewritePlaybackManifest(ctx context.Context, storageKey string, contents []byte, expiry time.Duration) ([]byte, error) {
	if expiry <= 0 {
		return nil, ErrProtectedUnavailable
	}
	directory := path.Dir(storageKey)
	lines := strings.Split(string(contents), "\n")
	type mediaReference struct {
		lineIndex int
		objectKey string
	}
	references := make([]mediaReference, 0)
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			if strings.Contains(strings.ToUpper(trimmed), "URI=") {
				return nil, errors.New("HLS tag URI requires explicit rewriting support")
			}
			continue
		}
		parsed, err := url.Parse(trimmed)
		if err != nil || parsed.IsAbs() || parsed.Host != "" || strings.HasPrefix(parsed.Path, "/") ||
			parsed.RawQuery != "" || parsed.Fragment != "" {
			return nil, errors.New("HLS media reference must be a plain relative path")
		}
		clean := path.Clean(parsed.Path)
		if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
			return nil, errors.New("HLS media reference escapes its rendition directory")
		}
		objectKey := path.Join(directory, clean)
		if !strings.HasPrefix(objectKey, directory+"/") {
			return nil, errors.New("HLS media reference escapes its rendition directory")
		}
		references = append(references, mediaReference{lineIndex: i, objectKey: objectKey})
	}
	if len(references) == 0 {
		return nil, errors.New("HLS manifest contains no media references")
	}
	for _, reference := range references {
		signed, err := s.store.PresignGetURL(ctx, reference.objectKey, expiry)
		if err != nil {
			return nil, err
		}
		lines[reference.lineIndex] = signed
	}
	return []byte(strings.Join(lines, "\n")), nil
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

// IssueLessonFileDownload issues a private download only when the requested
// attachment is still part of the Course's current approved Lesson graph. The
// attachment ID never substitutes for Course access: it is joined to the
// requested Course and live revision before the canonical evaluator runs.
func (s *DeliveryService) IssueLessonFileDownload(ctx context.Context, request LessonFileDownloadRequest) (DownloadAuthorization, error) {
	if request.StudentID == "" || request.CourseID == "" || request.LessonID == "" || request.FileID == "" {
		return DownloadAuthorization{}, ErrProtectedUnavailable
	}
	target, err := s.loadCurrentMaterialFileTarget(ctx, request.CourseID, request.LessonID, request.FileID, request.Locale)
	if err != nil || target.state != StateReady || target.storageKey == "" {
		return DownloadAuthorization{}, ErrProtectedUnavailable
	}
	return s.issueDownloadTarget(ctx, request.StudentID, target.kind, target)
}

func (s *DeliveryService) issueDownloadTarget(ctx context.Context, studentID string, kind AssetKind, target deliveryTarget) (DownloadAuthorization, error) {
	decision := s.evaluator.EvaluateTarget(ctx, studentID, target.lessonID, target.retiredAt, s.now().UTC())
	if !decision.Allowed {
		return DownloadAuthorization{}, denyProtected(decision.Reason)
	}
	url, err := s.presignDownload(ctx, target.storageKey, target.downloadName)
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

// namedDownloadStore is intentionally optional so existing test stores that
// model signing alone remain valid. Production storage implements it and
// supplies an attachment Content-Disposition for Student downloads.
type namedDownloadStore interface {
	PresignGetDownloadURL(context.Context, string, string, time.Duration) (string, error)
}

func (s *DeliveryService) presignDownload(ctx context.Context, key, filename string) (string, error) {
	if filename != "" {
		if store, ok := s.store.(namedDownloadStore); ok {
			return store.PresignGetDownloadURL(ctx, key, filename, s.signatureLifetime)
		}
	}
	return s.store.PresignGetURL(ctx, key, s.signatureLifetime)
}

// Material is one current, ready material attached to a stable Lesson identity.
//
// AssetVersionID is internal: it names the exact version this read resolved, which S5 needs to
// bind a report context to the instance actually rendered (D-065). It carries no JSON tag and is
// never serialized — the public material contract exposes only the kind.
type Material struct {
	FileID         string
	Kind           MaterialKind
	AssetVersionID string
	DisplayNameAr  string
	DisplayNameEn  string
	ContentType    string
	SizeBytes      int64
}

// MaterialKinds returns every current, ready attachment for stable Lesson
// identities in the approved live graph. It is a bounded bulk read and never
// signs a target or exposes storage detail.
func (s *DeliveryService) MaterialKinds(ctx context.Context, lessonIDs []string) (map[string][]Material, error) {
	result := make(map[string][]Material, len(lessonIDs))
	if len(lessonIDs) == 0 {
		return result, nil
	}
	// One bounded row per live attachment. The exact Asset Version remains internal
	// for report-context minting; Student read models receive only display metadata
	// plus an authorization route generated at the HTTP boundary.
	rows, err := s.db.Query(ctx, `
		SELECT cl.lesson_identity_id::text, lf.id::text, lf.kind::text, mav.id::text,
		       lf.display_name_ar, lf.display_name_en, mav.content_type, mav.size_bytes
		FROM courses c
		JOIN course_revisions cr ON cr.id = c.live_revision_id
			AND cr.course_id = c.id AND cr.state = 'APPROVED'
		JOIN course_sections cs ON cs.revision_id = cr.id AND cs.course_id = c.id
		JOIN course_lessons cl ON cl.section_id = cs.id AND cl.course_id = c.id
		JOIN lesson_files lf ON lf.lesson_id = cl.id
		JOIN media_asset_versions mav ON mav.id = lf.asset_version_id
			AND mav.kind::text = lf.kind::text AND mav.state = 'READY'
	`+ExactVersionProvenanceJoin+`
		WHERE cl.lesson_identity_id = ANY($1::uuid[])
		ORDER BY cl.lesson_identity_id ASC,
			CASE lf.kind WHEN 'RESOURCE' THEN 0 WHEN 'LAB_MATERIAL' THEN 1 ELSE 2 END ASC,
			lf.position ASC, lf.id ASC
	`, lessonIDs)
	if err != nil {
		return nil, fmt.Errorf("reading current lesson material kinds: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var lessonID, fileID, kind, assetVersionID string
		var material Material
		if err := rows.Scan(&lessonID, &fileID, &kind, &assetVersionID,
			&material.DisplayNameAr, &material.DisplayNameEn, &material.ContentType, &material.SizeBytes,
		); err != nil {
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
		material.FileID = fileID
		material.Kind = materialKind
		material.AssetVersionID = assetVersionID
		result[lessonID] = append(result[lessonID], material)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating current lesson material kinds: %w", err)
	}
	return result, nil
}

// IssuePreview is anonymous but has a strictly narrower publication query:
// only the exact live, published preview reference can be signed. It remains
// for safe legacy callers; public Course Details uses IssueCoursePreview so it
// never has to learn an Asset Version identifier.
func (s *DeliveryService) IssuePreview(ctx context.Context, assetVersionID string) (PreviewAuthorization, error) {
	if assetVersionID == "" {
		return PreviewAuthorization{}, ErrProtectedUnavailable
	}
	return s.issuePreview(ctx, `cr.preview_asset_version_id = $1::uuid`, assetVersionID)
}

// IssueCoursePreview signs only the current live preview for the requested
// public Course. The Course identifier is not an authorization grant: the
// query still proves lifecycle, revision, exact scanner evidence, kind,
// visibility, and retirement state before issuing the short-lived URL.
func (s *DeliveryService) IssueCoursePreview(ctx context.Context, courseID string) (PreviewAuthorization, error) {
	if courseID == "" {
		return PreviewAuthorization{}, ErrProtectedUnavailable
	}
	return s.issuePreview(ctx, `c.id = $1::uuid`, courseID)
}

func (s *DeliveryService) issuePreview(ctx context.Context, targetPredicate, targetID string) (PreviewAuthorization, error) {
	// A public preview stays scanner-gated under D-088. This join is
	// deliberately scan_attempts and not ExactVersionProvenanceJoin: no
	// trusted-validation evidence may ever make bytes publicly readable.
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
		WHERE `+targetPredicate+`
		  AND `+catalogpublic.PublishedOnly("c", "cr")+`
		  AND cr.state = 'APPROVED'
		  AND mav.kind = 'PREVIEW'
		  AND ma.kind = 'PREVIEW'
		  AND mav.content_type = 'video/mp4'
		  AND ma.visibility = 'PUBLIC_PREVIEW'
		  AND EXISTS (
			WITH RECURSIVE lineage AS (
				SELECT cr.id, cr.based_on_revision_id
				UNION ALL
				SELECT parent.id, parent.based_on_revision_id
				FROM course_revisions parent
				JOIN lineage child ON child.based_on_revision_id = parent.id
			)
			SELECT 1 FROM lineage WHERE lineage.id = ma.preview_origin_revision_id
		  )
	`, targetID).Scan(&target.assetVersionID, &target.kind, &target.state, &target.storageKey, &target.retiredAt)
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
	`+ExactVersionProvenanceJoin+`
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

func (s *DeliveryService) loadAdminReviewTarget(ctx context.Context, request AdminReviewPlaybackRequest) (deliveryTarget, error) {
	var target deliveryTarget
	err := s.db.QueryRow(ctx, `
		SELECT cl.lesson_identity_id::text, mav.id::text, mav.kind, mav.state, vr.storage_object_key,
		       COALESCE(mav.trusted_duration_ms, 0)
		FROM courses c
		JOIN course_revisions cr ON cr.id = $2::uuid AND cr.course_id = c.id AND cr.state = 'PENDING_REVIEW'
		JOIN course_sections cs ON cs.revision_id = cr.id AND cs.course_id = c.id
		JOIN course_lessons cl ON cl.section_id = cs.id AND cl.course_id = c.id
		JOIN media_asset_versions mav ON mav.id = $4::uuid AND mav.id = cl.video_asset_version_id
		JOIN media_assets ma ON ma.id = mav.logical_asset_id AND ma.course_id = c.id AND ma.retired_at IS NULL
	`+ExactVersionProvenanceJoin+`
		JOIN LATERAL (
			SELECT storage_object_key FROM video_renditions WHERE asset_version_id = mav.id ORDER BY name LIMIT 1
		) vr ON true
		WHERE c.id = $1::uuid AND cl.lesson_identity_id = $3::uuid
		  AND mav.kind = 'VIDEO' AND mav.state = 'READY'
		  AND mav.successful_processing_attempt_id IS NOT NULL AND mav.trusted_duration_ms IS NOT NULL
	`, request.CourseID, request.RevisionID, request.LessonID, request.AssetVersionID).Scan(
		&target.lessonID, &target.assetVersionID, &target.kind, &target.state, &target.storageKey, &target.durationMS,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return deliveryTarget{}, ErrProtectedUnavailable
	}
	if err != nil {
		return deliveryTarget{}, fmt.Errorf("loading submitted review delivery target: %w", err)
	}
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
	`+ExactVersionProvenanceJoin+`
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

// loadCurrentMaterialFileTarget binds a revision-scoped lesson_files row to
// the requested Course's current approved graph. Unlike the historical
// category route, this supports more than one Resource or Lab Material while
// retaining the same fail-closed provenance and retirement checks.
func (s *DeliveryService) loadCurrentMaterialFileTarget(ctx context.Context, courseID, lessonID, fileID, locale string) (deliveryTarget, error) {
	var target deliveryTarget
	var assetRetiredAt *time.Time
	var displayNameAr, displayNameEn string
	err := s.db.QueryRow(ctx, `
		SELECT cl.lesson_identity_id::text, mav.id::text, mav.kind, mav.state, mav.storage_object_key,
		       0, ma.retired_at, lf.display_name_ar, lf.display_name_en
		FROM courses c
		JOIN course_revisions cr ON cr.id = c.live_revision_id
			AND cr.course_id = c.id AND cr.state = 'APPROVED'
		JOIN course_sections cs ON cs.revision_id = cr.id AND cs.course_id = c.id
		JOIN course_lessons cl ON cl.section_id = cs.id AND cl.course_id = c.id
		JOIN lesson_files lf ON lf.lesson_id = cl.id AND lf.id = $3::uuid
		JOIN media_asset_versions mav ON mav.id = lf.asset_version_id
			AND mav.kind::text = lf.kind::text AND mav.state = 'READY'
		JOIN media_assets ma ON ma.id = mav.logical_asset_id AND ma.course_id = c.id
	`+ExactVersionProvenanceJoin+`
		WHERE c.id = $1::uuid AND cl.lesson_identity_id = $2::uuid
	`, courseID, lessonID, fileID).Scan(
		&target.lessonID, &target.assetVersionID, &target.kind, &target.state, &target.storageKey,
		&target.durationMS, &assetRetiredAt, &displayNameAr, &displayNameEn,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return deliveryTarget{}, ErrProtectedUnavailable
	}
	if err != nil {
		return deliveryTarget{}, fmt.Errorf("loading current lesson material file target: %w", err)
	}
	if target.kind != KindResource && target.kind != KindLabMaterial {
		return deliveryTarget{}, ErrProtectedUnavailable
	}
	if locale == "en" {
		target.downloadName = displayNameEn
	} else {
		target.downloadName = displayNameAr
	}
	target.retiredAt = assetRetiredAt
	return target, nil
}

func (s *DeliveryService) playbackSession(studentID, lessonID, assetVersionID string, expiresAt time.Time) string {
	payload, _ := json.Marshal(playbackSessionClaims{
		StudentID: studentID, LessonID: lessonID, AssetVersionID: assetVersionID, ExpiresAt: expiresAt.Unix(),
	})
	mac := hmac.New(sha256.New, s.buyerTagKey)
	_, _ = mac.Write([]byte("gradex:s4:playback-session:v2\x00"))
	_, _ = mac.Write(payload)
	signature := mac.Sum(nil)
	return base64.RawURLEncoding.EncodeToString(append(payload, signature...))
}

func (s *DeliveryService) adminReviewPlaybackSession(request AdminReviewPlaybackRequest, expiresAt time.Time) string {
	payload, _ := json.Marshal(adminReviewPlaybackClaims{
		AdminAccountID: request.AdminAccountID, CourseID: request.CourseID, RevisionID: request.RevisionID,
		LessonID: request.LessonID, AssetVersionID: request.AssetVersionID, ExpiresAt: expiresAt.Unix(),
	})
	mac := hmac.New(sha256.New, s.buyerTagKey)
	_, _ = mac.Write([]byte("gradex:s2:admin-review-playback:v1\x00"))
	_, _ = mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(append(payload, mac.Sum(nil)...))
}

func (s *DeliveryService) verifyPlaybackSession(token, studentID string, now time.Time) (playbackSessionClaims, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) <= sha256.Size {
		return playbackSessionClaims{}, ErrProtectedUnavailable
	}
	payload, signature := raw[:len(raw)-sha256.Size], raw[len(raw)-sha256.Size:]
	mac := hmac.New(sha256.New, s.buyerTagKey)
	_, _ = mac.Write([]byte("gradex:s4:playback-session:v2\x00"))
	_, _ = mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return playbackSessionClaims{}, ErrProtectedUnavailable
	}
	var claims playbackSessionClaims
	if err := json.Unmarshal(payload, &claims); err != nil || claims.StudentID == "" ||
		claims.LessonID == "" || claims.AssetVersionID == "" || claims.ExpiresAt <= 0 ||
		claims.StudentID != studentID || !now.Before(time.Unix(claims.ExpiresAt, 0)) {
		return playbackSessionClaims{}, ErrProtectedUnavailable
	}
	return claims, nil
}

func (s *DeliveryService) verifyAdminReviewPlaybackSession(token, adminAccountID string, now time.Time) (adminReviewPlaybackClaims, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) <= sha256.Size {
		return adminReviewPlaybackClaims{}, ErrProtectedUnavailable
	}
	payload, signature := raw[:len(raw)-sha256.Size], raw[len(raw)-sha256.Size:]
	mac := hmac.New(sha256.New, s.buyerTagKey)
	_, _ = mac.Write([]byte("gradex:s2:admin-review-playback:v1\x00"))
	_, _ = mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return adminReviewPlaybackClaims{}, ErrProtectedUnavailable
	}
	var claims adminReviewPlaybackClaims
	if err := json.Unmarshal(payload, &claims); err != nil || claims.AdminAccountID == "" || claims.CourseID == "" ||
		claims.RevisionID == "" || claims.LessonID == "" || claims.AssetVersionID == "" || claims.ExpiresAt <= 0 ||
		claims.AdminAccountID != adminAccountID || !now.Before(time.Unix(claims.ExpiresAt, 0)) {
		return adminReviewPlaybackClaims{}, ErrProtectedUnavailable
	}
	return claims, nil
}

func (s *DeliveryService) buyerTag(entitlementID, assetVersionID string) string {
	mac := hmac.New(sha256.New, s.buyerTagKey)
	_, _ = mac.Write([]byte("gradex:s4:lab-material-buyer-tag:v1\x00"))
	_, _ = mac.Write([]byte(entitlementID))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(assetVersionID))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
