//go:build integration

package entitlement

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	entitlementAdminDSN = "postgres://gradex:gradex@localhost:5432/postgres?sslmode=disable"
	entitlementDBName   = "gradex_entitlement_d8_test"
	entitlementTestDSN  = "postgres://gradex:gradex@localhost:5432/" + entitlementDBName + "?sslmode=disable"
)

type evaluationFixture struct {
	t        *testing.T
	ctx      context.Context
	pool     *pgxpool.Pool
	student  string
	course   string
	section1 string
	section2 string
	lessons  []string
	now      time.Time
}

func newEvaluationFixture(t *testing.T) *evaluationFixture {
	t.Helper()
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, entitlementAdminDSN)
	if err != nil {
		t.Fatalf("opening PostgreSQL admin pool: %v", err)
	}
	t.Cleanup(admin.Close)
	if _, err := admin.Exec(ctx, "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1", entitlementDBName); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+entitlementDBName); err != nil {
		t.Fatalf("dropping entitlement test database: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+entitlementDBName); err != nil {
		t.Fatalf("creating entitlement test database: %v", err)
	}
	_, file, _, _ := runtime.Caller(0)
	source := "file://" + filepath.ToSlash(filepath.Join(filepath.Dir(file), "../db/migrations"))
	m, err := migrate.New(source, entitlementTestDSN)
	if err != nil {
		t.Fatalf("opening migrations: %v", err)
	}
	t.Cleanup(func() { _, _ = m.Close() })
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("migrating entitlement test database: %v", err)
	}
	pool, err := pgxpool.New(ctx, entitlementTestDSN)
	if err != nil {
		t.Fatalf("opening entitlement pool: %v", err)
	}
	t.Cleanup(pool.Close)

	f := &evaluationFixture{t: t, ctx: ctx, pool: pool, student: uuid.NewString(), course: uuid.NewString(), section1: uuid.NewString(), section2: uuid.NewString(), now: time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)}
	owner := uuid.NewString()
	f.insertAccount(owner, "INSTRUCTOR")
	f.insertAccount(f.student, "STUDENT")
	if _, err := pool.Exec(ctx, `INSERT INTO courses (id, owner_account_id, lifecycle) VALUES ($1::uuid, $2::uuid, 'DRAFT')`, f.course, owner); err != nil {
		t.Fatalf("seeding Course: %v", err)
	}
	revision := uuid.NewString()
	if _, err := pool.Exec(ctx, `INSERT INTO course_revisions (id, course_id, state, revision_number, title_ar, title_en) VALUES ($1::uuid, $2::uuid, 'APPROVED', 1, 'دورة', 'Course')`, revision, f.course); err != nil {
		t.Fatalf("seeding revision: %v", err)
	}
	f.lessons = append(f.lessons, f.insertSectionAndLessons(revision, f.section1, 2)...)
	f.lessons = append(f.lessons, f.insertSectionAndLessons(revision, f.section2, 1)...)
	if _, err := pool.Exec(ctx, `UPDATE courses SET lifecycle = 'PUBLISHED', live_revision_id = $1::uuid WHERE id = $2::uuid`, revision, f.course); err != nil {
		t.Fatalf("publishing evaluation Course: %v", err)
	}
	return f
}

