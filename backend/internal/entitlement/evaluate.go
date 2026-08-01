package entitlement

import (
	"context"
	"errors"
	"time"
)

// Evaluator is the sole production authorization decision point for an
// Entitlement. Handlers must call it immediately before each protected issue.
type Evaluator struct{ reader Reader }

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

// EvaluateTarget is the same single decision point with the exact Asset
// Version retirement instant included by protected delivery. It prevents a
// delivery handler from re-implementing retirement eligibility while retaining
// the public Evaluate(student, lesson, now) boundary for ordinary operations.
func (e *Evaluator) EvaluateTarget(ctx context.Context, studentID, lessonID string, assetRetiredAt *time.Time, now time.Time) Decision {
	return e.evaluate(ctx, studentID, lessonID, assetRetiredAt, now)
}

func (e *Evaluator) evaluate(ctx context.Context, studentID, lessonID string, assetRetiredAt *time.Time, now time.Time) Decision {
	if e == nil || e.reader == nil || studentID == "" || lessonID == "" || now.IsZero() {
		return Decision{Reason: ReasonDependency}
	}
	snapshot, err := e.reader.Load(ctx, studentID, lessonID)
	if err != nil || !validSnapshot(snapshot, studentID) {
		return Decision{Reason: ReasonDependency}
	}

	applicable := make([]Record, 0, len(snapshot.Entitlements))
	for _, record := range snapshot.Entitlements {
		if record.StudentAccountID != studentID || !record.ScopeKind.Valid() || !record.GrantSource.Valid() ||
			!record.State.Valid() || record.AccessEndsAt.IsZero() || record.RetirementEligibilityAt.IsZero() || record.Revoked() || !Covers(record, snapshot.Lesson) {
			continue
		}
		applicable = append(applicable, record)
	}
	if len(applicable) == 0 {
		return Decision{Reason: ReasonNoApplicableGrant}
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
		return Decision{Reason: ReasonExpired}
	}
	if snapshot.Lesson.AccountStatus != "ACTIVE" {
		return Decision{Reason: ReasonAccountSuspended}
	}
	if snapshot.Lesson.CourseSuspended {
		return Decision{Reason: ReasonCourseSuspended}
	}
	retiredAt := earliestRetirement(snapshot.Lesson.RetiredAt, snapshot.Lesson.SectionRetiredAt, snapshot.Lesson.LessonRetiredAt, assetRetiredAt)
	for _, record := range active {
		if retiredAt == nil || record.RetirementEligibilityAt.Before(*retiredAt) {
			return Decision{Allowed: true, Reason: ReasonAllowed, EntitlementID: record.ID}
		}
	}
	return Decision{Reason: ReasonRetired}
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
