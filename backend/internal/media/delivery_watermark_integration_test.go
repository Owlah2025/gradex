//go:build integration

package media

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// The watermark is issued from the Account the entitlement decision was made
// about, not from anything the caller said about itself.
func TestPlaybackWatermarkIsIssuedForTheAuthenticatedStudent(t *testing.T) {
	f := newDeliveryFixture(t)

	issued, err := f.delivery.IssuePlayback(f.ctx, PlaybackRequest{
		StudentID: f.student, LessonID: f.lesson, AssetVersionID: f.video,
	})
	if err != nil {
		t.Fatalf("issuing playback: %v", err)
	}
	if issued.Watermark == nil {
		t.Fatal("authorized playback carried no watermark")
	}
	// The fixture Student is "D8 Student" at student-d8@example.test.
	if issued.Watermark.DisplayName != "D8 S." {
		t.Fatalf("watermark display name=%q, want %q", issued.Watermark.DisplayName, "D8 S.")
	}
	if issued.Watermark.MaskedIdentifier != "st***@example.test" {
		t.Fatalf("watermark masked identifier=%q, want %q", issued.Watermark.MaskedIdentifier, "st***@example.test")
	}
	if issued.Watermark.Code != f.delivery.watermarkCode(f.student) {
		t.Fatalf("watermark code=%q does not belong to the authenticated Student", issued.Watermark.Code)
	}
}

// A second Student on the same Lesson gets their own identity: the watermark
// follows the Account, never the content.
func TestPlaybackWatermarkDistinguishesTwoStudentsOnOneLesson(t *testing.T) {
	f := newDeliveryFixture(t)
	other := uuid.NewString()
	otherEmail := "other-d8@example.test"
	if _, err := f.pool.Exec(f.ctx,
		`INSERT INTO accounts (id, normalized_email, email, role, status, display_name, locale, email_verified_at)
		 VALUES ($1::uuid, $2, $2, 'STUDENT', 'ACTIVE', 'Other Watcher', 'en', now())`, other, otherEmail); err != nil {
		t.Fatalf("seeding second Student: %v", err)
	}
	f.seedGrantFor(other, otherEmail)

	mine, err := f.delivery.IssuePlayback(f.ctx, PlaybackRequest{
		StudentID: f.student, LessonID: f.lesson, AssetVersionID: f.video,
	})
	if err != nil {
		t.Fatalf("issuing playback for the first Student: %v", err)
	}
	theirs, err := f.delivery.IssuePlayback(f.ctx, PlaybackRequest{
		StudentID: other, LessonID: f.lesson, AssetVersionID: f.video,
	})
	if err != nil {
		t.Fatalf("issuing playback for the second Student: %v", err)
	}
	if mine.Watermark == nil || theirs.Watermark == nil {
		t.Fatal("one of the two authorizations carried no watermark")
	}
	if mine.Watermark.Code == theirs.Watermark.Code {
		t.Fatalf("both Students share the attribution code %q", mine.Watermark.Code)
	}
	if theirs.Watermark.DisplayName != "Other W." || theirs.Watermark.MaskedIdentifier != "ot***@example.test" {
		t.Fatalf("second Student's watermark=%+v", theirs.Watermark)
	}
}

// The code is what makes a single leaked frame attributable, so it must not
// move between Lessons or between sessions for the same Account.
func TestPlaybackWatermarkCodeIsStableAcrossSessions(t *testing.T) {
	f := newDeliveryFixture(t)

	first, err := f.delivery.IssuePlayback(f.ctx, PlaybackRequest{
		StudentID: f.student, LessonID: f.lesson, AssetVersionID: f.video,
	})
	if err != nil {
		t.Fatalf("issuing the first playback: %v", err)
	}
	second, err := f.delivery.IssuePlayback(f.ctx, PlaybackRequest{
		StudentID: f.student, LessonID: f.lesson, AssetVersionID: f.video,
	})
	if err != nil {
		t.Fatalf("issuing the second playback: %v", err)
	}
	if first.Watermark.Code != second.Watermark.Code {
		t.Fatalf("attribution code changed between sessions: %q then %q",
			first.Watermark.Code, second.Watermark.Code)
	}
}

// The serialized authorization is the exact surface a Student's browser sees.
// Nothing on it may name the Account, the session, or the storage.
func TestSerializedPlaybackAuthorizationLeaksNoInternalIdentifiers(t *testing.T) {
	f := newDeliveryFixture(t)

	issued, err := f.delivery.IssuePlayback(f.ctx, PlaybackRequest{
		StudentID: f.student, LessonID: f.lesson, AssetVersionID: f.video,
	})
	if err != nil {
		t.Fatalf("issuing playback: %v", err)
	}
	encoded, err := json.Marshal(issued)
	if err != nil {
		t.Fatalf("encoding authorization: %v", err)
	}
	watermark, err := json.Marshal(issued.Watermark)
	if err != nil {
		t.Fatalf("encoding watermark: %v", err)
	}

	// The raw Account UUID never appears anywhere on the response.
	if strings.Contains(string(encoded), f.student) {
		t.Fatalf("authorization carried the raw Student identifier: %s", encoded)
	}
	// The full correspondence address never appears on the watermark.
	if strings.Contains(string(watermark), f.studentEmail) {
		t.Fatalf("watermark carried the full email address: %s", watermark)
	}
	// The visible watermark is not a copy of the playback capability.
	if strings.Contains(string(watermark), issued.PlaybackSession) {
		t.Fatalf("watermark carried the playback session token: %s", watermark)
	}
	if strings.Contains(string(watermark), issued.AssetVersionID) {
		t.Fatalf("watermark carried a storage/version identifier: %s", watermark)
	}
	for _, forbidden := range []string{"storage.test", "video/hls", "X-Amz", "buyer_tag"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("authorization carried %q: %s", forbidden, encoded)
		}
	}
}