func (f *evaluationFixture) insertAccount(id, role string) {
	f.t.Helper()
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO accounts (id, normalized_email, email, role, status, display_name, locale, email_verified_at) VALUES ($1::uuid, $2, $2, $3, 'ACTIVE', 'D8 Fixture', 'en', now())`, id, id+"@example.test", role); err != nil {
		f.t.Fatalf("seeding %s: %v", role, err)
	}
}

func (f *evaluationFixture) insertSectionAndLessons(revision, sectionID string, count int) []string {
	f.t.Helper()
	sectionRow := uuid.NewString()
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO course_section_identities (id, course_id) VALUES ($1::uuid, $2::uuid)`, sectionID, f.course); err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO course_sections (id, revision_id, course_id, section_identity_id, title_ar, title_en, position) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'قسم', 'Section', $5)`, sectionRow, revision, f.course, sectionID, len(f.lessons)); err != nil {
		f.t.Fatal(err)
	}
	lessons := make([]string, 0, count)
	for i := 0; i < count; i++ {
		identity := uuid.NewString()
		lessonRow := uuid.NewString()
		if _, err := f.pool.Exec(f.ctx, `INSERT INTO course_lesson_identities (id, course_id, section_identity_id) VALUES ($1::uuid, $2::uuid, $3::uuid)`, identity, f.course, sectionID); err != nil {
			f.t.Fatal(err)
		}
		if _, err := f.pool.Exec(f.ctx, `INSERT INTO course_lessons (id, section_id, course_id, section_identity_id, lesson_identity_id, title_ar, title_en, position) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, 'درس', 'Lesson', $6)`, lessonRow, sectionRow, f.course, sectionID, identity, i); err != nil {
			f.t.Fatal(err)
		}
		lessons = append(lessons, identity)
	}
	return lessons
}

func (f *evaluationFixture) seed(id string, scope ScopeKind, scopeID string) Record {
	f.t.Helper()
	invID := uuid.NewString()
	var instructorID string
	if err := f.pool.QueryRow(f.ctx, `SELECT owner_account_id::text FROM courses WHERE id = $1::uuid`, f.course).Scan(&instructorID); err != nil {
		f.t.Fatalf("reading course owner: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO course_access_invitations (id, course_id, email, normalized_email, created_by_account_id, accepted_by_account_id, decided_by_account_id, state)
		VALUES ($1::uuid, $2::uuid, 'student@example.test', 'student@example.test', $3::uuid, $4::uuid, $3::uuid, 'APPROVED')
	`, invID, f.course, instructorID, f.student); err != nil {
		f.t.Fatalf("seeding test invitation: %v", err)
	}
	record := Record{ID: id, StudentAccountID: f.student, ScopeKind: scope, ScopeID: scopeID, CourseID: f.course, GrantSource: GrantSourceManualInvitation, SourceInvitationID: &invID, OriginalAccessEndsAt: f.now.Add(48 * time.Hour), AccessEndsAt: f.now.Add(24 * time.Hour), RetirementEligibilityAt: f.now.Add(-time.Hour), State: StateActive, CreatedAt: f.now.Add(-time.Hour), UpdatedAt: f.now.Add(-time.Hour)}
	if err := seedEvaluationRecord(f.ctx, f.pool, record); err != nil {
		f.t.Fatalf("seeding record: %v", err)
	}
	return record
}

