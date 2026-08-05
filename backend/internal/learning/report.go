package learning

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Content report creation (FR-029, FR-030, BR-145).
//
// A report has to stay meaningful after the content changes underneath it, so it records two
// things that are deliberately different: the **stable logical target** the Student reported, and
// the **exact visible revision or Asset Version** at the moment of submission. The stable target is
// what an Admin follows to today's content; the exact version is what the Student actually saw. A
// report that stored only the stable target would silently re-point at whatever is live when it is
// eventually read, which is exactly the evidence an Admin needs and cannot reconstruct.
//
// "Exact visible" means the instance rendered to the Student (D-065), not whatever is live when
// the submission arrives. Resolving current content at submission records the wrong instance the
// moment a page sits open across a revision or Asset Version change. The rendered instance is
// therefore captured at render time as a signed report context and arrives here as an
// already-verified binding; this package re-verifies that the binding forms a coherent, genuinely
// historical target before storing it.
//
// Scope: this is the domain boundary only. FR-033's Entitlement refusal is T063's route, the
// 5/hour throttle is T064's, and resolution is S8's. Nothing here moderates, hides, or alters
// reported content (FR-031, FR-035).

var (
	// ErrReportInvalid marks a request the Student could correct: an unknown kind or reason, a
	// malformed identifier, or a missing explanation where the reason requires one.
	ErrReportInvalid = errors.New("learning report request is invalid")

	// ErrReportTargetUnavailable is the single outcome for every target the Student cannot report:
	// not visible in the live approved graph, not theirs, the wrong kind for that target, or absent
	// entirely. One error for all of them, because distinguishing them confirms what exists.
	ErrReportTargetUnavailable = errors.New("learning report target is unavailable")

	// ErrReportDuplicate is the Student's own still-open report for the same target. It is distinct
	// from the throttle in T064 on purpose: they fail differently, so they are reported differently
	// (R-11).
	ErrReportDuplicate = errors.New("learning report already exists for this target")
)

// ReportTargetKind mirrors the closed `rep_target_kind` enumeration. Widening it is a migration.
type ReportTargetKind string

const (
	ReportTargetCourse      ReportTargetKind = "COURSE"
	ReportTargetLesson      ReportTargetKind = "LESSON"
	ReportTargetVideo       ReportTargetKind = "VIDEO"
	ReportTargetResource    ReportTargetKind = "RESOURCE"
	ReportTargetLabMaterial ReportTargetKind = "LAB_MATERIAL"
)

// ReportReason mirrors the closed `rep_reason` enumeration.
type ReportReason string

const (
	ReasonBrokenUnavailable          ReportReason = "broken_unavailable"
	ReasonInaccurate                 ReportReason = "inaccurate"
	ReasonInappropriate              ReportReason = "inappropriate"
	ReasonSuspectedCopyrightViolatio ReportReason = "suspected_copyright_violation"
	ReasonOther                      ReportReason = "other"
)

func (k ReportTargetKind) valid() bool {
	switch k {
	case ReportTargetCourse, ReportTargetLesson, ReportTargetVideo, ReportTargetResource, ReportTargetLabMaterial:
		return true
	}
	return false
}

// ValidReportReason exposes the closed reason set to the route, so the HTTP boundary can answer a
// reason it cannot accept as ordinary public validation without duplicating the enumeration. The
// domain still re-checks it: this is an early answer, not the authority.
func ValidReportReason(reason ReportReason) bool { return reason.valid() }

func (r ReportReason) valid() bool {
	switch r {
	case ReasonBrokenUnavailable, ReasonInaccurate, ReasonInappropriate, ReasonSuspectedCopyrightViolatio, ReasonOther:
		return true
	}
	return false
}

// ReportContent is what the Student wrote. The target and the instance it refers to arrive
// separately, as a VerifiedReportBinding — a caller cannot state them here.
type ReportContent struct {
	Reason      ReportReason
	Explanation string
}

