//go:build integration

package learning

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/google/uuid"

	"github.com/Owlah2025/gradex/backend/internal/entitlement"
	"github.com/Owlah2025/gradex/backend/internal/ratelimit"
)

// T067: the US4 acceptance matrix against real PostgreSQL and the production migrations
// (FR-029, FR-032, FR-033, FR-034, BR-145).
//
// T062–T065 each proved one mechanism. What this file proves is the set of guarantees a Student
// actually depends on, and — for two of them — that the guarantee lives *in the database* rather
// than in the Go code that usually gets there first.
//
// That distinction is the point of mutations 14 and 15. `normalizeReportContent` refuses `other`
// without an explanation, and `CreateReport` maps a unique violation to ErrReportDuplicate, so a
// test that only ever calls the Go path stays green even after the constraint or the index is
// dropped: the application check shadows the database one. The tests below therefore exercise the
// constraint and the index **directly**, through SQL the Go guard never sees, so removing either
// turns this file red — which is what those mutations require.

// reportRow is one direct-SQL insert, used to reach the table without passing through the domain's
// own validation. It is deliberately not the production path.
func insertReportDirectly(ctx context.Context, fixture learningFixture, reporter, kind, target, reason string, explanation *string, resolvedAt *time.Time) error {
	_, err := fixture.repository.pool.Exec(ctx, `
		INSERT INTO content_reports
			(reporter_account_id, target_kind, target_id, target_revision_ref, reason, explanation, resolved_at)
		VALUES ($1::uuid, $2, $3::uuid, $4::uuid, $5, $6, $7)
	`, reporter, kind, target, reportLiveRevision, reason, explanation, resolvedAt)
	return err
}

// constraintViolation extracts the PostgreSQL error code and constraint name, so a test can assert
// *which* database rule refused the write rather than merely that something did.
func constraintViolation(t *testing.T, err error) (code, constraint string) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("expected a PostgreSQL constraint violation, got %v", err)
	}
	return pgErr.Code, pgErr.ConstraintName
}

// seedReportEntitlement gives the fixture Student a live Course-scoped Entitlement.
func seedReportEntitlement(t *testing.T, ctx context.Context, fixture learningFixture, now time.Time) string {
	t.Helper()
	var instructorID string
	if err := fixture.repository.pool.QueryRow(ctx, `SELECT owner_account_id::text FROM courses WHERE id = $1::uuid`, fixture.courseID).Scan(&instructorID); err != nil {
		t.Fatalf("reading course owner: %v", err)
	}
	invID := uuid.NewString()
	if _, err := fixture.repository.pool.Exec(ctx, `
		INSERT INTO course_access_invitations (id, course_id, email, normalized_email, created_by_account_id, accepted_by_account_id, decided_by_account_id, state)
		VALUES ($1::uuid, $2::uuid, 'student@example.test', 'student@example.test', $3::uuid, $4::uuid, $3::uuid, 'APPROVED')
	`, invID, fixture.courseID, instructorID, fixture.studentID); err != nil {
		t.Fatalf("seeding invitation: %v", err)
	}
	var id string
	if err := fixture.repository.pool.QueryRow(ctx, `
		INSERT INTO entitlements
			(id, student_account_id, scope_kind, scope_id, course_id, grant_source, source_invitation_id,
			 original_access_ends_at, access_ends_at, retirement_eligibility_at, state)
		VALUES (gen_random_uuid(), $1::uuid, 'COURSE', $2::uuid, $2::uuid, 'MANUAL_INVITATION', $5::uuid, $3, $3, $4, 'ACTIVE')
		RETURNING id::text
	`, fixture.studentID, fixture.courseID, now.Add(time.Hour), now.Add(-time.Hour), invID).Scan(&id); err != nil {
		t.Fatalf("seeding entitlement: %v", err)
	}
	return id
}

// reportEntitlementGuard is the production authorization the route installs: the real S4 evaluator,
// bound to the report's own transaction. Nothing here re-implements entitlement policy.
func reportEntitlementGuard(t *testing.T, fixture learningFixture, lessonID string, now time.Time) ReportMutationGuard {
	t.Helper()
	reader, err := entitlement.NewRepository(fixture.repository.pool)
	if err != nil {
		t.Fatalf("constructing entitlement repository: %v", err)
	}
	evaluator, err := entitlement.NewEvaluator(reader)
	if err != nil {
		t.Fatalf("constructing entitlement evaluator: %v", err)
	}
	return func(ctx context.Context, tx pgx.Tx) error {
		if evaluator.EvaluateInTransaction(ctx, tx, fixture.studentID, lessonID, now).Allowed {
			return nil
		}
		return errReportNotEntitled
	}
}