func TestD8EvaluatorReadsAuthoritativeGraphAndGrantUnion(t *testing.T) {
	f := newEvaluationFixture(t)
	courseGrant := f.seed(uuid.NewString(), ScopeCourse, f.course)
	sectionGrant := f.seed(uuid.NewString(), ScopeSection, f.section1)
	repository, err := NewRepository(f.pool)
	if err != nil {
		t.Fatal(err)
	}
	evaluator, err := NewEvaluator(repository)
	if err != nil {
		t.Fatal(err)
	}

	for _, lessonID := range f.lessons {
		if got := evaluator.Evaluate(f.ctx, f.student, lessonID, f.now); !got.Allowed {
			t.Fatalf("Course grant did not cover complete graph lesson %s: %+v", lessonID, got)
		}
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE entitlements SET state = 'REVOKED', revoked_at = $1 WHERE id = $2::uuid`, f.now, courseGrant.ID); err != nil {
		t.Fatal(err)
	}
	for _, lessonID := range f.lessons[:2] {
		if got := evaluator.Evaluate(f.ctx, f.student, lessonID, f.now); !got.Allowed {
			t.Fatalf("independent Section grant did not survive Course grant revocation for %s: %+v", lessonID, got)
		}
	}
	if got := evaluator.Evaluate(f.ctx, f.student, f.lessons[2], f.now); got.Allowed || got.Reason != ReasonNoApplicableGrant {
		t.Fatalf("Section grant reached sibling section: %+v", got)
	}
	var courseRows, sectionRows int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM entitlements WHERE id IN ($1::uuid, $2::uuid) AND scope_kind = 'COURSE'`, courseGrant.ID, sectionGrant.ID).Scan(&courseRows); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM entitlements WHERE id IN ($1::uuid, $2::uuid) AND scope_kind = 'SECTION'`, courseGrant.ID, sectionGrant.ID).Scan(&sectionRows); err != nil {
		t.Fatal(err)
	}
	if courseRows != 1 || sectionRows != 1 {
		t.Fatalf("overlapping records were not independently preserved: course=%d section=%d", courseRows, sectionRows)
	}
}

func TestD8EvaluatorEffectiveExpirySuspensionAndRetirement(t *testing.T) {
	f := newEvaluationFixture(t)
	grant := f.seed(uuid.NewString(), ScopeCourse, f.course)
	repository, _ := NewRepository(f.pool)
	evaluator, _ := NewEvaluator(repository)
	lesson := f.lessons[0]
	if _, err := f.pool.Exec(f.ctx, `UPDATE entitlements SET original_access_ends_at = $1, access_ends_at = $2 WHERE id = $3::uuid`, f.now.Add(time.Hour), f.now, grant.ID); err != nil {
		t.Fatal(err)
	}
	if got := evaluator.Evaluate(f.ctx, f.student, lesson, f.now); got.Reason != ReasonExpired {
		t.Fatalf("effective expiry decision=%+v, want EXPIRED", got)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE entitlements SET access_ends_at = $1 WHERE id = $2::uuid`, f.now.Add(time.Hour), grant.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE courses SET access_suspended_at = $1, access_suspension_reason = 'fixture' WHERE id = $2::uuid`, f.now, f.course); err != nil {
		t.Fatal(err)
	}
	if got := evaluator.Evaluate(f.ctx, f.student, lesson, f.now); got.Reason != ReasonCourseSuspended {
		t.Fatalf("suspension decision=%+v", got)
	}
	var states int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM entitlements WHERE id = $1::uuid AND state = 'ACTIVE'`, grant.ID).Scan(&states); err != nil || states != 1 {
		t.Fatalf("suspension mutated entitlement, count=%d err=%v", states, err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE courses SET access_suspended_at = NULL, access_suspension_reason = NULL, retired_at = $1 WHERE id = $2::uuid`, f.now, f.course); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE entitlements SET retirement_eligibility_at = $1 WHERE id = $2::uuid`, f.now, grant.ID); err != nil {
		t.Fatal(err)
	}
	if got := evaluator.Evaluate(f.ctx, f.student, lesson, f.now); got.Reason != ReasonRetired {
		t.Fatalf("ineligible retirement decision=%+v", got)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE entitlements SET retirement_eligibility_at = $1 WHERE id = $2::uuid`, f.now.Add(-time.Hour), grant.ID); err != nil {
		t.Fatal(err)
	}
	if got := evaluator.Evaluate(f.ctx, f.student, lesson, f.now); !got.Allowed {
		t.Fatalf("qualifying retired decision=%+v", got)
	}
}

func TestD8GrantSourceConstraintRejectsAbsentProvenance(t *testing.T) {
	f := newEvaluationFixture(t)
	_, err := f.pool.Exec(f.ctx, `INSERT INTO entitlements (student_account_id, scope_kind, scope_id, course_id, grant_source, original_access_ends_at, access_ends_at, retirement_eligibility_at) VALUES ($1::uuid, 'COURSE', $2::uuid, $2::uuid, NULL, now() + interval '1 day', now() + interval '1 day', now())`, f.student, f.course)
	if err == nil {
		t.Fatal("schema accepted Entitlement without typed grant provenance")
	}
	if fmt.Sprint(err) == "" {
		t.Fatal("expected database provenance error")
	}
}
