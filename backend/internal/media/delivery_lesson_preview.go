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

	"github.com/Owlah2025/gradex/backend/internal/catalogpublic"
)

// Anonymous Lesson public preview.
//
// A visitor with no Account watches a real Lesson: the same video, the same
// transcode, the same canonical HLS renditions, and the same storage objects a
// paying Student streams. What differs between the two viewers is only what the
// server proves before it hands out a manifest.
//
// The design this implements is docs/lesson-public-preview.md, and the intent
// lives in course_lessons.allow_public_preview from migration 0046.

// lessonPreviewManifestRoot is the mounted path of the anonymous preview manifest
// routes, and the prefix every issued manifest URL and every rendition link in a
// generated master is built from.
//
// It is a single constant because the issuer and the router have to agree
// exactly: a manifest URL that names a path the router does not serve produces an
// authorization that looks valid, a player that attaches, and a video that never
// decodes a frame — a failure with no error anywhere to read. The router-wiring
// test pins these exact paths, and TestLessonPreviewManifestURLIsAMountedRoute
// ties the issued URL back to this constant.
const lessonPreviewManifestRoot = "/api/v1/media/lesson-previews/"

// lessonPreviewDomain separates this signature space from every other use of the
// same key.
//
// It is a DISTINCT domain rather than a relaxed Student playback token, and that
// is a structural guarantee rather than a checked one: a preview token presented
// to a protected Student endpoint fails signature verification, and a Student
// token presented here fails the same way. Neither needs a field comparison to
// refuse the other, so neither can be made to accept the other by loosening a
// nil check.
const lessonPreviewDomain = "gradex:s4:lesson-public-preview:v1\x00"

// maxLessonPreviewLifetime caps the anonymous bearer capability a single preview
// authorization can mint.
//
// It is deliberately NOT the 12-hour Student playback ceiling. A preview token
// carries no Student, no device, and no playback lease, so there is nothing
// behind it that can be revoked mid-stream and nothing that ties it to one
// viewer — it is a shared bearer capability for the length of its life, and that
// is stated plainly rather than mitigated with DRM. Its lifetime is therefore
// bounded by the length of the thing being watched rather than by the longest
// lifetime the system tolerates elsewhere.
//
// Two hours covers any single Lesson comfortably, including a long lecture
// watched with pauses, while keeping a leaked link short-lived. The effective
// lifetime is the configured signature grace plus the server-measured trusted
// duration, clamped here — the same rule protected playback uses, because an HLS
// rendition manifest mints every segment URL up front and a fixed short expiry
// would break a long Lesson while the player was still open.
//
// THIS CAP IS ABSOLUTE.
//
// No configuration value and no branch may produce an anonymous authorization
// that outlives it; lessonPreviewLifetime funnels every path through one clamp.
// A Lesson longer than two hours is NOT given a longer token — the security
// limit is not weakened to fit the content. Such a preview expires mid-playback,
// and the viewer re-opens the preview to obtain a fresh authorization, which is
// an ordinary re-request of a still-public capability rather than an escalation.
// Raising this constant is a security decision, not a tuning one.
const maxLessonPreviewLifetime = 2 * time.Hour

// LessonPreviewRequest names the Lesson an anonymous visitor asked to preview.
//
// It carries identifiers only. Neither identifier is an authorization grant: the
// resolver below re-proves the entire chain from the Course's published state
// down to the canonical renditions on every request.
type LessonPreviewRequest struct {
	CourseID string
	LessonID string
}

// LessonPreviewAuthorization is one short-lived anonymous preview capability.
type LessonPreviewAuthorization struct {
	PreviewSession string    `json:"preview_session"`
	ManifestURL    string    `json:"manifest_url"`
	CourseID       string    `json:"course_id"`
	LessonID       string    `json:"lesson_id"`
	AssetVersionID string    `json:"asset_version_id"`
	ExpiresAt      time.Time `json:"expires_at"`
}

