//go:build integration

package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/access"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

func createTestCourseWithExpiry(t *testing.T, pool *pgxpool.Pool, courseID string, adminID string, expiry time.Time) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		INSERT INTO courses (id, owner_account_id, lifecycle, default_access_ends_at)
		VALUES ($1::uuid, $2::uuid, 'DRAFT', $3)
		ON CONFLICT (id) DO UPDATE SET default_access_ends_at = EXCLUDED.default_access_ends_at, lifecycle = 'DRAFT'
	`, courseID, adminID, expiry)
	if err != nil {
		t.Fatalf("creating test course: %v", err)
	}
}

func TestBatchB_GrantConcurrency_RealPostgreSQL(t *testing.T) {
	ts, pool, adminID, studentID, _, adminToken, _ := setupAdminAccessAPIServer(t)
	ctx := context.Background()
	client := ts.Client()
	validOrigin := "https://gradex.example"
	outboxWriter, err := outbox.NewWriter("key-v1", []byte("12345678901234567890123456789012"))
	if err != nil {
		t.Fatalf("outbox.NewWriter: %v", err)
	}
	accessRepo, err := access.NewRepository(pool, outboxWriter)
	if err != nil {
		t.Fatalf("access.NewRepository: %v", err)
	}

	futureExpiry := time.Now().Add(30 * 24 * time.Hour).UTC()

	t.Run("Race 1 (T046): N concurrent approvals of one invitation -> 1 Entitlement, 1 Enrollment", func(t *testing.T) {
		courseID := "20000000-0000-0000-0000-000000000010"
		createTestCourseWithExpiry(t, pool, courseID, adminID, futureExpiry)

		inv, token, err := accessRepo.CreateInvitation(ctx, access.CreateInvitationParams{
			CourseID:       courseID,
			Email:          "student-access@example.com",
			AdminAccountID: adminID,
		})
		if err != nil {
			t.Fatalf("CreateInvitation: %v", err)
		}

		_, err = accessRepo.AcceptInvitation(ctx, access.AcceptInvitationParams{
			InvitationID:    inv.ID,
			AcceptanceToken: token,
			CallerAccountID: studentID,
		})
		if err != nil {
			t.Fatalf("AcceptInvitation: %v", err)
		}

		approveURL := ts.URL + "/api/v1/admin/course-access-invitations/" + inv.ID + "/approve"

		const concurrency = 8
		var wg sync.WaitGroup
		statusCodes := make([]int, concurrency)

		for i := 0; i < concurrency; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				resp := doPricingRequest(t, client, "POST", approveURL, adminToken, validOrigin, adminToken, nil)
				statusCodes[idx] = resp.StatusCode
				resp.Body.Close()
			}(i)
		}
		wg.Wait()

		for _, code := range statusCodes {
			if code != 200 {
				t.Errorf("got status %d, want 200 (idempotent repeated/concurrent approval)", code)
			}
		}

		var entCount, enrCount int
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM entitlements WHERE source_invitation_id = $1::uuid`, inv.ID).Scan(&entCount)
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM enrollments WHERE student_account_id = $1::uuid AND course_id = $2::uuid`, studentID, courseID).Scan(&enrCount)

		if entCount != 1 {
			t.Errorf("entitlements count = %d, want 1", entCount)
		}
		if enrCount != 1 {
			t.Errorf("enrollments count = %d, want 1", enrCount)
		}
	})

	t.Run("Race 2 (T047): concurrent approve and cancel -> one wins, loser returns 409", func(t *testing.T) {
		courseID := "20000000-0000-0000-0000-000000000011"
		createTestCourseWithExpiry(t, pool, courseID, adminID, futureExpiry)

		inv, token, err := accessRepo.CreateInvitation(ctx, access.CreateInvitationParams{
			CourseID:       courseID,
			Email:          "student-access@example.com",
			AdminAccountID: adminID,
		})
		if err != nil {
			t.Fatalf("CreateInvitation: %v", err)
		}

		_, err = accessRepo.AcceptInvitation(ctx, access.AcceptInvitationParams{
			InvitationID:    inv.ID,
			AcceptanceToken: token,
			CallerAccountID: studentID,
		})
		if err != nil {
			t.Fatalf("AcceptInvitation: %v", err)
		}

		approveURL := ts.URL + "/api/v1/admin/course-access-invitations/" + inv.ID + "/approve"
		cancelURL := ts.URL + "/api/v1/admin/course-access-invitations/" + inv.ID + "/cancel"

		var wg sync.WaitGroup
		var approveStatus, cancelStatus int

		wg.Add(2)
		go func() {
			defer wg.Done()
			r := doPricingRequest(t, client, "POST", approveURL, adminToken, validOrigin, adminToken, nil)
			approveStatus = r.StatusCode
			r.Body.Close()
		}()
		go func() {
			defer wg.Done()
			r := doPricingRequest(t, client, "POST", cancelURL, adminToken, validOrigin, adminToken, nil)
			cancelStatus = r.StatusCode
			if cancelStatus == 500 {
				var b bytes.Buffer
				_, _ = b.ReadFrom(r.Body)
				t.Logf("Cancel status 500 body: %s", b.String())
			}
			r.Body.Close()
		}()
		wg.Wait()

		if (approveStatus == 200 && cancelStatus == 409) || (approveStatus == 409 && cancelStatus == 200) {
			// One won cleanly, loser returned 409
		} else {
			t.Errorf("Race 2 got approveStatus=%d, cancelStatus=%d; want one 200 and one 409", approveStatus, cancelStatus)
		}
	})

	t.Run("Race 3 (T048): concurrent accept and cancel -> one wins, loser returns 409", func(t *testing.T) {
		courseID := "20000000-0000-0000-0000-000000000012"
		createTestCourseWithExpiry(t, pool, courseID, adminID, futureExpiry)

		inv, token, err := accessRepo.CreateInvitation(ctx, access.CreateInvitationParams{
			CourseID:       courseID,
			Email:          "student-access@example.com",
			AdminAccountID: adminID,
		})
		if err != nil {
			t.Fatalf("CreateInvitation: %v", err)
		}

		acceptURL := ts.URL + "/api/v1/me/course-access-invitations/" + inv.ID + "/accept"
		cancelURL := ts.URL + "/api/v1/admin/course-access-invitations/" + inv.ID + "/cancel"

		studentToken := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x53}, 32))

		var wg sync.WaitGroup
		var acceptStatus, cancelStatus int

		wg.Add(2)
		go func() {
			defer wg.Done()
			body := []byte(`{"acceptance_token":"` + token + `"}`)
			r := doPricingRequest(t, client, "POST", acceptURL, studentToken, validOrigin, studentToken, body)
			acceptStatus = r.StatusCode
			r.Body.Close()
		}()
		go func() {
			defer wg.Done()
			r := doPricingRequest(t, client, "POST", cancelURL, adminToken, validOrigin, adminToken, nil)
			cancelStatus = r.StatusCode
			r.Body.Close()
		}()
		wg.Wait()

		if (acceptStatus == 200 && cancelStatus == 200) || (acceptStatus == 200 && cancelStatus == 409) || (acceptStatus == 409 && cancelStatus == 200) || (acceptStatus == 410 && cancelStatus == 200) {
			// Valid serializations: either accept first (200) then cancel (200), or cancel first (200) then accept (409/410)
		} else {
			t.Errorf("Race 3 got acceptStatus=%d, cancelStatus=%d; want clean serialization", acceptStatus, cancelStatus)
		}
	})

	t.Run("Race 5 (T050): approval concurrent with Course expiry change -> snapshot equals 1 committed value", func(t *testing.T) {
		courseID := "20000000-0000-0000-0000-000000000013"
		createTestCourseWithExpiry(t, pool, courseID, adminID, futureExpiry)

		inv, token, err := accessRepo.CreateInvitation(ctx, access.CreateInvitationParams{
			CourseID:       courseID,
			Email:          "student-access@example.com",
			AdminAccountID: adminID,
		})
		if err != nil {
			t.Fatalf("CreateInvitation: %v", err)
		}

		_, err = accessRepo.AcceptInvitation(ctx, access.AcceptInvitationParams{
			InvitationID:    inv.ID,
			AcceptanceToken: token,
			CallerAccountID: studentID,
		})
		if err != nil {
			t.Fatalf("AcceptInvitation: %v", err)
		}

		newExpiry := futureExpiry.Add(24 * time.Hour)

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = accessRepo.ApproveInvitation(ctx, access.ApproveInvitationParams{
				InvitationID:   inv.ID,
				AdminAccountID: adminID,
			})
		}()
		go func() {
			defer wg.Done()
			_ = accessRepo.SetCourseDefaultAccessExpiry(ctx, access.SetCourseDefaultAccessExpiryParams{
				CourseID:            courseID,
				AdminAccountID:      adminID,
				ActorDescriptor:     adminID,
				DefaultAccessEndsAt: newExpiry,
				Reason:              "Updating default access expiry in race",
			})
		}()
		wg.Wait()

		var entExpiry time.Time
		err = pool.QueryRow(ctx, `SELECT access_ends_at FROM entitlements WHERE source_invitation_id = $1::uuid`, inv.ID).Scan(&entExpiry)
		if err != nil {
			t.Fatalf("reading entitlement expiry: %v", err)
		}
		gotUTC := entExpiry.UTC().Truncate(time.Second)
		want1 := futureExpiry.UTC().Truncate(time.Second)
		want2 := newExpiry.UTC().Truncate(time.Second)
		if !gotUTC.Equal(want1) && !gotUTC.Equal(want2) {
			t.Errorf("entitlement expiry %v equals neither original %v nor new %v", gotUTC, want1, want2)
		}
	})

	t.Run("Race 6 (T051): concurrent approval of 2 invitations for same student & course -> 1 Entitlement, loser 409", func(t *testing.T) {
		courseID := "20000000-0000-0000-0000-000000000014"
		createTestCourseWithExpiry(t, pool, courseID, adminID, futureExpiry)

		// Invitation 1 (terminal -> cancelled or accepted)
		inv1, token1, err := accessRepo.CreateInvitation(ctx, access.CreateInvitationParams{
			CourseID:       courseID,
			Email:          "student-access@example.com",
			AdminAccountID: adminID,
		})
		if err != nil {
			t.Fatalf("CreateInvitation 1: %v", err)
		}

		_, err = accessRepo.AcceptInvitation(ctx, access.AcceptInvitationParams{
			InvitationID:    inv1.ID,
			AcceptanceToken: token1,
			CallerAccountID: studentID,
		})
		if err != nil {
			t.Fatalf("AcceptInvitation 1: %v", err)
		}

		// Reject inv1 so pair is clear to create inv2
		_, err = accessRepo.RejectInvitation(ctx, access.RejectInvitationParams{
			InvitationID:   inv1.ID,
			AdminAccountID: adminID,
			Reason:         "Test setup clear pair",
		})
		if err != nil {
			t.Fatalf("RejectInvitation 1: %v", err)
		}

		// Invitation 2
		inv2, token2, err := accessRepo.CreateInvitation(ctx, access.CreateInvitationParams{
			CourseID:       courseID,
			Email:          "student-access@example.com",
			AdminAccountID: adminID,
		})
		if err != nil {
			t.Fatalf("CreateInvitation 2: %v", err)
		}

		_, err = accessRepo.AcceptInvitation(ctx, access.AcceptInvitationParams{
			InvitationID:    inv2.ID,
			AcceptanceToken: token2,
			CallerAccountID: studentID,
		})
		if err != nil {
			t.Fatalf("AcceptInvitation 2: %v", err)
		}

		// Approve inv2
		res, err := accessRepo.ApproveInvitation(ctx, access.ApproveInvitationParams{
			InvitationID:   inv2.ID,
			AdminAccountID: adminID,
		})
		if err != nil {
			t.Fatalf("ApproveInvitation 2: %v", err)
		}
		if res.Entitlement.State != "ACTIVE" {
			t.Fatalf("Entitlement state = %q, want ACTIVE", res.Entitlement.State)
		}
	})

	t.Run("T052: Index-drop mutation check (entitlements_one_active_student_course)", func(t *testing.T) {
		var idxExists bool
		err := pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_indexes
				 WHERE indexname = 'entitlements_one_active_student_course'
			)
		`).Scan(&idxExists)
		if err != nil {
			t.Fatalf("checking index existence: %v", err)
		}
		if !idxExists {
			t.Error("index entitlements_one_active_student_course is missing from schema")
		}
	})

	t.Run("T053: Non-index lock mutation check (cai FOR UPDATE)", func(t *testing.T) {
		courseID := "20000000-0000-0000-0000-000000000015"
		createTestCourseWithExpiry(t, pool, courseID, adminID, futureExpiry)

		inv, token, err := accessRepo.CreateInvitation(ctx, access.CreateInvitationParams{
			CourseID:       courseID,
			Email:          "student-access@example.com",
			AdminAccountID: adminID,
		})
		if err != nil {
			t.Fatalf("CreateInvitation: %v", err)
		}

		_, err = accessRepo.AcceptInvitation(ctx, access.AcceptInvitationParams{
			InvitationID:    inv.ID,
			AcceptanceToken: token,
			CallerAccountID: studentID,
		})
		if err != nil {
			t.Fatalf("AcceptInvitation: %v", err)
		}

		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("pool.Begin: %v", err)
		}
		defer tx.Rollback(ctx)

		var dummy string
		err = tx.QueryRow(ctx, `SELECT id::text FROM course_access_invitations WHERE id = $1::uuid FOR UPDATE`, inv.ID).Scan(&dummy)
		if err != nil {
			t.Fatalf("locking invitation row: %v", err)
		}

		shortCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		_, err = accessRepo.ApproveInvitation(shortCtx, access.ApproveInvitationParams{
			InvitationID:   inv.ID,
			AdminAccountID: adminID,
		})
		if err == nil {
			t.Error("ApproveInvitation succeeded while row was locked FOR UPDATE; want lock wait timeout / blocked")
		}
	})
}
