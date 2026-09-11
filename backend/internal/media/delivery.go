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

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/catalogpublic"
	"github.com/Owlah2025/gradex/backend/internal/entitlement"
	"github.com/Owlah2025/gradex/backend/internal/playback"
)

var ErrProtectedUnavailable = errors.New("protected media is unavailable")

// Playback-concurrency outcomes.
//
// These are deliberately separate from ErrProtectedUnavailable. The uniform
// protected refusal exists so a caller cannot learn about entitlement, Course
// inventory, or media identity from an error; none of that is at stake when an
// authenticated Student is told their own account is already watching
// something, and collapsing it into the uniform refusal would leave them with a
// video that will not start and no explanation.
var (
	// ErrPlaybackConflict means another trusted device of the same Account
	// holds the live lease. It carries nothing about that device.
	ErrPlaybackConflict = errors.New("protected playback is active on another device")

	// ErrPlaybackLeaseLost means the presented authorization no longer owns the
	// Account's playback: it expired, or a newer instance replaced it.
	ErrPlaybackLeaseLost = errors.New("protected playback lease is no longer held")

	// ErrPlaybackCoordinationUnavailable means authoritative playback state
	// could not be established, so nothing was decided and nothing is granted.
	ErrPlaybackCoordinationUnavailable = errors.New("protected playback coordination is unavailable")
)

// translatePlaybackError maps the coordinator's vocabulary into this package's,
// so no caller has to import both.
func translatePlaybackError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, playback.ErrHeldByAnotherDevice):
		return ErrPlaybackConflict
	case errors.Is(err, playback.ErrLeaseNotHeld):
		return ErrPlaybackLeaseLost
	default:
		return ErrPlaybackCoordinationUnavailable
	}
}

// ExactVersionProvenanceJoin admits an Asset Version only if these exact bytes
// carry one of the two legitimate safety provenances: successful exact-version
// malware-scan evidence, or D-088 trusted-validation evidence. It is an inner
// join, so an asset with neither is invisible to delivery.
//
// Public preview uses it too. D-088 originally kept every public preview
// scanner-gated; D-096 admits the MP4 public preview to the trusted profile, so
// a preview may now reach delivery on trusted-validation provenance. What keeps
// that honest is not the provenance test here but the readiness rule: a preview
// admitted on validation evidence cannot reach READY without successful FFmpeg
// processing over the exact stored object version, in both the service and the
// lifecycle trigger.
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

// PlaybackCoordinator is the cross-instance one-video-per-account authority.
//
// An interface so this package does not depend on Redis, and so the two
// operations stay distinct at the type level: acquiring is what mints a new
// playback instance, validating is a pure check that never creates one. Merging
// them into a single "ensure" call is precisely how a stale authorization would
// resurrect a lease another device legitimately took.
type PlaybackCoordinator interface {
	Acquire(ctx context.Context, lease playback.Lease) (playback.Acquisition, error)
	Validate(ctx context.Context, accountID, deviceID, leaseID string) error
	Renew(ctx context.Context, accountID, deviceID, leaseID string) (time.Time, error)
	Release(ctx context.Context, accountID, deviceID, leaseID string) error
	Settings() playback.Settings
}

type DeliveryOptions struct {
	DB                *pgxpool.Pool
	Store             DeliveryStore
	Evaluator         EntitlementEvaluator
	SignatureLifetime time.Duration
	BuyerTagKey       []byte
	Now               func() time.Time
	// Playback is required for Student protected playback and unused by every
	// other path. Its absence is refused at construction rather than tolerated:
	// a delivery service that can sign protected video but cannot coordinate it
	// would issue an unlimited number of concurrent streams per Account.
	Playback PlaybackCoordinator
}

type DeliveryService struct {
	db                *pgxpool.Pool
	store             DeliveryStore
	evaluator         EntitlementEvaluator
	signatureLifetime time.Duration
	buyerTagKey       []byte
	now               func() time.Time
	playback          PlaybackCoordinator
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
		playback: options.Playback,
	}, nil
}

