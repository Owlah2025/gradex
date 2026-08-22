//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/health"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/logging"
	"github.com/Owlah2025/gradex/backend/internal/media"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

type d064PipelineStore struct{ *learningIntegrationStore }

func (*d064PipelineStore) PresignPutURL(context.Context, string, string, time.Duration) (string, error) {
	return "", errors.New("unexpected upload signing")
}
func (*d064PipelineStore) HeadObjectVersion(context.Context, string, string) (int64, bool, error) {
	return 0, false, errors.New("unexpected object head")
}
func (*d064PipelineStore) DownloadPrefixVersion(context.Context, string, string, int64) ([]byte, error) {
	return nil, errors.New("unexpected object download")
}
func (*d064PipelineStore) HashObjectVersion(context.Context, string, string) (string, error) {
	return "", errors.New("unexpected object hash")
}

func seedD064Resource(t *testing.T, f learningIntegrationFixture) string {
	t.Helper()
	ctx := context.Background()
	var lessonRow, ownerID string
	if err := f.pool.QueryRow(ctx, `SELECT cl.id::text, c.owner_account_id::text FROM course_lessons cl JOIN courses c ON c.id = cl.course_id WHERE cl.lesson_identity_id = $1::uuid`, f.lessonID).Scan(&lessonRow, &ownerID); err != nil {
		t.Fatalf("resolving current lesson row: %v", err)
	}
	assetID, versionID, scanID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO media_assets (id, kind, owner_account_id, course_id, lesson_id, visibility) VALUES ($1::uuid, 'RESOURCE', $2::uuid, $3::uuid, $4::uuid, 'PROTECTED')`, []any{assetID, ownerID, f.courseID, f.lessonID}},
		{`INSERT INTO media_asset_versions (id, logical_asset_id, kind, state, storage_object_key, storage_object_version, content_type, size_bytes) VALUES ($1::uuid, $2::uuid, 'RESOURCE', 'QUARANTINED', 'resource/d064', 'v1', 'application/pdf', 12)`, []any{versionID, assetID}},
		{`INSERT INTO scan_attempts (id, asset_version_id, attempt_number, work_id, storage_object_version, outcome, scanner_identity) VALUES ($1::uuid, $2::uuid, 1, $3, 'v1', 'PASSED', 'd064-fixture')`, []any{scanID, versionID, "scan:" + versionID}},
		{`UPDATE media_asset_versions SET state = 'SCANNING' WHERE id = $1::uuid`, []any{versionID}},
		{`UPDATE media_asset_versions SET successful_scan_attempt_id = $1::uuid, state = 'SCAN_PASSED' WHERE id = $2::uuid`, []any{scanID, versionID}},
		{`UPDATE media_asset_versions SET state = 'READY' WHERE id = $1::uuid`, []any{versionID}},
		{`INSERT INTO lesson_files (lesson_id, kind, asset_version_id, display_name_ar, display_name_en, position) VALUES ($1::uuid, 'RESOURCE', $2::uuid, 'مرجع', 'Resource', 0)`, []any{lessonRow, versionID}},
	} {
		if _, err := f.pool.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seeding D-064 resource: %v", err)
		}
	}
	return versionID
}

func TestD064ProductionMediaRouterRedirectsThenRereadsAuthority(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	versionID := seedD064Resource(t, f)
	store := &d064PipelineStore{learningIntegrationStore: f.store}
	writer, err := outbox.NewWriter("d064-http", []byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatalf("creating media outbox: %v", err)
	}
	scannerSource, err := media.NewUnavailableScanner("D-064 integration scanner is not used")
	if err != nil {
		t.Fatalf("creating scanner source: %v", err)
	}
	scanner, err := media.NewScannerAdapter(scannerSource)
	if err != nil {
		t.Fatalf("creating scanner adapter: %v", err)
	}
	service, err := media.NewService(media.ServiceOptions{DB: f.pool, Store: store, Outbox: writer, Scanner: scanner, UploadURLExpiry: time.Minute, MaxUploadBytes: 1024})
	if err != nil {
		t.Fatalf("creating media service: %v", err)
	}
	delivery, err := media.NewDeliveryService(media.DeliveryOptions{
		DB: f.pool, Store: f.store, Evaluator: f.evaluator,
		SignatureLifetime: time.Minute, BuyerTagKey: []byte("01234567890123456789012345678901"), Now: f.clock.Now,
	})
	if err != nil {
		t.Fatalf("creating D-064 delivery: %v", err)
	}
	mediaFoundation, err := NewMediaFoundation(MediaFoundationOptions{Service: service, Delivery: delivery})
	if err != nil {
		t.Fatalf("creating media foundation: %v", err)
	}
	cfg, err := config.LoadFrom(config.MapLookup(map[string]string{
		"APP_ENV": "development", "REDIS_ADDR": "localhost:6379", "S3_ENDPOINT": "http://localhost:9000", "S3_BUCKET": "gradex-test",
	}), config.MapSecretResolver{
		"DATABASE_URL": "postgres://x", "S3_ACCESS_KEY": "a", "S3_SECRET_KEY": "b", "PLAYBACK_TOKEN_SECRET": "c",
	})
	if err != nil {
		t.Fatalf("loading integration configuration: %v", err)
	}
	router, err := NewRouter(cfg, logging.New(&syncBuffer{}, "d064-test", "development", logging.LevelFromString("info")), health.New(time.Second), learningIntegrationAuth{studentID: f.studentID}, identity.NewDBPrincipalResolver(f.pool), WithMediaFoundation(mediaFoundation), WithLearningFoundation(f.foundation))
	if err != nil {
		t.Fatalf("composing production media and learning router: %v", err)
	}
	path := "/api/v1/media/lessons/" + f.lessonID + "/materials/resource"
	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodGet, path, nil))
	if first.Code != http.StatusFound || first.Header().Get("Location") == "" || first.Header().Get("Cache-Control") != "no-store" || first.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("authorized entry status=%d headers=%v body=%q", first.Code, first.Header(), first.Body.String())
	}
	if first.Body.Len() != 0 || strings.Contains(first.Header().Get("Location"), versionID) {
		t.Fatalf("entry leaked non-redirect data: headers=%v body=%q", first.Header(), first.Body.String())
	}
	assertDenied := func(t *testing.T) {
		t.Helper()
		denied := httptest.NewRecorder()
		router.ServeHTTP(denied, httptest.NewRequest(http.MethodGet, path, nil))
		if denied.Code != http.StatusNotFound || denied.Header().Get("Location") != "" || denied.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("denied entry status=%d headers=%v body=%q", denied.Code, denied.Header(), denied.Body.String())
		}
	}
	t.Run("account suspension", func(t *testing.T) {
		if _, err := f.pool.Exec(context.Background(), `UPDATE accounts SET status = 'SUSPENDED' WHERE id = $1::uuid`, f.studentID); err != nil {
			t.Fatalf("suspending D-064 account: %v", err)
		}
		assertDenied(t)
	})
	if _, err := f.pool.Exec(context.Background(), `UPDATE accounts SET status = 'ACTIVE' WHERE id = $1::uuid`, f.studentID); err != nil {
		t.Fatalf("restoring D-064 account fixture: %v", err)
	}
	t.Run("entitlement expiry", func(t *testing.T) {
		if _, err := f.pool.Exec(context.Background(), `UPDATE entitlements SET access_ends_at = $1 WHERE student_account_id = $2::uuid`, f.clock.Now(), f.studentID); err != nil {
			t.Fatalf("expiring D-064 entitlement: %v", err)
		}
		assertDenied(t)
	})
}

func TestD064LearningReadModelsExposeActiveKindsAndHideRetainedExpiryActions(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	versionID := seedD064Resource(t, f)
	seedD064CandidateOnlyResource(t, f, versionID)
	headers := map[string]string{"Accept-Language": "en"}
	home := f.requestWithHeaders(http.MethodGet, "/api/v1/learn/courses/"+f.courseID, "", headers)
	assertReadSuccess(t, home)
	var homeBody courseHomeResponse
	if err := json.Unmarshal(home.Body.Bytes(), &homeBody); err != nil {
		t.Fatalf("decoding active Course Home: %v", err)
	}
	if homeBody.LearningStatus != learningStatusActive {
		t.Fatalf("Course Home learning status = %q, want active", homeBody.LearningStatus)
	}
	homeLesson := d064CourseHomeLesson(t, homeBody, f.lessonID)
	assertD064ActiveResource(t, "Course Home", homeLesson.Resources, homeLesson.LabMaterials, f.courseID, f.lessonID)

	lesson := f.requestWithHeaders(http.MethodGet, "/api/v1/learn/courses/"+f.courseID+"/lessons/"+f.lessonID, "", headers)
	assertReadSuccess(t, lesson)
	var lessonBody lessonResponse
	if err := json.Unmarshal(lesson.Body.Bytes(), &lessonBody); err != nil {
		t.Fatalf("decoding active Lesson: %v", err)
	}
	if lessonBody.LearningStatus != learningStatusActive || lessonBody.CourseID != f.courseID || lessonBody.LessonID != f.lessonID {
		t.Fatalf("active Lesson projection = %+v, want active %s/%s", lessonBody, f.courseID, f.lessonID)
	}
	assertD064ActiveResource(t, "Lesson", lessonBody.Resources, lessonBody.LabMaterials, f.courseID, f.lessonID)
	if got := f.store.callCount(); got != 0 {
		t.Fatalf("read-model rendering signed %d protected targets", got)
	}
	if _, err := f.pool.Exec(context.Background(), `UPDATE entitlements SET access_ends_at = $1 WHERE student_account_id = $2::uuid`, f.clock.Now(), f.studentID); err != nil {
		t.Fatalf("expiring read-model entitlement: %v", err)
	}
	expiredHome := f.requestWithHeaders(http.MethodGet, "/api/v1/learn/courses/"+f.courseID, "", headers)
	assertReadSuccess(t, expiredHome)
	var expiredHomeBody courseHomeResponse
	if err := json.Unmarshal(expiredHome.Body.Bytes(), &expiredHomeBody); err != nil {
		t.Fatalf("decoding expired Course Home: %v", err)
	}
	if expiredHomeBody.LearningStatus != learningStatusExpired {
		t.Fatalf("expired Course Home status = %q, want expired", expiredHomeBody.LearningStatus)
	}
	expiredHomeLesson := d064CourseHomeLesson(t, expiredHomeBody, f.lessonID)
	if len(expiredHomeLesson.Resources) != 0 || len(expiredHomeLesson.LabMaterials) != 0 {
		t.Fatalf("expired Course Home exposes materials: %+v", expiredHomeLesson)
	}

	expiredLesson := f.requestWithHeaders(http.MethodGet, "/api/v1/learn/courses/"+f.courseID+"/lessons/"+f.lessonID, "", headers)
	assertReadSuccess(t, expiredLesson)
	var expiredLessonBody lessonResponse
	if err := json.Unmarshal(expiredLesson.Body.Bytes(), &expiredLessonBody); err != nil {
		t.Fatalf("decoding expired Lesson: %v", err)
	}
	if expiredLessonBody.LearningStatus != learningStatusExpired || len(expiredLessonBody.Resources) != 0 || len(expiredLessonBody.LabMaterials) != 0 {
		t.Fatalf("expired Lesson projection = %+v, want expired without materials", expiredLessonBody)
	}
}

func seedD064CandidateOnlyResource(t *testing.T, f learningIntegrationFixture, versionID string) {
	t.Helper()
	ctx := context.Background()
	var sectionIdentityID string
	if err := f.pool.QueryRow(ctx, `SELECT section_identity_id::text FROM course_lesson_identities WHERE id = $1::uuid`, f.lessonID).Scan(&sectionIdentityID); err != nil {
		t.Fatalf("resolving D-064 stable Section: %v", err)
	}
	candidateRevisionID, candidateSectionID, candidateLessonID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO course_revisions (id, course_id, state, revision_number, title_ar, title_en) VALUES ($1::uuid, $2::uuid, 'DRAFT', 2, 'مسودة', 'Draft')`, []any{candidateRevisionID, f.courseID}},
		{`INSERT INTO course_sections (id, revision_id, course_id, section_identity_id, title_ar, title_en, position) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'قسم مسودة', 'Draft Section', 0)`, []any{candidateSectionID, candidateRevisionID, f.courseID, sectionIdentityID}},
		{`INSERT INTO course_lessons (id, section_id, course_id, section_identity_id, lesson_identity_id, title_ar, title_en, position) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, 'درس مسودة', 'Draft Lesson', 0)`, []any{candidateLessonID, candidateSectionID, f.courseID, sectionIdentityID, f.lessonID}},
		{`INSERT INTO lesson_files (lesson_id, kind, asset_version_id, display_name_ar, display_name_en, position) VALUES ($1::uuid, 'RESOURCE', $2::uuid, 'مرجع مرشح', 'Candidate-only Resource', 0)`, []any{candidateLessonID, versionID}},
	} {
		if _, err := f.pool.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seeding D-064 candidate-only resource: %v", err)
		}
	}
}