// lessonPreviewClaims binds a preview session to exactly one live relationship.
//
// There is no StudentID, no DeviceID, and no LeaseID, and there is no field here
// that could be added to make it one: the type grants no entitlement and names
// no viewer. Every identity in it is re-proved against live data when the token
// is presented, so a token that was valid when issued stops working the moment
// the Course is unpublished, the revision changes, the Lesson stops being
// previewable, or the asset is retired.
type lessonPreviewClaims struct {
	CourseID       string `json:"course_id"`
	RevisionID     string `json:"revision_id"`
	LessonID       string `json:"lesson_id"`
	AssetVersionID string `json:"asset_version_id"`
	ExpiresAt      int64  `json:"expires_at"`
}

// IssueLessonPreview authorizes anonymous preview of one live, publicly
// previewable Lesson video.
func (s *DeliveryService) IssueLessonPreview(
	ctx context.Context,
	request LessonPreviewRequest,
) (LessonPreviewAuthorization, error) {
	if request.CourseID == "" || request.LessonID == "" {
		return LessonPreviewAuthorization{}, ErrProtectedUnavailable
	}
	target, revisionID, err := s.loadLessonPreviewTarget(ctx, request.CourseID, request.LessonID, "")
	if err != nil {
		return LessonPreviewAuthorization{}, err
	}
	now := s.now().UTC()
	expiresAt := now.Add(lessonPreviewLifetime(s.signatureLifetime, target.durationMS))
	claims := lessonPreviewClaims{
		CourseID: request.CourseID, RevisionID: revisionID,
		LessonID: target.lessonID, AssetVersionID: target.assetVersionID,
		ExpiresAt: expiresAt.Unix(),
	}
	session := s.signLessonPreviewSession(claims)
	return LessonPreviewAuthorization{
		PreviewSession: session,
		ManifestURL:    lessonPreviewManifestRoot + session + "/index.m3u8",
		CourseID:       request.CourseID,
		LessonID:       target.lessonID,
		AssetVersionID: target.assetVersionID,
		ExpiresAt:      expiresAt,
	}, nil
}

// IssueLessonPreviewManifest renders the protected adaptive master for a
// re-authorized preview session.
//
// It shares the master renderer with Student playback, so the manifest is
// generated from the same persisted canonical renditions. It does not presign the
// original uploaded object, does not create a second master, and does not copy or
// re-encode anything.
func (s *DeliveryService) IssueLessonPreviewManifest(ctx context.Context, token string) (PlaybackManifest, error) {
	claims, err := s.authorizeLessonPreviewSession(ctx, token)
	if err != nil {
		return PlaybackManifest{}, err
	}
	root := lessonPreviewManifestRoot + token
	return s.issueMasterManifest(ctx, claims.AssetVersionID, root)
}

// IssueLessonPreviewRenditionManifest resolves the selector only inside the
// authoritative rendition set of the re-authorized exact Asset Version.
func (s *DeliveryService) IssueLessonPreviewRenditionManifest(
	ctx context.Context,
	token, selector string,
) (PlaybackManifest, error) {
	claims, err := s.authorizeLessonPreviewSession(ctx, token)
	if err != nil {
		return PlaybackManifest{}, err
	}
	return s.issueRenditionManifest(ctx, claims.AssetVersionID, selector, time.Unix(claims.ExpiresAt, 0))
}

