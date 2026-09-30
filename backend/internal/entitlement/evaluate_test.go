package entitlement

import (
	"context"
	"errors"
	"testing"
	"time"
)

type readerFunc func(context.Context, string, string) (Snapshot, error)

func (f readerFunc) Load(ctx context.Context, studentID, lessonID string) (Snapshot, error) {
	return f(ctx, studentID, lessonID)
}

type courseReadReaderFunc struct {
	snapshot  Snapshot
	snapshots []CourseReadSnapshot
}

func (f courseReadReaderFunc) Load(context.Context, string, string) (Snapshot, error) {
	return f.snapshot, nil
}

func (f courseReadReaderFunc) LoadCourseReadSnapshots(context.Context, string) ([]CourseReadSnapshot, error) {
	return f.snapshots, nil
}

func TestEvaluatorRequiresReader(t *testing.T) {
	if _, err := NewEvaluator(nil); err == nil {
		t.Fatal("NewEvaluator(nil) succeeded")
	}
}

func TestEvaluateCourseAndSectionScopeAcrossCompleteGraph(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	student := "student"
	lessons := []Lesson{
		{ID: "a1", CourseID: "course-a", SectionID: "section-1", AccountStatus: "ACTIVE"},
		{ID: "a2", CourseID: "course-a", SectionID: "section-1", AccountStatus: "ACTIVE"},
		{ID: "a3", CourseID: "course-a", SectionID: "section-2", AccountStatus: "ACTIVE"},
		{ID: "b1", CourseID: "course-b", SectionID: "section-3", AccountStatus: "ACTIVE"},
	}
	courseGrant := testRecord("course", student, ScopeCourse, "course-a", "course-a", now)
	sectionGrant := testRecord("section", student, ScopeSection, "section-1", "course-a", now)

	for _, tc := range []struct {
		name  string
		grant []Record
		want  map[string]bool
	}{
		{"course covers every lesson in its complete course", []Record{courseGrant}, map[string]bool{"a1": true, "a2": true, "a3": true, "b1": false}},
		{"section covers only its own section", []Record{sectionGrant}, map[string]bool{"a1": true, "a2": true, "a3": false, "b1": false}},
		{"overlapping grants are an independent union", []Record{courseGrant, sectionGrant}, map[string]bool{"a1": true, "a2": true, "a3": true, "b1": false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, lesson := range lessons {
				lesson := lesson
				e, err := NewEvaluator(readerFunc(func(context.Context, string, string) (Snapshot, error) {
					return Snapshot{Lesson: lesson, Entitlements: tc.grant}, nil
				}))
				if err != nil {
					t.Fatal(err)
				}
				if got := e.Evaluate(context.Background(), student, lesson.ID, now); got.Allowed != tc.want[lesson.ID] {
					t.Fatalf("lesson %s decision=%+v, want allowed=%v", lesson.ID, got, tc.want[lesson.ID])
				}
			}
		})
	}
}