type PlaybackRequest struct {
	StudentID      string
	LessonID       string
	AssetVersionID string
	// DeviceID is the trusted device the calling session is bound to, and
	// SessionID the family that asked. Both are resolved by the HTTP boundary
	// from the session cookie; a client cannot supply either.
	DeviceID  string
	SessionID string
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
	// Watermark is the server-decided Student identity the protected player
	// renders over the video (see delivery_watermark.go). It is a pointer and
	// omitted when empty because Admin review playback deliberately carries no
	// Student identity: an Admin reviewing a submitted Lesson must never be
	// handed a watermark, and absence is how that stays true by construction.
	Watermark *PlaybackWatermark `json:"watermark,omitempty"`
	// Heartbeat tells the first-party player how often to renew the account's
	// playback lease. It is published rather than assumed so the interval and
	// the server's TTL cannot drift apart across a deployment. Absent for Admin
	// review playback, which holds no lease.
	Heartbeat *PlaybackHeartbeat `json:"heartbeat,omitempty"`
}

// PlaybackHeartbeat is the renewal contract handed to the player.
type PlaybackHeartbeat struct {
	IntervalSeconds int       `json:"interval_seconds"`
	LeaseExpiresAt  time.Time `json:"lease_expires_at"`
}

// PlaybackSessionRequest binds an existing signed playback authorization to
// the trusted device making this request. A token copied to another device of
// the same Account must not validate, renew, or release the original device's
// lease.
type PlaybackSessionRequest struct {
	StudentID string
	DeviceID  string
	Token     string
}

type PlaybackRenditionRequest struct {
	PlaybackSessionRequest
	Selector string
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
	hasRenditions  bool
}

func (target deliveryTarget) readyVideo() bool {
	return target.kind == KindVideo && target.state == StateReady && target.hasRenditions
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
	if err != nil || !target.readyVideo() {
		return PlaybackAuthorization{}, ErrProtectedUnavailable
	}
	decision := s.evaluator.EvaluateTarget(ctx, request.StudentID, request.LessonID, target.retiredAt, s.now().UTC())
	if !decision.Allowed {
		return PlaybackAuthorization{}, denyProtected(decision.Reason)
	}
	// The watermark identity is read only here, after the decision above allowed
	// this Student to watch this exact version. The client contributes nothing to
	// it, and a Student whose identity cannot be read receives no authorization
	// rather than an unwatermarked one.
	watermark, err := s.playbackWatermark(ctx, request.StudentID)
	if err != nil {
		return PlaybackAuthorization{}, ErrProtectedUnavailable
	}
	// The lease is taken last, after this Student has been proven entitled to
	// this exact version. Taking it earlier would let an unentitled request
	// evict the Account's real playback before being refused.
	acquisition, err := s.acquirePlayback(ctx, request)
	if err != nil {
		return PlaybackAuthorization{}, err
	}

	now := s.now().UTC()
	expiresAt := now.Add(playbackLifetime(s.signatureLifetime, target.durationMS))
	playbackSession := s.playbackSession(playbackSessionClaims{
		StudentID: request.StudentID, LessonID: request.LessonID,
		AssetVersionID: target.assetVersionID, ExpiresAt: expiresAt.Unix(),
		DeviceID: request.DeviceID, LeaseID: acquisition.Lease.LeaseID,
	})
	return PlaybackAuthorization{
		PlaybackSession: playbackSession,
		ManifestURL:     "/api/v1/media/playback-manifests/" + playbackSession + "/index.m3u8",
		AssetVersionID:  target.assetVersionID,
		ExpiresAt:       expiresAt,
		Watermark:       watermark,
		Heartbeat: &PlaybackHeartbeat{
			IntervalSeconds: int(s.playback.Settings().HeartbeatInterval / time.Second),
			LeaseExpiresAt:  acquisition.Lease.ExpiresAt,
		},
	}, nil
}

