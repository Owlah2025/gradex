//go:build integration

package academic

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
)

// T3 Student academic profile domain proofs, against real PostgreSQL and the
// real schema constraints. Nothing here re-implements SQL in Go.

const (
	profileTestDSN    = "postgres://gradex:gradex@localhost:5432/gradex_profile_test?sslmode=disable"
	profileAdminDSN   = "postgres://gradex:gradex@localhost:5432/postgres?sslmode=disable"
	profileTestDBName = "gradex_profile_test"
)

type profileFixture struct {
	repo        *Repository
	pool        *pgxpool.Pool
	student     string
	otherPerson string
	institution string
	college     string
	department  string
	program     string
	curriculum  string
	// A second Program under a different College, used for Program-change proofs.
	otherCollege string
	otherProgram string
}

func newProfileFixture(t *testing.T) *profileFixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	admin, err := pgxpool.New(ctx, profileAdminDSN)
	if err != nil {
		t.Skipf("PostgreSQL is unavailable: %v", err)
	}
	_, _ = admin.Exec(ctx,
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1", profileTestDBName)
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+profileTestDBName); err != nil {
		t.Fatalf("dropping the profile test database: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+profileTestDBName); err != nil {
		t.Fatalf("creating the profile test database: %v", err)
	}
	admin.Close()

	migrator, err := migrate.New("file://../db/migrations", profileTestDSN)
	if err != nil {
		t.Fatalf("opening migrations: %v", err)
	}
	if err := migrator.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("migrating the profile test database: %v", err)
	}
	_, _ = migrator.Close()

	pool, err := pgxpool.New(ctx, profileTestDSN)
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	t.Cleanup(pool.Close)

	repo, err := NewRepository(pool)
	if err != nil {
		t.Fatalf("NewRepository: %v", err)
	}
	f := &profileFixture{repo: repo, pool: pool}

	account := func(email, role string) string {
		var id string
		if err := pool.QueryRow(ctx, `
			INSERT INTO accounts (normalized_email, email, role, status, display_name)
			VALUES ($1, $1, $2, 'ACTIVE', $1) RETURNING id::text`, email, role).Scan(&id); err != nil {
			t.Fatalf("seeding %s: %v", email, err)
		}
		return id
	}
	f.student = account("t3-student@example.test", "STUDENT")
	f.otherPerson = account("t3-other@example.test", "STUDENT")

	if err := pool.QueryRow(ctx, `
		INSERT INTO institutions (country_code, slug, name_ar, name_en, max_academic_level)
		VALUES ('KW', 't3-university', 'جامعة', 'T3 University', 5) RETURNING id::text`).
		Scan(&f.institution); err != nil {
		t.Fatalf("seeding institution: %v", err)
	}
	unit := func(slug, name string, parent *string, kind string) string {
		var id string
		if err := pool.QueryRow(ctx, `
			INSERT INTO academic_units (institution_id, parent_unit_id, kind, slug, name_ar, name_en)
			VALUES ($1::uuid, $2::uuid, $3, $4, $5, $5) RETURNING id::text`,
			f.institution, parent, kind, slug, name).Scan(&id); err != nil {
			t.Fatalf("seeding unit %s: %v", slug, err)
		}
		return id
	}
	f.college = unit("science", "College of Science", nil, "COLLEGE")
	f.department = unit("computer-science", "Computer Science", &f.college, "DEPARTMENT")
	f.otherCollege = unit("life-sciences", "College of Life Sciences", nil, "COLLEGE")
	otherDept := unit("information-science", "Information Science", &f.otherCollege, "DEPARTMENT")

	program := func(slug, name, owner string) string {
		var id string
		if err := pool.QueryRow(ctx, `
			INSERT INTO programs (institution_id, owning_unit_id, slug, name_ar, name_en, degree_kind)
			VALUES ($1::uuid, $2::uuid, $3, $4, $4, 'BSC') RETURNING id::text`,
			f.institution, owner, slug, name).Scan(&id); err != nil {
			t.Fatalf("seeding program %s: %v", slug, err)
		}
		return id
	}
	f.program = program("computer-science", "Computer Science", f.department)
	f.otherProgram = program("data-science", "Data Science", otherDept)

	curriculum := func(program, label string) string {
		var id string
		if err := pool.QueryRow(ctx, `
			INSERT INTO curricula (program_id, institution_id, version_label, status)
			VALUES ($1::uuid, $2::uuid, $3, 'ACTIVE') RETURNING id::text`,
			program, f.institution, label).Scan(&id); err != nil {
			t.Fatalf("seeding curriculum %s: %v", label, err)
		}
		return id
	}
	f.curriculum = curriculum(f.program, "2024")
	curriculum(f.otherProgram, "current")
	return f
}

