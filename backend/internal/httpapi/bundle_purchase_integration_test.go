//go:build integration

package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/access"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
	"github.com/jackc/pgx/v5/pgxpool"
)

const bundlePurchaseStudentID = "10000000-0000-0000-0000-000000000003"
const bundlePurchaseInstructorID = "10000000-0000-0000-0000-000000000002"

// newBundlePurchaseFixture uses the same real migrated PostgreSQL boundary as
// the retained Course purchase journey. No repository or transaction is mocked.
func newBundlePurchaseFixture(t *testing.T) (*access.Repository, *pgxpool.Pool, *httptest.Server, string, string, string, []string) {
	t.Helper()
	ts, pool, adminID, _, firstCourseID, _, studentToken := setupAdminAccessAPIServer(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE accounts SET email_verified_at=now() WHERE id=$1::uuid`, bundlePurchaseStudentID); err != nil {
		t.Fatalf("verifying Student: %v", err)
	}
	courses := []string{
		firstCourseID,
		"20000000-0000-0000-0000-000000000002",
		"20000000-0000-0000-0000-000000000003",
	}
	expiry := time.Now().UTC().Add(90 * 24 * time.Hour)
	for index, courseID := range courses {
		revisionID := []string{
			"21000000-0000-0000-0000-000000000001",
			"21000000-0000-0000-0000-000000000002",
			"21000000-0000-0000-0000-000000000003",
		}[index]
		if index > 0 {
			if _, err := pool.Exec(ctx, `INSERT INTO courses (id, owner_account_id, lifecycle) VALUES ($1::uuid,$2::uuid,'DRAFT')`, courseID, bundlePurchaseInstructorID); err != nil {
				t.Fatalf("creating Course %d: %v", index, err)
			}
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO course_revisions (id, course_id, state, revision_number, title_ar, title_en)
			VALUES ($1::uuid,$2::uuid,'APPROVED',1,$3,$4)
		`, revisionID, courseID, "مقرر الحزمة", "Bundle Course"); err != nil {
			t.Fatalf("creating Course revision %d: %v", index, err)
		}
		if _, err := pool.Exec(ctx, `
			UPDATE courses SET lifecycle='PUBLISHED', live_revision_id=$1::uuid,
			default_access_ends_at=$2 WHERE id=$3::uuid
		`, revisionID, expiry, courseID); err != nil {
			t.Fatalf("publishing Course %d: %v", index, err)
		}
	}
	bundleID := "30000000-0000-0000-0000-000000000001"
	if _, err := pool.Exec(ctx, `
		INSERT INTO bundles (id,title_ar,title_en,description_ar,description_en,lifecycle,created_by_account_id,updated_by_account_id)
		VALUES ($1::uuid,'باقة هندسة','Engineering Bundle','وصف','Description','PUBLISHED',$2::uuid,$2::uuid)
	`, bundleID, adminID); err != nil {
		t.Fatalf("creating Bundle: %v", err)
	}
	for position, courseID := range courses {
		if _, err := pool.Exec(ctx, `INSERT INTO bundle_courses (bundle_id,course_id,position) VALUES ($1::uuid,$2::uuid,$3)`, bundleID, courseID, position); err != nil {
			t.Fatalf("adding Bundle member: %v", err)
		}
	}
	offer := int64(40000)
	if _, err := pool.Exec(ctx, `
		INSERT INTO bundle_price_changes (bundle_id,new_value_minor_units,offer_price_minor_units,changed_by_account_id,reason)
		VALUES ($1::uuid,60000,$2,$3::uuid,'launch offer')
	`, bundleID, offer, adminID); err != nil {
		t.Fatalf("pricing Bundle: %v", err)
	}
	writer, err := outbox.NewWriter("key-v1", bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	repo, err := access.NewRepository(pool, writer)
	if err != nil {
		t.Fatal(err)
	}
	return repo, pool, ts, studentToken, adminID, bundleID, courses
}

