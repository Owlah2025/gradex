package entitlement

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Evaluator is the sole production authorization decision point for an
// Entitlement. Handlers must call it immediately before each protected issue.
type Evaluator struct{ reader Reader }

type transactionalReader interface {
	readerFor(pgx.Tx) Reader
}

type courseReadReader interface {
	LoadCourseReadSnapshots(context.Context, string) ([]CourseReadSnapshot, error)
}

func NewEvaluator(reader Reader) (*Evaluator, error) {
	if reader == nil {
		return nil, errors.New("entitlement reader is required")
	}
	return &Evaluator{reader: reader}, nil
}

// Evaluate applies the S4 ordering: applicable non-revoked scope, effective
// expiry, Account suspension, Course emergency suspension, then retirement.
// Every missing/invalid input fails closed as a typed dependency denial.
func (e *Evaluator) Evaluate(ctx context.Context, studentID, lessonID string, now time.Time) Decision {
	return e.evaluate(ctx, studentID, lessonID, nil, now)
}

// EvaluateInTransaction applies the identical S4 policy through a reader
// bound to tx. Repository-backed readers take shared locks over the authority
// rows, so an authority mutation cannot commit between this final decision and
// the protected Progress write in that transaction.
func (e *Evaluator) EvaluateInTransaction(ctx context.Context, tx pgx.Tx, studentID, lessonID string, now time.Time) Decision {
	if e == nil || tx == nil {
		return Decision{Reason: ReasonDependency}
	}
	reader, ok := e.reader.(transactionalReader)
	if !ok {
		return Decision{Reason: ReasonDependency}
	}
	return (&Evaluator{reader: reader.readerFor(tx)}).evaluate(ctx, studentID, lessonID, nil, now)
}

// EvaluateTarget is the same single decision point with the exact Asset
// Version retirement instant included by protected delivery. It prevents a
// delivery handler from re-implementing retirement eligibility while retaining
// the public Evaluate(student, lesson, now) boundary for ordinary operations.
func (e *Evaluator) EvaluateTarget(ctx context.Context, studentID, lessonID string, assetRetiredAt *time.Time, now time.Time) Decision {
	return e.evaluate(ctx, studentID, lessonID, assetRetiredAt, now)
}

// EvaluateRead classifies a protected read without turning an expired
// entitlement into playback or Progress authority. S5 may use the expired
// classification only after it confirms a retained Enrollment.
func (e *Evaluator) EvaluateRead(ctx context.Context, studentID, lessonID string, now time.Time) ReadDecision {
	if e == nil || e.reader == nil || studentID == "" || lessonID == "" || now.IsZero() {
		return ReadDecision{State: ReadDenied, Reason: ReasonDependency}
	}
	snapshot, err := e.reader.Load(ctx, studentID, lessonID)
	if err != nil || !validSnapshot(snapshot, studentID) {
		return ReadDecision{State: ReadDenied, Reason: ReasonDependency}
	}
	if snapshot.Lesson.AccountStatus != "ACTIVE" {
		return ReadDecision{State: ReadDenied, Reason: ReasonAccountSuspended}
	}
	if snapshot.Lesson.CourseSuspended {
		return ReadDecision{State: ReadDenied, Reason: ReasonCourseSuspended}
	}
	decision, expiresAt, courseWide := classifySnapshot(snapshot, studentID, nil, now)
	return readDecision(decision, expiresAt, courseWide)
}

// EvaluateCourseReads classifies all enrolled Courses through the S4-owned
// bulk reader. The query boundary is intentionally separate from S5 so the
// learning package cannot reproduce Entitlement scope or expiry policy.
func (e *Evaluator) EvaluateCourseReads(ctx context.Context, studentID string, now time.Time) (map[string]ReadDecision, error) {
	if e == nil || e.reader == nil || studentID == "" || now.IsZero() {
		return nil, fmt.Errorf("entitlement read classification requires student and clock")
	}
	reader, ok := e.reader.(courseReadReader)
	if !ok {
		return nil, fmt.Errorf("entitlement reader does not support course read classification")
	}
	snapshots, err := reader.LoadCourseReadSnapshots(ctx, studentID)
	if err != nil {
		return nil, err
	}
	decisions := make(map[string]ReadDecision, len(snapshots))
	for _, snapshot := range snapshots {
		if snapshot.CourseID == "" || !validSnapshot(Snapshot{Lesson: snapshot.Lesson, Entitlements: snapshot.Entitlements}, studentID) {
			continue
		}
		if snapshot.Lesson.AccountStatus != "ACTIVE" || snapshot.Lesson.CourseSuspended {
			decisions[snapshot.CourseID] = ReadDecision{State: ReadDenied, Reason: readRuntimeReason(snapshot.Lesson)}
			continue
		}
		decision, expiresAt, courseWide := classifySnapshot(Snapshot{Lesson: snapshot.Lesson, Entitlements: snapshot.Entitlements}, studentID, nil, now)
		decisions[snapshot.CourseID] = readDecision(decision, expiresAt, courseWide)
	}
	return decisions, nil
}