func level(n int) *int { return &n }

func TestProfileStartsNotStarted(t *testing.T) {
	f := newProfileFixture(t)
	profile, err := f.repo.GetProfile(context.Background(), f.student)
	if err != nil {
		t.Fatalf("reading an absent profile: %v", err)
	}
	// The absence of a row is a normal state, never an error.
	if profile.SetupState != SetupNotStarted {
		t.Fatalf("setup state = %s, want NOT_STARTED", profile.SetupState)
	}
	if profile.InstitutionID != nil || profile.ProgramID != nil {
		t.Fatal("a NOT_STARTED profile carries academic data")
	}
}

func TestSkipThenCompleteTransition(t *testing.T) {
	f := newProfileFixture(t)
	ctx := context.Background()

	skipped, err := f.repo.SkipOnboarding(ctx, f.student)
	if err != nil {
		t.Fatalf("skipping: %v", err)
	}
	if skipped.SetupState != SetupSkipped {
		t.Fatalf("setup state = %s, want SKIPPED", skipped.SetupState)
	}
	// A deferral is empty: it can never be mistaken for a real profile.
	if skipped.InstitutionID != nil || skipped.ProgramID != nil ||
		skipped.CurriculumID != nil || skipped.CurrentLevel != nil || skipped.EnrollmentStatus != nil {
		t.Fatalf("a SKIPPED profile carries academic data: %+v", skipped)
	}
	// Skipping twice is idempotent, not an error.
	if _, err := f.repo.SkipOnboarding(ctx, f.student); err != nil {
		t.Fatalf("skipping twice: %v", err)
	}

	completed, err := f.repo.SaveProfile(ctx, SaveProfileRequest{
		AccountID: f.student, InstitutionID: f.institution,
		EnrollmentStatus: EnrollmentEnrolled, ProgramID: f.program, CurrentLevel: level(2),
	})
	if err != nil {
		t.Fatalf("completing after a skip: %v", err)
	}
	if completed.SetupState != SetupComplete {
		t.Fatalf("setup state = %s, want COMPLETED", completed.SetupState)
	}
	var rows int
	if err := f.pool.QueryRow(ctx,
		`SELECT count(*) FROM student_academic_profiles WHERE account_id = $1::uuid`, f.student).Scan(&rows); err != nil {
		t.Fatalf("counting rows: %v", err)
	}
	if rows != 1 {
		t.Fatalf("SKIPPED → COMPLETED produced %d rows, want 1", rows)
	}
}