var errReportNotEntitled = errors.New("no current entitlement authorises this report")

// TestEveryTargetKindIsAcceptedAndStoresBothIdentities is FR-029's target set and FR-030's dual
// identity, one row at a time: the stable logical target an Admin follows, and the exact instance
// the Student saw.
func TestEveryTargetKindIsAcceptedAndStoresBothIdentities(t *testing.T) {
	fixture := newLearningFixture(t)
	ctx := context.Background()
	video, resource, lab := seedReportMedia(t, ctx, fixture)
	clock := fixedReportClock()

	kinds := []struct {
		kind           ReportTargetKind
		stableTarget   string
		visibleVersion string
	}{
		{ReportTargetCourse, fixture.courseID, ""},
		{ReportTargetLesson, fixture.lessonID, ""},
		{ReportTargetVideo, fixture.lessonID, video},
		{ReportTargetResource, fixture.lessonID, resource},
		{ReportTargetLabMaterial, fixture.lessonID, lab},
	}

	for _, testCase := range kinds {
		t.Run(string(testCase.kind), func(t *testing.T) {
			binding := contextFor(t, fixture, testCase.kind, testCase.stableTarget, reportLiveRevision, testCase.visibleVersion)
			report, err := fixture.repository.CreateReport(ctx, binding, ReportContent{Reason: ReasonInaccurate}, clock)
			if err != nil {
				t.Fatalf("%s was not accepted: %v", testCase.kind, err)
			}

			stored := readStoredReport(t, ctx, fixture, report.ID)
			if stored.targetKind != string(testCase.kind) {
				t.Fatalf("stored kind = %s, want %s", stored.targetKind, testCase.kind)
			}
			if stored.targetID != testCase.stableTarget {
				t.Fatalf("%s stored target %s, want the stable identity %s", testCase.kind, stored.targetID, testCase.stableTarget)
			}
			// COURSE and LESSON name a Revision; the media kinds name the exact Asset Version.
			wantRef := reportLiveRevision
			if testCase.visibleVersion != "" {
				wantRef = testCase.visibleVersion
			}
			if stored.revisionRef == nil || *stored.revisionRef != wantRef {
				t.Fatalf("%s stored instance %v, want the exact visible %s", testCase.kind, stored.revisionRef, wantRef)
			}
			if stored.reporter != fixture.studentID {
				t.Fatalf("%s attributed to %s", testCase.kind, stored.reporter)
			}
			if stored.resolvedAt != nil {
				t.Fatalf("%s was created already resolved", testCase.kind)
			}
			if !stored.createdAt.UTC().Equal(clock().UTC()) {
				t.Fatalf("%s created_at = %s, want the injected clock", testCase.kind, stored.createdAt.UTC())
			}
		})
	}

	if total := countReports(t, ctx, fixture); total != len(kinds) {
		t.Fatalf("report rows = %d, want one per target kind (%d)", total, len(kinds))
	}
}