// authorizeLessonPreviewSession verifies the signature and then re-proves the
// whole publication chain. Verification alone is not authorization: the signature
// says the claims were issued by this server, and the resolver says they are still
// true.
func (s *DeliveryService) authorizeLessonPreviewSession(ctx context.Context, token string) (lessonPreviewClaims, error) {
	claims, err := s.verifyLessonPreviewSession(token, s.now().UTC())
	if err != nil {
		return lessonPreviewClaims{}, ErrProtectedUnavailable
	}
	// Re-proved against the CURRENT live revision, and the revision in the claims
	// must be that revision. A token minted against a revision that has since been
	// superseded is refused rather than allowed to keep serving an old Lesson.
	target, revisionID, err := s.loadLessonPreviewTarget(ctx, claims.CourseID, claims.LessonID, claims.AssetVersionID)
	if err != nil {
		return lessonPreviewClaims{}, err
	}
	if !hmac.Equal([]byte(revisionID), []byte(claims.RevisionID)) {
		return lessonPreviewClaims{}, ErrProtectedUnavailable
	}
	if !hmac.Equal([]byte(target.assetVersionID), []byte(claims.AssetVersionID)) {
		return lessonPreviewClaims{}, ErrProtectedUnavailable
	}
	return claims, nil
}

// loadLessonPreviewTarget is the anonymous authorization chain.
//
// Every link is re-proved server-side against live data, and none of it is taken
// from the request:
//
//	the Course is published, unsuspended, not retired
//	 -> courses.live_revision_id names the revision
//	 -> the Lesson belongs to THAT revision
//	 -> allow_public_preview is true on that revision's row
//	 -> the Lesson carries this video asset version
//	 -> the logical Asset is not retired
//	 -> exact-version safety provenance holds
//	 -> the media is READY
//	 -> canonical renditions exist
//
// Candidate revision data can never satisfy it, because the visibility predicate
// ties the revision to courses.live_revision_id. Neither can a Lesson that was
// previewable in a previous live revision and is not in the current one.
//
// READY, not PLAYABLE. A PLAYABLE asset is deliverable but holds an incomplete
// ladder, and the partial-delivery behaviour that exists for entitled Students is
// deliberately not extended to anonymous visitors. A non-READY preview fails
// closed with the ordinary inventory-safe unavailable answer, which reveals
// nothing about why.
//
// An empty assetVersionID matches whatever video the Lesson currently carries,
// which is what issuance needs; a non-empty one pins the exact version, which is
// what re-authorization needs.
func (s *DeliveryService) loadLessonPreviewTarget(
	ctx context.Context,
	courseID, lessonID, assetVersionID string,
) (deliveryTarget, string, error) {
	var target deliveryTarget
	var revisionID string
	err := s.db.QueryRow(ctx, `
		SELECT cr.id::text, cl.lesson_identity_id::text, mav.id::text, mav.kind, mav.state,
		       mav.storage_object_key,
		       COALESCE(mav.trusted_duration_ms, (
			SELECT vr.duration_ms FROM video_renditions vr
			WHERE vr.asset_version_id = mav.id
			ORDER BY vr.created_at ASC, vr.name ASC LIMIT 1
		       ), 0),
		       EXISTS (SELECT 1 FROM video_renditions vr WHERE vr.asset_version_id = mav.id)
		FROM courses c
		JOIN course_revisions cr ON cr.course_id = c.id
		JOIN course_sections cs ON cs.revision_id = cr.id AND cs.course_id = c.id
		JOIN course_lessons cl ON cl.section_id = cs.id AND cl.course_id = c.id
		JOIN media_asset_versions mav ON mav.id = cl.video_asset_version_id
		JOIN media_assets ma ON ma.id = mav.logical_asset_id
			AND ma.course_id = c.id AND ma.retired_at IS NULL
	`+ExactVersionProvenanceJoin+`
		WHERE c.id = $1::uuid
		  AND `+catalogpublic.PublishedOnly("c", "cr")+`
		  AND cl.lesson_identity_id = $2::uuid
		  AND cl.allow_public_preview
		  AND mav.kind = 'VIDEO'
		  AND ma.kind = 'VIDEO'
		  AND mav.state = 'READY'
		  AND mav.successful_processing_attempt_id IS NOT NULL
		  AND mav.trusted_duration_ms IS NOT NULL
		  AND ($3 = '' OR mav.id = $3::uuid)
	`, courseID, lessonID, assetVersionID).Scan(
		&revisionID, &target.lessonID, &target.assetVersionID, &target.kind, &target.state,
		&target.storageKey, &target.durationMS, &target.hasRenditions,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return deliveryTarget{}, "", ErrProtectedUnavailable
	}
	if err != nil {
		return deliveryTarget{}, "", fmt.Errorf("loading public lesson preview target: %w", err)
	}
	// readyVideo rather than streamableVideo: this surface really does mean
	// finished, not watchable-now.
	if !target.readyVideo() {
		return deliveryTarget{}, "", ErrProtectedUnavailable
	}
	return target, revisionID, nil
}