func TestBundleConfirmationGrantsSnapshotAndPreservesExistingAccess(t *testing.T) {
	repo, pool, _, _, adminID, bundleID, courses := newBundlePurchaseFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()

	var invitationID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO course_access_invitations (
			email,normalized_email,course_id,created_by_account_id,accepted_by_account_id,
			decided_by_account_id,state,accepted_at,decided_at
		) VALUES ('student-access@example.com','student-access@example.com',$1::uuid,$2::uuid,$3::uuid,$2::uuid,'APPROVED',$4,$4)
		RETURNING id::text
	`, courses[0], adminID, bundlePurchaseStudentID, now).Scan(&invitationID); err != nil {
		t.Fatalf("creating existing grant provenance: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO enrollments (student_account_id,course_id) VALUES ($1::uuid,$2::uuid)`, bundlePurchaseStudentID, courses[0]); err != nil {
		t.Fatalf("creating existing enrollment: %v", err)
	}
	var existingEntitlementID string
	strongerExpiry := now.Add(180 * 24 * time.Hour)
	if err := pool.QueryRow(ctx, `
		INSERT INTO entitlements (
			student_account_id,scope_kind,scope_id,course_id,grant_source,source_invitation_id,
			original_access_ends_at,access_ends_at,retirement_eligibility_at,state
		) VALUES ($1::uuid,'COURSE',$2::uuid,$2::uuid,'MANUAL_INVITATION',$3::uuid,$4,$4,$5,'ACTIVE')
		RETURNING id::text
	`, bundlePurchaseStudentID, courses[0], invitationID, strongerExpiry, now).Scan(&existingEntitlementID); err != nil {
		t.Fatalf("creating existing entitlement: %v", err)
	}

	request, err := repo.CreateStudentBundlePurchaseRequest(ctx, access.CreateStudentBundlePurchaseRequestParams{
		BundleID: bundleID, StudentAccountID: bundlePurchaseStudentID, Now: now,
	})
	if err != nil {
		t.Fatalf("creating Bundle purchase: %v", err)
	}
	if request.PriceMinorUnits != 40000 || request.RegularPriceMinorUnits == nil || *request.RegularPriceMinorUnits != 60000 || len(request.BundleItems) != 3 {
		t.Fatalf("Bundle snapshot = %#v", request)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM bundle_courses WHERE bundle_id=$1::uuid AND course_id=$2::uuid`, bundleID, courses[2]); err != nil {
		t.Fatalf("editing current Bundle: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO bundle_price_changes (bundle_id,new_value_minor_units,changed_by_account_id,reason) VALUES ($1::uuid,90000,$2::uuid,'Price after request')`, bundleID, adminID); err != nil {
		t.Fatalf("repricing current Bundle: %v", err)
	}

	result, err := repo.ConfirmPurchaseRequest(ctx, access.ConfirmPurchaseRequestParams{
		PurchaseRequestID: request.ID, AdminAccountID: adminID, Now: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("confirming Bundle payment: %v", err)
	}
	if result.PurchaseRequest.State != access.PurchaseRequestAccessGranted || result.PurchaseRequest.PriceMinorUnits != 40000 || result.Invitation != nil || len(result.BundleGrants) != 3 {
		t.Fatalf("Bundle confirmation = %#v", result)
	}
	var entitlementCount, invitationCount int
	if err := pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM entitlements WHERE student_account_id=$1::uuid AND course_id=ANY($2::uuid[])),
		       (SELECT count(*) FROM course_access_invitations WHERE id<>$3::uuid AND normalized_email='student-access@example.com')
	`, bundlePurchaseStudentID, courses, invitationID).Scan(&entitlementCount, &invitationCount); err != nil {
		t.Fatal(err)
	}
	if entitlementCount != 3 || invitationCount != 0 {
		t.Fatalf("entitlements=%d Bundle invitations=%d", entitlementCount, invitationCount)
	}
	var preservedID string
	if err := pool.QueryRow(ctx, `SELECT entitlement_id::text FROM bundle_purchase_grants WHERE purchase_request_id=$1::uuid AND course_id=$2::uuid AND disposition='PRESERVED'`, request.ID, courses[0]).Scan(&preservedID); err != nil {
		t.Fatalf("reading preserved provenance: %v", err)
	}
	if preservedID != existingEntitlementID {
		t.Fatalf("preserved entitlement=%s want %s", preservedID, existingEntitlementID)
	}

	replay, err := repo.ConfirmPurchaseRequest(ctx, access.ConfirmPurchaseRequestParams{
		PurchaseRequestID: request.ID, AdminAccountID: adminID, Now: now.Add(2 * time.Minute),
	})
	if err != nil || len(replay.BundleGrants) != 3 {
		t.Fatalf("replayed confirmation = %#v, %v", replay, err)
	}
	var grantRows, bundleEvents int
	if err := pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM bundle_purchase_grants WHERE purchase_request_id=$1::uuid),
		       (SELECT count(*) FROM outbox_events WHERE event_type='access.bundle_granted' AND aggregate_id=$1::uuid)
	`, request.ID).Scan(&grantRows, &bundleEvents); err != nil {
		t.Fatal(err)
	}
	if grantRows != 3 || bundleEvents != 1 {
		t.Fatalf("grant rows=%d Bundle events=%d", grantRows, bundleEvents)
	}
}