// acquirePlayback mints one playback instance for this device.
//
// The lease identity is generated here rather than by the coordinator so the
// same value can be signed into the authorization in the same step. A lease
// nobody holds an authorization for would be a stuck slot; an authorization
// with no matching lease would be authority over nothing.
func (s *DeliveryService) acquirePlayback(ctx context.Context, request PlaybackRequest) (playback.Acquisition, error) {
	if s.playback == nil || request.DeviceID == "" {
		// Fail closed. A delivery service without coordination, or a request
		// whose session is not bound to a trusted device, must not produce
		// protected video.
		return playback.Acquisition{}, ErrPlaybackCoordinationUnavailable
	}
	leaseID, err := uuid.NewRandom()
	if err != nil {
		return playback.Acquisition{}, ErrPlaybackCoordinationUnavailable
	}
	acquisition, err := s.playback.Acquire(ctx, playback.Lease{
		LeaseID: leaseID.String(), AccountID: request.StudentID,
		DeviceID: request.DeviceID, SessionID: request.SessionID,
		LessonID: request.LessonID,
	})
	return acquisition, translatePlaybackError(err)
}

const maxPlaybackManifestBytes = 1024 * 1024

// playbackSessionDomain separates this signature space from every other use of
// the same key.
//
// The version moves to v3 because the claims now carry device and lease
// identity, and a token minted before this change describes a playback instance
// that holds no lease. Accepting one would be exactly the stale-authorization
// replay the lease exists to stop, so the domain change refuses them by
// construction. In-flight players re-request an authorization, which is the
// same thing they already do when a signature expires.
const playbackSessionDomain = "gradex:s4:playback-session:v3\x00"

type playbackSessionClaims struct {
	StudentID      string `json:"student_id"`
	LessonID       string `json:"lesson_id"`
	AssetVersionID string `json:"asset_version_id"`
	ExpiresAt      int64  `json:"expires_at"`
	// DeviceID and LeaseID bind this authorization to one playback instance on
	// one trusted device. Without them a manifest request could only be checked
	// against the Student, and every device of that Student would satisfy it.
	DeviceID string `json:"device_id"`
	LeaseID  string `json:"lease_id"`
}

// IssuePlaybackManifest returns a protected adaptive master generated only
// from persisted renditions belonging to the revalidated exact Asset Version.
func (s *DeliveryService) IssuePlaybackManifest(ctx context.Context, request PlaybackSessionRequest) (PlaybackManifest, error) {
	claims, err := s.authorizeStudentPlaybackSession(ctx, request)
	if err != nil {
		return PlaybackManifest{}, err
	}
	root := "/api/v1/media/playback-manifests/" + request.Token
	return s.issueMasterManifest(ctx, claims.AssetVersionID, root)
}

// IssuePlaybackRenditionManifest resolves the selector only inside the
// authoritative rendition set loaded for the revalidated exact Asset Version.
func (s *DeliveryService) IssuePlaybackRenditionManifest(ctx context.Context, request PlaybackRenditionRequest) (PlaybackManifest, error) {
	claims, err := s.authorizeStudentPlaybackSession(ctx, request.PlaybackSessionRequest)
	if err != nil {
		return PlaybackManifest{}, err
	}
	return s.issueRenditionManifest(ctx, claims.AssetVersionID, request.Selector, time.Unix(claims.ExpiresAt, 0))
}

// IssueAdminReviewPlayback creates a short-lived manifest session for the
// exact submitted Lesson that an Admin review route has already authorized.
func (s *DeliveryService) IssueAdminReviewPlayback(ctx context.Context, request AdminReviewPlaybackRequest) (PlaybackAuthorization, error) {
	if request.AdminAccountID == "" || request.CourseID == "" || request.RevisionID == "" || request.LessonID == "" || request.AssetVersionID == "" {
		return PlaybackAuthorization{}, ErrProtectedUnavailable
	}
	target, err := s.loadAdminReviewTarget(ctx, request)
	if err != nil || !target.readyVideo() {
		return PlaybackAuthorization{}, ErrProtectedUnavailable
	}
	now := s.now().UTC()
	expiresAt := now.Add(playbackLifetime(s.signatureLifetime, target.durationMS))
	playbackSession := s.adminReviewPlaybackSession(request, expiresAt)
	return PlaybackAuthorization{
		PlaybackSession: playbackSession,
		ManifestURL:     "/api/v1/admin/review/playback-manifests/" + playbackSession + "/index.m3u8",
		AssetVersionID:  target.assetVersionID,
		ExpiresAt:       expiresAt,
	}, nil
}