func readRuntimeReason(lesson Lesson) Reason {
	if lesson.AccountStatus != "ACTIVE" {
		return ReasonAccountSuspended
	}
	return ReasonCourseSuspended
}

func (e *Evaluator) evaluate(ctx context.Context, studentID, lessonID string, assetRetiredAt *time.Time, now time.Time) Decision {
	if e == nil || e.reader == nil || studentID == "" || lessonID == "" || now.IsZero() {
		return Decision{Reason: ReasonDependency}
	}
	snapshot, err := e.reader.Load(ctx, studentID, lessonID)
	if err != nil || !validSnapshot(snapshot, studentID) {
		return Decision{Reason: ReasonDependency}
	}
	decision, _, _ := classifySnapshot(snapshot, studentID, assetRetiredAt, now)
	return decision
}

func classifySnapshot(snapshot Snapshot, studentID string, assetRetiredAt *time.Time, now time.Time) (Decision, *time.Time, bool) {

	applicable := make([]Record, 0, len(snapshot.Entitlements))
	for _, record := range snapshot.Entitlements {
		if record.StudentAccountID != studentID || !record.ScopeKind.Valid() || !record.GrantSource.Valid() ||
			!record.State.Valid() || record.AccessEndsAt.IsZero() || record.RetirementEligibilityAt.IsZero() || record.Revoked() || !Covers(record, snapshot.Lesson) {
			continue
		}
		applicable = append(applicable, record)
	}
	if len(applicable) == 0 {
		return Decision{Reason: ReasonNoApplicableGrant}, nil, false
	}
	courseWide := false
	for _, record := range applicable {
		courseWide = courseWide || record.ScopeKind == ScopeCourse
	}

	active := make([]Record, 0, len(applicable))
	for _, record := range applicable {
		// access_ends_at is the effective, authoritative instant. At equality
		// access has already ended.
		if !now.Before(record.AccessEndsAt) {
			continue
		}
		active = append(active, record)
	}
	if len(active) == 0 {
		return Decision{Reason: ReasonExpired}, latestAccessEnd(applicable), courseWide
	}
	if snapshot.Lesson.AccountStatus != "ACTIVE" {
		return Decision{Reason: ReasonAccountSuspended}, nil, courseWide
	}
	if snapshot.Lesson.CourseSuspended {
		return Decision{Reason: ReasonCourseSuspended}, nil, courseWide
	}
	retiredAt := earliestRetirement(snapshot.Lesson.RetiredAt, snapshot.Lesson.SectionRetiredAt, snapshot.Lesson.LessonRetiredAt, assetRetiredAt)
	for _, record := range active {
		if retiredAt == nil || record.RetirementEligibilityAt.Before(*retiredAt) {
			return Decision{Allowed: true, Reason: ReasonAllowed, EntitlementID: record.ID}, latestAccessEnd(active), courseWide
		}
	}
	return Decision{Reason: ReasonRetired}, nil, courseWide
}

func readDecision(decision Decision, expiresAt *time.Time, courseWide bool) ReadDecision {
	if decision.Allowed {
		return ReadDecision{State: ReadActive, Reason: ReasonAllowed, ExpiresAt: expiresAt, CourseWide: courseWide}
	}
	if decision.Reason == ReasonExpired {
		return ReadDecision{State: ReadExpired, Reason: ReasonExpired, ExpiresAt: expiresAt, CourseWide: courseWide}
	}
	return ReadDecision{State: ReadDenied, Reason: decision.Reason, CourseWide: courseWide}
}

func latestAccessEnd(records []Record) *time.Time {
	var latest *time.Time
	for _, record := range records {
		value := record.AccessEndsAt.UTC()
		if latest == nil || value.After(*latest) {
			copy := value
			latest = &copy
		}
	}
	return latest
}

func earliestRetirement(values ...*time.Time) *time.Time {
	var earliest *time.Time
	for _, value := range values {
		if value == nil {
			continue
		}
		if earliest == nil || value.Before(*earliest) {
			copy := value.UTC()
			earliest = &copy
		}
	}
	return earliest
}

func validSnapshot(snapshot Snapshot, studentID string) bool {
	return snapshot.Lesson.ID != "" && snapshot.Lesson.CourseID != "" && snapshot.Lesson.SectionID != "" &&
		snapshot.Lesson.AccountStatus != "" && studentID != ""
}