func TestEnrolledSaveResolvesActiveCurriculumServerSide(t *testing.T) {
	f := newProfileFixture(t)
	saved, err := f.repo.SaveProfile(context.Background(), SaveProfileRequest{
		AccountID: f.student, InstitutionID: f.institution,
		EnrollmentStatus: EnrollmentEnrolled, ProgramID: f.program, CurrentLevel: level(3),
	})
	if err != nil {
		t.Fatalf("saving an enrolled profile: %v", err)
	}
	if saved.CurriculumID == nil || *saved.CurriculumID != f.curriculum {
		t.Fatalf("curriculum = %v, want the server-resolved ACTIVE plan %s", saved.CurriculumID, f.curriculum)
	}
	if saved.CurriculumLabel == nil || *saved.CurriculumLabel != "2024" {
		t.Fatalf("curriculum label = %v, want 2024", saved.CurriculumLabel)
	}
	// The College is derived from the Program's ancestry, never stored twice.
	if saved.AcademicUnitID != nil {
		t.Fatal("an enrolled profile stored a redundant academic unit")
	}
	if saved.CollegeName == nil || *saved.CollegeName != "College of Science" {
		t.Fatalf("derived college = %v, want College of Science", saved.CollegeName)
	}
	if saved.DepartmentName == nil || *saved.DepartmentName != "Computer Science" {
		t.Fatalf("derived department = %v", saved.DepartmentName)
	}
}

func TestClientCannotChooseCurriculum(t *testing.T) {
	f := newProfileFixture(t)
	_, err := f.repo.SaveProfile(context.Background(), SaveProfileRequest{
		AccountID: f.student, InstitutionID: f.institution,
		EnrollmentStatus: EnrollmentEnrolled, ProgramID: f.program,
		SuppliedCurriculumID: f.curriculum,
	})
	// Even supplying the *correct* curriculum is refused: the field is not the
	// client's to send, and silently ignoring it would hide the contract.
	if !errors.Is(err, ErrCurriculumNotSelectable) {
		t.Fatalf("client-supplied curriculum error = %v, want ErrCurriculumNotSelectable", err)
	}
}

func TestLevelEditPreservesHistoricalCurriculum(t *testing.T) {
	f := newProfileFixture(t)
	ctx := context.Background()

	if _, err := f.repo.SaveProfile(ctx, SaveProfileRequest{
		AccountID: f.student, InstitutionID: f.institution,
		EnrollmentStatus: EnrollmentEnrolled, ProgramID: f.program, CurrentLevel: level(1),
	}); err != nil {
		t.Fatalf("initial save: %v", err)
	}

	// The university publishes a newer plan and supersedes the old one.
	var newer string
	if _, err := f.pool.Exec(ctx,
		`UPDATE curricula SET status = 'SUPERSEDED' WHERE id = $1::uuid`, f.curriculum); err != nil {
		t.Fatalf("superseding: %v", err)
	}
	if err := f.pool.QueryRow(ctx, `
		INSERT INTO curricula (program_id, institution_id, version_label, status)
		VALUES ($1::uuid, $2::uuid, '2026', 'ACTIVE') RETURNING id::text`,
		f.program, f.institution).Scan(&newer); err != nil {
		t.Fatalf("seeding the newer plan: %v", err)
	}

	// The Student edits only their level.
	updated, err := f.repo.SaveProfile(ctx, SaveProfileRequest{
		AccountID: f.student, InstitutionID: f.institution,
		EnrollmentStatus: EnrollmentEnrolled, ProgramID: f.program, CurrentLevel: level(4),
	})
	if err != nil {
		t.Fatalf("level edit: %v", err)
	}
	// They stay on the plan they enrolled under.
	if updated.CurriculumID == nil || *updated.CurriculumID != f.curriculum {
		t.Fatalf("curriculum = %v after a level edit, want the original %s", updated.CurriculumID, f.curriculum)
	}
	if updated.CurrentLevel == nil || *updated.CurrentLevel != 4 {
		t.Fatalf("level = %v, want 4", updated.CurrentLevel)
	}

	// Changing Program does resolve the new Program's current ACTIVE plan.
	changed, err := f.repo.SaveProfile(ctx, SaveProfileRequest{
		AccountID: f.student, InstitutionID: f.institution,
		EnrollmentStatus: EnrollmentEnrolled, ProgramID: f.otherProgram,
	})
	if err != nil {
		t.Fatalf("program change: %v", err)
	}
	if changed.CurriculumID == nil || *changed.CurriculumID == f.curriculum {
		t.Fatalf("curriculum = %v after a program change, want the new program's plan", changed.CurriculumID)
	}
	// And returning to the original Program now resolves *its* current ACTIVE
	// plan, because this is a Program change rather than a level edit.
	back, err := f.repo.SaveProfile(ctx, SaveProfileRequest{
		AccountID: f.student, InstitutionID: f.institution,
		EnrollmentStatus: EnrollmentEnrolled, ProgramID: f.program,
	})
	if err != nil {
		t.Fatalf("returning to the original program: %v", err)
	}
	if back.CurriculumID == nil || *back.CurriculumID != newer {
		t.Fatalf("curriculum = %v, want the now-active %s", back.CurriculumID, newer)
	}
}

