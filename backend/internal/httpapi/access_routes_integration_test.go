//go:build integration

package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/access"
	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/health"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/logging"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
	"github.com/Owlah2025/gradex/backend/internal/ratelimit"
)

func setupAdminAccessAPIServer(t *testing.T) (*httptest.Server, *pgxpool.Pool, string, string, string, string, string) {
	t.Helper()
	freshSchema(t)
	p, ctx := pool(t)

	adminID := "10000000-0000-0000-0000-000000000001"
	instID := "10000000-0000-0000-0000-000000000002"
	studentID := "10000000-0000-0000-0000-000000000003"
	otherStudentID := "10000000-0000-0000-0000-000000000004"

	_, err := p.Exec(ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name) VALUES
		($1, 'admin-access@example.com', 'admin-access@example.com', 'ADMIN', 'ACTIVE', 'Admin Access User'),
		($2, 'instructor-access@example.com', 'instructor-access@example.com', 'INSTRUCTOR', 'ACTIVE', 'Instructor Access User'),
		($3, 'student-access@example.com', 'student-access@example.com', 'STUDENT', 'ACTIVE', 'Student Access User'),
		($4, 'other-student@example.com', 'other-student@example.com', 'STUDENT', 'ACTIVE', 'Other Student User')
	`, adminID, instID, studentID, otherStudentID)
	if err != nil {
		t.Fatalf("seeding accounts: %v", err)
	}

	courseID := "20000000-0000-0000-0000-000000000001"
	_, err = p.Exec(ctx, `
		INSERT INTO courses (id, owner_account_id, lifecycle) VALUES ($1, $2, 'DRAFT')
	`, courseID, instID)
	if err != nil {
		t.Fatalf("seeding course: %v", err)
	}

	outboxWriter, err := outbox.NewWriter("key-v1", bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("outbox.NewWriter: %v", err)
	}

	accessRepo, err := access.NewRepository(p, outboxWriter)
	if err != nil {
		t.Fatalf("access.NewRepository: %v", err)
	}

	accessFoundation, err := NewAccessFoundation(AccessFoundationOptions{
		Repository: accessRepo,
	})
	if err != nil {
		t.Fatalf("NewAccessFoundation: %v", err)
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
	logger := logging.New(buf, "gradex-access-test", "development", logging.LevelFromString("info"))
	reporter := health.New(1 * time.Second)
	reporter.MarkStarted()

	adminToken := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x51}, 32))
	instToken := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x52}, 32))
	studentToken := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x53}, 32))
	otherStudentToken := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x54}, 32))
	now := time.Now().UTC()

	adminView := identity.SessionView{
		Session: identity.AuthenticatedSession{
			AccountID:         adminID,
			SessionID:         "admin-access-session-id",
			Role:              identity.RoleAdmin,
			CredentialState:   identity.CredentialActive,
			AuthenticatedAt:   now,
			IdleExpiresAt:     now.Add(24 * time.Hour),
			AbsoluteExpiresAt: now.Add(24 * time.Hour),
		},
	}
	instView := identity.SessionView{
		Session: identity.AuthenticatedSession{
			AccountID:         instID,
			SessionID:         "inst-access-session-id",
			Role:              identity.RoleInstructor,
			CredentialState:   identity.CredentialActive,
			AuthenticatedAt:   now,
			IdleExpiresAt:     now.Add(24 * time.Hour),
			AbsoluteExpiresAt: now.Add(24 * time.Hour),
		},
	}
	studentView := identity.SessionView{
		Session: identity.AuthenticatedSession{
			AccountID:         studentID,
			SessionID:         "student-access-session-id",
			Role:              identity.RoleStudent,
			CredentialState:   identity.CredentialActive,
			AuthenticatedAt:   now,
			IdleExpiresAt:     now.Add(24 * time.Hour),
			AbsoluteExpiresAt: now.Add(24 * time.Hour),
		},
	}
	otherStudentView := identity.SessionView{
		Session: identity.AuthenticatedSession{
			AccountID:         otherStudentID,
			SessionID:         "other-student-session-id",
			Role:              identity.RoleStudent,
			CredentialState:   identity.CredentialActive,
			AuthenticatedAt:   now,
			IdleExpiresAt:     now.Add(24 * time.Hour),
			AbsoluteExpiresAt: now.Add(24 * time.Hour),
		},
	}

	sessionRepo := &tokenSessionRepo{
		sessions: map[string]identity.SessionView{
			adminToken:                              adminView,
			identity.DigestToken(adminToken):        adminView,
			instToken:                               instView,
			identity.DigestToken(instToken):         instView,
			studentToken:                            studentView,
			identity.DigestToken(studentToken):      studentView,
			otherStudentToken:                       otherStudentView,
			identity.DigestToken(otherStudentToken): otherStudentView,
		},
	}

	limiter, _ := ratelimit.New(fakeRateStore{}, bytes.Repeat([]byte{0x31}, 32), time.Second)
	sessionPolicies := map[string]ratelimit.Policy{
		"session-bootstrap":  ratelimit.DevelopmentAnonymousBootstrapPolicy(),
		"sessions":           ratelimit.DevelopmentLoginPolicy(),
		"session-resolution": ratelimit.DevelopmentSessionPolicy("session-resolution"),
		"session-renewals":   ratelimit.DevelopmentSessionPolicy("session-renewals"),
		"session-logout":     ratelimit.DevelopmentSessionPolicy("session-logout"),
	}

	sessionFoundation, err := NewSessionFoundation(SessionFoundationOptions{
		PublicOrigin:        "https://gradex.example",
		CookieSigningKey:    bytes.Repeat([]byte{0x31}, 32),
		AnonymousCSRFKey:    bytes.Repeat([]byte{0x32}, 32),
		AnonymousSessionTTL: 24 * time.Hour,
		Repository:          sessionRepo,
		Limiter:             limiter,
		EndpointPolicies:    sessionPolicies,
	})
	if err != nil {
		t.Fatalf("NewSessionFoundation: %v", err)
	}

	principals := dbPrincipalResolver{pool: p}

	r, err := NewRouter(cfg, logger, reporter, sessionFoundation.authenticator, principals,
		WithSessionFoundation(sessionFoundation),
		WithAccessFoundation(accessFoundation),
	)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	ts := httptest.NewTLSServer(r)
	t.Cleanup(ts.Close)

	return ts, p, adminID, studentID, courseID, adminToken, studentToken
}

func TestAdminAccessExpiryHTTPAPI_RealPostgreSQL(t *testing.T) {
	ts, pool, adminID, _, courseID, adminToken, _ := setupAdminAccessAPIServer(t)
	ctx := context.Background()

	client := ts.Client()
	validOrigin := "https://gradex.example"
	expiryURL := ts.URL + "/api/v1/admin/courses/" + courseID + "/default-access-expiry"

	t.Run("Admin PUT default access expiry succeeds with Kuwait date conversion and audit record", func(t *testing.T) {
		body := []byte(`{"date": "2026-12-31", "reason": "Semester end 2026"}`)
		resp := doPricingRequest(t, client, "PUT", expiryURL, adminToken, validOrigin, adminToken, body)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("PUT default access expiry status = %d, want 200", resp.StatusCode)
		}

		var res struct {
			CourseID            string    `json:"course_id"`
			DefaultAccessEndsAt time.Time `json:"default_access_ends_at"`
			Reason              string    `json:"reason"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
			t.Fatalf("decoding response: %v", err)
		}

		wantUTC := "2026-12-31T21:00:00Z"
		if res.DefaultAccessEndsAt.UTC().Format(time.RFC3339) != wantUTC {
			t.Errorf("got DefaultAccessEndsAt = %s, want %s", res.DefaultAccessEndsAt.UTC().Format(time.RFC3339), wantUTC)
		}

		var dbEndsAt time.Time
		err := pool.QueryRow(ctx, `SELECT default_access_ends_at FROM courses WHERE id = $1::uuid`, courseID).Scan(&dbEndsAt)
		if err != nil {
			t.Fatalf("reading default_access_ends_at from DB: %v", err)
		}
		if dbEndsAt.UTC().Format(time.RFC3339) != wantUTC {
			t.Errorf("DB default_access_ends_at = %s, want %s", dbEndsAt.UTC().Format(time.RFC3339), wantUTC)
		}

		var auditRecord struct {
			Module   string `json:"module"`
			Metadata string `json:"metadata"`
		}
		err = pool.QueryRow(ctx, `
			SELECT module, metadata::text FROM audit_events
			WHERE action = 'COURSE_DEFAULT_ACCESS_EXPIRY_SET' AND target_type = 'COURSE' AND target_id = $1 AND actor_account_id = $2::uuid
		`, courseID, adminID).Scan(&auditRecord.Module, &auditRecord.Metadata)
		if err != nil {
			t.Fatalf("reading audit_events record: %v", err)
		}
		if auditRecord.Module != "CATALOG_AND_AUTHORING" {
			t.Errorf("got audit module = %q, want %q", auditRecord.Module, "CATALOG_AND_AUTHORING")
		}
		if !strings.Contains(auditRecord.Metadata, "new_default_access_ends_at") || !strings.Contains(auditRecord.Metadata, wantUTC) {
			t.Errorf("got audit metadata = %q, want containing new_default_access_ends_at %q", auditRecord.Metadata, wantUTC)
		}
	})

	t.Run("Malformed course ID returns 404", func(t *testing.T) {
		malformedURL := ts.URL + "/api/v1/admin/courses/invalid-uuid/default-access-expiry"
		body := []byte(`{"date": "2026-12-31", "reason": "Valid reason"}`)
		resp := doPricingRequest(t, client, "PUT", malformedURL, adminToken, validOrigin, adminToken, body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("Malformed course ID status = %d, want 404", resp.StatusCode)
		}
	})

	t.Run("Past date returns 422 and creates no DB or audit mutation", func(t *testing.T) {
		var initialAuditCount int
		_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_events`).Scan(&initialAuditCount)

		body := []byte(`{"date": "2020-01-01", "reason": "Past date"}`)
		resp := doPricingRequest(t, client, "PUT", expiryURL, adminToken, validOrigin, adminToken, body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("Past date status = %d, want 422", resp.StatusCode)
		}

		var afterAuditCount int
		_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_events`).Scan(&afterAuditCount)
		if afterAuditCount != initialAuditCount {
			t.Errorf("audit_events count changed from %d to %d on past date refusal", initialAuditCount, afterAuditCount)
		}
	})

	t.Run("Instructor role returns 403 and creates no DB or audit mutation", func(t *testing.T) {
		instToken := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x52}, 32))
		body := []byte(`{"date": "2026-12-31", "reason": "Instructor attempt"}`)

		var initialAuditCount int
		_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_events`).Scan(&initialAuditCount)

		resp := doPricingRequest(t, client, "PUT", expiryURL, instToken, validOrigin, instToken, body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("Instructor PUT default access expiry status = %d, want 403", resp.StatusCode)
		}

		var afterAuditCount int
		_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_events`).Scan(&afterAuditCount)
		if afterAuditCount != initialAuditCount {
			t.Errorf("audit_events count changed from %d to %d on forbidden role refusal", initialAuditCount, afterAuditCount)
		}
	})
}