// Requirement: Admin review playback must not accidentally impersonate a
// Student. The Admin route shares the response type, so absence is asserted on
// the wire as well as on the struct.
func TestAdminReviewPlaybackCarriesNoStudentWatermark(t *testing.T) {
	f := newDeliveryFixture(t)
	admin := uuid.NewString()
	if _, err := f.pool.Exec(f.ctx,
		`UPDATE course_revisions SET state = 'PENDING_REVIEW' WHERE course_id = $1::uuid`, f.courseID); err != nil {
		t.Fatalf("making revision reviewable: %v", err)
	}

	issued, err := f.delivery.IssueAdminReviewPlayback(f.ctx, AdminReviewPlaybackRequest{
		AdminAccountID: admin, CourseID: f.courseID, RevisionID: mustRevisionID(t, f),
		LessonID: f.lesson, AssetVersionID: f.video,
	})
	if err != nil {
		t.Fatalf("issuing admin review playback: %v", err)
	}
	if issued.Watermark != nil {
		t.Fatalf("admin review playback carried a watermark: %+v", issued.Watermark)
	}
	encoded, err := json.Marshal(issued)
	if err != nil {
		t.Fatalf("encoding admin authorization: %v", err)
	}
	if strings.Contains(string(encoded), "watermark") {
		t.Fatalf("admin review response carried a watermark field: %s", encoded)
	}
	// And the existing Admin review contract is otherwise untouched.
	if issued.AssetVersionID != f.video ||
		!strings.HasPrefix(issued.ManifestURL, "/api/v1/admin/review/playback-manifests/") {
		t.Fatalf("admin review issuance changed shape: %+v", issued)
	}
}

// The watermark is built after the entitlement decision, so a denial must still
// produce the one uniform refusal and no identity at all.
func TestDeniedPlaybackIssuesNoWatermark(t *testing.T) {
	f := newDeliveryFixture(t)
	stranger := uuid.NewString()
	if _, err := f.pool.Exec(f.ctx,
		`INSERT INTO accounts (id, normalized_email, email, role, status, display_name, locale, email_verified_at)
		 VALUES ($1::uuid, $2, $2, 'STUDENT', 'ACTIVE', 'Unentitled Student', 'en', now())`,
		stranger, "stranger-d8@example.test"); err != nil {
		t.Fatalf("seeding unentitled Student: %v", err)
	}

	issued, err := f.delivery.IssuePlayback(f.ctx, PlaybackRequest{
		StudentID: stranger, LessonID: f.lesson, AssetVersionID: f.video,
	})
	if err == nil {
		t.Fatal("an unentitled Student received playback")
	}
	if issued.Watermark != nil {
		t.Fatalf("a refused authorization carried a watermark: %+v", issued.Watermark)
	}
	if issued.PlaybackSession != "" || issued.ManifestURL != "" {
		t.Fatalf("a refused authorization carried a capability: %+v", issued)
	}
}

// seedGrantFor mirrors the fixture's own Course grant for a second Student, so
// two Accounts can be authorized against one Lesson through the real path.
func (f *deliveryFixture) seedGrantFor(studentID, email string) {
	f.t.Helper()
	invitationID := uuid.NewString()
	if _, err := f.pool.Exec(f.ctx,
		`INSERT INTO course_access_invitations (id, course_id, email, normalized_email,
		     created_by_account_id, accepted_by_account_id, decided_by_account_id, state)
		 VALUES ($1::uuid, $2::uuid, $3, $3, $4::uuid, $4::uuid, $4::uuid, 'APPROVED')`,
		invitationID, f.courseID, email, studentID); err != nil {
		f.t.Fatalf("seeding invitation for %s: %v", email, err)
	}
	if _, err := f.pool.Exec(f.ctx,
		`INSERT INTO entitlements (id, student_account_id, scope_kind, scope_id, course_id,
		     grant_source, source_invitation_id, original_access_ends_at, access_ends_at,
		     retirement_eligibility_at, state)
		 VALUES (gen_random_uuid(), $1::uuid, 'COURSE', $2::uuid, $2::uuid, 'MANUAL_INVITATION',
		     $3::uuid, $4, $4, $5, 'ACTIVE')`,
		studentID, f.courseID, invitationID, f.now.Add(24*time.Hour), f.now.Add(-time.Hour)); err != nil {
		f.t.Fatalf("seeding entitlement for %s: %v", email, err)
	}
}