func TestUndeclaredKeepsCollegeContextAndNoProgram(t *testing.T) {
	f := newProfileFixture(t)
	saved, err := f.repo.SaveProfile(context.Background(), SaveProfileRequest{
		AccountID: f.student, InstitutionID: f.institution,
		EnrollmentStatus: EnrollmentUndeclared, AcademicUnitID: f.college, CurrentLevel: level(1),
	})
	if err != nil {
		t.Fatalf("saving an undeclared profile: %v", err)
	}
	if saved.EnrollmentStatus == nil || *saved.EnrollmentStatus != EnrollmentUndeclared {
		t.Fatalf("status = %v, want UNDECLARED", saved.EnrollmentStatus)
	}
	// The College context is retained — the whole point of D-092 §2.
	if saved.AcademicUnitID == nil || *saved.AcademicUnitID != f.college {
		t.Fatalf("academic unit = %v, want the selected College", saved.AcademicUnitID)
	}
	if saved.ProgramID != nil || saved.CurriculumID != nil {
		t.Fatal("an undeclared profile stored a Program or a plan")
	}
	// No placeholder Program was invented anywhere in the catalog.
	var fake int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM programs WHERE name_en ILIKE '%undeclared%' OR slug ILIKE '%undeclared%'`).
		Scan(&fake); err != nil {
		t.Fatalf("checking for a fake Program: %v", err)
	}
	if fake != 0 {
		t.Fatalf("%d placeholder Undeclared Programs exist", fake)
	}
}

func TestNonDegreeAndFoundationShapes(t *testing.T) {
	f := newProfileFixture(t)
	ctx := context.Background()

	nonDegree, err := f.repo.SaveProfile(ctx, SaveProfileRequest{
		AccountID: f.student, InstitutionID: f.institution, EnrollmentStatus: EnrollmentNonDegree,
	})
	if err != nil {
		t.Fatalf("saving a non-degree profile: %v", err)
	}
	if nonDegree.ProgramID != nil || nonDegree.CurriculumID != nil {
		t.Fatal("a non-degree profile fabricated a Program or a plan")
	}

	// This institution declares no foundation stage, so the state is refused.
	if _, err := f.repo.SaveProfile(ctx, SaveProfileRequest{
		AccountID: f.student, InstitutionID: f.institution, EnrollmentStatus: EnrollmentFoundation,
	}); !errors.Is(err, ErrFoundationUnsupported) {
		t.Fatalf("foundation error = %v, want ErrFoundationUnsupported", err)
	}

	// Where an institution does declare one, it is accepted — the rule is data,
	// not a hardcoded exception for one university.
	var foundationInstitution string
	if err := f.pool.QueryRow(ctx, `
		INSERT INTO institutions (country_code, slug, name_ar, name_en, max_academic_level, has_foundation_stage)
		VALUES ('KW', 't3-foundation-university', 'ج', 'Foundation University', 4, true)
		RETURNING id::text`).Scan(&foundationInstitution); err != nil {
		t.Fatalf("seeding a foundation institution: %v", err)
	}
	if _, err := f.repo.SaveProfile(ctx, SaveProfileRequest{
		AccountID: f.student, InstitutionID: foundationInstitution, EnrollmentStatus: EnrollmentFoundation,
	}); err != nil {
		t.Fatalf("foundation save where the institution supports it: %v", err)
	}
}

func TestProfileRefusesIncoherentCombinations(t *testing.T) {
	f := newProfileFixture(t)
	ctx := context.Background()

	// A second institution whose Program must never be reachable from the first.
	var otherInstitution, foreignProgram, foreignUnit string
	if err := f.pool.QueryRow(ctx, `
		INSERT INTO institutions (country_code, slug, name_ar, name_en, max_academic_level)
		VALUES ('KW', 't3-other-university', 'ج', 'Other University', 4) RETURNING id::text`).
		Scan(&otherInstitution); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	if err := f.pool.QueryRow(ctx, `
		INSERT INTO academic_units (institution_id, kind, slug, name_ar, name_en)
		VALUES ($1::uuid, 'COLLEGE', 'foreign', 'أ', 'Foreign College') RETURNING id::text`,
		otherInstitution).Scan(&foreignUnit); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	if err := f.pool.QueryRow(ctx, `
		INSERT INTO programs (institution_id, owning_unit_id, slug, name_ar, name_en, degree_kind)
		VALUES ($1::uuid, $2::uuid, 'foreign-program', 'ب', 'Foreign Program', 'BSC') RETURNING id::text`,
		otherInstitution, foreignUnit).Scan(&foreignProgram); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	cases := []struct {
		name string
		req  SaveProfileRequest
		want error
	}{
		{"cross-institution program", SaveProfileRequest{
			AccountID: f.student, InstitutionID: f.institution,
			EnrollmentStatus: EnrollmentEnrolled, ProgramID: foreignProgram}, ErrProgramNotSelectable},
		{"cross-institution college", SaveProfileRequest{
			AccountID: f.student, InstitutionID: f.institution,
			EnrollmentStatus: EnrollmentUndeclared, AcademicUnitID: foreignUnit}, ErrUnitNotSelectable},
		{"enrolled without a program", SaveProfileRequest{
			AccountID: f.student, InstitutionID: f.institution,
			EnrollmentStatus: EnrollmentEnrolled}, ErrProfileInvalid},
		{"enrolled with a redundant college", SaveProfileRequest{
			AccountID: f.student, InstitutionID: f.institution, EnrollmentStatus: EnrollmentEnrolled,
			ProgramID: f.program, AcademicUnitID: f.college}, ErrProfileInvalid},
		{"undeclared carrying a program", SaveProfileRequest{
			AccountID: f.student, InstitutionID: f.institution,
			EnrollmentStatus: EnrollmentUndeclared, ProgramID: f.program}, ErrProfileInvalid},
		{"unknown status", SaveProfileRequest{
			AccountID: f.student, InstitutionID: f.institution,
			EnrollmentStatus: EnrollmentStatus("GRADUATED")}, ErrProfileInvalid},
		{"no institution", SaveProfileRequest{
			AccountID: f.student, EnrollmentStatus: EnrollmentUndeclared}, ErrProfileInvalid},
		{"level below range", SaveProfileRequest{
			AccountID: f.student, InstitutionID: f.institution,
			EnrollmentStatus: EnrollmentEnrolled, ProgramID: f.program, CurrentLevel: level(0)}, ErrLevelOutOfRange},
		{"level above institution maximum", SaveProfileRequest{
			AccountID: f.student, InstitutionID: f.institution,
			EnrollmentStatus: EnrollmentEnrolled, ProgramID: f.program, CurrentLevel: level(6)}, ErrLevelOutOfRange},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := f.repo.SaveProfile(ctx, tc.req); !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}

	// The bounds themselves are the institution's own, not constants.
	for _, valid := range []int{1, 5} {
		if _, err := f.repo.SaveProfile(ctx, SaveProfileRequest{
			AccountID: f.student, InstitutionID: f.institution,
			EnrollmentStatus: EnrollmentEnrolled, ProgramID: f.program, CurrentLevel: level(valid),
		}); err != nil {
			t.Fatalf("level %d was refused: %v", valid, err)
		}
	}
	// Nothing above refused a save yet left a row behind.
	var rows int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM student_academic_profiles`).Scan(&rows); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if rows != 1 {
		t.Fatalf("profile rows = %d, want exactly the one valid save", rows)
	}
}