// Report is the created row, as stored.
type Report struct {
	ID                string
	ReporterAccountID string
	TargetKind        ReportTargetKind
	TargetID          string
	// TargetRevisionRef is the exact visible instance the Student was shown: the Course Revision
	// for COURSE and LESSON, and the Media Asset Version for VIDEO, RESOURCE, and LAB_MATERIAL.
	TargetRevisionRef string
	Reason            ReportReason
	Explanation       string
	CreatedAt         time.Time
}

// ReportClock is the injected submission-instant boundary, so `created_at` is deterministic under
// test and never an implicit wall-clock read.
type ReportClock func() time.Time

// ReportMutationGuard runs inside the report transaction immediately before the relational
// verification and the insert. HTTP composition supplies the authoritative current-access decision
// (FR-033); this package remains independent of the Entitlement model and performs no authorization
// query of its own. It is the same seam SaveProgressGuarded uses for the Progress write.
type ReportMutationGuard func(context.Context, pgx.Tx) error

// CreateReport records one report against one already-verified rendered instance.
//
// The binding must already be cryptographically verified by the caller (T063 verifies the report
// context at the HTTP boundary). A signature only proves the server minted those values, so this
// still verifies independently that they form a coherent target: that the revision belongs to the
// Course, that the stable target existed in that revision, and that the Asset Version was the one
// bound to that target and kind. Nothing here re-resolves what is live now — doing so is exactly
// how a report filed from a stale page came to record the wrong instance.
//
// This form carries no authorization guard and is therefore the domain boundary only. Every route
// uses CreateReportGuarded, because a report must not outlive the access that permitted it.
func (r *Repository) CreateReport(ctx context.Context, binding VerifiedReportBinding, content ReportContent, now ReportClock) (Report, error) {
	return r.createReport(ctx, binding, content, now, nil)
}

// CreateReportGuarded couples the current-access decision, the relational binding verification, and
// the insert in one PostgreSQL transaction.
//
// Without this, three steps could interleave with an authority change: the route authorizes, the
// Entitlement is revoked or expires, and the report still lands. The evaluator's transactional
// reader takes shared locks over the authority rows, so a committed access-state change cannot
// slide between the final decision and the row this transaction writes.
func (r *Repository) CreateReportGuarded(ctx context.Context, binding VerifiedReportBinding, content ReportContent, now ReportClock, guard ReportMutationGuard) (Report, error) {
	if guard == nil {
		return Report{}, fmt.Errorf("learning report requires an authorization guard")
	}
	return r.createReport(ctx, binding, content, now, guard)
}

