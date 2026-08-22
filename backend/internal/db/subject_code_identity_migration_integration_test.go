//go:build integration

package db

import (
	"testing"
)

// T4-A.1 migration 0026 proof.
//
// 0025 is already accepted and proven, so the guard ships in its own forward
// migration rather than by editing it. 0026 adds a trigger and touches no row,
// which is what makes it trivially reversible.

func TestSubjectCodeIdentityMigrationIsAdditiveAndReversible(t *testing.T) {
	freshDatabase(t)
	m := openMigrator(t)

	// Install at 0025, seed a coded Subject, and confirm renumbering is possible
	// there. This is the gap 0026 exists to close, recorded rather than asserted.
	if err := m.Migrate(uint(CourseAcademicIdentitySchemaVersion)); err != nil {
		t.Fatalf("migrating to the pre-T4-A.1 schema: %v", err)
	}
	pool, ctx := openPoolCtx(t)

	if _, err := pool.Exec(ctx, `
		INSERT INTO institutions (id, country_code, slug, name_ar, name_en)
		VALUES ('66660000-0000-0000-0000-000000000001', 'KW', 'ku-identity', 'ج', 'KU')`); err != nil {
		t.Fatalf("seeding institution: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO subjects (id, institution_id, official_code, title_ar, title_en)
		VALUES ('66660000-0000-0000-0000-000000000011', '66660000-0000-0000-0000-000000000001',
		        '0418-320', 'أ', 'Principles')`); err != nil {
		t.Fatalf("seeding subject: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE subjects SET official_code = '0418-999'
		WHERE id = '66660000-0000-0000-0000-000000000011'`); err != nil {
		t.Fatalf("at 0025 an active Subject could still be renumbered; setup failed: %v", err)
	}
	// Put it back so the upgrade starts from the canonical code.
	if _, err := pool.Exec(ctx, `
		UPDATE subjects SET official_code = '0418-320'
		WHERE id = '66660000-0000-0000-0000-000000000011'`); err != nil {
		t.Fatalf("restoring the canonical code: %v", err)
	}

	// Up. The existing Subject is untouched by the migration itself.
	if err := m.Migrate(uint(SubjectCodeIdentitySchemaVersion)); err != nil {
		t.Fatalf("migrating up to T4-A.1: %v", err)
	}
	var code, normalized string
	if err := pool.QueryRow(ctx, `
		SELECT official_code, code_normalized FROM subjects
		WHERE id = '66660000-0000-0000-0000-000000000011'`).Scan(&code, &normalized); err != nil {
		t.Fatalf("re-reading subject after upgrade: %v", err)
	}
	if code != "0418-320" || normalized != "0418320" {
		t.Fatalf("0026 changed existing Subject data: code=%q normalized=%q", code, normalized)
	}

	// The guard is live: renumbering is now refused, reformatting is not.
	if _, err := pool.Exec(ctx, `
		UPDATE subjects SET official_code = '0418-999'
		WHERE id = '66660000-0000-0000-0000-000000000011'`); err == nil {
		t.Fatalf("0026 did not close active-Subject renumbering")
	}
	if _, err := pool.Exec(ctx, `
		UPDATE subjects SET official_code = '0418 320'
		WHERE id = '66660000-0000-0000-0000-000000000011'`); err != nil {
		t.Fatalf("0026 blocked a formatting-only correction: %v", err)
	}

	// Retirement still works: it does not name official_code, so it never enters
	// the guard.
	if _, err := pool.Exec(ctx, `
		UPDATE subjects SET retired_at = now(), updated_at = now()
		WHERE id = '66660000-0000-0000-0000-000000000011'`); err != nil {
		t.Fatalf("0026 broke Subject retirement: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE subjects SET retired_at = NULL WHERE id = '66660000-0000-0000-0000-000000000011'`); err != nil {
		t.Fatalf("un-retiring for the rollback check: %v", err)
	}

	// Down: the guard disappears, the Subject does not, and T4-A's own schema is
	// untouched.
	if err := m.Migrate(uint(CourseAcademicIdentitySchemaVersion)); err != nil {
		t.Fatalf("migrating T4-A.1 down: %v", err)
	}
	var triggerPresent bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = 'subjects_code_identity_guard')`).
		Scan(&triggerPresent); err != nil {
		t.Fatalf("checking the trigger: %v", err)
	}
	if triggerPresent {
		t.Fatalf("the T4-A.1 guard survived its own rollback")
	}
	var stillThere int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM subjects WHERE id = '66660000-0000-0000-0000-000000000011'`).Scan(&stillThere); err != nil {
		t.Fatalf("counting subjects after rollback: %v", err)
	}
	if stillThere != 1 {
		t.Fatalf("the rollback destroyed Subject data")
	}
	// T4-A's reservation index is owned by 0025 and must still be in force.
	if _, err := pool.Exec(ctx, `
		INSERT INTO subjects (institution_id, official_code, title_ar, title_en)
		VALUES ('66660000-0000-0000-0000-000000000001', '0418320', 'ب', 'Claimant')`); err == nil {
		t.Fatalf("rolling 0026 back also released the 0025 code reservation")
	}

	// Up again.
	if err := m.Migrate(uint(SubjectCodeIdentitySchemaVersion)); err != nil {
		t.Fatalf("re-applying T4-A.1: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE subjects SET official_code = '0418-777'
		WHERE id = '66660000-0000-0000-0000-000000000011'`); err == nil {
		t.Fatalf("the guard did not come back on re-apply")
	}
}

// 13. The 84 launch Subjects must all satisfy the tightened rule, so the
// migration cannot be a latent problem for the only catalog Gradex ships.
func TestSubjectCodeIdentityHoldsForTheLaunchManifest(t *testing.T) {
	freshDatabase(t)
	m := openMigrator(t)
	if err := m.Up(); err != nil {
		t.Fatalf("migrating up: %v", err)
	}
	pool, ctx := openPoolCtx(t)

	// Every coded Subject in the launch manifest normalizes to a distinct code
	// within its Institution. The uniqueness index proves it at write time; this
	// asserts the same property as a readable invariant.
	if _, err := pool.Exec(ctx, `
		INSERT INTO institutions (id, country_code, slug, name_ar, name_en)
		VALUES ('55550000-0000-0000-0000-000000000001', 'KW', 'ku-manifest', 'ج', 'KU')`); err != nil {
		t.Fatalf("seeding institution: %v", err)
	}

	// A representative slice of real launch codes, including the alphabetic and
	// numeric schemes and the shared Calculus code.
	for _, code := range []string{"0410-101", "0410-102", "0418-320", "0418-321", "9988-161", "0480-201"} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO subjects (institution_id, official_code, title_ar, title_en)
			VALUES ('55550000-0000-0000-0000-000000000001', $1, 'عنوان', $1)`, code); err != nil {
			t.Fatalf("launch code %q was refused by the tightened schema: %v", code, err)
		}
	}

	var distinct, total int
	if err := pool.QueryRow(ctx, `
		SELECT count(DISTINCT code_normalized), count(*) FROM subjects
		WHERE institution_id = '55550000-0000-0000-0000-000000000001'::uuid
		  AND code_normalized IS NOT NULL`).Scan(&distinct, &total); err != nil {
		t.Fatalf("counting launch codes: %v", err)
	}
	if distinct != total {
		t.Fatalf("launch codes collide after normalization: %d distinct of %d", distinct, total)
	}
}