// IssueAdminReviewPlaybackManifest preserves the separate Admin-bound review
// session while sharing the safe persisted-rendition master renderer.
func (s *DeliveryService) IssueAdminReviewPlaybackManifest(ctx context.Context, adminAccountID, token string) (PlaybackManifest, error) {
	claims, err := s.authorizeAdminReviewPlaybackSession(ctx, adminAccountID, token)
	if err != nil {
		return PlaybackManifest{}, err
	}
	root := "/api/v1/admin/review/playback-manifests/" + token
	return s.issueMasterManifest(ctx, claims.AssetVersionID, root)
}

// IssueAdminReviewPlaybackRenditionManifest keeps Admin review authorization
// separate from Student entitlement while sharing rendition selection/signing.
func (s *DeliveryService) IssueAdminReviewPlaybackRenditionManifest(ctx context.Context, adminAccountID, token, selector string) (PlaybackManifest, error) {
	claims, err := s.authorizeAdminReviewPlaybackSession(ctx, adminAccountID, token)
	if err != nil {
		return PlaybackManifest{}, err
	}
	return s.issueRenditionManifest(ctx, claims.AssetVersionID, selector, time.Unix(claims.ExpiresAt, 0))
}

func (s *DeliveryService) authorizeStudentPlaybackSession(ctx context.Context, request PlaybackSessionRequest) (playbackSessionClaims, error) {
	now := s.now().UTC()
	claims, err := s.verifyPlaybackSession(request.Token, request.StudentID, now)
	if err != nil {
		return playbackSessionClaims{}, ErrProtectedUnavailable
	}
	if request.DeviceID == "" || !hmac.Equal([]byte(claims.DeviceID), []byte(request.DeviceID)) {
		return playbackSessionClaims{}, ErrPlaybackLeaseLost
	}
	target, err := s.loadApprovedTarget(ctx, claims.LessonID, claims.AssetVersionID, KindVideo)
	if err != nil || !target.readyVideo() {
		return playbackSessionClaims{}, ErrProtectedUnavailable
	}
	decision := s.evaluator.EvaluateTarget(ctx, request.StudentID, claims.LessonID, target.retiredAt, now)
	if !decision.Allowed {
		return playbackSessionClaims{}, denyProtected(decision.Reason)
	}
	// The lease is validated, never acquired. A manifest request whose lease
	// has expired or been replaced is refused and must go back through
	// authorization, which re-checks entitlement and device trust on the way.
	if err := s.validatePlayback(ctx, claims); err != nil {
		return playbackSessionClaims{}, err
	}
	return claims, nil
}

// RenewPlayback is the player heartbeat.
//
// It re-runs the same authorization the manifest path does before extending
// anything, so a heartbeat is also the runtime revalidation of a stream already
// in progress: an Entitlement that expired, a Course version that was retired,
// or a suspended Account all stop the renewal, and the first-party player stops
// with it. A heartbeat that merely refreshed a TTL would keep the Account's
// playback slot alive for a Student who is no longer allowed to watch.
//
// It renews only the exact lease presented. An older tab on the same device
// holds an older lease id and is told the lease is gone, which is how it learns
// to stop instead of fighting the newer playback for the slot.
func (s *DeliveryService) RenewPlayback(ctx context.Context, request PlaybackSessionRequest) (PlaybackHeartbeat, error) {
	claims, err := s.authorizeStudentPlaybackSession(ctx, request)
	if err != nil {
		return PlaybackHeartbeat{}, err
	}
	if s.playback == nil {
		return PlaybackHeartbeat{}, ErrPlaybackCoordinationUnavailable
	}
	expiresAt, err := s.playback.Renew(ctx, claims.StudentID, claims.DeviceID, claims.LeaseID)
	if err != nil {
		return PlaybackHeartbeat{}, translatePlaybackError(err)
	}
	return PlaybackHeartbeat{
		IntervalSeconds: int(s.playback.Settings().HeartbeatInterval / time.Second),
		LeaseExpiresAt:  expiresAt,
	}, nil
}