func (r *Repository) createReport(ctx context.Context, binding VerifiedReportBinding, content ReportContent, now ReportClock, guard ReportMutationGuard) (Report, error) {
	if r == nil || r.pool == nil {
		return Report{}, ErrReportTargetUnavailable
	}
	normalized, err := normalizeReportContent(content)
	if err != nil {
		return Report{}, err
	}
	if err := validateBindingShape(binding); err != nil {
		return Report{}, err
	}
	if now == nil {
		return Report{}, fmt.Errorf("learning report requires a clock")
	}

	transaction, err := r.pool.Begin(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("beginning report transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	// Authority first, inside the same transaction: content the Student may no longer reach is not
	// reportable, and the decision must not be able to go stale before the row is written.
	if guard != nil {
		if err := guard(ctx, transaction); err != nil {
			return Report{}, err
		}
	}

	if err := r.verifyReportBinding(ctx, transaction, binding); err != nil {
		return Report{}, err
	}

	revisionRef := binding.VisibleCourseRevisionID
	if binding.VisibleAssetVersionID != "" {
		revisionRef = binding.VisibleAssetVersionID
	}

	createdAt := now().UTC()
	report := Report{
		ReporterAccountID: binding.ReporterAccountID,
		TargetKind:        binding.TargetKind,
		TargetID:          binding.StableTargetID,
		TargetRevisionRef: revisionRef,
		Reason:            normalized.Reason,
		Explanation:       normalized.Explanation,
		CreatedAt:         createdAt,
	}

	var explanation any
	if normalized.Explanation != "" {
		explanation = normalized.Explanation
	}

	r.observeQuery("learning.report.insert")
	err = transaction.QueryRow(ctx, `
		INSERT INTO content_reports
			(reporter_account_id, target_kind, target_id, target_revision_ref, reason, explanation, created_at)
		VALUES ($1::uuid, $2, $3::uuid, $4::uuid, $5, $6, $7)
		RETURNING id::text, created_at
	`, binding.ReporterAccountID, string(binding.TargetKind), binding.StableTargetID, revisionRef,
		string(normalized.Reason), explanation, createdAt).Scan(&report.ID, &report.CreatedAt)
	if err != nil {
		// The Student's own still-open report for this target (D-066: version-independent).
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "rep_no_duplicate_open" {
			return Report{}, ErrReportDuplicate
		}
		if errors.As(err, &pgErr) && pgErr.Code == "23514" && pgErr.ConstraintName == "rep_other_needs_explanation" {
			return Report{}, ErrReportInvalid
		}
		return Report{}, fmt.Errorf("inserting content report: %w", err)
	}

	if err := transaction.Commit(ctx); err != nil {
		return Report{}, fmt.Errorf("committing content report: %w", err)
	}
	return report, nil
}

func validateBindingShape(binding VerifiedReportBinding) error {
	if !binding.TargetKind.valid() {
		return fmt.Errorf("%w: unknown target kind", ErrReportInvalid)
	}
	if err := validateContextShape(binding.TargetKind, binding.VisibleAssetVersionID); err != nil {
		return fmt.Errorf("%w: %s", ErrReportInvalid, err)
	}
	for _, value := range []string{
		binding.ReporterAccountID, binding.CourseID, binding.StableTargetID, binding.VisibleCourseRevisionID,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: incomplete binding", ErrReportInvalid)
		}
	}
	return nil
}

// verifyReportBinding proves the (Course, revision, stable target, Asset Version) tuple is a real,
// internally consistent instance — historically valid, not necessarily current.
//
// The revision is deliberately *not* required to still be the live pointer: a Student reporting a
// page rendered from revision A must succeed after revision B goes live. What is required is that
// the tuple genuinely existed: A belongs to this Course, the stable target existed within A, and
// the Asset Version was the one A bound for that target and kind. An arbitrary historical or
// foreign version cannot satisfy those joins.
func (r *Repository) verifyReportBinding(ctx context.Context, transaction pgx.Tx, binding VerifiedReportBinding) error {
	var exists bool
	var err error

	switch binding.TargetKind {
	case ReportTargetCourse:
		r.observeQuery("learning.report.binding")
		err = transaction.QueryRow(ctx, `
			SELECT true
			FROM course_revisions cr
			JOIN courses c ON c.id = cr.course_id
			JOIN enrollments e ON e.course_id = c.id AND e.student_account_id = $3::uuid
			WHERE cr.id = $2::uuid AND cr.course_id = $1::uuid AND cr.state = 'APPROVED'
			  AND c.id = $1::uuid
		`, binding.StableTargetID, binding.VisibleCourseRevisionID, binding.ReporterAccountID).Scan(&exists)

	case ReportTargetLesson:
		r.observeQuery("learning.report.binding")
		err = transaction.QueryRow(ctx, `
			SELECT true
			FROM course_lessons cl
			JOIN course_sections cs ON cs.id = cl.section_id AND cs.course_id = cl.course_id
			JOIN course_revisions cr ON cr.id = cs.revision_id AND cr.course_id = cl.course_id
				AND cr.state = 'APPROVED'
			JOIN enrollments e ON e.course_id = cl.course_id AND e.student_account_id = $4::uuid
			WHERE cl.lesson_identity_id = $1::uuid AND cr.id = $2::uuid AND cl.course_id = $3::uuid
		`, binding.StableTargetID, binding.VisibleCourseRevisionID, binding.CourseID, binding.ReporterAccountID).Scan(&exists)

	case ReportTargetVideo:
		r.observeQuery("learning.report.binding")
		err = transaction.QueryRow(ctx, `
			SELECT true
			FROM course_lessons cl
			JOIN course_sections cs ON cs.id = cl.section_id AND cs.course_id = cl.course_id
			JOIN course_revisions cr ON cr.id = cs.revision_id AND cr.course_id = cl.course_id
				AND cr.state = 'APPROVED'
			JOIN media_asset_versions mav ON mav.id = cl.video_asset_version_id
				AND mav.kind = 'VIDEO'
			JOIN media_assets ma ON ma.id = mav.logical_asset_id AND ma.course_id = cl.course_id
			JOIN enrollments e ON e.course_id = cl.course_id AND e.student_account_id = $5::uuid
			WHERE cl.lesson_identity_id = $1::uuid AND cr.id = $2::uuid AND cl.course_id = $3::uuid
			  AND cl.video_asset_version_id = $4::uuid
		`, binding.StableTargetID, binding.VisibleCourseRevisionID, binding.CourseID,
			binding.VisibleAssetVersionID, binding.ReporterAccountID).Scan(&exists)

	case ReportTargetResource, ReportTargetLabMaterial:
		kind := string(binding.TargetKind)
		r.observeQuery("learning.report.binding")
		err = transaction.QueryRow(ctx, `
			SELECT true
			FROM course_lessons cl
			JOIN course_sections cs ON cs.id = cl.section_id AND cs.course_id = cl.course_id
			JOIN course_revisions cr ON cr.id = cs.revision_id AND cr.course_id = cl.course_id
				AND cr.state = 'APPROVED'
			JOIN lesson_files lf ON lf.lesson_id = cl.id AND lf.kind = $5::lesson_file_kind
			JOIN media_asset_versions mav ON mav.id = lf.asset_version_id AND mav.id = $4::uuid
				AND mav.kind = $6::media_asset_kind
			JOIN media_assets ma ON ma.id = mav.logical_asset_id AND ma.course_id = cl.course_id
			JOIN enrollments e ON e.course_id = cl.course_id AND e.student_account_id = $7::uuid
			WHERE cl.lesson_identity_id = $1::uuid AND cr.id = $2::uuid AND cl.course_id = $3::uuid
		`, binding.StableTargetID, binding.VisibleCourseRevisionID, binding.CourseID,
			binding.VisibleAssetVersionID, kind, kind, binding.ReporterAccountID).Scan(&exists)

	default:
		return fmt.Errorf("%w: unknown target kind", ErrReportInvalid)
	}

	if errors.Is(err, pgx.ErrNoRows) {
		return ErrReportTargetUnavailable
	}
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "22P02" {
			return fmt.Errorf("%w: malformed identifier", ErrReportInvalid)
		}
		return fmt.Errorf("verifying report binding: %w", err)
	}
	if !exists {
		return ErrReportTargetUnavailable
	}
	return nil
}

func normalizeReportContent(content ReportContent) (ReportContent, error) {
	if !content.Reason.valid() {
		return ReportContent{}, fmt.Errorf("%w: unknown reason", ErrReportInvalid)
	}

	// Surrounding whitespace is not content, and an explanation of only whitespace is not an
	// explanation. No maximum length is applied here: the request body bound belongs to the route
	// (T063), as it does for every other write in this codebase.
	content.Explanation = strings.TrimSpace(content.Explanation)

	if content.Reason == ReasonOther && content.Explanation == "" {
		return ReportContent{}, fmt.Errorf("%w: reason %q requires an explanation", ErrReportInvalid, ReasonOther)
	}
	return content, nil
}
