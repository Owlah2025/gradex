//go:build integration

package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/academic"
	"github.com/Owlah2025/gradex/backend/internal/catalog"
	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/health"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/logging"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
	"github.com/Owlah2025/gradex/backend/internal/ratelimit"
)

// T1 (MVP-F17) proof. Every assertion drives the real HTTP surface against real
// PostgreSQL: the router, the capability middleware, the domain repository, and
// the schema constraints all participate. Nothing here re-implements SQL in Go
// and calls that a proof.

type academicTestEnv struct {
	server          *httptest.Server
	pool            *pgxpool.Pool
	adminToken      string
	instructorToken string
	studentToken    string
}

func setupAcademicAPIServer(t *testing.T) *academicTestEnv {
	t.Helper()
	freshSchema(t)
	p, ctx := pool(t)

	adminID := "20000000-0000-0000-0000-000000000001"
	instructorID := "20000000-0000-0000-0000-000000000002"
	studentID := "20000000-0000-0000-0000-000000000003"

	if _, err := p.Exec(ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name) VALUES
		($1, 'cat-admin@example.com', 'cat-admin@example.com', 'ADMIN', 'ACTIVE', 'Catalog Admin'),
		($2, 'cat-inst@example.com', 'cat-inst@example.com', 'INSTRUCTOR', 'ACTIVE', 'Catalog Instructor'),
		($3, 'cat-student@example.com', 'cat-student@example.com', 'STUDENT', 'ACTIVE', 'Catalog Student')
	`, adminID, instructorID, studentID); err != nil {
		t.Fatalf("seeding accounts: %v", err)
	}

	repo, err := academic.NewRepository(p)
	if err != nil {
		t.Fatalf("academic.NewRepository: %v", err)
	}
	foundation, err := NewAcademicFoundation(AcademicFoundationOptions{Repository: repo})
	if err != nil {
		t.Fatalf("NewAcademicFoundation: %v", err)
	}

	cfg, err := config.LoadFrom(config.MapLookup(map[string]string{
		"APP_ENV": "development", "REDIS_ADDR": "localhost:6379",
		"S3_ENDPOINT": "http://localhost:9000", "S3_BUCKET": "gradex-test",
	}), config.MapSecretResolver{
		"DATABASE_URL": authzTestDSN, "S3_ACCESS_KEY": "a",
		"S3_SECRET_KEY": "b", "PLAYBACK_TOKEN_SECRET": "c",
	})
	if err != nil {
		t.Fatalf("config: %v", err)
	}

	buf := &syncBuffer{}
	logger := logging.New(buf, "gradex-api-test", "development", logging.LevelFromString("info"))
	reporter := health.New(1 * time.Second)
	reporter.MarkStarted()

	now := time.Now().UTC()
	view := func(accountID string, role identity.Role) identity.SessionView {
		return identity.SessionView{Session: identity.AuthenticatedSession{
			AccountID: accountID, SessionID: accountID + "-session", Role: role,
			CredentialState: identity.CredentialActive, AuthenticatedAt: now,
			IdleExpiresAt: now.Add(24 * time.Hour), AbsoluteExpiresAt: now.Add(24 * time.Hour),
		}}
	}
	adminToken := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x51}, 32))
	instructorToken := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x52}, 32))
	studentToken := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x53}, 32))

	adminView := view(adminID, identity.RoleAdmin)
	instructorView := view(instructorID, identity.RoleInstructor)
	studentView := view(studentID, identity.RoleStudent)
	sessionRepo := &tokenSessionRepo{sessions: map[string]identity.SessionView{
		adminToken: adminView, identity.DigestToken(adminToken): adminView,
		instructorToken: instructorView, identity.DigestToken(instructorToken): instructorView,
		studentToken: studentView, identity.DigestToken(studentToken): studentView,
	}}

	limiter, _ := ratelimit.New(fakeRateStore{}, bytes.Repeat([]byte{0x31}, 32), time.Second)
	sessionFoundation, err := NewSessionFoundation(SessionFoundationOptions{
		PublicOrigin:        "https://gradex.example",
		CookieSigningKey:    bytes.Repeat([]byte{0x31}, 32),
		AnonymousCSRFKey:    bytes.Repeat([]byte{0x32}, 32),
		AnonymousSessionTTL: 24 * time.Hour,
		Repository:          sessionRepo,
		Compromised:         testCompromisedSource(t),
		Limiter:             limiter,
		EndpointPolicies:    testSessionEndpointPolicies(),
	})
	if err != nil {
		t.Fatalf("NewSessionFoundation: %v", err)
	}

	obWriter, err := outbox.NewWriter("key-v1", bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("outbox.NewWriter: %v", err)
	}
	catalogRepo, err := catalog.NewRepository(p, obWriter)
	if err != nil {
		t.Fatalf("catalog.NewRepository: %v", err)
	}
	catalogFoundation, err := NewCatalogFoundation(CatalogFoundationOptions{
		Repository:     catalogRepo,
		AssetValidator: catalog.NewDBAssetVersionValidator(p),
	})
	if err != nil {
		t.Fatalf("NewCatalogFoundation: %v", err)
	}

	r, err := NewRouter(cfg, logger, reporter, sessionFoundation.authenticator, dbPrincipalResolver{pool: p},
		WithSessionFoundation(sessionFoundation),
		WithAcademicFoundation(foundation),
		WithCatalogFoundation(catalogFoundation),
	)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	ts := httptest.NewTLSServer(r)
	t.Cleanup(ts.Close)

	return &academicTestEnv{
		server: ts, pool: p, adminToken: adminToken,
		instructorToken: instructorToken, studentToken: studentToken,
	}
}

func (e *academicTestEnv) call(t *testing.T, method, path, token string, body any) (int, []byte) {
	t.Helper()
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			t.Fatalf("marshaling request: %v", err)
		}
	}
	resp := doPricingRequest(t, e.server.Client(), method, e.server.URL+path,
		token, "https://gradex.example", token, payload)
	defer resp.Body.Close()
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatalf("reading response: %v", err)
	}
	return resp.StatusCode, buf.Bytes()
}

func (e *academicTestEnv) mustCreate(t *testing.T, path string, body any) map[string]any {
	t.Helper()
	status, raw := e.call(t, http.MethodPost, path, e.adminToken, body)
	if status != http.StatusCreated {
		t.Fatalf("POST %s status = %d, want 201; body %s", path, status, raw)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decoding %s response: %v", path, err)
	}
	return decoded
}

func idOf(t *testing.T, decoded map[string]any) string {
	t.Helper()
	id, ok := decoded["id"].(string)
	if !ok || id == "" {
		t.Fatalf("response carried no id: %v", decoded)
	}
	return id
}

func TestAcademicCatalogAdminAPIRealPostgreSQL(t *testing.T) {
	env := setupAcademicAPIServer(t)
	ctx := context.Background()

	var institutionID, collegeID, departmentID, programID, curriculumID, calculusID string

	t.Run("catalog starts empty", func(t *testing.T) {
		status, raw := env.call(t, http.MethodGet, "/api/v1/admin/academic/institutions", env.adminToken, nil)
		if status != http.StatusOK {
			t.Fatalf("empty catalog list status = %d, want 200; body %s", status, raw)
		}
		var items []map[string]any
		if err := json.Unmarshal(raw, &items); err != nil {
			t.Fatalf("decoding institutions: %v", err)
		}
		if len(items) != 0 {
			t.Fatalf("T1 shipped %d institutions; the launch catalog belongs to T2", len(items))
		}
	})

	t.Run("Admin creates an Institution", func(t *testing.T) {
		created := env.mustCreate(t, "/api/v1/admin/academic/institutions", map[string]any{
			"country_code": "KW", "slug": "t1-university",
			"name_ar": "جامعة الاختبار", "name_en": "T1 University",
			"max_academic_level": 5, "has_foundation_stage": true,
		})
		institutionID = idOf(t, created)
		// Five levels, not four: the launch institution's own regulation
		// derives standing from credits across five bands.
		if created["max_academic_level"].(float64) != 5 {
			t.Fatalf("max_academic_level = %v, want 5", created["max_academic_level"])
		}
	})

	t.Run("Instructor cannot mutate the Academic Catalog", func(t *testing.T) {
		status, _ := env.call(t, http.MethodPost, "/api/v1/admin/academic/institutions", env.instructorToken,
			map[string]any{"country_code": "KW", "slug": "instructor-made", "name_ar": "x", "name_en": "x", "max_academic_level": 4})
		if status != http.StatusForbidden {
			t.Fatalf("Instructor create status = %d, want 403", status)
		}
		status, _ = env.call(t, http.MethodGet, "/api/v1/admin/academic/institutions", env.instructorToken, nil)
		if status != http.StatusForbidden {
			t.Fatalf("Instructor read status = %d, want 403", status)
		}
	})

	t.Run("Student cannot mutate the Academic Catalog", func(t *testing.T) {
		status, _ := env.call(t, http.MethodPost, "/api/v1/admin/academic/institutions", env.studentToken,
			map[string]any{"country_code": "KW", "slug": "student-made", "name_ar": "x", "name_en": "x", "max_academic_level": 4})
		if status != http.StatusForbidden {
			t.Fatalf("Student create status = %d, want 403", status)
		}
	})

	t.Run("anonymous cannot mutate the Academic Catalog", func(t *testing.T) {
		status, _ := env.call(t, http.MethodPost, "/api/v1/admin/academic/institutions", "",
			map[string]any{"country_code": "KW", "slug": "anon-made", "name_ar": "x", "name_en": "x", "max_academic_level": 4})
		if status != http.StatusUnauthorized && status != http.StatusForbidden {
			t.Fatalf("anonymous create status = %d, want 401 or 403", status)
		}
	})

	t.Run("Admin creates a nested academic unit hierarchy", func(t *testing.T) {
		college := env.mustCreate(t, "/api/v1/admin/academic/institutions/"+institutionID+"/units", map[string]any{
			"kind": "COLLEGE", "slug": "engineering-petroleum",
			"name_ar": "كلية الهندسة والبترول", "name_en": "College of Engineering and Petroleum",
		})
		collegeID = idOf(t, college)
		department := env.mustCreate(t, "/api/v1/admin/academic/institutions/"+institutionID+"/units", map[string]any{
			"kind": "DEPARTMENT", "slug": "computer-engineering", "parent_unit_id": collegeID,
			"name_ar": "قسم هندسة الحاسوب", "name_en": "Computer Engineering Department",
		})
		departmentID = idOf(t, department)
		if department["parent_unit_id"] != collegeID {
			t.Fatalf("department parent = %v, want %s", department["parent_unit_id"], collegeID)
		}
		// A unit with no parent must remain legal: AASU has no department layer
		// and AUM has departments outside any college.
		env.mustCreate(t, "/api/v1/admin/academic/institutions/"+institutionID+"/units", map[string]any{
			"kind": "SERVICE_UNIT", "slug": "integrative-studies",
			"name_ar": "كلية الدراسات التكاملية", "name_en": "College of Integrative Studies",
		})
	})

	t.Run("Admin renames and re-parents an academic unit", func(t *testing.T) {
		status, raw := env.call(t, http.MethodPatch, "/api/v1/admin/academic/units/"+departmentID,
			env.adminToken, map[string]any{"name_en": "Computer Engineering Dept."})
		if status != http.StatusOK {
			t.Fatalf("rename unit status = %d, want 200; body %s", status, raw)
		}
		var renamed map[string]any
		if err := json.Unmarshal(raw, &renamed); err != nil {
			t.Fatalf("decoding renamed unit: %v", err)
		}
		if renamed["name_en"] != "Computer Engineering Dept." {
			t.Fatalf("rename did not persist: %v", renamed["name_en"])
		}
		// Renaming must not silently move the unit.
		if renamed["parent_unit_id"] != collegeID {
			t.Fatalf("rename changed the parent to %v", renamed["parent_unit_id"])
		}
		// Detaching to the institution root and re-attaching must both work.
		status, _ = env.call(t, http.MethodPatch, "/api/v1/admin/academic/units/"+departmentID,
			env.adminToken, map[string]any{"reparent_to": ""})
		if status != http.StatusOK {
			t.Fatalf("detach unit status = %d, want 200", status)
		}
		status, _ = env.call(t, http.MethodPatch, "/api/v1/admin/academic/units/"+departmentID,
			env.adminToken, map[string]any{"reparent_to": collegeID})
		if status != http.StatusOK {
			t.Fatalf("re-attach unit status = %d, want 200", status)
		}
	})

	t.Run("cross-institution parent is refused", func(t *testing.T) {
		other := env.mustCreate(t, "/api/v1/admin/academic/institutions", map[string]any{
			"country_code": "KW", "slug": "t1-other-university",
			"name_ar": "جامعة أخرى", "name_en": "Other University", "max_academic_level": 4,
		})
		otherID := idOf(t, other)
		status, raw := env.call(t, http.MethodPost,
			"/api/v1/admin/academic/institutions/"+otherID+"/units", env.adminToken, map[string]any{
				"kind": "DEPARTMENT", "slug": "borrowed", "parent_unit_id": collegeID,
				"name_ar": "مستعار", "name_en": "Borrowed",
			})
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("cross-institution parent status = %d, want 422; body %s", status, raw)
		}
		if !bytes.Contains(raw, []byte("CROSS_INSTITUTION_RELATIONSHIP")) {
			t.Fatalf("cross-institution rejection did not name the violation: %s", raw)
		}
	})

	t.Run("self-parent is refused", func(t *testing.T) {
		status, _ := env.call(t, http.MethodPatch, "/api/v1/admin/academic/units/"+collegeID,
			env.adminToken, map[string]any{"reparent_to": collegeID})
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("self-parent status = %d, want 422", status)
		}
	})

	t.Run("multi-node cycle is refused", func(t *testing.T) {
		// college -> department already exists; re-parenting college under
		// department would close a two-node cycle.
		status, raw := env.call(t, http.MethodPatch, "/api/v1/admin/academic/units/"+collegeID,
			env.adminToken, map[string]any{"reparent_to": departmentID})
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("cycle status = %d, want 422; body %s", status, raw)
		}
		if !bytes.Contains(raw, []byte("ACADEMIC_UNIT_CYCLE")) {
			t.Fatalf("cycle rejection did not name the violation: %s", raw)
		}
		// A three-node cycle must be refused too, not only the trivial pair.
		deeper := env.mustCreate(t, "/api/v1/admin/academic/institutions/"+institutionID+"/units", map[string]any{
			"kind": "DEPARTMENT", "slug": "deeper-unit", "parent_unit_id": departmentID,
			"name_ar": "أعمق", "name_en": "Deeper",
		})
		status, _ = env.call(t, http.MethodPatch, "/api/v1/admin/academic/units/"+collegeID,
			env.adminToken, map[string]any{"reparent_to": idOf(t, deeper)})
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("three-node cycle status = %d, want 422", status)
		}
	})

	t.Run("Program belongs to its Institution and owning unit", func(t *testing.T) {
		program := env.mustCreate(t, "/api/v1/admin/academic/institutions/"+institutionID+"/programs", map[string]any{
			"slug": "computer-engineering", "owning_unit_id": departmentID,
			"name_ar": "هندسة الحاسوب", "name_en": "Computer Engineering", "degree_kind": "BSC",
		})
		programID = idOf(t, program)
		if program["institution_id"] != institutionID || program["owning_unit_id"] != departmentID {
			t.Fatalf("program ownership = %v/%v", program["institution_id"], program["owning_unit_id"])
		}
		// Department is not Major: the same department owns a second Program.
		env.mustCreate(t, "/api/v1/admin/academic/institutions/"+institutionID+"/programs", map[string]any{
			"slug": "software-engineering", "owning_unit_id": departmentID,
			"name_ar": "هندسة البرمجيات", "name_en": "Software Engineering", "degree_kind": "BSC",
		})
	})

	t.Run("cross-institution Program owning unit is refused", func(t *testing.T) {
		other := env.mustCreate(t, "/api/v1/admin/academic/institutions", map[string]any{
			"country_code": "KW", "slug": "t1-third-university",
			"name_ar": "جامعة ثالثة", "name_en": "Third University", "max_academic_level": 4,
		})
		status, raw := env.call(t, http.MethodPost,
			"/api/v1/admin/academic/institutions/"+idOf(t, other)+"/programs", env.adminToken, map[string]any{
				"slug": "borrowed-program", "owning_unit_id": departmentID,
				"name_ar": "مستعار", "name_en": "Borrowed", "degree_kind": "BSC",
			})
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("cross-institution program status = %d, want 422; body %s", status, raw)
		}
	})

	t.Run("Curriculum belongs to its Program and only one is ACTIVE", func(t *testing.T) {
		curriculum := env.mustCreate(t, "/api/v1/admin/academic/programs/"+programID+"/curricula", map[string]any{
			"version_label": "2026", "effective_from_year": 2026,
		})
		curriculumID = idOf(t, curriculum)
		if curriculum["program_id"] != programID || curriculum["status"] != "ACTIVE" {
			t.Fatalf("curriculum = %v", curriculum)
		}
		status, _ := env.call(t, http.MethodPost, "/api/v1/admin/academic/programs/"+programID+"/curricula",
			env.adminToken, map[string]any{"version_label": "2027"})
		if status != http.StatusConflict {
			t.Fatalf("second ACTIVE curriculum status = %d, want 409", status)
		}
		// Explicit supersession is the only way to replace the active plan, and
		// the superseded version is retained rather than overwritten.
		superseding := env.mustCreate(t, "/api/v1/admin/academic/programs/"+programID+"/curricula", map[string]any{
			"version_label": "2027", "supersede_active": true,
		})
		var previousStatus string
		if err := env.pool.QueryRow(ctx,
			`SELECT status::text FROM curricula WHERE id = $1::uuid`, curriculumID).Scan(&previousStatus); err != nil {
			t.Fatalf("re-reading superseded curriculum: %v", err)
		}
		if previousStatus != "SUPERSEDED" {
			t.Fatalf("previous curriculum status = %s, want SUPERSEDED", previousStatus)
		}
		curriculumID = idOf(t, superseding)
	})

	t.Run("Subject with an official code is created and deduplicated", func(t *testing.T) {
		subject := env.mustCreate(t, "/api/v1/admin/academic/institutions/"+institutionID+"/subjects", map[string]any{
			"official_code": "0410-101", "title_ar": "حساب التفاضل والتكامل ١", "title_en": "Calculus I",
			"owning_unit_id": collegeID,
		})
		calculusID = idOf(t, subject)

		// Punctuation and spacing variants are the same canonical code.
		for _, variant := range []string{"0410-101", "0410101", "0410 101", "  0410--101 "} {
			status, raw := env.call(t, http.MethodPost,
				"/api/v1/admin/academic/institutions/"+institutionID+"/subjects", env.adminToken, map[string]any{
					"official_code": variant, "title_ar": "عنوان آخر", "title_en": "Another Title",
				})
			if status != http.StatusConflict {
				t.Fatalf("duplicate code %q status = %d, want 409; body %s", variant, status, raw)
			}
			// The conflict must be actionable: it names the existing Subject.
			var problem map[string]any
			if err := json.Unmarshal(raw, &problem); err != nil {
				t.Fatalf("decoding conflict: %v", err)
			}
			if problem["code"] != "SUBJECT_ALREADY_EXISTS" {
				t.Fatalf("conflict code = %v, want SUBJECT_ALREADY_EXISTS", problem["code"])
			}
			existing, ok := problem["existing_subject"].(map[string]any)
			if !ok || existing["id"] != calculusID {
				t.Fatalf("conflict did not name the existing Subject: %s", raw)
			}
		}
	})

	t.Run("the same code in another Institution is a different Subject", func(t *testing.T) {
		other := env.mustCreate(t, "/api/v1/admin/academic/institutions", map[string]any{
			"country_code": "KW", "slug": "t1-fourth-university",
			"name_ar": "جامعة رابعة", "name_en": "Fourth University", "max_academic_level": 4,
		})
		env.mustCreate(t, "/api/v1/admin/academic/institutions/"+idOf(t, other)+"/subjects", map[string]any{
			"official_code": "0410-101", "title_ar": "حساب ١", "title_en": "Calculus I",
		})
	})

	t.Run("code-less Subjects deduplicate per normalized title", func(t *testing.T) {
		env.mustCreate(t, "/api/v1/admin/academic/institutions/"+institutionID+"/subjects", map[string]any{
			"title_ar": "مادة بلا رمز", "title_en": "Codeless Subject",
		})
		status, _ := env.call(t, http.MethodPost,
			"/api/v1/admin/academic/institutions/"+institutionID+"/subjects", env.adminToken, map[string]any{
				"title_ar": "مادة بلا رمز", "title_en": "Completely Different",
			})
		if status != http.StatusConflict {
			t.Fatalf("duplicate Arabic code-less title status = %d, want 409", status)
		}
		status, _ = env.call(t, http.MethodPost,
			"/api/v1/admin/academic/institutions/"+institutionID+"/subjects", env.adminToken, map[string]any{
				"title_ar": "عنوان مختلف تماما", "title_en": "Codeless Subject",
			})
		if status != http.StatusConflict {
			t.Fatalf("duplicate English code-less title status = %d, want 409", status)
		}
	})

	t.Run("concurrent duplicate Subject creation cannot win twice", func(t *testing.T) {
		const attempts = 8
		results := make(chan int, attempts)
		for i := 0; i < attempts; i++ {
			go func() {
				status, _ := env.call(t, http.MethodPost,
					"/api/v1/admin/academic/institutions/"+institutionID+"/subjects", env.adminToken, map[string]any{
						"official_code": "0430-101", "title_ar": "فيزياء عامة ١", "title_en": "General Physics I",
					})
				results <- status
			}()
		}
		created := 0
		for i := 0; i < attempts; i++ {
			if <-results == http.StatusCreated {
				created++
			}
		}
		if created != 1 {
			t.Fatalf("concurrent duplicate creation produced %d subjects, want exactly 1", created)
		}
		var rows int
		if err := env.pool.QueryRow(ctx, `
			SELECT count(*) FROM subjects
			WHERE institution_id = $1::uuid AND code_normalized = '0430101'`,
			institutionID).Scan(&rows); err != nil {
			t.Fatalf("counting concurrent subjects: %v", err)
		}
		if rows != 1 {
			t.Fatalf("database holds %d rows for the same canonical code", rows)
		}
	})

	t.Run("code normalization drives search", func(t *testing.T) {
		for _, query := range []string{"0410-101", "0410101", "calculus", "حساب"} {
			status, raw := env.call(t, http.MethodGet,
				"/api/v1/admin/academic/institutions/"+institutionID+"/subjects?q="+url.QueryEscape(query),
				env.adminToken, nil)
			if status != http.StatusOK {
				t.Fatalf("subject search %q status = %d", query, status)
			}
			var items []map[string]any
			if err := json.Unmarshal(raw, &items); err != nil {
				t.Fatalf("decoding subject search: %v", err)
			}
			found := false
			for _, item := range items {
				if item["id"] == calculusID {
					found = true
				}
			}
			if !found {
				t.Fatalf("search %q did not find the canonical Subject; got %s", query, raw)
			}
		}
	})

	t.Run("same-Institution Curriculum mapping succeeds", func(t *testing.T) {
		mapped := env.mustCreate(t, "/api/v1/admin/academic/curricula/"+curriculumID+"/subjects", map[string]any{
			"subject_id": calculusID, "requirement_kind": "MAJOR_CORE",
			"recommended_level": 1, "recommended_semester": 1, "credits": 3,
		})
		if mapped["subject_id"] != calculusID {
			t.Fatalf("mapping subject = %v", mapped["subject_id"])
		}
		status, _ := env.call(t, http.MethodPost, "/api/v1/admin/academic/curricula/"+curriculumID+"/subjects",
			env.adminToken, map[string]any{"subject_id": calculusID, "requirement_kind": "MAJOR_ELECTIVE"})
		if status != http.StatusConflict {
			t.Fatalf("duplicate mapping status = %d, want 409", status)
		}
	})

	t.Run("one canonical Subject serves several Programs", func(t *testing.T) {
		electrical := env.mustCreate(t, "/api/v1/admin/academic/institutions/"+institutionID+"/programs", map[string]any{
			"slug": "electrical-engineering", "owning_unit_id": collegeID,
			"name_ar": "هندسة كهربائية", "name_en": "Electrical Engineering", "degree_kind": "BSC",
		})
		electricalCurriculum := env.mustCreate(t,
			"/api/v1/admin/academic/programs/"+idOf(t, electrical)+"/curricula",
			map[string]any{"version_label": "2026"})
		env.mustCreate(t, "/api/v1/admin/academic/curricula/"+idOf(t, electricalCurriculum)+"/subjects",
			map[string]any{"subject_id": calculusID, "requirement_kind": "MAJOR_CORE", "recommended_level": 1})

		var subjectRows int
		if err := env.pool.QueryRow(ctx, `
			SELECT count(*) FROM subjects WHERE institution_id = $1::uuid AND code_normalized = '0410101'`,
			institutionID).Scan(&subjectRows); err != nil {
			t.Fatalf("counting Calculus rows: %v", err)
		}
		if subjectRows != 1 {
			t.Fatalf("Calculus I exists %d times; it must be one canonical Subject", subjectRows)
		}
		var mappings int
		if err := env.pool.QueryRow(ctx,
			`SELECT count(*) FROM curriculum_subjects WHERE subject_id = $1::uuid`, calculusID).Scan(&mappings); err != nil {
			t.Fatalf("counting Calculus mappings: %v", err)
		}
		if mappings != 2 {
			t.Fatalf("Calculus mappings = %d, want 2 across two Programs", mappings)
		}
	})

	t.Run("cross-Institution Curriculum mapping is refused", func(t *testing.T) {
		foreign := env.mustCreate(t, "/api/v1/admin/academic/institutions", map[string]any{
			"country_code": "KW", "slug": "t1-fifth-university",
			"name_ar": "جامعة خامسة", "name_en": "Fifth University", "max_academic_level": 4,
		})
		foreignSubject := env.mustCreate(t,
			"/api/v1/admin/academic/institutions/"+idOf(t, foreign)+"/subjects",
			map[string]any{"official_code": "XX-1", "title_ar": "أجنبي", "title_en": "Foreign"})
		status, raw := env.call(t, http.MethodPost,
			"/api/v1/admin/academic/curricula/"+curriculumID+"/subjects", env.adminToken,
			map[string]any{"subject_id": idOf(t, foreignSubject), "requirement_kind": "MAJOR_CORE"})
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("cross-institution mapping status = %d, want 422; body %s", status, raw)
		}
		if !bytes.Contains(raw, []byte("CROSS_INSTITUTION_RELATIONSHIP")) {
			t.Fatalf("cross-institution mapping rejection did not name the violation: %s", raw)
		}
	})

	t.Run("recommended level respects the Institution bound", func(t *testing.T) {
		physics := env.mustCreate(t, "/api/v1/admin/academic/institutions/"+institutionID+"/subjects",
			map[string]any{"official_code": "0430-102", "title_ar": "فيزياء ٢", "title_en": "General Physics II"})
		// This Institution declares five levels, so level 5 is valid and level 9
		// is not. Neither bound is hardcoded anywhere.
		env.mustCreate(t, "/api/v1/admin/academic/curricula/"+curriculumID+"/subjects", map[string]any{
			"subject_id": idOf(t, physics), "requirement_kind": "MAJOR_CORE", "recommended_level": 5,
		})
		chemistry := env.mustCreate(t, "/api/v1/admin/academic/institutions/"+institutionID+"/subjects",
			map[string]any{"official_code": "0420-101", "title_ar": "كيمياء ١", "title_en": "General Chemistry I"})
		status, raw := env.call(t, http.MethodPost,
			"/api/v1/admin/academic/curricula/"+curriculumID+"/subjects", env.adminToken, map[string]any{
				"subject_id": idOf(t, chemistry), "requirement_kind": "MAJOR_CORE", "recommended_level": 9,
			})
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("out-of-range level status = %d, want 422; body %s", status, raw)
		}
		if !bytes.Contains(raw, []byte("RECOMMENDED_LEVEL_OUT_OF_RANGE")) {
			t.Fatalf("level rejection did not name the violation: %s", raw)
		}
	})

	t.Run("retirement leaves history intact and blocks new selection", func(t *testing.T) {
		spare := env.mustCreate(t, "/api/v1/admin/academic/institutions/"+institutionID+"/subjects",
			map[string]any{"official_code": "0999-001", "title_ar": "مادة مؤقتة", "title_en": "Temporary Subject"})
		spareID := idOf(t, spare)
		env.mustCreate(t, "/api/v1/admin/academic/curricula/"+curriculumID+"/subjects",
			map[string]any{"subject_id": spareID, "requirement_kind": "FREE_ELECTIVE"})

		status, _ := env.call(t, http.MethodPost, "/api/v1/admin/academic/subjects/"+spareID+"/retire", env.adminToken, nil)
		if status != http.StatusOK {
			t.Fatalf("retire subject status = %d, want 200", status)
		}

		// The row and its mapping survive: academic history is never destroyed.
		var retiredAt *time.Time
		if err := env.pool.QueryRow(ctx,
			`SELECT retired_at FROM subjects WHERE id = $1::uuid`, spareID).Scan(&retiredAt); err != nil {
			t.Fatalf("re-reading retired subject: %v", err)
		}
		if retiredAt == nil {
			t.Fatal("retire did not stamp retired_at")
		}
		var mappingSurvives int
		if err := env.pool.QueryRow(ctx,
			`SELECT count(*) FROM curriculum_subjects WHERE subject_id = $1::uuid`, spareID).Scan(&mappingSurvives); err != nil {
			t.Fatalf("counting retired subject mappings: %v", err)
		}
		if mappingSurvives != 1 {
			t.Fatalf("retirement destroyed %d curriculum mappings", 1-mappingSurvives)
		}

		// It leaves the default active listing.
		status, raw := env.call(t, http.MethodGet,
			"/api/v1/admin/academic/institutions/"+institutionID+"/subjects", env.adminToken, nil)
		if status != http.StatusOK {
			t.Fatalf("listing subjects status = %d", status)
		}
		if bytes.Contains(raw, []byte(spareID)) {
			t.Fatalf("retired Subject still appears in the active listing")
		}
		// And it cannot be newly mapped.
		newCurriculum := env.mustCreate(t, "/api/v1/admin/academic/programs/"+programID+"/curricula",
			map[string]any{"version_label": "2028", "supersede_active": true})
		status, _ = env.call(t, http.MethodPost,
			"/api/v1/admin/academic/curricula/"+idOf(t, newCurriculum)+"/subjects", env.adminToken,
			map[string]any{"subject_id": spareID, "requirement_kind": "FREE_ELECTIVE"})
		if status != http.StatusConflict {
			t.Fatalf("mapping a retired Subject status = %d, want 409", status)
		}
	})

	t.Run("a referenced academic unit cannot be retired", func(t *testing.T) {
		status, _ := env.call(t, http.MethodPost,
			"/api/v1/admin/academic/units/"+departmentID+"/retire", env.adminToken, nil)
		if status != http.StatusConflict {
			t.Fatalf("retiring a referenced unit status = %d, want 409", status)
		}
	})

	t.Run("every Admin mutation is audited", func(t *testing.T) {
		for _, action := range []string{
			"ACADEMIC_INSTITUTION_CREATED",
			"ACADEMIC_UNIT_CREATED",
			"ACADEMIC_UNIT_UPDATED",
			"ACADEMIC_PROGRAM_CREATED",
			"ACADEMIC_CURRICULUM_CREATED",
			"ACADEMIC_SUBJECT_CREATED",
			"ACADEMIC_SUBJECT_RETIRED",
			"ACADEMIC_CURRICULUM_SUBJECT_MAPPED",
		} {
			var count int
			if err := env.pool.QueryRow(ctx, `
				SELECT count(*) FROM audit_events
				WHERE action = $1 AND module = 'CATALOG_AND_AUTHORING' AND actor_role = 'ADMIN'`,
				action).Scan(&count); err != nil {
				t.Fatalf("counting %s audits: %v", action, err)
			}
			if count == 0 {
				t.Fatalf("no audit event was written for %s", action)
			}
		}
		// A refused mutation must leave no audit behind.
		var forbidden int
		if err := env.pool.QueryRow(ctx, `
			SELECT count(*) FROM audit_events WHERE actor_role <> 'ADMIN' AND action LIKE 'ACADEMIC_%'`).
			Scan(&forbidden); err != nil {
			t.Fatalf("counting non-admin academic audits: %v", err)
		}
		if forbidden != 0 {
			t.Fatalf("%d academic audit events were written by a non-Admin actor", forbidden)
		}
	})

	t.Run("unmapping a Subject leaves the canonical Subject intact", func(t *testing.T) {
		status, _ := env.call(t, http.MethodDelete,
			"/api/v1/admin/academic/curricula/"+curriculumID+"/subjects/"+calculusID, env.adminToken, nil)
		if status != http.StatusNoContent {
			t.Fatalf("unmap status = %d, want 204", status)
		}
		var subjectSurvives int
		if err := env.pool.QueryRow(ctx,
			`SELECT count(*) FROM subjects WHERE id = $1::uuid`, calculusID).Scan(&subjectSurvives); err != nil {
			t.Fatalf("re-reading Subject after unmap: %v", err)
		}
		if subjectSurvives != 1 {
			t.Fatal("unmapping destroyed the canonical Subject")
		}
	})
}

// TestAcademicCatalogDoesNotTouchLegacyTaxonomyOrCourses proves the T1 boundary:
// the new catalog is additive and nothing about Course classification changed.
func TestAcademicCatalogDoesNotTouchLegacyTaxonomyOrCourses(t *testing.T) {
	env := setupAcademicAPIServer(t)
	ctx := context.Background()

	var termID string
	if err := env.pool.QueryRow(ctx, `
		INSERT INTO taxonomy_terms (kind, label_ar, label_en, academic_code)
		VALUES ('SUBJECT', 'تفاضل', 'Calculus', 'LEGACY-9') RETURNING id::text`).Scan(&termID); err != nil {
		t.Fatalf("seeding legacy term: %v", err)
	}

	institution := env.mustCreate(t, "/api/v1/admin/academic/institutions", map[string]any{
		"country_code": "KW", "slug": "boundary-university",
		"name_ar": "جامعة الحدود", "name_en": "Boundary University", "max_academic_level": 5,
	})
	env.mustCreate(t, "/api/v1/admin/academic/institutions/"+idOf(t, institution)+"/subjects",
		map[string]any{"official_code": "0410-101", "title_ar": "حساب ١", "title_en": "Calculus I"})

	// The legacy vocabulary is unchanged and still usable.
	var legacyCount int
	if err := env.pool.QueryRow(ctx, `SELECT count(*) FROM taxonomy_terms WHERE id = $1::uuid`, termID).
		Scan(&legacyCount); err != nil || legacyCount != 1 {
		t.Fatalf("legacy taxonomy term count = %d (err=%v), want 1", legacyCount, err)
	}
	if _, err := env.pool.Exec(ctx, `
		INSERT INTO taxonomy_terms (kind, label_ar, label_en) VALUES ('MAJOR', 'هندسة', 'Engineering')`); err != nil {
		t.Fatalf("the legacy taxonomy write path broke under T1: %v", err)
	}

	// No academic table references a Course, and no Course column references
	// the academic catalog.
	var academicCourseLinks int
	if err := env.pool.QueryRow(ctx, `
		SELECT count(*) FROM information_schema.table_constraints tc
		JOIN information_schema.constraint_column_usage ccu ON ccu.constraint_name = tc.constraint_name
		WHERE tc.constraint_type = 'FOREIGN KEY'
		  AND tc.table_name IN ('institutions','academic_units','programs','curricula','subjects','curriculum_subjects')
		  AND ccu.table_name IN ('courses','course_revisions','entitlements','enrollments')`).
		Scan(&academicCourseLinks); err != nil {
		t.Fatalf("inspecting academic foreign keys: %v", err)
	}
	if academicCourseLinks != 0 {
		t.Fatalf("the Academic Catalog holds %d foreign keys into Course/entitlement tables in T1", academicCourseLinks)
	}
}