// ReleasePlayback is the cooperative stop: the player leaving a lesson hands
// the slot back rather than making the Student's other device wait out the TTL.
//
// It deliberately does not re-check entitlement. Giving up authority is always
// safe, and refusing to release because the Student's access just expired would
// strand the slot for the length of the TTL.
func (s *DeliveryService) ReleasePlayback(ctx context.Context, request PlaybackSessionRequest) error {
	now := s.now().UTC()
	claims, err := s.verifyPlaybackSession(request.Token, request.StudentID, now)
	if err != nil {
		return ErrProtectedUnavailable
	}
	if request.DeviceID == "" || !hmac.Equal([]byte(claims.DeviceID), []byte(request.DeviceID)) {
		return ErrPlaybackLeaseLost
	}
	if s.playback == nil {
		return ErrPlaybackCoordinationUnavailable
	}
	return translatePlaybackError(
		s.playback.Release(ctx, claims.StudentID, claims.DeviceID, claims.LeaseID),
	)
}

func (s *DeliveryService) validatePlayback(ctx context.Context, claims playbackSessionClaims) error {
	if s.playback == nil {
		return ErrPlaybackCoordinationUnavailable
	}
	return translatePlaybackError(s.playback.Validate(ctx, claims.StudentID, claims.DeviceID, claims.LeaseID))
}

