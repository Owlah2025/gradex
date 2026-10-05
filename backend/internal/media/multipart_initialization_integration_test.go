//go:build integration

package media

import (
	"errors"
	"github.com/google/uuid"
	"testing"
)

func TestMultipartCreationResponseLossAndConcurrentRetryReuseOneIntent(t *testing.T) {
	f, store, completion := newMultipartFixture(t)
	req := UploadRequest{ClientRequestID: uuid.NewString(), OwnerAccountID: f.instructorID, CourseID: f.courseID, Kind: KindVideo, ContentType: completion.ContentType, SizeBytes: completion.SizeBytes}
	store.failCreate = true
	if _, err := f.service.BeginMultipartUpload(f.ctx, req); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	tickets := make(chan MultipartUploadTicket, 2)
	failures := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { ticket, err := f.service.BeginMultipartUpload(f.ctx, req); tickets <- ticket; failures <- err }()
	}
	first, second := <-tickets, <-tickets
	for i := 0; i < 2; i++ {
		if err := <-failures; err != nil {
			t.Fatal(err)
		}
	}
	if first.AssetVersionID == "" || first.AssetVersionID != second.AssetVersionID || first.UploadID != second.UploadID || store.creates != 2 {
		t.Fatalf("tickets=%+v %+v creates=%d", first, second, store.creates)
	}
	var versions int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM media_asset_versions`).Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if versions != 2 {
		t.Fatalf("versions=%d; want baseline plus one new intent", versions)
	}
	req.SizeBytes++
	if _, err := f.service.BeginMultipartUpload(f.ctx, req); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed metadata=%v", err)
	}
	req.SizeBytes--
	req.OwnerAccountID = f.adminID
	if _, err := f.service.BeginMultipartUpload(f.ctx, req); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("foreign nonce=%v", err)
	}
}

func TestMultipartLessonBindingAndCancelledQuotaReservations(t *testing.T) {
	f, store, _ := newMultipartFixture(t)
	ownLesson := seedMediaLesson(t, f, f.courseID)
	otherCourse := uuid.NewString()
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO courses(id,owner_account_id,lifecycle) VALUES($1::uuid,$2::uuid,'DRAFT')`, otherCourse, f.instructorID); err != nil {
		t.Fatal(err)
	}
	foreignLesson := seedMediaLesson(t, f, otherCourse)
	f.service.limits.resourceLessonMax = 8
	req := UploadRequest{ClientRequestID: uuid.NewString(), OwnerAccountID: f.instructorID, CourseID: f.courseID, LessonID: foreignLesson, Kind: KindResource, ContentType: "application/pdf", SizeBytes: 8}
	for _, lesson := range []string{foreignLesson, uuid.NewString()} {
		req.LessonID = lesson
		if _, err := f.service.BeginMultipartUpload(f.ctx, req); !errors.Is(err, ErrNotAuthorized) {
			t.Fatalf("lesson=%s err=%v", lesson, err)
		}
	}
	var count int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM media_asset_versions`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("foreign lesson requests created versions")
	}
	req.LessonID = ownLesson
	first, err := f.service.BeginMultipartUpload(f.ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	req.ClientRequestID = uuid.NewString()
	if _, err := f.service.BeginMultipartUpload(f.ctx, req); !errors.Is(err, ErrValidation) {
		t.Fatalf("quota overflow=%v", err)
	}
	if err := f.service.AbortMultipartUpload(f.ctx, MultipartSessionRequest{f.instructorID, first.AssetVersionID}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.BeginMultipartUpload(f.ctx, req); err != nil {
		t.Fatalf("cancelled reservation retained quota: %v", err)
	}
	if store.aborts != 1 {
		t.Fatal("cancellation did not reach provider")
	}
}