func TestConcurrentBundleConfirmationConverges(t *testing.T) {
	repo, _, _, _, adminID, bundleID, _ := newBundlePurchaseFixture(t)
	ctx := context.Background()
	request, err := repo.CreateStudentBundlePurchaseRequest(ctx, access.CreateStudentBundlePurchaseRequestParams{
		BundleID: bundleID, StudentAccountID: bundlePurchaseStudentID, Now: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := repo.ConfirmPurchaseRequest(ctx, access.ConfirmPurchaseRequestParams{
				PurchaseRequestID: request.ID, AdminAccountID: adminID, Now: time.Now().UTC(),
			})
			if err == nil && (result.PurchaseRequest.State != access.PurchaseRequestAccessGranted || len(result.BundleGrants) != 3) {
				err = errors.New("confirmation did not return the complete logical grant")
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestConcurrentBundlePurchasesSharingCoursesPreserveOneEntitlementSet(t *testing.T) {
	repo, pool, _, _, adminID, bundleID, courses := newBundlePurchaseFixture(t)
	ctx := context.Background()
	const secondBundleID = "30000000-0000-0000-0000-000000000002"
	if _, err := pool.Exec(ctx, `INSERT INTO bundles (id,title_ar,title_en,description_ar,description_en,lifecycle,created_by_account_id,updated_by_account_id) VALUES ($1::uuid,'باقة ثانية','Second Bundle','وصف','Description','PUBLISHED',$2::uuid,$2::uuid)`, secondBundleID, adminID); err != nil {
		t.Fatal(err)
	}
	for position, courseID := range courses {
		if _, err := pool.Exec(ctx, `INSERT INTO bundle_courses (bundle_id,course_id,position) VALUES ($1::uuid,$2::uuid,$3)`, secondBundleID, courseID, position); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO bundle_price_changes (bundle_id,new_value_minor_units,changed_by_account_id,reason) VALUES ($1::uuid,61000,$2::uuid,'Second Bundle price')`, secondBundleID, adminID); err != nil {
		t.Fatal(err)
	}
	requests := make([]access.PurchaseRequest, 0, 2)
	for _, target := range []string{bundleID, secondBundleID} {
		request, err := repo.CreateStudentBundlePurchaseRequest(ctx, access.CreateStudentBundlePurchaseRequestParams{BundleID: target, StudentAccountID: bundlePurchaseStudentID, Now: time.Now().UTC()})
		if err != nil {
			t.Fatal(err)
		}
		requests = append(requests, request)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, request := range requests {
		request := request
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := repo.ConfirmPurchaseRequest(ctx, access.ConfirmPurchaseRequestParams{PurchaseRequestID: request.ID, AdminAccountID: adminID, Now: time.Now().UTC()})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var entitlements, provenance int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM entitlements WHERE student_account_id=$1::uuid AND course_id=ANY($2::uuid[]) AND state='ACTIVE'),(SELECT count(*) FROM bundle_purchase_grants WHERE purchase_request_id=ANY($3::uuid[]))`, bundlePurchaseStudentID, courses, []string{requests[0].ID, requests[1].ID}).Scan(&entitlements, &provenance); err != nil {
		t.Fatal(err)
	}
	if entitlements != 3 || provenance != 6 {
		t.Fatalf("shared Bundle entitlements=%d provenance=%d", entitlements, provenance)
	}
}

func TestBundleConfirmationFailureRollsBackEveryGrant(t *testing.T) {
	repo, pool, _, _, adminID, bundleID, courses := newBundlePurchaseFixture(t)
	ctx := context.Background()
	request, err := repo.CreateStudentBundlePurchaseRequest(ctx, access.CreateStudentBundlePurchaseRequestParams{
		BundleID: bundleID, StudentAccountID: bundlePurchaseStudentID, Now: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		CREATE FUNCTION fail_selected_bundle_grant() RETURNS TRIGGER AS $$
		BEGIN
			IF NEW.course_id = '`+courses[1]+`'::uuid THEN
				RAISE EXCEPTION 'injected Bundle entitlement failure';
			END IF;
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER fail_selected_bundle_grant_trigger
		BEFORE INSERT ON entitlements FOR EACH ROW EXECUTE FUNCTION fail_selected_bundle_grant();
	`); err != nil {
		t.Fatalf("installing failure seam: %v", err)
	}
	if _, err := repo.ConfirmPurchaseRequest(ctx, access.ConfirmPurchaseRequestParams{
		PurchaseRequestID: request.ID, AdminAccountID: adminID, Now: time.Now().UTC(),
	}); err == nil {
		t.Fatal("Bundle confirmation unexpectedly succeeded")
	}
	var state string
	var entitlements, enrollments, provenance, events int
	if err := pool.QueryRow(ctx, `
		SELECT state::text,
		 (SELECT count(*) FROM entitlements WHERE student_account_id=$2::uuid AND course_id=ANY($3::uuid[])),
		 (SELECT count(*) FROM enrollments WHERE student_account_id=$2::uuid AND course_id=ANY($3::uuid[])),
		 (SELECT count(*) FROM bundle_purchase_grants WHERE purchase_request_id=$1::uuid),
		 (SELECT count(*) FROM outbox_events WHERE event_type='access.bundle_granted' AND aggregate_id=$1::uuid)
		FROM purchase_requests WHERE id=$1::uuid
	`, request.ID, bundlePurchaseStudentID, courses).Scan(&state, &entitlements, &enrollments, &provenance, &events); err != nil {
		t.Fatal(err)
	}
	if state != "WAITING_PAYMENT" || entitlements != 0 || enrollments != 0 || provenance != 0 || events != 0 {
		t.Fatalf("rollback state=%s entitlements=%d enrollments=%d provenance=%d events=%d", state, entitlements, enrollments, provenance, events)
	}
}

func TestCourseConfirmationStillRequiresStudentInvitationAcceptance(t *testing.T) {
	_, pool, adminID, _, courseID, _, _ := setupAdminAccessAPIServer(t)
	ctx := context.Background()
	const revisionID = "25000000-0000-0000-0000-000000000001"
	if _, err := pool.Exec(ctx, `UPDATE accounts SET email_verified_at=now() WHERE id=$1::uuid`, bundlePurchaseStudentID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO course_revisions (id,course_id,state,revision_number,title_ar,title_en) VALUES ($1::uuid,$2::uuid,'APPROVED',1,'مقرر فردي','Individual Course')`, revisionID, courseID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE courses SET lifecycle='PUBLISHED',live_revision_id=$1::uuid,default_access_ends_at=now()+interval '60 days' WHERE id=$2::uuid`, revisionID, courseID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO course_price_changes (course_id,new_value_minor_units,changed_by_account_id,reason) VALUES ($1::uuid,25000,$2::uuid,'Course price')`, courseID, adminID); err != nil {
		t.Fatal(err)
	}
	writer, err := outbox.NewWriter("key-v1", bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	repo, err := access.NewRepository(pool, writer)
	if err != nil {
		t.Fatal(err)
	}
	request, err := repo.CreateStudentPurchaseRequest(ctx, access.CreateStudentPurchaseRequestParams{
		CourseID: courseID, StudentAccountID: bundlePurchaseStudentID, Now: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	confirmed, err := repo.ConfirmPurchaseRequest(ctx, access.ConfirmPurchaseRequestParams{
		PurchaseRequestID: request.ID, AdminAccountID: adminID, Now: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if confirmed.PurchaseRequest.State != access.PurchaseRequestInvitationCreated || confirmed.Invitation == nil || len(confirmed.BundleGrants) != 0 {
		t.Fatalf("Course confirmation changed semantics: %#v", confirmed)
	}
	var entitlements int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM entitlements WHERE student_account_id=$1::uuid AND course_id=$2::uuid`, bundlePurchaseStudentID, courseID).Scan(&entitlements); err != nil {
		t.Fatal(err)
	}
	if entitlements != 0 {
		t.Fatalf("Course confirmation granted %d entitlements before Student acceptance", entitlements)
	}
}

func TestBundlePurchaseHTTPRejectsClientAuthorityAndCrossAccountAccess(t *testing.T) {
	_, pool, ts, studentToken, _, bundleID, _ := newBundlePurchaseFixture(t)
	ctx := context.Background()
	client := ts.Client()
	endpoint := ts.URL + "/api/v1/me/purchase-requests"

	created := purchaseFlowRequest(t, client, endpoint, studentToken, "https://gradex.example",
		[]byte(`{"bundle_id":"`+bundleID+`"}`))
	defer created.Body.Close()
	if created.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(created.Body)
		t.Fatalf("Bundle request status=%d body=%s", created.StatusCode, body)
	}
	var response struct {
		Price int64                       `json:"price_minor_units"`
		Items []access.BundlePurchaseItem `json:"bundle_items"`
	}
	if err := json.NewDecoder(created.Body).Decode(&response); err != nil || response.Price != 40000 || len(response.Items) != 3 {
		t.Fatalf("server-derived Bundle response=%#v error=%v", response, err)
	}

	otherToken := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x54}, 32))
	other := purchaseFlowGet(t, client, endpoint, otherToken)
	defer other.Body.Close()
	var otherRows struct {
		Requests []access.PurchaseRequest `json:"purchase_requests"`
	}
	if other.StatusCode != http.StatusOK {
		t.Fatalf("other Student read status=%d", other.StatusCode)
	}
	if err := json.NewDecoder(other.Body).Decode(&otherRows); err != nil || len(otherRows.Requests) != 0 {
		t.Fatalf("cross-account Bundle requests=%#v error=%v", otherRows, err)
	}

	instructorToken := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x52}, 32))
	instructor := purchaseFlowRequest(t, client, endpoint, instructorToken, "https://gradex.example", []byte(`{"bundle_id":"`+bundleID+`"}`))
	defer instructor.Body.Close()
	if instructor.StatusCode != http.StatusForbidden {
		t.Fatalf("Instructor Bundle purchase status=%d", instructor.StatusCode)
	}

	if _, err := pool.Exec(ctx, `UPDATE bundles SET lifecycle='ARCHIVED' WHERE id=$1::uuid`, bundleID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE accounts SET email_verified_at=now() WHERE id='10000000-0000-0000-0000-000000000004'::uuid`); err != nil {
		t.Fatal(err)
	}
	archived := purchaseFlowRequest(t, client, endpoint, otherToken, "https://gradex.example", []byte(`{"bundle_id":"`+bundleID+`"}`))
	defer archived.Body.Close()
	if archived.StatusCode != http.StatusNotFound {
		t.Fatalf("archived Bundle request status=%d", archived.StatusCode)
	}

	studentConfirm := purchaseFlowRequest(t, client, ts.URL+"/api/v1/admin/purchase-requests/00000000-0000-0000-0000-000000000001/confirm-payment", studentToken, "https://gradex.example", nil)
	defer studentConfirm.Body.Close()
	if studentConfirm.StatusCode != http.StatusForbidden {
		t.Fatalf("Student confirmation status=%d", studentConfirm.StatusCode)
	}
}

func TestBundlePurchaseHTTPRejectsClientPriceAndMembership(t *testing.T) {
	_, _, ts, studentToken, _, bundleID, _ := newBundlePurchaseFixture(t)
	tampered := purchaseFlowRequest(t, ts.Client(), ts.URL+"/api/v1/me/purchase-requests", studentToken, "https://gradex.example",
		[]byte(`{"bundle_id":"`+bundleID+`","price_minor_units":1,"course_ids":[]}`))
	defer tampered.Body.Close()
	if tampered.StatusCode != http.StatusBadRequest {
		t.Fatalf("tampered Bundle request status=%d", tampered.StatusCode)
	}
}

func TestBundlePurchaseCourseSnapshotRejectsMutation(t *testing.T) {
	repo, pool, _, _, _, bundleID, _ := newBundlePurchaseFixture(t)
	ctx := context.Background()
	request, err := repo.CreateStudentBundlePurchaseRequest(ctx, access.CreateStudentBundlePurchaseRequestParams{
		BundleID: bundleID, StudentAccountID: bundlePurchaseStudentID, Now: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE purchase_request_bundle_items SET position=9 WHERE purchase_request_id=$1::uuid AND position=0`, request.ID); err == nil {
		t.Fatal("Bundle purchase snapshot update unexpectedly succeeded")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM purchase_request_bundle_items WHERE purchase_request_id=$1::uuid`, request.ID); err == nil {
		t.Fatal("Bundle purchase snapshot delete unexpectedly succeeded")
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM purchase_request_bundle_items WHERE purchase_request_id=$1::uuid`, request.ID).Scan(&count); err != nil || count != 3 {
		t.Fatalf("snapshot rows=%d error=%v", count, err)
	}
}