func TestProgramWithoutActiveCurriculumFailsCleanly(t *testing.T) {
	f := newProfileFixture(t)
	ctx := context.Background()
	var orphan string
	if err := f.pool.QueryRow(ctx, `
		INSERT INTO programs (institution_id, owning_unit_id, slug, name_ar, name_en, degree_kind)
		VALUES ($1::uuid, $2::uuid, 'no-plan', 'ب', 'Program Without Plan', 'BSC') RETURNING id::text`,
		f.institution, f.department).Scan(&orphan); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	_, err := f.repo.SaveProfile(ctx, SaveProfileRequest{
		AccountID: f.student, InstitutionID: f.institution,
		EnrollmentStatus: EnrollmentEnrolled, ProgramID: orphan,
	})
	// A nameable domain refusal, never a constraint violation surfacing as 500.
	if !errors.Is(err, ErrNoActiveCurriculum) {
		t.Fatalf("error = %v, want ErrNoActiveCurriculum", err)
	}
	var rows int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM student_academic_profiles`).Scan(&rows); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if rows != 0 {
		t.Fatal("a program with no active plan was partially saved")
	}
}

func TestRetiredProgramStaysReadableButUnselectable(t *testing.T) {
	f := newProfileFixture(t)
	ctx := context.Background()
	if _, err := f.repo.SaveProfile(ctx, SaveProfileRequest{
		AccountID: f.student, InstitutionID: f.institution,
		EnrollmentStatus: EnrollmentEnrolled, ProgramID: f.program, CurrentLevel: level(2),
	}); err != nil {
		t.Fatalf("initial save: %v", err)
	}
	if _, err := f.pool.Exec(ctx,
		`UPDATE programs SET retired_at = now() WHERE id = $1::uuid`, f.program); err != nil {
		t.Fatalf("retiring: %v", err)
	}

	// Historical profile state survives and still reads.
	profile, err := f.repo.GetProfile(ctx, f.student)
	if err != nil {
		t.Fatalf("reading a profile on a retired Program: %v", err)
	}
	if profile.ProgramID == nil || *profile.ProgramID != f.program {
		t.Fatal("the retired Program was erased from the profile")
	}
	if profile.ProgramName == nil {
		t.Fatal("the retired Program's name no longer resolves")
	}

	// It is no longer offered, and cannot be newly chosen.
	options, err := f.repo.ListProgramOptions(ctx, f.institution, f.college)
	if err != nil {
		t.Fatalf("listing options: %v", err)
	}
	for _, option := range options {
		if option.ID == f.program {
			t.Fatal("a retired Program is still offered as selectable")
		}
	}
	if _, err := f.repo.SaveProfile(ctx, SaveProfileRequest{
		AccountID: f.otherPerson, InstitutionID: f.institution,
		EnrollmentStatus: EnrollmentEnrolled, ProgramID: f.program,
	}); !errors.Is(err, ErrProgramNotSelectable) {
		t.Fatalf("selecting a retired Program error = %v, want ErrProgramNotSelectable", err)
	}
}

func TestConcurrentSavesLeaveOneCoherentRow(t *testing.T) {
	f := newProfileFixture(t)
	ctx := context.Background()

	const racers = 6
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			// Alternate between two entirely different coherent profiles. A
			// last-write mixture would produce a row holding one field from each.
			if slot%2 == 0 {
				_, _ = f.repo.SaveProfile(ctx, SaveProfileRequest{
					AccountID: f.student, InstitutionID: f.institution,
					EnrollmentStatus: EnrollmentEnrolled, ProgramID: f.program, CurrentLevel: level(2),
				})
				return
			}
			_, _ = f.repo.SaveProfile(ctx, SaveProfileRequest{
				AccountID: f.student, InstitutionID: f.institution,
				EnrollmentStatus: EnrollmentUndeclared, AcademicUnitID: f.college,
			})
		}(i)
	}
	wg.Wait()

	var rows int
	if err := f.pool.QueryRow(ctx,
		`SELECT count(*) FROM student_academic_profiles WHERE account_id = $1::uuid`, f.student).Scan(&rows); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if rows != 1 {
		t.Fatalf("concurrent saves produced %d rows, want 1", rows)
	}
	final, err := f.repo.GetProfile(ctx, f.student)
	if err != nil {
		t.Fatalf("reading the final row: %v", err)
	}
	// Whichever writer won, the row must be one of the two coherent shapes and
	// never a blend of both.
	switch *final.EnrollmentStatus {
	case EnrollmentEnrolled:
		if final.ProgramID == nil || final.CurriculumID == nil || final.AcademicUnitID != nil {
			t.Fatalf("an interleaved enrolled row: %+v", final)
		}
	case EnrollmentUndeclared:
		if final.ProgramID != nil || final.CurriculumID != nil || final.AcademicUnitID == nil {
			t.Fatalf("an interleaved undeclared row: %+v", final)
		}
	default:
		t.Fatalf("unexpected final status %v", *final.EnrollmentStatus)
	}
}

func TestOptionProjectionsExposeOnlyActiveCatalogData(t *testing.T) {
	f := newProfileFixture(t)
	ctx := context.Background()

	institutions, err := f.repo.ListInstitutionOptions(ctx)
	if err != nil {
		t.Fatalf("listing institutions: %v", err)
	}
	if len(institutions) == 0 {
		t.Fatal("no institutions offered; this detector would pass vacuously")
	}
	// The level bound travels with the institution so no surface hardcodes it.
	for _, option := range institutions {
		if option.ID == f.institution && option.MaxAcademicLevel != 5 {
			t.Fatalf("max academic level = %d, want the institution's own 5", option.MaxAcademicLevel)
		}
	}

	// Only Colleges are offered as Colleges; a Department never is.
	colleges, err := f.repo.ListCollegeOptions(ctx, f.institution)
	if err != nil {
		t.Fatalf("listing colleges: %v", err)
	}
	for _, option := range colleges {
		if option.ID == f.department {
			t.Fatal("a Department was offered as a College")
		}
	}

	// Programs resolve through the College's whole subtree, which is what lets a
	// Student skip the Department step entirely.
	programs, err := f.repo.ListProgramOptions(ctx, f.institution, f.college)
	if err != nil {
		t.Fatalf("listing programs: %v", err)
	}
	if len(programs) != 1 || programs[0].ID != f.program {
		t.Fatalf("College of Science programs = %+v, want only Computer Science", programs)
	}
	if programs[0].DepartmentNameEn == nil || *programs[0].DepartmentNameEn != "Computer Science" {
		t.Fatalf("department context = %v, want it returned for display", programs[0].DepartmentNameEn)
	}
	// A different College yields only its own Programs.
	otherPrograms, err := f.repo.ListProgramOptions(ctx, f.institution, f.otherCollege)
	if err != nil {
		t.Fatalf("listing other programs: %v", err)
	}
	if len(otherPrograms) != 1 || otherPrograms[0].ID != f.otherProgram {
		t.Fatalf("College of Life Sciences programs = %+v", otherPrograms)
	}

	// Retired rows never appear.
	if _, err := f.pool.Exec(ctx,
		`UPDATE academic_units SET retired_at = now() WHERE id = $1::uuid`, f.otherCollege); err != nil {
		t.Fatalf("retiring: %v", err)
	}
	colleges, err = f.repo.ListCollegeOptions(ctx, f.institution)
	if err != nil {
		t.Fatalf("re-listing colleges: %v", err)
	}
	for _, option := range colleges {
		if option.ID == f.otherCollege {
			t.Fatal("a retired College is still offered")
		}
	}
}
