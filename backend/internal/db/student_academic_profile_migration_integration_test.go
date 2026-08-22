//go:build integration

package db

import (
	"context"
	"testing"
)

// 0024 is the D-092 T3 Student Academic Profile migration. It is additive and
// referenced by nothing that already exists, so rolling it back must restore the
// pre-T3 database exactly.

func TestStudentAcademicProfileMigrationIsAdditiveAndReversible(t *testing.T) {
	freshDatabase(t)
	m := openMigrator(t)
	// Targeted by version rather than by Up(), so this case keeps testing 0024
	// itself as later additive migrations land on top of it.
	if err := m.Migrate(uint(StudentAcademicProfileSchemaVersion)); err != nil {
		t.Fatalf("migrating up: %v", err)
	}
	pool := openPool(t)
	ctx := context.Background()

	if !tableExists(t, pool, "student_academic_profiles") {
		t.Fatal("0024 did not create student_academic_profiles")
	}

	// Everything T3 must leave alone: the Academic Catalog, the legacy taxonomy,
	// and every account and access table.
	for _, table := range []string{
		"institutions", "academic_units", "programs", "curricula", "subjects", "curriculum_subjects",
		"taxonomy_terms", "courses", "course_revisions", "accounts", "entitlements", "enrollments",
		"course_access_invitations", "purchase_requests", "progress",
	} {
		if !tableExists(t, pool, table) {
			t.Fatalf("0024 removed pre-existing table %s", table)
		}
	}

	// The profile references the Academic Catalog and nothing in the access
	// domain, which is the schema-level half of "discovery data never gates".
	var accessLinks int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM information_schema.table_constraints tc
		JOIN information_schema.constraint_column_usage ccu ON ccu.constraint_name = tc.constraint_name
		WHERE tc.constraint_type = 'FOREIGN KEY'
		  AND tc.table_name = 'student_academic_profiles'
		  AND ccu.table_name IN ('courses','course_revisions','entitlements','enrollments',
		                         'course_access_invitations','purchase_requests','progress')`).
		Scan(&accessLinks); err != nil {
		t.Fatalf("inspecting profile foreign keys: %v", err)
	}
	if accessLinks != 0 {
		t.Fatalf("the Student profile holds %d foreign keys into Course or access tables", accessLinks)
	}
	// And nothing in the access domain references the profile either.
	var reverseLinks int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM information_schema.table_constraints tc
		JOIN information_schema.constraint_column_usage ccu ON ccu.constraint_name = tc.constraint_name
		WHERE tc.constraint_type = 'FOREIGN KEY'
		  AND ccu.table_name = 'student_academic_profiles'
		  AND tc.table_name <> 'student_academic_profiles'`).Scan(&reverseLinks); err != nil {
		t.Fatalf("inspecting reverse foreign keys: %v", err)
	}
	if reverseLinks != 0 {
		t.Fatalf("%d tables reference the Student profile; nothing may depend on discovery data", reverseLinks)
	}

	if err := m.Migrate(uint(AcademicCatalogSchemaVersion)); err != nil {
		t.Fatalf("rolling 0024 back: %v", err)
	}
	if tableExists(t, pool, "student_academic_profiles") {
		t.Fatal("0024 down left student_academic_profiles behind")
	}
	// The Academic Catalog and the legacy taxonomy survive the rollback.
	for _, table := range []string{"institutions", "subjects", "taxonomy_terms", "courses"} {
		if !tableExists(t, pool, table) {
			t.Fatalf("0024 down removed %s", table)
		}
	}
	if err := m.Migrate(uint(StudentAcademicProfileSchemaVersion)); err != nil {
		t.Fatalf("re-applying 0024: %v", err)
	}
	if !tableExists(t, pool, "student_academic_profiles") {
		t.Fatal("0024 up after down did not restore the profile table")
	}
}

