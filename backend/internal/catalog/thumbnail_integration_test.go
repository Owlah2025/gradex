//go:build integration

package catalog

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/catalogpublic"
	"github.com/Owlah2025/gradex/backend/internal/db"
	"github.com/Owlah2025/gradex/backend/internal/media"
	"github.com/google/uuid"
)

type thumbnailTestStore struct {
	objects    map[string][]byte
	deleted    []string
	failDelete bool
}

func (s *thumbnailTestStore) PresignPutSizedURL(context.Context, string, string, int64, time.Duration) (string, error) {
	return "https://storage.test/upload", nil
}
func (s *thumbnailTestStore) PresignPutURL(context.Context, string, string, time.Duration) (string, error) {
	return "https://storage.test/upload", nil
}
func (s *thumbnailTestStore) HeadObjectVersion(_ context.Context, key, version string) (int64, bool, error) {
	b, ok := s.objects[key]
	return int64(len(b)), ok, nil
}
func (s *thumbnailTestStore) DownloadPrefixVersion(_ context.Context, key, version string, limit int64) ([]byte, error) {
	b := s.objects[key]
	if int64(len(b)) > limit {
		b = b[:limit]
	}
	return b, nil
}
func (s *thumbnailTestStore) HashObjectVersion(_ context.Context, key, version string) (string, error) {
	sum := sha256.Sum256(s.objects[key])
	return hex.EncodeToString(sum[:]), nil
}
func (s *thumbnailTestStore) PutObject(_ context.Context, key string, b []byte, _ string) error {
	s.objects[key] = b
	return nil
}
func (s *thumbnailTestStore) DownloadObject(_ context.Context, key string) ([]byte, error) {
	return s.objects[key], nil
}
func (s *thumbnailTestStore) DeleteThumbnailObjects(_ context.Context, course, asset string) error {
	if s.failDelete {
		return errors.New("storage unavailable")
	}
	s.deleted = append(s.deleted, asset)
	for key := range s.objects {
		if strings.Contains(key, asset) {
			delete(s.objects, key)
		}
	}
	return nil
}

func thumbnailService(t *testing.T, f *d5Fixture) (*media.Service, *thumbnailTestStore) {
	t.Helper()
	store := &thumbnailTestStore{objects: map[string][]byte{}}
	unavailable, err := media.NewUnavailableScanner("thumbnail tests use raster validation")
	if err != nil {
		t.Fatal(err)
	}
	scanner, err := media.NewScannerAdapter(unavailable)
	if err != nil {
		t.Fatal(err)
	}
	service, err := media.NewService(media.ServiceOptions{DB: f.p, Store: store, Outbox: testOutboxWriter(t), Scanner: scanner, UploadURLExpiry: 15 * time.Minute, MaxUploadBytes: 10 * 1024 * 1024, OperatingMode: media.OperatingModeTrustedInstructor})
	if err != nil {
		t.Fatal(err)
	}
	return service, store
}

func uploadThumbnail(t *testing.T, f *d5Fixture, s *media.Service, store *thumbnailTestStore, revision, format string) string {
	t.Helper()
	var body []byte
	if format == "webp" {
		var err error
		body, err = os.ReadFile("../media/testdata/thumbnail.webp")
		if err != nil {
			t.Fatal(err)
		}
	} else {
		var b bytes.Buffer
		img := image.NewRGBA(image.Rect(0, 0, 1200, 675))
		var err error
		if format == "png" {
			err = png.Encode(&b, img)
		} else {
			err = jpeg.Encode(&b, img, nil)
		}
		if err != nil {
			t.Fatal(err)
		}
		body = b.Bytes()
	}
	ticket, err := s.BeginUpload(f.ctx, media.UploadRequest{OwnerAccountID: f.ownerID, CourseID: f.courseID, RevisionID: revision, Kind: media.KindThumbnail, ContentType: "image/" + format, SizeBytes: int64(len(body))})
	if err != nil {
		t.Fatal(err)
	}
	store.objects[ticket.StorageObjectKey] = body
	sum := sha256.Sum256(body)
	req := media.CompleteUploadRequest{OwnerAccountID: f.ownerID, AssetVersionID: ticket.AssetVersionID, ProviderEventID: uuid.NewString(), StorageObjectKey: ticket.StorageObjectKey, StorageObjectVersion: "version-1", ContentType: "image/" + format, SizeBytes: int64(len(body)), SHA256Hex: hex.EncodeToString(sum[:])}
	result, err := s.CompleteUpload(f.ctx, req)
	if err != nil || result.State != media.StateReady {
		t.Fatalf("complete: %+v %v", result, err)
	}
	duplicate, err := s.CompleteUpload(f.ctx, req)
	if err != nil || !duplicate.Duplicate {
		t.Fatalf("replay: %+v %v", duplicate, err)
	}
	return ticket.AssetVersionID
}