// ST15-4: the protected learning contract deliberately separates Resources
// from Lab Materials. This proves the active Resource identity and delivery
// handle without accepting a foreign or differently typed material in its place.
func assertD064ActiveResource(t *testing.T, surface string, resources, labMaterials []learningMaterialResponse, courseID, lessonID string) {
	t.Helper()
	if len(resources) != 1 || len(labMaterials) != 0 {
		t.Fatalf("%s materials = resources:%+v lab_materials:%+v, want one Resource and no Lab Materials", surface, resources, labMaterials)
	}
	resource := resources[0]
	pathPrefix := "/media/courses/" + courseID + "/lessons/" + lessonID + "/materials/"
	if resource.Title != "Resource" || resource.FileType != "PDF" || resource.SizeBytes != 12 || !strings.HasPrefix(resource.DownloadAuthorizationPath, pathPrefix) {
		t.Fatalf("%s Resource = %+v, want Resource PDF (12 bytes) authorized under %q", surface, resource, pathPrefix)
	}
}

func d064CourseHomeLesson(t *testing.T, home courseHomeResponse, lessonID string) courseHomeLessonResponse {
	t.Helper()
	var found *courseHomeLessonResponse
	for _, section := range home.Sections {
		for _, lesson := range section.Lessons {
			if lesson.LessonID != lessonID {
				continue
			}
			if found != nil {
				t.Fatalf("Course Home repeats Lesson %s", lessonID)
			}
			copy := lesson
			found = &copy
		}
	}
	if found == nil {
		t.Fatalf("Course Home does not expose Lesson %s: %+v", lessonID, home.Sections)
	}
	return *found
}