// TestEveryFixedReasonIsAcceptedAndStoredExactly walks FR-029's closed reason set through the real
// insert. Each reason takes a distinct target so D-066 never masks an acceptance.
func TestEveryFixedReasonIsAcceptedAndStoredExactly(t *testing.T) {
	fixture := newLearningFixture(t)
	ctx := context.Background()
	video, resource, lab := seedReportMedia(t, ctx, fixture)
	clock := fixedReportClock()

	// One target per reason: five reasons, five kinds.
	targets := []struct {
		kind    ReportTargetKind
		target  string
		version string
	}{
		{ReportTargetCourse, fixture.courseID, ""},
		{ReportTargetLesson, fixture.lessonID, ""},
		{ReportTargetVideo, fixture.lessonID, video},
		{ReportTargetResource, fixture.lessonID, resource},
		{ReportTargetLabMaterial, fixture.lessonID, lab},
	}
	reasons := []ReportReason{
		ReasonBrokenUnavailable,
		ReasonInaccurate,
		ReasonInappropriate,
		ReasonSuspectedCopyrightViolatio,
		ReasonOther,
	}
	if len(reasons) != len(targets) {
		t.Fatalf("the matrix needs one target per reason: %d reasons, %d targets", len(reasons), len(targets))
	}

	for index, reason := range reasons {
		target := targets[index]
		content := ReportContent{Reason: reason}
		if reason == ReasonOther {
			content.Explanation = "الشرح المطلوب"
		}
		report, err := fixture.repository.CreateReport(ctx,
			contextFor(t, fixture, target.kind, target.target, reportLiveRevision, target.version), content, clock)
		if err != nil {
			t.Fatalf("reason %q was not accepted: %v", reason, err)
		}
		stored := readStoredReport(t, ctx, fixture, report.ID)
		if stored.reason != string(reason) {
			t.Fatalf("stored reason = %q, want the wire value %q", stored.reason, reason)
		}
	}

	if total := countReports(t, ctx, fixture); total != len(reasons) {
		t.Fatalf("report rows = %d, want one per reason (%d)", total, len(reasons))
	}
}

// TestAReasonOutsideTheFixedSetIsRefusedByBothGuards proves FR-029's closed set holds at the domain
// boundary *and* at the database, so widening it is a migration rather than an edit.
func TestAReasonOutsideTheFixedSetIsRefusedByBothGuards(t *testing.T) {
	fixture := newLearningFixture(t)
	ctx := context.Background()
	clock := fixedReportClock()

	// The domain refuses before any SQL runs.
	for _, unknown := range []ReportReason{"spam", "SPAM", "broken", "", "inaccurate ", "other_reason"} {
		if _, err := fixture.repository.CreateReport(ctx,
			contextFor(t, fixture, ReportTargetLesson, fixture.lessonID, reportLiveRevision, ""),
			ReportContent{Reason: unknown}, clock); !errors.Is(err, ErrReportInvalid) {
			t.Fatalf("reason %q was not refused by the domain: %v", unknown, err)
		}
	}
	if total := countReports(t, ctx, fixture); total != 0 {
		t.Fatalf("a refused reason wrote %d rows", total)
	}

	// And the database refuses it too, reached directly so the domain guard cannot shadow it.
	err := insertReportDirectly(ctx, fixture, fixture.studentID, "LESSON", fixture.lessonID, "spam", nil, nil)
	code, constraint := constraintViolation(t, err)
	if code != "23514" || constraint != "rep_reason" {
		t.Fatalf("an off-set reason was refused by %s/%s, want the rep_reason check", code, constraint)
	}

	// The same holds for the target kind: both enumerations are closed at the schema.
	err = insertReportDirectly(ctx, fixture, fixture.studentID, "SECTION", fixture.lessonID, "inaccurate", nil, nil)
	code, constraint = constraintViolation(t, err)
	if code != "23514" || constraint != "rep_target_kind" {
		t.Fatalf("an off-set target kind was refused by %s/%s, want the rep_target_kind check", code, constraint)
	}

	if total := countReports(t, ctx, fixture); total != 0 {
		t.Fatalf("a refused direct insert wrote %d rows", total)
	}
}

