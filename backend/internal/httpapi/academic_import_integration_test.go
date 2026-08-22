//go:build integration

package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

// The Admin import path. Two things are proved here that the CLI cannot prove:
// that the authenticated Admin is preserved as the audited actor, and that no
// caller-supplied path or URL can make the server read data that was not
// reviewed and checked in.

func TestAcademicCatalogImportAdminAPI(t *testing.T) {
	env := setupAcademicAPIServer(t)
	ctx := context.Background()

	var institutionID string

	t.Run("Admin sees only known manifest identifiers", func(t *testing.T) {
		status, raw := env.call(t, http.MethodGet, "/api/v1/admin/academic/manifests", env.adminToken, nil)
		if status != http.StatusOK {
			t.Fatalf("listing manifests status = %d, want 200; body %s", status, raw)
		}
		var items []map[string]any
		if err := json.Unmarshal(raw, &items); err != nil {
			t.Fatalf("decoding manifests: %v", err)
		}
		if len(items) != 1 || items[0]["manifest"] != "kuwait-university-launch-v1" {
			t.Fatalf("manifests = %s; Kuwait University is the only launch institution", raw)
		}
		// The listing carries no filesystem path a caller could reuse.
		if bytes.Contains(raw, []byte("/internal/")) || bytes.Contains(raw, []byte(".yaml")) {
			t.Fatalf("the manifest listing leaked a filesystem path: %s", raw)
		}
	})

	t.Run("Instructor cannot dry-run or apply", func(t *testing.T) {
		for _, mode := range []string{"dry_run", "apply"} {
			status, _ := env.call(t, http.MethodPost,
				"/api/v1/admin/academic/institutions/00000000-0000-0000-0000-000000000000/import",
				env.instructorToken, map[string]any{"manifest": "kuwait-university-launch-v1", "mode": mode})
			if status != http.StatusForbidden {
				t.Fatalf("Instructor %s status = %d, want 403", mode, status)
			}
		}
		status, _ := env.call(t, http.MethodGet, "/api/v1/admin/academic/manifests", env.instructorToken, nil)
		if status != http.StatusForbidden {
			t.Fatalf("Instructor manifest listing status = %d, want 403", status)
		}
	})

	t.Run("Student and anonymous cannot import", func(t *testing.T) {
		status, _ := env.call(t, http.MethodPost,
			"/api/v1/admin/academic/institutions/00000000-0000-0000-0000-000000000000/import",
			env.studentToken, map[string]any{"manifest": "kuwait-university-launch-v1", "mode": "apply"})
		if status != http.StatusForbidden {
			t.Fatalf("Student import status = %d, want 403", status)
		}
		status, _ = env.call(t, http.MethodPost,
			"/api/v1/admin/academic/institutions/00000000-0000-0000-0000-000000000000/import",
			"", map[string]any{"manifest": "kuwait-university-launch-v1", "mode": "apply"})
		if status != http.StatusUnauthorized && status != http.StatusForbidden {
			t.Fatalf("anonymous import status = %d, want 401 or 403", status)
		}
	})

	t.Run("no filesystem path or URL is accepted as a manifest", func(t *testing.T) {
		for _, hostile := range []string{
			"../../../etc/passwd",
			"/etc/passwd",
			"https://example.test/manifest.yaml",
			"internal/academic/manifest/data/kuwait-university/manifest.yaml",
			"kuwait-university-launch-v1.yaml",
		} {
			status, raw := env.call(t, http.MethodPost,
				"/api/v1/admin/academic/institutions/00000000-0000-0000-0000-000000000000/import",
				env.adminToken, map[string]any{"manifest": hostile, "mode": "dry_run"})
			if status != http.StatusUnprocessableEntity {
				t.Fatalf("manifest %q status = %d, want 422; body %s", hostile, status, raw)
			}
		}
		// An identifier-shaped but unknown manifest is a 404, never a fallback.
		status, _ := env.call(t, http.MethodPost,
			"/api/v1/admin/academic/institutions/00000000-0000-0000-0000-000000000000/import",
			env.adminToken, map[string]any{"manifest": "aasu-launch-v1", "mode": "dry_run"})
		if status != http.StatusNotFound {
			t.Fatalf("unknown manifest status = %d, want 404", status)
		}
	})

	t.Run("Admin dry run reports a plan and writes nothing", func(t *testing.T) {
		status, raw := env.call(t, http.MethodPost,
			"/api/v1/admin/academic/institutions/00000000-0000-0000-0000-000000000000/import",
			env.adminToken, map[string]any{"manifest": "kuwait-university-launch-v1", "mode": "dry_run"})
		if status != http.StatusOK {
			t.Fatalf("dry run status = %d, want 200; body %s", status, raw)
		}
		var plan struct {
			Applied bool `json:"applied"`
			Counts  struct {
				Create int `json:"create"`
			} `json:"counts"`
		}
		if err := json.Unmarshal(raw, &plan); err != nil {
			t.Fatalf("decoding plan: %v", err)
		}
		if plan.Applied || plan.Counts.Create == 0 {
			t.Fatalf("dry run plan = %+v, want an unapplied plan with creates", plan)
		}
		var institutions int
		if err := env.pool.QueryRow(ctx, `SELECT count(*) FROM institutions`).Scan(&institutions); err != nil {
			t.Fatalf("counting institutions: %v", err)
		}
		if institutions != 0 {
			t.Fatalf("an Admin dry run wrote %d institutions", institutions)
		}
	})

	t.Run("Admin apply imports Kuwait University under the Admin's own identity", func(t *testing.T) {
		status, raw := env.call(t, http.MethodPost,
			"/api/v1/admin/academic/institutions/00000000-0000-0000-0000-000000000000/import",
			env.adminToken, map[string]any{"manifest": "kuwait-university-launch-v1", "mode": "apply"})
		if status != http.StatusOK {
			t.Fatalf("apply status = %d, want 200; body %s", status, raw)
		}
		if err := env.pool.QueryRow(ctx,
			`SELECT id::text FROM institutions WHERE slug = 'kuwait-university'`).Scan(&institutionID); err != nil {
			t.Fatalf("Kuwait University was not imported: %v", err)
		}

		// The HTTP path keeps the authenticated Admin: only the CLI uses SYSTEM.
		var adminAudits, systemAudits int
		if err := env.pool.QueryRow(ctx, `
			SELECT count(*) FILTER (WHERE actor_role = 'ADMIN' AND actor_account_id IS NOT NULL),
			       count(*) FILTER (WHERE actor_role = 'SYSTEM')
			FROM audit_events WHERE action LIKE 'ACADEMIC_%'`).Scan(&adminAudits, &systemAudits); err != nil {
			t.Fatalf("counting audits: %v", err)
		}
		if adminAudits == 0 {
			t.Fatal("the Admin import wrote no audit under the acting Admin")
		}
		if systemAudits != 0 {
			t.Fatalf("the Admin import wrote %d SYSTEM audits; the acting Admin must be preserved", systemAudits)
		}
	})

	t.Run("a repeated Admin apply changes nothing", func(t *testing.T) {
		before := map[string]int{}
		for _, table := range []string{"institutions", "academic_units", "programs", "curricula", "subjects", "curriculum_subjects"} {
			var n int
			if err := env.pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil {
				t.Fatalf("counting %s: %v", table, err)
			}
			before[table] = n
		}
		status, raw := env.call(t, http.MethodPost,
			"/api/v1/admin/academic/institutions/"+institutionID+"/import",
			env.adminToken, map[string]any{"manifest": "kuwait-university-launch-v1", "mode": "apply"})
		if status != http.StatusOK {
			t.Fatalf("repeated apply status = %d; body %s", status, raw)
		}
		var plan struct {
			Counts struct {
				Create int `json:"create"`
				Update int `json:"update"`
				Noop   int `json:"noop"`
			} `json:"counts"`
		}
		if err := json.Unmarshal(raw, &plan); err != nil {
			t.Fatalf("decoding plan: %v", err)
		}
		if plan.Counts.Create != 0 || plan.Counts.Update != 0 || plan.Counts.Noop == 0 {
			t.Fatalf("repeated apply counts = %+v, want all no-ops", plan.Counts)
		}
		for table, n := range before {
			var now int
			if err := env.pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&now); err != nil {
				t.Fatalf("re-counting %s: %v", table, err)
			}
			if now != n {
				t.Fatalf("%s changed from %d to %d on a repeated Admin apply", table, n, now)
			}
		}
	})

	t.Run("imported data is visible through the ordinary Admin catalog reads", func(t *testing.T) {
		status, raw := env.call(t, http.MethodGet,
			"/api/v1/admin/academic/institutions/"+institutionID+"/subjects?q=0410-101", env.adminToken, nil)
		if status != http.StatusOK {
			t.Fatalf("subject search status = %d; body %s", status, raw)
		}
		var subjects []map[string]any
		if err := json.Unmarshal(raw, &subjects); err != nil {
			t.Fatalf("decoding subjects: %v", err)
		}
		if len(subjects) != 1 || subjects[0]["official_code"] != "0410-101" {
			t.Fatalf("searching the imported catalog returned %s", raw)
		}
	})

	t.Run("a manifest cannot be imported into a different institution", func(t *testing.T) {
		other := env.mustCreate(t, "/api/v1/admin/academic/institutions", map[string]any{
			"country_code": "KW", "slug": "t2-other-university",
			"name_ar": "جامعة أخرى", "name_en": "Other University", "max_academic_level": 4,
		})
		status, raw := env.call(t, http.MethodPost,
			"/api/v1/admin/academic/institutions/"+idOf(t, other)+"/import",
			env.adminToken, map[string]any{"manifest": "kuwait-university-launch-v1", "mode": "dry_run"})
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("cross-institution import status = %d, want 422; body %s", status, raw)
		}
	})

	t.Run("the legacy Course taxonomy path is untouched by import", func(t *testing.T) {
		var terms int
		if err := env.pool.QueryRow(ctx, `SELECT count(*) FROM taxonomy_terms`).Scan(&terms); err != nil {
			t.Fatalf("counting legacy terms: %v", err)
		}
		if terms != 0 {
			t.Fatalf("the import created %d legacy taxonomy terms", terms)
		}
		// And no Course carries an academic-catalog reference. courses.subject_id
		// exists from T4-A (D-093 4) onward, so what matters here is that the
		// IMPORT never populates it: a catalog import writes catalog rows and
		// never reaches into a Course.
		var academicCourses int
		if err := env.pool.QueryRow(ctx, `
			SELECT count(*) FROM courses
			WHERE subject_id IS NOT NULL
			   OR institution_id IS NOT NULL
			   OR classification_model <> 'LEGACY_TAXONOMY'`).Scan(&academicCourses); err != nil {
			t.Fatalf("checking course academic references: %v", err)
		}
		if academicCourses != 0 {
			t.Fatalf("the import gave %d Courses an academic-catalog reference", academicCourses)
		}
	})
}