func TestThumbnailApprovalBoundaryReplacementRemovalAndHistory(t *testing.T) {
	f := newD5Fixture(t)
	service, store := thumbnailService(t, f)
	public, err := catalogpublic.NewRepository(f.p, catalogpublic.PublishedOnly)
	if err != nil {
		t.Fatal(err)
	}
	assertPublic := func(want *string) {
		t.Helper()
		course, err := public.Detail(f.ctx, f.courseID, false)
		if err != nil || course == nil {
			t.Fatalf("public: %v", err)
		}
		if want == nil {
			if course.Thumbnail != nil {
				t.Fatal("candidate thumbnail leaked")
			}
		} else if course.Thumbnail == nil || course.Thumbnail.AssetVersionID != *want {
			t.Fatalf("wrong live thumbnail: %+v", course.Thumbnail)
		}
		for _, filters := range []catalogpublic.Filters{{}, {RelevantProgramSlug: "unmatched-program"}} {
			list, err := public.Browse(f.ctx, false, 1, 100, "", false, filters)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, item := range list.Items {
				if item.ID == f.courseID {
					found = true
					if want == nil && item.Thumbnail != nil {
						t.Fatal("candidate leaked into catalogue list")
					}
					if want != nil && (item.Thumbnail == nil || item.Thumbnail.AssetVersionID != *want) {
						t.Fatal("catalogue ordering lost the live thumbnail")
					}
				}
			}
			if !found {
				t.Fatal("course missing from public catalogue")
			}
		}
	}
	assertPublic(nil)
	candidate := f.candidate(t)
	a := uploadThumbnail(t, f, service, store, candidate.ID, "jpeg")
	if err := db.CheckThumbnailRollbackSafety(f.ctx, f.p); err == nil {
		t.Fatal("rollback guard accepted thumbnail history")
	}
	var dirty bool
	if err := f.p.QueryRow(f.ctx, `SELECT dirty FROM schema_migrations`).Scan(&dirty); err != nil || dirty {
		t.Fatalf("rollback preflight changed schema state: dirty=%t error=%v", dirty, err)
	}
	selectAsset := func(revision string, asset, expected *string) {
		t.Helper()
		_, err := f.repo.SetThumbnail(f.ctx, SetThumbnailRequest{CourseID: f.courseID, RevisionID: revision, OwnerAccountID: f.ownerID, AssetVersionID: asset, ExpectedAssetVersionID: expected}, f.ownerID)
		if err != nil {
			t.Fatal(err)
		}
	}
	selectAsset(candidate.ID, &a, nil)
	assertPublic(nil)
	read := media.ThumbnailReadRequest{CourseID: f.courseID, RevisionID: candidate.ID, AssetVersionID: a, Variant: "card", Public: true}
	if _, err = service.ReadThumbnail(f.ctx, read); !errors.Is(err, media.ErrNotFound) {
		t.Fatalf("candidate public delivery: %v", err)
	}
	read.Public = false
	read.Viewer = media.Viewer{AccountID: f.adminID}
	if body, err := service.ReadThumbnail(f.ctx, read); err != nil || len(body) == 0 {
		t.Fatalf("admin preview: %v", err)
	}
	read.Viewer = media.Viewer{AccountID: uuid.NewString()}
	if _, err = service.ReadThumbnail(f.ctx, read); !errors.Is(err, media.ErrNotFound) {
		t.Fatalf("unauthorized preview: %v", err)
	}
	// D-097: an already-published Course's candidate is never submitted for
	// review, so the boundary that matters is publication, not submission —
	// and up to that moment the candidate thumbnail is still private.
	assertPublic(nil)
	orphan := uploadThumbnail(t, f, service, store, candidate.ID, "jpeg")
	// Replacing the candidate's current selection: the compare-and-set names
	// the cover it expects to displace, so a stale tab cannot win.
	selectAsset(candidate.ID, &orphan, &a)
	b := uploadThumbnail(t, f, service, store, candidate.ID, "png")
	selectAsset(candidate.ID, &b, &orphan)
	if _, err = f.repo.SetThumbnail(f.ctx, SetThumbnailRequest{CourseID: f.courseID, RevisionID: candidate.ID, OwnerAccountID: f.ownerID, AssetVersionID: &a, ExpectedAssetVersionID: nil}, f.ownerID); err == nil {
		t.Fatal("stale selection overwrote replacement")
	}
	for _, stage := range []approvalFailureStage{approvalAfterSupersede, approvalAfterApprove, approvalAfterPointer, approvalAfterAudit, approvalAfterOutbox} {
		if err = f.publish(withApprovalFailure(f.ctx, stage), candidate.ID); err == nil {
			t.Fatalf("failure injection %s succeeded", stage)
		}
		assertPublic(nil)
	}
	if err = f.publish(f.ctx, candidate.ID); err != nil {
		t.Fatal(err)
	}
	assertPublic(&b)
	// A published revision's thumbnail is frozen: the next change belongs to
	// the next candidate.
	if _, err = f.repo.SetThumbnail(f.ctx, SetThumbnailRequest{CourseID: f.courseID, RevisionID: candidate.ID, OwnerAccountID: f.ownerID, ExpectedAssetVersionID: &b}, f.ownerID); err == nil {
		t.Fatal("published thumbnail mutated")
	}
	read.Public = true
	read.AssetVersionID = b
	if body, err := service.ReadThumbnail(f.ctx, read); err != nil || len(body) == 0 {
		t.Fatalf("approved delivery: %v", err)
	}
	next := f.candidate(t)
	if next.ThumbnailAssetVersionID == nil || *next.ThumbnailAssetVersionID != b {
		t.Fatal("candidate did not inherit live thumbnail")
	}
	selectAsset(next.ID, nil, &b)
	assertPublic(&b)
	if err = f.publish(f.ctx, next.ID); err != nil {
		t.Fatal(err)
	}
	assertPublic(nil)
	store.failDelete = true
	future := time.Now().Add(8 * 24 * time.Hour)
	if err = media.CleanupThumbnails(f.ctx, f.p, store, future); err == nil {
		t.Fatal("storage deletion error swallowed")
	}
	store.failDelete = false
	if err = media.CleanupThumbnails(f.ctx, f.p, store, future); err != nil {
		t.Fatal(err)
	}
	// Both covers this candidate discarded before publishing — the first
	// selection and the one that replaced it — are unreferenced and collected.
	// The cover that was actually published is still referenced by the
	// superseded revision, so it survives: history is not deleted.
	deleted := map[string]bool{}
	for _, id := range store.deleted {
		deleted[id] = true
	}
	if len(store.deleted) != 2 || !deleted[a] || !deleted[orphan] {
		t.Fatalf("cleanup deleted the wrong set: %v, want exactly the two discarded candidate covers", store.deleted)
	}
	if deleted[b] {
		t.Fatal("cleanup deleted a cover still referenced by a superseded revision")
	}
}