// TestOtherWithoutExplanationIsRefusedAtTheDatabaseConstraint is mutation 14's tripwire.
//
// The domain already refuses this, which is exactly why the check here goes around it: if the only
// evidence ran through `normalizeReportContent`, dropping `rep_other_needs_explanation` would leave
// every test green while the guarantee was gone. FR-029 is asserted where it survives a handler
// bug.
func TestOtherWithoutExplanationIsRefusedAtTheDatabaseConstraint(t *testing.T) {
	fixture := newLearningFixture(t)
	ctx := context.Background()

	blank := ""
	spaces := "     "
	// Each case uses its own target so a refusal is the explanation rule's doing and never the
	// duplicate index's.
	for _, testCase := range []struct {
		name        string
		kind        string
		target      string
		explanation *string
	}{
		{"null explanation", "LESSON", fixture.lessonID, nil},
		{"empty explanation", "COURSE", fixture.courseID, &blank},
		{"spaces-only explanation", "VIDEO", fixture.lessonID, &spaces},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			err := insertReportDirectly(ctx, fixture, fixture.studentID, testCase.kind, testCase.target, "other", testCase.explanation, nil)
			code, constraint := constraintViolation(t, err)
			if code != "23514" || constraint != "rep_other_needs_explanation" {
				t.Fatalf("refused by %s/%s, want the rep_other_needs_explanation check", code, constraint)
			}
		})
	}

	// Where the two layers differ, and why the pair is what FR-029 actually rests on.
	//
	// The constraint reads `length(btrim(explanation)) > 0`, and PostgreSQL's single-argument
	// `btrim` strips spaces only — so a tab- or newline-only explanation satisfies it. Go's
	// `strings.TrimSpace` strips all Unicode whitespace, so the domain refuses that input before
	// any SQL runs and no such row can be created through the production path. This asserts both
	// halves rather than pretending the constraint is the stronger of the two.
	t.Run("tab and newline only are refused by the domain that trims them", func(t *testing.T) {
		exotic := "\t\n "
		if _, err := fixture.repository.CreateReport(ctx,
			contextFor(t, fixture, ReportTargetLesson, fixture.lessonID, reportLiveRevision, ""),
			ReportContent{Reason: ReasonOther, Explanation: exotic}, fixedReportClock()); !errors.Is(err, ErrReportInvalid) {
			t.Fatalf("the domain must refuse a whitespace-only explanation, got %v", err)
		}
		if total := countReports(t, ctx, fixture); total != 0 {
			t.Fatalf("a refused explanation wrote %d rows", total)
		}
	})

	// The constraint is specific to `other`: every other reason may omit an explanation.
	if err := insertReportDirectly(ctx, fixture, fixture.studentID, "LESSON", fixture.lessonID, "inaccurate", nil, nil); err != nil {
		t.Fatalf("a non-other reason must not require an explanation: %v", err)
	}
	// And `other` with real content is accepted, including non-Latin text.
	arabic := "الصوت غير موجود"
	if err := insertReportDirectly(ctx, fixture, fixture.studentID, "COURSE", fixture.courseID, "other", &arabic, nil); err != nil {
		t.Fatalf("other with an explanation must be accepted: %v", err)
	}

	if total := countReports(t, ctx, fixture); total != 2 {
		t.Fatalf("report rows = %d, want only the two accepted inserts", total)
	}
}

// TestDuplicateIsRefusedByThePartialUniqueIndexItself is mutation 15's tripwire.
//
// The domain maps a unique violation to ErrReportDuplicate, so a test that only calls CreateReport
// proves the mapping rather than the index. This reaches the table directly, and asserts the exact
// index that refused — which is what makes dropping `rep_no_duplicate_open` fail here.
func TestDuplicateIsRefusedByThePartialUniqueIndexItself(t *testing.T) {
	fixture := newLearningFixture(t)
	ctx := context.Background()

	if err := insertReportDirectly(ctx, fixture, fixture.studentID, "LESSON", fixture.lessonID, "inaccurate", nil, nil); err != nil {
		t.Fatalf("first report: %v", err)
	}

	// Same Student, same kind, same stable target, still unresolved.
	err := insertReportDirectly(ctx, fixture, fixture.studentID, "LESSON", fixture.lessonID, "inappropriate", nil, nil)
	code, constraint := constraintViolation(t, err)
	if code != "23505" || constraint != "rep_no_duplicate_open" {
		t.Fatalf("the duplicate was refused by %s/%s, want the rep_no_duplicate_open index", code, constraint)
	}

	// The index is deliberately partial: resolving the first row lifts it (S8's behaviour, used
	// here only as fixture setup).
	resolved := time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC)
	if _, err := fixture.repository.pool.Exec(ctx, `
		UPDATE content_reports
		SET resolved_at = $1, resolved_by_account_id = $2::uuid,
		    resolution_action = 'DISMISSED', resolution_reason = 'fixture setup'
		WHERE reporter_account_id = $2::uuid
	`, resolved, fixture.studentID); err != nil {
		t.Fatalf("resolving as fixture setup: %v", err)
	}
	if err := insertReportDirectly(ctx, fixture, fixture.studentID, "LESSON", fixture.lessonID, "inaccurate", nil, nil); err != nil {
		t.Fatalf("after resolution the target is reportable again: %v", err)
	}

	// And it is keyed on all three columns: a different kind, and a different Student, are distinct.
	if err := insertReportDirectly(ctx, fixture, fixture.studentID, "COURSE", fixture.courseID, "inaccurate", nil, nil); err != nil {
		t.Fatalf("a different target kind must not collide: %v", err)
	}
	seedSecondStudent(t, ctx, fixture, true)
	if err := insertReportDirectly(ctx, fixture, reportOtherStudent, "LESSON", fixture.lessonID, "inaccurate", nil, nil); err != nil {
		t.Fatalf("another Student must not collide: %v", err)
	}
}