func TestEvaluateAcceptsPurchaseRequestGrantSource(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	record := testRecord("purchase", "student", ScopeCourse, "course", "course", now)
	record.GrantSource = GrantSourcePurchaseRequest
	evaluator, err := NewEvaluator(readerFunc(func(context.Context, string, string) (Snapshot, error) {
		return Snapshot{
			Lesson:       Lesson{ID: "lesson", CourseID: "course", SectionID: "section", AccountStatus: "ACTIVE"},
			Entitlements: []Record{record},
		}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got := evaluator.Evaluate(context.Background(), "student", "lesson", now); !got.Allowed || got.Reason != ReasonAllowed {
		t.Fatalf("purchase-backed entitlement decision=%+v, want allowed", got)
	}
}

func TestEvaluateUsesEffectiveExpiryAndAppliesRuntimeDenials(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	student := "student"
	base := Lesson{ID: "lesson", CourseID: "course", SectionID: "section", AccountStatus: "ACTIVE"}
	record := testRecord("grant", student, ScopeCourse, "course", "course", now)

	cases := []struct {
		name   string
		lesson Lesson
		change func(*Record)
		want   Reason
	}{
		{"effective expiry wins over original", base, func(r *Record) { r.OriginalAccessEndsAt = now.Add(time.Hour); r.AccessEndsAt = now }, ReasonExpired},
		{"equality is expired", base, func(r *Record) { r.AccessEndsAt = now }, ReasonExpired},
		{"revoked denies", base, func(r *Record) { r.State = StateRevoked; at := now.Add(-time.Minute); r.RevokedAt = &at }, ReasonNoApplicableGrant},
		{"suspended Account denies", Lesson{ID: base.ID, CourseID: base.CourseID, SectionID: base.SectionID, AccountStatus: "SUSPENDED"}, func(*Record) {}, ReasonAccountSuspended},
		{"emergency suspension denies", Lesson{ID: base.ID, CourseID: base.CourseID, SectionID: base.SectionID, AccountStatus: "ACTIVE", CourseSuspended: true}, func(*Record) {}, ReasonCourseSuspended},
		{"retired content needs eligibility before retirement", func() Lesson {
			retired := now.Add(-time.Minute)
			return Lesson{ID: base.ID, CourseID: base.CourseID, SectionID: base.SectionID, AccountStatus: "ACTIVE", RetiredAt: &retired}
		}(), func(r *Record) { r.RetirementEligibilityAt = now }, ReasonRetired},
		{"retired Section needs eligibility before retirement", func() Lesson {
			retired := now.Add(-time.Minute)
			return Lesson{ID: base.ID, CourseID: base.CourseID, SectionID: base.SectionID, AccountStatus: "ACTIVE", SectionRetiredAt: &retired}
		}(), func(r *Record) { r.RetirementEligibilityAt = now }, ReasonRetired},
		{"retired Lesson needs eligibility before retirement", func() Lesson {
			retired := now.Add(-time.Minute)
			return Lesson{ID: base.ID, CourseID: base.CourseID, SectionID: base.SectionID, AccountStatus: "ACTIVE", LessonRetiredAt: &retired}
		}(), func(r *Record) { r.RetirementEligibilityAt = now }, ReasonRetired},
		{"retired qualifying content allows", func() Lesson {
			retired := now.Add(-time.Minute)
			return Lesson{ID: base.ID, CourseID: base.CourseID, SectionID: base.SectionID, AccountStatus: "ACTIVE", RetiredAt: &retired}
		}(), func(r *Record) { r.RetirementEligibilityAt = now.Add(-time.Hour) }, ReasonAllowed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidate := record
			tc.change(&candidate)
			e, err := NewEvaluator(readerFunc(func(context.Context, string, string) (Snapshot, error) {
				return Snapshot{Lesson: tc.lesson, Entitlements: []Record{candidate}}, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			if got := e.Evaluate(context.Background(), student, base.ID, now); got.Reason != tc.want || got.Allowed != (tc.want == ReasonAllowed) {
				t.Fatalf("decision=%+v, want reason=%s", got, tc.want)
			}
		})
	}
}

func TestEvaluateFailsClosedForMissingOrFaultedInputs(t *testing.T) {
	e, err := NewEvaluator(readerFunc(func(context.Context, string, string) (Snapshot, error) {
		return Snapshot{}, errors.New("database unavailable")
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got := e.Evaluate(context.Background(), "student", "lesson", time.Now()); got.Allowed || got.Reason != ReasonDependency {
		t.Fatalf("fault decision=%+v, want dependency denial", got)
	}
}

func TestEvaluateReadSeparatesActiveExpiredAndDeniedPresentation(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	student := "student"
	lesson := Lesson{ID: "lesson", CourseID: "course", SectionID: "section", AccountStatus: "ACTIVE"}
	cases := []struct {
		name       string
		endsAt     time.Time
		state      State
		wantState  ReadState
		wantCourse bool
		wantReason Reason
	}{
		{name: "active course grant", endsAt: now.Add(time.Hour), state: StateActive, wantState: ReadActive, wantCourse: true, wantReason: ReasonAllowed},
		{name: "effective expiry retained for display", endsAt: now, state: StateActive, wantState: ReadExpired, wantCourse: true, wantReason: ReasonExpired},
		{name: "revoked grant denied", endsAt: now.Add(time.Hour), state: StateRevoked, wantState: ReadDenied, wantCourse: false, wantReason: ReasonNoApplicableGrant},
		{name: "suspended Account denies even retained expiry", endsAt: now, state: StateActive, wantState: ReadDenied, wantCourse: false, wantReason: ReasonAccountSuspended},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			record := testRecord("grant", student, ScopeCourse, "course", "course", now)
			record.AccessEndsAt, record.OriginalAccessEndsAt, record.State = tc.endsAt, tc.endsAt, tc.state
			if tc.wantReason == ReasonAccountSuspended {
				lesson.AccountStatus = "SUSPENDED"
			}
			if tc.state == StateRevoked {
				revokedAt := now.Add(-time.Minute)
				record.RevokedAt = &revokedAt
			}
			evaluator, err := NewEvaluator(courseReadReaderFunc{snapshot: Snapshot{Lesson: lesson, Entitlements: []Record{record}}})
			if err != nil {
				t.Fatal(err)
			}
			got := evaluator.EvaluateRead(context.Background(), student, lesson.ID, now)
			if got.State != tc.wantState || got.CourseWide != tc.wantCourse || got.Reason != tc.wantReason {
				t.Fatalf("read decision = %+v, want state=%s course-wide=%v reason=%s", got, tc.wantState, tc.wantCourse, tc.wantReason)
			}
			if got.State == ReadExpired && (got.ExpiresAt == nil || !got.ExpiresAt.Equal(tc.endsAt)) {
				t.Fatalf("expired read expiry = %v, want %s", got.ExpiresAt, tc.endsAt)
			}
		})
	}
}

func TestEvaluateCourseReadsUsesOneBulkClassificationBoundary(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	student := "student"
	active := testRecord("active", student, ScopeCourse, "course-a", "course-a", now)
	expired := testRecord("expired", student, ScopeCourse, "course-b", "course-b", now)
	expired.AccessEndsAt = now
	evaluator, err := NewEvaluator(courseReadReaderFunc{snapshots: []CourseReadSnapshot{
		{CourseID: "course-a", Lesson: Lesson{ID: "a", CourseID: "course-a", SectionID: "section", AccountStatus: "ACTIVE"}, Entitlements: []Record{active}},
		{CourseID: "course-b", Lesson: Lesson{ID: "b", CourseID: "course-b", SectionID: "section", AccountStatus: "ACTIVE"}, Entitlements: []Record{expired}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := evaluator.EvaluateCourseReads(context.Background(), student, now)
	if err != nil {
		t.Fatal(err)
	}
	if got["course-a"].State != ReadActive || got["course-b"].State != ReadExpired {
		t.Fatalf("bulk read classifications = %+v", got)
	}
}

func TestEvaluateCourseHistoryClassifiesEndingAndRuntimeStates(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	student := "student"
	baseLesson := Lesson{ID: "lesson", CourseID: "course", SectionID: "section", AccountStatus: "ACTIVE"}
	active := testRecord("active", student, ScopeCourse, "course", "course", now)
	expiredAt := now.Add(-2 * time.Hour)
	expired := active
	expired.AccessEndsAt = expiredAt
	expired.OriginalAccessEndsAt = expiredAt
	revokedAt := now.Add(-time.Hour)
	revoked := active
	revoked.State = StateRevoked
	revoked.RevokedAt = &revokedAt
	retiredAt := now.Add(-time.Minute)
	retired := active
	retired.RetirementEligibilityAt = now

	for _, tc := range []struct {
		name          string
		lesson        Lesson
		entitlements  []Record
		wantState     ReadState
		wantReason    Reason
		wantEnded     Reason
		wantEndedAt   *time.Time
		wantCourse    bool
		wantInResults bool
	}{
		{name: "active", lesson: baseLesson, entitlements: []Record{active}, wantState: ReadActive, wantReason: ReasonAllowed, wantCourse: true, wantInResults: true},
		{name: "expired", lesson: baseLesson, entitlements: []Record{expired}, wantState: ReadExpired, wantReason: ReasonExpired, wantEnded: ReasonExpired, wantEndedAt: &expiredAt, wantCourse: true, wantInResults: true},
		{name: "revoked", lesson: baseLesson, entitlements: []Record{revoked}, wantState: ReadDenied, wantReason: ReasonNoApplicableGrant, wantEnded: ReasonRevoked, wantEndedAt: &revokedAt, wantCourse: true, wantInResults: true},
		{name: "newer revocation wins over older expiry", lesson: baseLesson, entitlements: []Record{expired, revoked}, wantState: ReadExpired, wantReason: ReasonExpired, wantEnded: ReasonRevoked, wantEndedAt: &revokedAt, wantCourse: true, wantInResults: true},
		{name: "newer expiry wins over older revocation", lesson: baseLesson, entitlements: []Record{func() Record {
			candidate := expired
			candidate.AccessEndsAt = now.Add(-30 * time.Minute)
			candidate.OriginalAccessEndsAt = candidate.AccessEndsAt
			return candidate
		}(), func() Record {
			candidate := revoked
			at := now.Add(-2 * time.Hour)
			candidate.RevokedAt = &at
			return candidate
		}()}, wantState: ReadExpired, wantReason: ReasonExpired, wantEnded: ReasonExpired, wantEndedAt: func() *time.Time {
			at := now.Add(-30 * time.Minute)
			return &at
		}(), wantCourse: true, wantInResults: true},
		{name: "course suspension is checked before entitlement expiry", lesson: Lesson{ID: baseLesson.ID, CourseID: baseLesson.CourseID, SectionID: baseLesson.SectionID, AccountStatus: "ACTIVE", CourseSuspended: true}, entitlements: []Record{active}, wantState: ReadDenied, wantReason: ReasonCourseSuspended, wantCourse: true, wantInResults: true},
		{name: "retired content remains classified but is not active", lesson: Lesson{ID: baseLesson.ID, CourseID: baseLesson.CourseID, SectionID: baseLesson.SectionID, AccountStatus: "ACTIVE", RetiredAt: &retiredAt}, entitlements: []Record{retired}, wantState: ReadDenied, wantReason: ReasonRetired, wantCourse: true, wantInResults: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			evaluator, err := NewEvaluator(courseReadReaderFunc{snapshots: []CourseReadSnapshot{{
				CourseID: "course", Lesson: tc.lesson, Entitlements: tc.entitlements,
			}}})
			if err != nil {
				t.Fatal(err)
			}
			got, err := evaluator.EvaluateCourseHistory(context.Background(), student, now)
			if err != nil {
				t.Fatal(err)
			}
			classification, present := got["course"]
			if present != tc.wantInResults {
				t.Fatalf("history presence = %v, want %v; decisions=%+v", present, tc.wantInResults, got)
			}
			if !present {
				return
			}
			if classification.Decision.State != tc.wantState || classification.Decision.Reason != tc.wantReason || classification.CourseWide != tc.wantCourse {
				t.Fatalf("history classification = %+v, want state=%s reason=%s course-wide=%v", classification, tc.wantState, tc.wantReason, tc.wantCourse)
			}
			if classification.EndedReason != tc.wantEnded {
				t.Fatalf("history ended reason = %s, want %s", classification.EndedReason, tc.wantEnded)
			}
			if tc.wantEndedAt == nil {
				if classification.EndedAt != nil {
					t.Fatalf("history ended at = %v, want nil", classification.EndedAt)
				}
				return
			}
			if classification.EndedAt == nil || !classification.EndedAt.Equal(*tc.wantEndedAt) {
				t.Fatalf("history ended at = %v, want %v", classification.EndedAt, *tc.wantEndedAt)
			}
		})
	}
}

func testRecord(id, student string, scope ScopeKind, scopeID, courseID string, now time.Time) Record {
	return Record{ID: id, StudentAccountID: student, ScopeKind: scope, ScopeID: scopeID, CourseID: courseID,
		GrantSource: GrantSourceManualInvitation, OriginalAccessEndsAt: now.Add(2 * time.Hour), AccessEndsAt: now.Add(time.Hour),
		RetirementEligibilityAt: now.Add(-time.Hour), State: StateActive, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour)}
}