func TestBatchA_CourseAccessInvitationHTTPAPI_RealPostgreSQL(t *testing.T) {
	ts, pool, adminID, _, courseID, adminToken, studentToken := setupAdminAccessAPIServer(t)
	ctx := context.Background()

	client := ts.Client()
	validOrigin := "https://gradex.example"
	invitationsURL := ts.URL + "/api/v1/admin/course-access-invitations"

	t.Run("Admin creates invitation successfully (201, audit, outbox, 0 enrollments, 0 entitlements)", func(t *testing.T) {
		body := []byte(`{"course_id":"` + courseID + `","email":"  Student-Access@Example.COM  ","admin_note":"Batch A test"}`)
		resp := doPricingRequest(t, client, "POST", invitationsURL, adminToken, validOrigin, adminToken, body)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("POST invitation status = %d, want 201", resp.StatusCode)
		}

		var inv access.Invitation
		if err := json.NewDecoder(resp.Body).Decode(&inv); err != nil {
			t.Fatalf("decoding response: %v", err)
		}

		if inv.NormalizedEmail != "student-access@example.com" {
			t.Errorf("NormalizedEmail = %q, want %q", inv.NormalizedEmail, "student-access@example.com")
		}
		if inv.State != access.StatePendingStudentAcceptance {
			t.Errorf("State = %q, want %q", inv.State, access.StatePendingStudentAcceptance)
		}

		// Assert zero enrollments and zero entitlements
		var enrCount, entCount int
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM enrollments`).Scan(&enrCount)
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM entitlements`).Scan(&entCount)
		if enrCount != 0 || entCount != 0 {
			t.Errorf("got enrollments = %d, entitlements = %d; want 0, 0", enrCount, entCount)
		}

		// Assert audit event exists
		var auditCount int
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action = 'COURSE_ACCESS_INVITATION_ISSUED' AND target_id = $1`, inv.ID).Scan(&auditCount)
		if auditCount != 1 {
			t.Errorf("audit event count = %d, want 1", auditCount)
		}

		// Assert outbox intent exists (T020, T024)
		var outboxCount int
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE aggregate_id = $1::uuid`, inv.ID).Scan(&outboxCount)
		if outboxCount != 1 {
			t.Errorf("outbox event count = %d, want 1", outboxCount)
		}
	})

	t.Run("Duplicate invitation returns 409 duplicate-invitation and secret not in body", func(t *testing.T) {
		body := []byte(`{"course_id":"` + courseID + `","email":"student-access@example.com"}`)
		resp := doPricingRequest(t, client, "POST", invitationsURL, adminToken, validOrigin, adminToken, body)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusConflict {
			t.Fatalf("Duplicate invitation status = %d, want 409", resp.StatusCode)
		}

		var prob struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&prob)
		if prob.Code != "DUPLICATE_INVITATION" {
			t.Errorf("prob.Code = %q, want DUPLICATE_INVITATION", prob.Code)
		}
	})

	t.Run("Ineligible recipient (non-student) returns 409 ineligible-recipient and commits zero partial rows", func(t *testing.T) {
		body := []byte(`{"course_id":"` + courseID + `","email":"instructor-access@example.com"}`)
		resp := doPricingRequest(t, client, "POST", invitationsURL, adminToken, validOrigin, adminToken, body)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusConflict {
			t.Fatalf("Ineligible recipient status = %d, want 409", resp.StatusCode)
		}

		var prob struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&prob)
		if prob.Code != "INELIGIBLE_RECIPIENT" {
			t.Errorf("prob.Code = %q, want INELIGIBLE_RECIPIENT", prob.Code)
		}

		// Assert zero partial rows committed inside transaction (R-A3)
		var invCount, secretCount, auditCount, outboxCount int
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM course_access_invitations WHERE normalized_email = 'instructor-access@example.com'`).Scan(&invCount)
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM identity_action_secrets WHERE purpose = 'COURSE_ACCESS_INVITATION' AND issued_at > now() - interval '1 minute'`).Scan(&secretCount)
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action = 'COURSE_ACCESS_INVITATION_ISSUED' AND metadata->>'normalized_email' = 'instructor-access@example.com'`).Scan(&auditCount)
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE type = 'access.invitation_issued' AND safe_payload->>'course_id' = $1 AND safe_payload->>'normalized_email' = 'instructor-access@example.com'`, courseID).Scan(&outboxCount)

		if invCount != 0 || auditCount != 0 || outboxCount != 0 {
			t.Errorf("got invCount=%d, auditCount=%d, outboxCount=%d; want 0, 0, 0", invCount, auditCount, outboxCount)
		}
	})

	t.Run("Malformed email addresses return 422 and create zero database rows (R-A2)", func(t *testing.T) {
		malformedEmails := []string{
			"x@y",
			"@x.com",
			"foo bar@x.com",
			"a@",
			"@example.com",
			"user@localhost",
			"user name@example.com",
			strings.Repeat("a", 315) + "@example.com", // >320 chars
		}

		for _, malformed := range malformedEmails {
			t.Run("email="+malformed, func(t *testing.T) {
				reqBody, _ := json.Marshal(map[string]any{
					"course_id": courseID,
					"email":     malformed,
				})
				resp := doPricingRequest(t, client, "POST", invitationsURL, adminToken, validOrigin, adminToken, reqBody)
				defer resp.Body.Close()

				if resp.StatusCode != http.StatusUnprocessableEntity {
					t.Fatalf("Malformed email %q status = %d, want 422 (never 500)", malformed, resp.StatusCode)
				}

				var prob struct {
					Code       string `json:"code"`
					Violations []struct {
						Parameter string `json:"parameter"`
					} `json:"violations"`
				}
				if err := json.NewDecoder(resp.Body).Decode(&prob); err != nil {
					t.Fatalf("decoding error response: %v", err)
				}
				if prob.Code != "VALIDATION_FAILED" {
					t.Errorf("Code = %q, want VALIDATION_FAILED", prob.Code)
				}

				// Verify zero DB rows were created
				var invCount, secretCount, auditCount, outboxCount int
				_ = pool.QueryRow(ctx, `SELECT count(*) FROM course_access_invitations WHERE email = $1 OR normalized_email = $1`, malformed).Scan(&invCount)
				_ = pool.QueryRow(ctx, `SELECT count(*) FROM identity_action_secrets WHERE purpose = 'COURSE_ACCESS_INVITATION' AND issued_at > now() - interval '10 seconds'`).Scan(&secretCount)
				_ = pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE created_at > now() - interval '10 seconds' AND action = 'COURSE_ACCESS_INVITATION_ISSUED'`).Scan(&auditCount)
				_ = pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE created_at > now() - interval '10 seconds' AND type = 'access.invitation_issued'`).Scan(&outboxCount)

				if invCount != 0 || auditCount != 0 || outboxCount != 0 {
					t.Errorf("malformed %q created DB rows: inv=%d, audit=%d, outbox=%d, want all 0", malformed, invCount, auditCount, outboxCount)
				}
			})
		}
	})

	t.Run("Student acceptance flow (200, state -> PENDING_ADMIN_APPROVAL, 0 entitlement, 0 enrollment)", func(t *testing.T) {
		// 1. Create a fresh invitation for recipient
		newCourseID := "20000000-0000-0000-0000-000000000002"
		_, err := pool.Exec(ctx, `INSERT INTO courses (id, owner_account_id, lifecycle) VALUES ($1, $2, 'DRAFT')`, newCourseID, adminID)
		if err != nil {
			t.Fatalf("seeding new course: %v", err)
		}

		outboxWriter, err := outbox.NewWriter("key-v1", bytes.Repeat([]byte{0x42}, 32))
		if err != nil {
			t.Fatalf("outbox.NewWriter: %v", err)
		}
		accessRepo, err := access.NewRepository(pool, outboxWriter)
		if err != nil {
			t.Fatalf("access.NewRepository: %v", err)
		}

		inv, token, err := accessRepo.CreateInvitation(ctx, access.CreateInvitationParams{
			CourseID:       newCourseID,
			Email:          "student-access@example.com",
			AdminAccountID: adminID,
		})
		if err != nil {
			t.Fatalf("CreateInvitation: %v", err)
		}

		// 2. Student accepts valid link
		acceptURL := ts.URL + "/api/v1/me/course-access-invitations/" + inv.ID + "/accept"
		acceptBody := []byte(`{"acceptance_token":"` + token + `"}`)

		resp := doPricingRequest(t, client, "POST", acceptURL, studentToken, validOrigin, studentToken, acceptBody)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Acceptance status = %d, want 200", resp.StatusCode)
		}

		var acceptedProj access.StudentInvitation
		if err := json.NewDecoder(resp.Body).Decode(&acceptedProj); err != nil {
			t.Fatalf("decoding accepted projection: %v", err)
		}

		if acceptedProj.State != access.StatePendingAdminApproval {
			t.Errorf("Accepted state = %q, want PENDING_ADMIN_APPROVAL", acceptedProj.State)
		}

		// ASSERTION FOR T032: ZERO entitlements and ZERO enrollments created during acceptance!
		var enrCount, entCount int
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM enrollments WHERE course_id = $1::uuid`, newCourseID).Scan(&enrCount)
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM entitlements WHERE source_invitation_id = $1::uuid`, inv.ID).Scan(&entCount)
		if enrCount != 0 || entCount != 0 {
			t.Fatalf("CRITICAL REGRESSION (T032): Acceptance created enrollments = %d, entitlements = %d; WANT ZERO", enrCount, entCount)
		}

		// 3. Repeated acceptance returns 409 invitation-state-conflict
		resp2 := doPricingRequest(t, client, "POST", acceptURL, studentToken, validOrigin, studentToken, acceptBody)
		defer resp2.Body.Close()
		if resp2.StatusCode != http.StatusConflict {
			t.Errorf("Repeated acceptance status = %d, want 409", resp2.StatusCode)
		}

		// 4. Wrong student identity returns 404 byte-identical to not-found (T030)
		otherStudentToken := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x54}, 32))
		resp3 := doPricingRequest(t, client, "POST", acceptURL, otherStudentToken, validOrigin, otherStudentToken, acceptBody)
		defer resp3.Body.Close()
		if resp3.StatusCode != http.StatusNotFound {
			t.Errorf("Wrong identity acceptance status = %d, want 404", resp3.StatusCode)
		}
	})

	t.Run("Expired or invalid secret returns 410 and leaves invitation state unchanged", func(t *testing.T) {
		courseID3 := "20000000-0000-0000-0000-000000000003"
		_, _ = pool.Exec(ctx, `INSERT INTO courses (id, owner_account_id, lifecycle) VALUES ($1, $2, 'DRAFT')`, courseID3, adminID)

		body := []byte(`{"course_id":"` + courseID3 + `","email":"student-access@example.com"}`)
		resp := doPricingRequest(t, client, "POST", invitationsURL, adminToken, validOrigin, adminToken, body)
		var inv access.Invitation
		_ = json.NewDecoder(resp.Body).Decode(&inv)
		resp.Body.Close()

		acceptURL := ts.URL + "/api/v1/me/course-access-invitations/" + inv.ID + "/accept"
		badTokenBody := []byte(`{"acceptance_token":"invalid-token-secret-bytes-12345678"}`)

		resp = doPricingRequest(t, client, "POST", acceptURL, studentToken, validOrigin, studentToken, badTokenBody)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusGone {
			t.Fatalf("Invalid token status = %d, want 410", resp.StatusCode)
		}

		// Verify state is still PENDING_STUDENT_ACCEPTANCE in DB
		var currentDBState string
		_ = pool.QueryRow(ctx, `SELECT state FROM course_access_invitations WHERE id = $1::uuid`, inv.ID).Scan(&currentDBState)
		if currentDBState != string(access.StatePendingStudentAcceptance) {
			t.Errorf("DB state after failed secret = %q, want PENDING_STUDENT_ACCEPTANCE", currentDBState)
		}
	})

	t.Run("Concurrent invitation creation race (T049) under real PostgreSQL", func(t *testing.T) {
		courseID4 := "20000000-0000-0000-0000-000000000004"
		_, _ = pool.Exec(ctx, `INSERT INTO courses (id, owner_account_id, lifecycle) VALUES ($1, $2, 'DRAFT')`, courseID4, adminID)

		const concurrency = 8
		var wg sync.WaitGroup
		statusCodes := make([]int, concurrency)

		body := []byte(`{"course_id":"` + courseID4 + `","email":"student-access@example.com"}`)

		for i := 0; i < concurrency; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				r := doPricingRequest(t, client, "POST", invitationsURL, adminToken, validOrigin, adminToken, body)
				statusCodes[idx] = r.StatusCode
				r.Body.Close()
			}(i)
		}
		wg.Wait()

		var createdCount, conflictCount, otherCount int
		for _, st := range statusCodes {
			switch st {
			case http.StatusCreated:
				createdCount++
			case http.StatusConflict:
				conflictCount++
			default:
				otherCount++
			}
		}

		if createdCount != 1 {
			t.Errorf("Concurrent creation got %d status 201 Created, want exactly 1", createdCount)
		}
		if conflictCount != concurrency-1 {
			t.Errorf("Concurrent creation got %d status 409 Conflict, want %d", conflictCount, concurrency-1)
		}
		if otherCount != 0 {
			t.Errorf("Concurrent creation got %d unexpected status codes (500 etc)", otherCount)
		}
	})
}