func TestStudentAcademicProfileSchemaRefusesIncoherentRows(t *testing.T) {
	freshDatabase(t)
	m := openMigrator(t)
	if err := m.Up(); err != nil {
		t.Fatalf("migrating up: %v", err)
	}
	pool := openPool(t)
	ctx := context.Background()

	var account, institution, college, program, curriculum string
	if err := pool.QueryRow(ctx, `
		INSERT INTO accounts (normalized_email, email, role, status, display_name)
		VALUES ('m24@example.test', 'm24@example.test', 'STUDENT', 'ACTIVE', 'Student')
		RETURNING id::text`).Scan(&account); err != nil {
		t.Fatalf("seeding account: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO institutions (country_code, slug, name_ar, name_en, max_academic_level)
		VALUES ('KW', 'm24-university', 'ج', 'M24 University', 5) RETURNING id::text`).
		Scan(&institution); err != nil {
		t.Fatalf("seeding institution: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO academic_units (institution_id, kind, slug, name_ar, name_en)
		VALUES ($1::uuid, 'COLLEGE', 'science', 'ع', 'Science') RETURNING id::text`,
		institution).Scan(&college); err != nil {
		t.Fatalf("seeding college: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO programs (institution_id, owning_unit_id, slug, name_ar, name_en, degree_kind)
		VALUES ($1::uuid, $2::uuid, 'cs', 'ح', 'CS', 'BSC') RETURNING id::text`,
		institution, college).Scan(&program); err != nil {
		t.Fatalf("seeding program: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO curricula (program_id, institution_id, version_label, status)
		VALUES ($1::uuid, $2::uuid, '2026', 'ACTIVE') RETURNING id::text`,
		program, institution).Scan(&curriculum); err != nil {
		t.Fatalf("seeding curriculum: %v", err)
	}

	// Each of these is a row shape the product must never hold, refused by the
	// schema itself rather than only by application code.
	refused := []struct {
		name      string
		statement string
		args      []any
	}{
		{"a SKIPPED profile carrying academic data",
			`INSERT INTO student_academic_profiles (account_id, setup_state, institution_id)
			 VALUES ($1::uuid, 'SKIPPED', $2::uuid)`, []any{account, institution}},
		{"a COMPLETED profile with no institution",
			`INSERT INTO student_academic_profiles (account_id, setup_state, enrollment_status)
			 VALUES ($1::uuid, 'COMPLETED', 'UNDECLARED')`, []any{account}},
		{"an ENROLLED profile with no program",
			`INSERT INTO student_academic_profiles (account_id, setup_state, enrollment_status, institution_id)
			 VALUES ($1::uuid, 'COMPLETED', 'ENROLLED', $2::uuid)`, []any{account, institution}},
		{"an UNDECLARED profile carrying a program",
			`INSERT INTO student_academic_profiles (account_id, setup_state, enrollment_status,
				institution_id, program_id, curriculum_id)
			 VALUES ($1::uuid, 'COMPLETED', 'UNDECLARED', $2::uuid, $3::uuid, $4::uuid)`,
			[]any{account, institution, program, curriculum}},
		{"an enrolled profile storing the College a second time",
			`INSERT INTO student_academic_profiles (account_id, setup_state, enrollment_status,
				institution_id, academic_unit_id, program_id, curriculum_id)
			 VALUES ($1::uuid, 'COMPLETED', 'ENROLLED', $2::uuid, $3::uuid, $4::uuid, $5::uuid)`,
			[]any{account, institution, college, program, curriculum}},
		{"a curriculum belonging to a different program",
			`INSERT INTO student_academic_profiles (account_id, setup_state, enrollment_status,
				institution_id, program_id, curriculum_id)
			 VALUES ($1::uuid, 'COMPLETED', 'ENROLLED', $2::uuid, $3::uuid,
				(SELECT id FROM curricula WHERE id <> $4::uuid LIMIT 1))`,
			[]any{account, institution, program, curriculum}},
		{"a level with no institution",
			`INSERT INTO student_academic_profiles (account_id, setup_state, current_level)
			 VALUES ($1::uuid, 'SKIPPED', 3)`, []any{account}},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, tc.statement, tc.args...); err == nil {
				_, _ = pool.Exec(ctx, `DELETE FROM student_academic_profiles`)
				t.Fatalf("the schema accepted %s", tc.name)
			}
		})
	}

	// The two coherent shapes are accepted.
	if _, err := pool.Exec(ctx, `
		INSERT INTO student_academic_profiles (account_id, setup_state, enrollment_status,
			institution_id, program_id, curriculum_id, current_level)
		VALUES ($1::uuid, 'COMPLETED', 'ENROLLED', $2::uuid, $3::uuid, $4::uuid, 5)`,
		account, institution, program, curriculum); err != nil {
		t.Fatalf("a valid enrolled profile was refused: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE student_academic_profiles
		SET enrollment_status = 'UNDECLARED', program_id = NULL, curriculum_id = NULL,
			academic_unit_id = $2::uuid
		WHERE account_id = $1::uuid`, account, college); err != nil {
		t.Fatalf("a valid undeclared profile was refused: %v", err)
	}
	// One profile per Account, enforced by the primary key.
	if _, err := pool.Exec(ctx, `
		INSERT INTO student_academic_profiles (account_id, setup_state) VALUES ($1::uuid, 'SKIPPED')`,
		account); err == nil {
		t.Fatal("the schema accepted a second profile for one Account")
	}
}