// TestNoCurrentEntitlementRefusesTheReport is FR-033 at the domain's authorization seam, using the
// real S4 evaluator inside the report's own transaction — the same composition the route installs.
//
// The context is valid and the target is relationally coherent in every case here; only the
// Student's current access differs, which is the whole claim: a context grants nothing.
func TestNoCurrentEntitlementRefusesTheReport(t *testing.T) {
	now := time.Date(2026, 8, 4, 9, 30, 0, 0, time.UTC)
	clock := fixedReportClock()

	transitions := []struct {
		name    string
		prepare func(t *testing.T, ctx context.Context, fixture learningFixture)
	}{
		{"no entitlement at all", func(*testing.T, context.Context, learningFixture) {}},
		{"expired entitlement", func(t *testing.T, ctx context.Context, fixture learningFixture) {
			seedReportEntitlement(t, ctx, fixture, now)
			if _, err := fixture.repository.pool.Exec(ctx,
				`UPDATE entitlements SET original_access_ends_at = $1, access_ends_at = $1`, now.Add(-time.Minute)); err != nil {
				t.Fatalf("expiring entitlement: %v", err)
			}
		}},
		{"revoked entitlement", func(t *testing.T, ctx context.Context, fixture learningFixture) {
			seedReportEntitlement(t, ctx, fixture, now)
			if _, err := fixture.repository.pool.Exec(ctx,
				`UPDATE entitlements SET state = 'REVOKED', revoked_at = $1`, now); err != nil {
				t.Fatalf("revoking entitlement: %v", err)
			}
		}},
		{"suspended account", func(t *testing.T, ctx context.Context, fixture learningFixture) {
			seedReportEntitlement(t, ctx, fixture, now)
			if _, err := fixture.repository.pool.Exec(ctx,
				`UPDATE accounts SET status = 'SUSPENDED' WHERE id = $1::uuid`, fixture.studentID); err != nil {
				t.Fatalf("suspending account: %v", err)
			}
		}},
		{"emergency course suspension", func(t *testing.T, ctx context.Context, fixture learningFixture) {
			seedReportEntitlement(t, ctx, fixture, now)
			if _, err := fixture.repository.pool.Exec(ctx,
				`UPDATE courses SET access_suspended_at = $1, access_suspension_reason = 't067' WHERE id = $2::uuid`,
				now, fixture.courseID); err != nil {
				t.Fatalf("suspending course access: %v", err)
			}
		}},
	}

	for _, transition := range transitions {
		t.Run(transition.name, func(t *testing.T) {
			fixture := newLearningFixture(t)
			ctx := context.Background()
			seedReportMedia(t, ctx, fixture)
			transition.prepare(t, ctx, fixture)

			_, err := fixture.repository.CreateReportGuarded(ctx,
				contextFor(t, fixture, ReportTargetLesson, fixture.lessonID, reportLiveRevision, ""),
				ReportContent{Reason: ReasonInaccurate}, clock,
				reportEntitlementGuard(t, fixture, fixture.lessonID, now))
			if !errors.Is(err, errReportNotEntitled) {
				t.Fatalf("%s must refuse the report, got %v", transition.name, err)
			}
			if total := countReports(t, ctx, fixture); total != 0 {
				t.Fatalf("%s wrote %d report rows", transition.name, total)
			}
		})
	}

	// The positive control: the identical call succeeds when access is current, so the refusals
	// above are the Entitlement's doing and not a broken fixture.
	t.Run("current entitlement admits the report", func(t *testing.T) {
		fixture := newLearningFixture(t)
		ctx := context.Background()
		seedReportMedia(t, ctx, fixture)
		seedReportEntitlement(t, ctx, fixture, now)

		report, err := fixture.repository.CreateReportGuarded(ctx,
			contextFor(t, fixture, ReportTargetLesson, fixture.lessonID, reportLiveRevision, ""),
			ReportContent{Reason: ReasonInaccurate}, clock,
			reportEntitlementGuard(t, fixture, fixture.lessonID, now))
		if err != nil {
			t.Fatalf("an entitled Student must be able to report: %v", err)
		}
		if countReports(t, ctx, fixture) != 1 {
			t.Fatal("the entitled report was not stored")
		}
		// Progress is never an authorization input, and reporting creates none.
		var progressRows int
		if err := fixture.repository.pool.QueryRow(ctx, `SELECT count(*) FROM progress`).Scan(&progressRows); err != nil {
			t.Fatalf("counting progress: %v", err)
		}
		if progressRows != 0 {
			t.Fatalf("reporting created %d Progress rows", progressRows)
		}
		_ = report
	})
}