func (s *DeliveryService) authorizeAdminReviewPlaybackSession(ctx context.Context, adminAccountID, token string) (adminReviewPlaybackClaims, error) {
	claims, err := s.verifyAdminReviewPlaybackSession(token, adminAccountID, s.now().UTC())
	if err != nil {
		return adminReviewPlaybackClaims{}, ErrProtectedUnavailable
	}
	target, err := s.loadAdminReviewTarget(ctx, AdminReviewPlaybackRequest{
		AdminAccountID: adminAccountID, CourseID: claims.CourseID, RevisionID: claims.RevisionID,
		LessonID: claims.LessonID, AssetVersionID: claims.AssetVersionID,
	})
	if err != nil || !target.readyVideo() {
		return adminReviewPlaybackClaims{}, ErrProtectedUnavailable
	}
	return claims, nil
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
// query still proves lifecycle, revision, exact-version safety provenance,
// kind, visibility, and retirement state before issuing the short-lived URL.
func (s *DeliveryService) IssueCoursePreview(ctx context.Context, courseID string) (PreviewAuthorization, error) {
	if courseID == "" {
		return PreviewAuthorization{}, ErrProtectedUnavailable
	}
	return s.issuePreview(ctx, `c.id = $1::uuid`, courseID)
}

func (s *DeliveryService) issuePreview(ctx context.Context, targetPredicate, targetID string) (PreviewAuthorization, error) {
	// D-096 admits the MP4 public preview to the trusted profile, so this join
	// accepts either legitimate provenance for these exact bytes. It does not
	// widen what may be signed: lifecycle, revision lineage, kind, visibility,
	// retirement, content type, and READY are all still proved below, and a
	// trusted preview only reaches READY after successful FFmpeg processing.
	var target deliveryTarget
	err := s.db.QueryRow(ctx, `
		SELECT cr.preview_asset_version_id::text, mav.kind, mav.state, mav.storage_object_key,
		       COALESCE(mav.trusted_duration_ms, 0), ma.retired_at
		FROM courses c
		JOIN course_revisions cr ON cr.id = c.live_revision_id AND cr.course_id = c.id
		JOIN media_asset_versions mav ON mav.id = cr.preview_asset_version_id
		JOIN media_assets ma ON ma.id = mav.logical_asset_id
	`+ExactVersionProvenanceJoin+`
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
	`, targetID).Scan(&target.assetVersionID, &target.kind, &target.state, &target.storageKey, &target.durationMS, &target.retiredAt)
	if err != nil || target.state != StateReady || target.retiredAt != nil {
		return PreviewAuthorization{}, ErrProtectedUnavailable
	}
	lifetime := playbackLifetime(s.signatureLifetime, target.durationMS)
	url, err := s.store.PresignGetURL(ctx, target.storageKey, lifetime)
	if err != nil {
		return PreviewAuthorization{}, ErrProtectedUnavailable
	}
	now := s.now().UTC()
	return PreviewAuthorization{URL: url, AssetVersionID: target.assetVersionID, ExpiresAt: now.Add(lifetime)}, nil
}

// maxPlaybackLifetime bounds the bearer capability a single authorization can
// mint. Trusted duration is server-measured, but ffprobe reports the duration a
// container declares, and a crafted upload can declare one far longer than the
// bytes it carries. Segment URLs cannot be revoked before their absolute
// expiry, so the lifetime is clamped rather than trusted without limit.
const maxPlaybackLifetime = 12 * time.Hour

// playbackLifetime keeps the capability valid for the trusted duration plus
// the configured grace period. HLS rendition manifests mint every segment URL
// up front, so a fixed five-minute expiry would otherwise break a 90-minute
// lecture even while the player remained open. A non-positive, absurd, or
// overflowing duration falls back to the configured grace alone.
func playbackLifetime(grace time.Duration, durationMS int64) time.Duration {
	if durationMS <= 0 || durationMS > int64(maxPlaybackLifetime/time.Millisecond) {
		return grace
	}
	lifetime := grace + time.Duration(durationMS)*time.Millisecond
	if lifetime <= 0 || lifetime > maxPlaybackLifetime+grace {
		return grace
	}
	return lifetime
}

func (s *DeliveryService) loadApprovedTarget(ctx context.Context, lessonID, assetVersionID string, kind AssetKind) (deliveryTarget, error) {
	var target deliveryTarget
	var assetRetiredAt *time.Time
	err := s.db.QueryRow(ctx, `
		SELECT cl.lesson_identity_id::text, mav.id::text, mav.kind, mav.state,
		       mav.storage_object_key, COALESCE(mav.trusted_duration_ms, 0), ma.retired_at,
		       EXISTS (SELECT 1 FROM video_renditions vr WHERE vr.asset_version_id = mav.id)
		FROM course_lessons cl
		JOIN course_sections cs ON cs.id = cl.section_id AND cs.course_id = cl.course_id
		JOIN course_revisions cr ON cr.id = cs.revision_id AND cr.course_id = cl.course_id
		JOIN courses c ON c.id = cl.course_id
		JOIN media_asset_versions mav ON mav.id = $2::uuid
		JOIN media_assets ma ON ma.id = mav.logical_asset_id AND ma.course_id = cl.course_id
	`+ExactVersionProvenanceJoin+`
		WHERE cl.lesson_identity_id = $1::uuid
		  AND mav.kind = $3::media_asset_kind
		  AND mav.state = 'READY'
		  AND (mav.kind <> 'VIDEO' OR (
			mav.successful_processing_attempt_id IS NOT NULL
			AND mav.trusted_duration_ms IS NOT NULL
			AND EXISTS (SELECT 1 FROM video_renditions vr WHERE vr.asset_version_id = mav.id)
		  ))
		  AND (
				(cl.video_asset_version_id = mav.id AND c.live_revision_id = cr.id AND cr.state = 'APPROVED')
				OR (cl.video_asset_version_id = mav.id AND cr.state = 'SUPERSEDED')
				OR (EXISTS (SELECT 1 FROM lesson_files lf WHERE lf.lesson_id = cl.id AND lf.asset_version_id = mav.id
					AND (($3::media_asset_kind = 'RESOURCE' AND lf.kind = 'RESOURCE')
						OR ($3::media_asset_kind = 'LAB_MATERIAL' AND lf.kind = 'LAB_MATERIAL')))
					AND (c.live_revision_id = cr.id AND cr.state = 'APPROVED' OR cr.state = 'SUPERSEDED'))
		  )
	`, lessonID, assetVersionID, kind).Scan(&target.lessonID, &target.assetVersionID, &target.kind, &target.state, &target.storageKey, &target.durationMS, &assetRetiredAt, &target.hasRenditions)
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
		SELECT cl.lesson_identity_id::text, mav.id::text, mav.kind, mav.state, mav.storage_object_key,
		       COALESCE(mav.trusted_duration_ms, 0),
		       EXISTS (SELECT 1 FROM video_renditions vr WHERE vr.asset_version_id = mav.id)
		FROM courses c
		JOIN course_revisions cr ON cr.id = $2::uuid AND cr.course_id = c.id AND cr.state = 'PENDING_REVIEW'
		JOIN course_sections cs ON cs.revision_id = cr.id AND cs.course_id = c.id
		JOIN course_lessons cl ON cl.section_id = cs.id AND cl.course_id = c.id
		JOIN media_asset_versions mav ON mav.id = $4::uuid AND mav.id = cl.video_asset_version_id
		JOIN media_assets ma ON ma.id = mav.logical_asset_id AND ma.course_id = c.id AND ma.retired_at IS NULL
	`+ExactVersionProvenanceJoin+`
		WHERE c.id = $1::uuid AND cl.lesson_identity_id = $3::uuid
		  AND mav.kind = 'VIDEO' AND mav.state = 'READY'
		  AND mav.successful_processing_attempt_id IS NOT NULL AND mav.trusted_duration_ms IS NOT NULL
	`, request.CourseID, request.RevisionID, request.LessonID, request.AssetVersionID).Scan(
		&target.lessonID, &target.assetVersionID, &target.kind, &target.state, &target.storageKey, &target.durationMS, &target.hasRenditions,
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

func (s *DeliveryService) playbackSession(claims playbackSessionClaims) string {
	payload, _ := json.Marshal(claims)
	mac := hmac.New(sha256.New, s.buyerTagKey)
	_, _ = mac.Write([]byte(playbackSessionDomain))
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
	_, _ = mac.Write([]byte(playbackSessionDomain))
	_, _ = mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return playbackSessionClaims{}, ErrProtectedUnavailable
	}
	var claims playbackSessionClaims
	if err := json.Unmarshal(payload, &claims); err != nil || claims.StudentID == "" ||
		claims.LessonID == "" || claims.AssetVersionID == "" || claims.ExpiresAt <= 0 ||
		claims.DeviceID == "" || claims.LeaseID == "" ||
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

// AdminReviewPreviewRequest is the exact candidate revision public preview an
// already-authorized Admin is reviewing. Capability enforcement stays at the
// HTTP boundary; every content rule below is proved here.
type AdminReviewPreviewRequest struct {
	AdminAccountID string
	CourseID       string
	RevisionID     string
	AssetVersionID string
}

// IssueAdminReviewPreview signs the public preview owned by the exact submitted
// revision an Admin is reviewing.
//
// # WHY THIS IS NOT IssueCoursePreview
//
// The public route deliberately requires `c.live_revision_id = cr.id` and
// `cr.state = 'APPROVED'`, because a preview must not be openly published
// before the Course is. A revision under review satisfies neither, so an Admin
// could read that a preview existed and never watch the thing they were
// approving. Relaxing the public predicate would have made every candidate
// preview anonymously reachable, which is why this is a separate, authenticated
// issuance instead.
//
// # WHAT IT DOES NOT RELAX
//
// Every other rule the public query proves is proved here too: PREVIEW kind on
// both the Asset and its Version, `PUBLIC_PREVIEW` visibility, `video/mp4`
// content type, READY state, a non-retired logical Asset, exact-version safety
// provenance, and preview-origin lineage. It adds two: the revision must be the
// exact one named, in `PENDING_REVIEW`, and the Asset Version must be the one
// that revision points at. It grants nothing beyond one expiring URL for those
// exact bytes, and mints no session, entitlement, or enrollment.
func (s *DeliveryService) IssueAdminReviewPreview(ctx context.Context, request AdminReviewPreviewRequest) (PreviewAuthorization, error) {
	if request.AdminAccountID == "" || request.CourseID == "" || request.RevisionID == "" || request.AssetVersionID == "" {
		return PreviewAuthorization{}, ErrProtectedUnavailable
	}
	var target deliveryTarget
	err := s.db.QueryRow(ctx, `
		SELECT cr.preview_asset_version_id::text, mav.kind, mav.state, mav.storage_object_key, ma.retired_at
		FROM courses c
		JOIN course_revisions cr ON cr.id = $2::uuid AND cr.course_id = c.id AND cr.state = 'PENDING_REVIEW'
		JOIN media_asset_versions mav ON mav.id = cr.preview_asset_version_id AND mav.id = $3::uuid
		JOIN media_assets ma ON ma.id = mav.logical_asset_id
	`+ExactVersionProvenanceJoin+`
		WHERE c.id = $1::uuid
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
	`, request.CourseID, request.RevisionID, request.AssetVersionID).Scan(
		&target.assetVersionID, &target.kind, &target.state, &target.storageKey, &target.retiredAt,
	)
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