func TestThumbnailUploadAndAttachmentOwnership(t *testing.T) {
	f := newD5Fixture(t)
	s, store := thumbnailService(t, f)
	candidate := f.candidate(t)
	a := uploadThumbnail(t, f, s, store, candidate.ID, "webp")
	otherOwner := uuid.NewString()
	if _, err := f.p.Exec(f.ctx, `INSERT INTO accounts(id,normalized_email,email,role,status,display_name) VALUES($1::uuid,$2,$2,'INSTRUCTOR','ACTIVE','Other Instructor')`, otherOwner, otherOwner+"@example.test"); err != nil {
		t.Fatal(err)
	}
	otherCourse, err := f.repo.CreateCourse(f.ctx, CreateCourseRequest{OwnerAccountID: otherOwner, TitleAr: "مقرر آخر", TitleEn: "Other course"}, otherOwner)
	if err != nil {
		t.Fatal(err)
	}
	otherFixture := *f
	otherFixture.courseID = otherCourse.ID
	otherFixture.ownerID = otherOwner
	foreignAsset := uploadThumbnail(t, &otherFixture, s, store, otherCourse.EditableRevision.ID, "png")
	unfinished, err := s.BeginUpload(f.ctx, media.UploadRequest{OwnerAccountID: f.ownerID, CourseID: f.courseID, RevisionID: candidate.ID, Kind: media.KindThumbnail, ContentType: "image/png", SizeBytes: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, req := range []SetThumbnailRequest{
		{CourseID: f.courseID, RevisionID: candidate.ID, OwnerAccountID: f.adminID, AssetVersionID: &a},
		{CourseID: f.courseID, RevisionID: f.liveID, OwnerAccountID: f.ownerID, AssetVersionID: &a},
		{CourseID: f.courseID, RevisionID: candidate.ID, OwnerAccountID: f.ownerID, AssetVersionID: &f.previewOld},
		{CourseID: f.courseID, RevisionID: candidate.ID, OwnerAccountID: f.ownerID, AssetVersionID: &foreignAsset},
		{CourseID: f.courseID, RevisionID: candidate.ID, OwnerAccountID: f.ownerID, AssetVersionID: &unfinished.AssetVersionID},
		{CourseID: otherCourse.ID, RevisionID: otherCourse.EditableRevision.ID, OwnerAccountID: f.ownerID, AssetVersionID: &foreignAsset},
	} {
		if _, err := f.repo.SetThumbnail(f.ctx, req, f.ownerID); err == nil {
			t.Fatalf("invalid attachment accepted: %+v", req)
		}
	}
	_, err = s.BeginUpload(f.ctx, media.UploadRequest{OwnerAccountID: f.adminID, CourseID: f.courseID, RevisionID: candidate.ID, Kind: media.KindThumbnail, ContentType: "image/png", SizeBytes: 100})
	if !errors.Is(err, media.ErrNotAuthorized) {
		t.Fatalf("foreign upload: %v", err)
	}
	for _, req := range []media.UploadRequest{
		{OwnerAccountID: f.ownerID, CourseID: f.courseID, RevisionID: f.liveID, Kind: media.KindThumbnail, ContentType: "image/png", SizeBytes: 100},
		{OwnerAccountID: f.ownerID, CourseID: f.courseID, RevisionID: candidate.ID, Kind: media.KindThumbnail, ContentType: "image/svg+xml", SizeBytes: 100},
		{OwnerAccountID: f.ownerID, CourseID: f.courseID, RevisionID: candidate.ID, Kind: media.KindThumbnail, ContentType: "image/png", SizeBytes: media.ThumbnailMaxBytes + 1},
	} {
		if _, err := s.BeginUpload(f.ctx, req); err == nil {
			t.Fatalf("invalid intent accepted: %+v", req)
		}
	}
}