// TestThrottleAndDuplicateAreIndependentControls is FR-032's two mechanisms held apart (R-11).
//
// The throttle is the production `learning-report-v1` policy; the duplicate rule is the database
// index. The test composes both around the real repository and shows each one refusing while the
// other would have allowed — which is the property that makes them worth having separately.
func TestThrottleAndDuplicateAreIndependentControls(t *testing.T) {
	fixture := newLearningFixture(t)
	ctx := context.Background()
	video, resource, lab := seedReportMedia(t, ctx, fixture)
	clock := fixedReportClock()

	limiter, err := ratelimit.New(unavailableReportStore{}, []byte(strings.Repeat("t", 32)), time.Second)
	if err != nil {
		t.Fatalf("constructing limiter: %v", err)
	}
	policy := ratelimit.ProtectedLearningReportPolicy()
	admit := func() bool {
		return limiter.Decide(ctx, policy, ratelimit.Input{Identifier: fixture.studentID}).Allowed
	}

	// Five distinct targets, so the duplicate index never fires and the only possible refusal is
	// the quota.
	targets := []struct {
		kind    ReportTargetKind
		target  string
		version string
	}{
		{ReportTargetCourse, fixture.courseID, ""},
		{ReportTargetLesson, fixture.lessonID, ""},
		{ReportTargetVideo, fixture.lessonID, video},
		{ReportTargetResource, fixture.lessonID, resource},
		{ReportTargetLabMaterial, fixture.lessonID, lab},
	}
	if int64(len(targets)) != ratelimit.ProtectedLearningReportsPerHour {
		t.Fatalf("the matrix must consume the whole quota: %d targets, %d allowed",
			len(targets), ratelimit.ProtectedLearningReportsPerHour)
	}

	for index, target := range targets {
		if !admit() {
			t.Fatalf("attempt %d was throttled inside the quota", index+1)
		}
		if _, err := fixture.repository.CreateReport(ctx,
			contextFor(t, fixture, target.kind, target.target, reportLiveRevision, target.version),
			ReportContent{Reason: ReasonInaccurate}, clock); err != nil {
			t.Fatalf("attempt %d: %v", index+1, err)
		}
	}

	// The quota is spent. The domain would still accept a sixth distinct target — proven by the
	// throttle refusing before the domain is ever asked, and the row count standing still.
	if admit() {
		t.Fatal("a sixth attempt was admitted past the 5/hour quota")
	}
	if total := countReports(t, ctx, fixture); total != len(targets) {
		t.Fatalf("report rows = %d, want %d", total, len(targets))
	}

	// The other direction: a fresh Student has quota, and is still refused by the duplicate index
	// for a target they have already reported. Quota and duplication fail differently.
	seedSecondStudent(t, ctx, fixture, true)
	otherStudentAdmits := func() bool {
		return limiter.Decide(ctx, policy, ratelimit.Input{Identifier: reportOtherStudent}).Allowed
	}
	otherBinding := renderContext(t, renderTime(), ReportContextRequest{
		ReporterAccountID: reportOtherStudent, SessionID: testSession, CourseID: fixture.courseID,
		TargetKind: ReportTargetLesson, StableTargetID: fixture.lessonID,
		VisibleCourseRevisionID: reportLiveRevision,
	})
	if !otherStudentAdmits() {
		t.Fatal("a second Student must hold an independent quota")
	}
	if _, err := fixture.repository.CreateReport(ctx, otherBinding, ReportContent{Reason: ReasonInaccurate}, clock); err != nil {
		t.Fatalf("the second Student's first report: %v", err)
	}
	if !otherStudentAdmits() {
		t.Fatal("the second Student still has quota after one report")
	}
	if _, err := fixture.repository.CreateReport(ctx, otherBinding, ReportContent{Reason: ReasonInaccurate}, clock); !errors.Is(err, ErrReportDuplicate) {
		t.Fatalf("with quota remaining, a repeat must be refused as a duplicate, got %v", err)
	}
}