// lessonPreviewLifetime is the measured-duration rule under ONE final clamp to
// the anonymous preview ceiling.
//
// The clamp is applied to every return path, deliberately, because the previous
// shape did not clamp all of them and the 2-hour cap was therefore not absolute:
//
//   - an unknown or non-positive trusted duration returned the configured grace
//     verbatim, so a deployment configuring a signature grace above two hours
//     minted anonymous tokens above the cap. Configuration must not be able to
//     raise a security limit;
//   - a trusted duration ABOVE two hours took that same early return and got
//     only the grace — a three-hour Lesson received a shorter token than a
//     ninety-minute one, which is backwards. Such a Lesson now receives the full
//     ceiling, which is the most this capability is ever allowed to grant.
//
// Every value this returns is in (0, maxLessonPreviewLifetime]. Nothing below
// may widen it, and no caller may add to it.
func lessonPreviewLifetime(grace time.Duration, durationMS int64) time.Duration {
	if grace < 0 {
		grace = 0
	}
	lifetime := grace
	if durationMS > 0 {
		measured := time.Duration(durationMS) * time.Millisecond
		// Guard the multiplication itself: a duration large enough to overflow
		// time.Duration would otherwise wrap to a negative or small value and
		// slip past a naive upper-bound check.
		if measured <= 0 || measured > maxLessonPreviewLifetime {
			return maxLessonPreviewLifetime
		}
		lifetime = grace + measured
	}
	return clampLessonPreviewLifetime(lifetime)
}

// clampLessonPreviewLifetime is the single place the anonymous preview ceiling
// is enforced. Every issued anonymous authorization passes through it.
func clampLessonPreviewLifetime(lifetime time.Duration) time.Duration {
	if lifetime > maxLessonPreviewLifetime || lifetime <= 0 {
		return maxLessonPreviewLifetime
	}
	return lifetime
}

func (s *DeliveryService) signLessonPreviewSession(claims lessonPreviewClaims) string {
	payload, _ := json.Marshal(claims)
	mac := hmac.New(sha256.New, s.buyerTagKey)
	_, _ = mac.Write([]byte(lessonPreviewDomain))
	_, _ = mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(append(payload, mac.Sum(nil)...))
}

func (s *DeliveryService) verifyLessonPreviewSession(token string, now time.Time) (lessonPreviewClaims, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) <= sha256.Size {
		return lessonPreviewClaims{}, ErrProtectedUnavailable
	}
	payload, signature := raw[:len(raw)-sha256.Size], raw[len(raw)-sha256.Size:]
	mac := hmac.New(sha256.New, s.buyerTagKey)
	_, _ = mac.Write([]byte(lessonPreviewDomain))
	_, _ = mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return lessonPreviewClaims{}, ErrProtectedUnavailable
	}
	var claims lessonPreviewClaims
	if err := json.Unmarshal(payload, &claims); err != nil ||
		claims.CourseID == "" || claims.RevisionID == "" || claims.LessonID == "" ||
		claims.AssetVersionID == "" || claims.ExpiresAt <= 0 ||
		!now.Before(time.Unix(claims.ExpiresAt, 0)) {
		return lessonPreviewClaims{}, ErrProtectedUnavailable
	}
	return claims, nil
}