// unavailableReportStore forces the limiter onto its bounded local fallback, which carries the
// identical 5/hour limit. The threshold under test is the policy's, not the backend's.
type unavailableReportStore struct{}

func (unavailableReportStore) Decide(context.Context, []ratelimit.Entry) (bool, error) {
	return false, fmt.Errorf("distributed rate-limit store is unavailable")
}

// TestReportCreationDisclosesNoQueueOrModerationState is FR-034 at the domain boundary.
//
// The route's acknowledgement is proven safe in T065; what a *domain* test can add is that there is
// nothing for it to leak — the value the repository returns carries no moderation state, the table
// holds no queue column, and creating a report tells the reporter nothing about anyone else's.
func TestReportCreationDisclosesNoQueueOrModerationState(t *testing.T) {
	fixture := newLearningFixture(t)
	ctx := context.Background()
	clock := fixedReportClock()

	// Another Student has already reported this Lesson. Nothing the reporter receives may reveal it.
	seedSecondStudent(t, ctx, fixture, true)
	if err := insertReportDirectly(ctx, fixture, reportOtherStudent, "LESSON", fixture.lessonID, "inappropriate", nil, nil); err != nil {
		t.Fatalf("seeding another Student's report: %v", err)
	}

	report, err := fixture.repository.CreateReport(ctx,
		contextFor(t, fixture, ReportTargetLesson, fixture.lessonID, reportLiveRevision, ""),
		ReportContent{Reason: ReasonInaccurate}, clock)
	if err != nil {
		t.Fatalf("creating report: %v", err)
	}

	// The returned value describes one row: this one.
	if report.ID == "" || !report.CreatedAt.UTC().Equal(clock().UTC()) {
		t.Fatalf("returned report = %+v", report)
	}
	if report.ReporterAccountID != fixture.studentID {
		t.Fatalf("returned report names reporter %s", report.ReporterAccountID)
	}
	stored := readStoredReport(t, ctx, fixture, report.ID)
	if stored.resolvedAt != nil {
		t.Fatal("a new report carries a resolution state")
	}

	// The domain exposes no count, no neighbour, and no queue position to expose.
	if report.TargetKind != ReportTargetLesson || report.TargetID != fixture.lessonID {
		t.Fatalf("returned report = %+v", report)
	}

	// Resolution metadata is server-only; the Student route still publishes no queue or moderation state.
	rows, err := fixture.repository.pool.Query(ctx, `
		SELECT column_name FROM information_schema.columns
		WHERE table_name = 'content_reports' ORDER BY column_name
	`)
	if err != nil {
		t.Fatalf("reading content_reports columns: %v", err)
	}
	defer rows.Close()
	var columns []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scanning column: %v", err)
		}
		columns = append(columns, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating columns: %v", err)
	}
	want := []string{
		"created_at", "explanation", "id", "reason", "reporter_account_id",
		"resolution_action", "resolution_reason", "resolved_at", "resolved_by_account_id",
		"target_id", "target_kind", "target_revision_ref",
	}
	if strings.Join(columns, ",") != strings.Join(want, ",") {
		t.Fatalf("content_reports columns = %v, want exactly %v", columns, want)
	}
	for _, forbidden := range []string{"queue", "position", "priority", "severity", "assigned", "moderator", "outcome", "sla"} {
		for _, column := range columns {
			if strings.Contains(column, forbidden) {
				t.Fatalf("content_reports carries a moderation surface: %s", column)
			}
		}
	}

	// Both reports exist; the reporter was told about exactly one.
	if total := countReports(t, ctx, fixture); total != 2 {
		t.Fatalf("report rows = %d, want 2", total)
	}
}
