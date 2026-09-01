//go:build integration

package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/entitlement"
	"github.com/Owlah2025/gradex/backend/internal/health"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/learning"
	"github.com/Owlah2025/gradex/backend/internal/logging"
	"github.com/Owlah2025/gradex/backend/internal/media"
	"github.com/Owlah2025/gradex/backend/internal/ratelimit"
)

type learningIntegrationStore struct {
	mu    sync.Mutex
	calls int
}

func (s *learningIntegrationStore) PresignGetURL(_ context.Context, key string, _ time.Duration) (string, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	return "https://storage.example.test/" + key, nil
}

func (*learningIntegrationStore) DownloadObject(context.Context, string) ([]byte, error) {
	return []byte("#EXTM3U\n#EXTINF:6,\nsegment000.ts\n#EXT-X-ENDLIST\n"), nil
}

func (s *learningIntegrationStore) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

type learningIntegrationRateStore struct{}

func (learningIntegrationRateStore) Decide(context.Context, []ratelimit.Entry) (bool, error) {
	return true, nil
}

// learningIntegrationAuth publishes the authenticated Account and its session
// exactly as the production authenticators do. sessionID overrides the derived
// value so a test can present the same Student in a second session.
type learningIntegrationAuth struct {
	studentID string
	sessionID string
}

func (a learningIntegrationAuth) UserFromRequest(c *gin.Context) (string, error) {
	session := a.sessionID
	if session == "" {
		session = "test-session-" + a.studentID
	}
	c.Set("authenticated_session", identity.Session{ID: session, AccountID: a.studentID, State: identity.SessionActive})
	return a.studentID, nil
}

type learningIntegrationClock struct{ now time.Time }

func (c *learningIntegrationClock) Now() time.Time { return c.now.UTC() }

type learningQueryCounts struct {
	mu     sync.Mutex
	counts map[string]int
}

func (c *learningQueryCounts) add(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.counts[name]++
}

func (c *learningQueryCounts) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.counts = make(map[string]int)
}

func (c *learningQueryCounts) get(name string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.counts[name]
}

// learningIntegrationEvaluator records calls while delegating every decision
// to the real S4 evaluator. It makes the T034 handler-local evaluation
// observable without replacing the authoritative policy in a production-route
// test.
type learningIntegrationEvaluator struct {
	delegate *entitlement.Evaluator
	mu       sync.Mutex
	evaluate int
	tx       int
	target   int
}

func (e *learningIntegrationEvaluator) Evaluate(ctx context.Context, studentID, lessonID string, now time.Time) entitlement.Decision {
	e.mu.Lock()
	e.evaluate++
	e.mu.Unlock()
	return e.delegate.Evaluate(ctx, studentID, lessonID, now)
}

func (e *learningIntegrationEvaluator) EvaluateInTransaction(ctx context.Context, tx pgx.Tx, studentID, lessonID string, now time.Time) entitlement.Decision {
	e.mu.Lock()
	e.tx++
	e.mu.Unlock()
	return e.delegate.EvaluateInTransaction(ctx, tx, studentID, lessonID, now)
}

func (e *learningIntegrationEvaluator) EvaluateRead(ctx context.Context, studentID, lessonID string, now time.Time) entitlement.ReadDecision {
	e.mu.Lock()
	e.evaluate++
	e.mu.Unlock()
	return e.delegate.EvaluateRead(ctx, studentID, lessonID, now)
}

func (e *learningIntegrationEvaluator) EvaluateCourseReads(ctx context.Context, studentID string, now time.Time) (map[string]entitlement.ReadDecision, error) {
	e.mu.Lock()
	e.evaluate++
	e.mu.Unlock()
	return e.delegate.EvaluateCourseReads(ctx, studentID, now)
}

func (e *learningIntegrationEvaluator) EvaluateTarget(ctx context.Context, studentID, lessonID string, retiredAt *time.Time, now time.Time) entitlement.Decision {
	e.mu.Lock()
	e.target++
	e.mu.Unlock()
	return e.delegate.EvaluateTarget(ctx, studentID, lessonID, retiredAt, now)
}

func (e *learningIntegrationEvaluator) counts() (evaluate, tx, target int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.evaluate, e.tx, e.target
}

type learningIntegrationFixture struct {
	pool       *pgxpool.Pool
	repository *learning.Repository
	foundation *LearningFoundation
	router     *gin.Engine
	studentID  string
	courseID   string
	lessonID   string
	versionID  string
	clock      *learningIntegrationClock
	store      *learningIntegrationStore
	evaluator  *learningIntegrationEvaluator
	queries    *learningQueryCounts
	// logs captures the production logger's output so a test can hold the
	// operational record to the same disclosure boundary as the response.
	logs *syncBuffer
}

// learningFixtureOptions varies only what a test needs to vary. The zero value
// reproduces the accepted fixture exactly: an always-allowing rate store and
// the session derived from the Student.
type learningFixtureOptions struct {
	// rateStore replaces the always-allowing store, so a test can exercise the
	// production limiter against a real backend.
	rateStore ratelimit.Store
	// sessionID presents the same Student in a different authenticated session.
	sessionID string
	// studentID, courseID, and lessonID reuse an existing seeded graph instead
	// of creating a new one.
	studentID string
	pool      *pgxpool.Pool
}

func newLearningIntegrationFixture(t *testing.T) learningIntegrationFixture {
	t.Helper()
	return newLearningIntegrationFixtureWith(t, learningFixtureOptions{})
}

func newLearningIntegrationFixtureWith(t *testing.T, options learningFixtureOptions) learningIntegrationFixture {
	t.Helper()
	pool := options.pool
	if pool == nil {
		pool = freshHTTPAdmissionPool(t)
	}
	ctx := context.Background()
	student := options.studentID
	if student == "" {
		student = uuid.NewString()
	}
	f := learningIntegrationFixture{
		pool: pool, studentID: student, courseID: uuid.NewString(), lessonID: uuid.NewString(), versionID: uuid.NewString(),
		clock:   &learningIntegrationClock{now: time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)},
		queries: &learningQueryCounts{counts: make(map[string]int)},
	}
	seedLearningIntegrationGraph(t, ctx, f, options.studentID != "")
	observe := f.queries.add
	repository, err := learning.NewRepositoryWithQueryObserver(pool, observe)
	if err != nil {
		t.Fatalf("constructing learning repository: %v", err)
	}
	entitlementRepository, err := entitlement.NewRepositoryWithQueryObserver(pool, observe)
	if err != nil {
		t.Fatalf("constructing entitlement repository: %v", err)
	}
	evaluator, err := entitlement.NewEvaluator(entitlementRepository)
	if err != nil {
		t.Fatalf("constructing entitlement evaluator: %v", err)
	}
	store := &learningIntegrationStore{}
	recordingEvaluator := &learningIntegrationEvaluator{delegate: evaluator}
	delivery, err := media.NewDeliveryService(media.DeliveryOptions{
		DB: pool, Store: store, Evaluator: recordingEvaluator,
		SignatureLifetime: time.Minute, BuyerTagKey: bytes.Repeat([]byte{0x44}, 32), Now: f.clock.Now,
	})
	if err != nil {
		t.Fatalf("constructing production media delivery: %v", err)
	}
	var rateStore ratelimit.Store = learningIntegrationRateStore{}
	if options.rateStore != nil {
		rateStore = options.rateStore
	}
	limiter, err := ratelimit.New(rateStore, bytes.Repeat([]byte{0x45}, 32), time.Second)
	if err != nil {
		t.Fatalf("constructing learning limiter: %v", err)
	}
	foundation, err := NewLearningFoundation(LearningFoundationOptions{
		ReportContexts: testReportContextIssuer(t),
		Repository:     repository, Evaluator: recordingEvaluator, Media: delivery, Limiter: limiter,
		Now: f.clock.Now,
		Policies: map[string]ratelimit.Policy{
			"learning-progress-source": ratelimit.ProtectedLearningProgressSourcePolicy(),
			"learning-progress":        ratelimit.ProtectedLearningProgressPolicy(),
			"learning-report":          ratelimit.ProtectedLearningReportPolicy(),
			// Every endpoint in requiredLearningPolicyEndpoints must be present or the
			// foundation refuses to construct, so the playback ceilings belong here too.
			"learning-playback-source": ratelimit.ProtectedLearningPlaybackSourcePolicy(),
			"learning-playback":        ratelimit.ProtectedLearningPlaybackPolicy(),
		},
	})
	if err != nil {
		t.Fatalf("constructing production learning foundation: %v", err)
	}
	cfg, err := config.LoadFrom(config.MapLookup(map[string]string{
		"APP_ENV": "development", "REDIS_ADDR": "localhost:6379", "S3_ENDPOINT": "http://localhost:9000", "S3_BUCKET": "gradex-test",
	}), config.MapSecretResolver{
		"DATABASE_URL": "postgres://x", "S3_ACCESS_KEY": "a", "S3_SECRET_KEY": "b", "PLAYBACK_TOKEN_SECRET": "c",
	})
	if err != nil {
		t.Fatalf("loading integration configuration: %v", err)
	}
	logs := &syncBuffer{}
	router, err := NewRouter(
		cfg, logging.New(logs, "gradex-api-test", "development", logging.LevelFromString("info")), health.New(time.Second),
		learningIntegrationAuth{studentID: f.studentID, sessionID: options.sessionID}, identity.NewDBPrincipalResolver(pool), WithLearningFoundation(foundation),
	)
	if err != nil {
		t.Fatalf("constructing production learning router: %v", err)
	}
	f.repository, f.foundation, f.router, f.store, f.evaluator, f.logs = repository, foundation, router, store, recordingEvaluator, logs
	return f
}

// seedLearningIntegrationGraph builds one Course graph the Student is enrolled
// in and entitled to. reuseStudent keeps an already-seeded Account, so a second
// Course can be attached to the same Student in the same database.
func seedLearningIntegrationGraph(t *testing.T, ctx context.Context, f learningIntegrationFixture, reuseStudent bool) {
	t.Helper()
	instructorID, revisionID, sectionIdentityID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	sectionRowID, lessonRowID := uuid.NewString(), uuid.NewString()
	accounts := []struct{ id, email, role string }{{instructorID, "s5-instructor-" + instructorID + "@example.test", "INSTRUCTOR"}}
	if !reuseStudent {
		accounts = append(accounts, struct{ id, email, role string }{f.studentID, "s5-student-" + f.studentID + "@example.test", "STUDENT"})
	}
	for _, account := range accounts {
		if _, err := f.pool.Exec(ctx, `INSERT INTO accounts (id, normalized_email, email, role, status, display_name) VALUES ($1::uuid, $2, $2, $3, 'ACTIVE', 'S5 integration')`, account.id, account.email, account.role); err != nil {
			t.Fatalf("seeding account: %v", err)
		}
	}
	if !reuseStudent {
		if _, err := f.pool.Exec(ctx, `INSERT INTO password_credentials (account_id, password_hash, state) VALUES ($1::uuid, '$argon2id$fixture', 'ACTIVE')`, f.studentID); err != nil {
			t.Fatalf("seeding active student credential: %v", err)
		}
	}
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO courses (id, owner_account_id, lifecycle) VALUES ($1::uuid, $2::uuid, 'DRAFT')`, []any{f.courseID, instructorID}},
		{`INSERT INTO course_revisions (id, course_id, state, revision_number, title_ar, title_en) VALUES ($1::uuid, $2::uuid, 'APPROVED', 1, 'دورة', 'Course')`, []any{revisionID, f.courseID}},
		{`INSERT INTO course_section_identities (id, course_id) VALUES ($1::uuid, $2::uuid)`, []any{sectionIdentityID, f.courseID}},
		{`INSERT INTO course_lesson_identities (id, course_id, section_identity_id) VALUES ($1::uuid, $2::uuid, $3::uuid)`, []any{f.lessonID, f.courseID, sectionIdentityID}},
		{`INSERT INTO course_sections (id, revision_id, course_id, section_identity_id, title_ar, title_en, position) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'قسم', 'Section', 0)`, []any{sectionRowID, revisionID, f.courseID, sectionIdentityID}},
		{`INSERT INTO course_lessons (id, section_id, course_id, section_identity_id, lesson_identity_id, title_ar, title_en, position) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, 'درس', 'Lesson', 0)`, []any{lessonRowID, sectionRowID, f.courseID, sectionIdentityID, f.lessonID}},
		{`INSERT INTO enrollments (student_account_id, course_id) VALUES ($1::uuid, $2::uuid)`, []any{f.studentID, f.courseID}},
	}
	for _, statement := range statements {
		if _, err := f.pool.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seeding learning graph: %v", err)
		}
	}
	assetID, scanID, processingID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	if _, err := f.pool.Exec(ctx, `INSERT INTO media_assets (id, kind, owner_account_id, course_id, lesson_id, visibility) VALUES ($1::uuid, 'VIDEO', $2::uuid, $3::uuid, $4::uuid, 'PROTECTED')`, assetID, instructorID, f.courseID, f.lessonID); err != nil {
		t.Fatalf("seeding video asset: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO media_asset_versions (id, logical_asset_id, kind, state, storage_object_key, storage_object_version, content_type, size_bytes) VALUES ($1::uuid, $2::uuid, 'VIDEO', 'QUARANTINED', 'quarantine/s5-video-'||$3, $3, 'video/mp4', 1)`, f.versionID, assetID, f.versionID); err != nil {
		t.Fatalf("seeding video version: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO scan_attempts (id, asset_version_id, attempt_number, work_id, storage_object_version, outcome, scanner_identity) VALUES ($1::uuid, $2::uuid, 1, 'scan:s5-video-'||$3, $3, 'PASSED', 'fixture')`, scanID, f.versionID, f.versionID); err != nil {
		t.Fatalf("seeding video scan: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE media_asset_versions SET state = 'SCANNING' WHERE id = $1::uuid`, f.versionID); err != nil {
		t.Fatalf("starting scan state: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE media_asset_versions SET successful_scan_attempt_id = $1::uuid, state = 'SCAN_PASSED' WHERE id = $2::uuid`, scanID, f.versionID); err != nil {
		t.Fatalf("passing scan state: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO processing_attempts (id, asset_version_id, operation_id, state, output_prefix, rendition_count, trusted_duration_ms) VALUES ($1::uuid, $2::uuid, 'process:s5-video-'||$3, 'SUCCEEDED', 'video/s5-'||$3, 1, 60000)`, processingID, f.versionID, f.versionID); err != nil {
		t.Fatalf("seeding video processing: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO video_renditions (asset_version_id, name, storage_object_key, width, height, bitrate_kbps, duration_ms) VALUES ($1::uuid, '720p', 'video/s5-'||$2||'/playlist.m3u8', 1280, 720, 2800, 60000)`, f.versionID, f.versionID); err != nil {
		t.Fatalf("seeding video rendition: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE media_asset_versions SET state = 'PROCESSING' WHERE id = $1::uuid`, f.versionID); err != nil {
		t.Fatalf("starting processing state: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE media_asset_versions SET successful_processing_attempt_id = $1::uuid, trusted_duration_ms = 60000, state = 'READY' WHERE id = $2::uuid`, processingID, f.versionID); err != nil {
		t.Fatalf("readying video: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE course_lessons SET video_asset_version_id = $1::uuid WHERE id = $2::uuid`, f.versionID, lessonRowID); err != nil {
		t.Fatalf("binding video to lesson: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE courses SET live_revision_id = $1::uuid, lifecycle = 'PUBLISHED' WHERE id = $2::uuid`, revisionID, f.courseID); err != nil {
		t.Fatalf("publishing course: %v", err)
	}
	invID := uuid.NewString()
	if _, err := f.pool.Exec(ctx, `INSERT INTO course_access_invitations (id, course_id, email, normalized_email, created_by_account_id, accepted_by_account_id, decided_by_account_id, state) VALUES ($1::uuid, $2::uuid, 'student@example.com', 'student@example.com', $3::uuid, $3::uuid, $3::uuid, 'APPROVED')`, invID, f.courseID, f.studentID); err != nil {
		t.Fatalf("seeding invitation: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO entitlements (id, student_account_id, scope_kind, scope_id, course_id, grant_source, source_invitation_id, original_access_ends_at, access_ends_at, retirement_eligibility_at, state) VALUES ($1::uuid, $2::uuid, 'COURSE', $3::uuid, $3::uuid, 'MANUAL_INVITATION', $4::uuid, $5, $5, $6, 'ACTIVE')`, uuid.NewString(), f.studentID, f.courseID, invID, f.clock.Now().Add(time.Hour), f.clock.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("seeding entitlement: %v", err)
	}
}

func (f learningIntegrationFixture) request(method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	f.router.ServeHTTP(response, request)
	return response
}

func (f learningIntegrationFixture) replaceLiveRevision(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	var sectionIdentityID string
	if err := f.pool.QueryRow(ctx, `SELECT section_identity_id::text FROM course_lesson_identities WHERE id = $1::uuid`, f.lessonID).Scan(&sectionIdentityID); err != nil {
		t.Fatalf("resolving stable section identity: %v", err)
	}
	revisionID, sectionRowID := uuid.NewString(), uuid.NewString()
	if _, err := f.pool.Exec(ctx, `INSERT INTO course_revisions (id, course_id, state, revision_number, title_ar, title_en) VALUES ($1::uuid, $2::uuid, 'APPROVED', 2, 'دورة محدثة', 'Updated Course')`, revisionID, f.courseID); err != nil {
		t.Fatalf("creating replacement revision: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO course_sections (id, revision_id, course_id, section_identity_id, title_ar, title_en, position) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'قسم محدث', 'Updated Section', 0)`, sectionRowID, revisionID, f.courseID, sectionIdentityID); err != nil {
		t.Fatalf("creating replacement section: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO course_lessons (id, section_id, course_id, section_identity_id, lesson_identity_id, title_ar, title_en, position, video_asset_version_id) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, 'درس محدث', 'Updated Lesson', 0, $6::uuid)`, uuid.NewString(), sectionRowID, f.courseID, sectionIdentityID, f.lessonID, f.versionID); err != nil {
		t.Fatalf("creating replacement lesson: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE courses SET live_revision_id = $1::uuid WHERE id = $2::uuid`, revisionID, f.courseID); err != nil {
		t.Fatalf("switching live revision: %v", err)
	}
}

func TestProgressRoutePreservesCompletionAndResumeAcrossRevisionReplacement(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	playbackPath := "/api/v1/learn/lessons/" + f.lessonID + "/playback"
	if response := f.request(http.MethodPost, playbackPath, ""); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), f.versionID) {
		t.Fatalf("initial protected playback = %d %s", response.Code, response.Body.String())
	}
	progressPath := "/api/v1/learn/lessons/" + f.lessonID + "/progress"
	if response := f.request(http.MethodPut, progressPath, `{"position_seconds":54,"asset_version_id":"`+f.versionID+`"}`); response.Code != http.StatusOK {
		t.Fatalf("trusted 90%% progress = %d %s", response.Code, response.Body.String())
	}
	if response := f.request(http.MethodPut, progressPath, `{"position_seconds":12,"asset_version_id":"`+f.versionID+`"}`); response.Code != http.StatusOK {
		t.Fatalf("backward/retried progress = %d %s", response.Code, response.Body.String())
	}
	f.replaceLiveRevision(t)
	if response := f.request(http.MethodPut, progressPath, `{"position_seconds":18,"asset_version_id":"`+f.versionID+`"}`); response.Code != http.StatusOK {
		t.Fatalf("progress after instructor revision replacement = %d %s", response.Code, response.Body.String())
	}
	if response := f.request(http.MethodPost, playbackPath, ""); response.Code != http.StatusOK {
		t.Fatalf("playback after simulated termination = %d %s", response.Code, response.Body.String())
	}
	enrollment, err := f.repository.EnrollmentForLesson(context.Background(), f.studentID, f.lessonID)
	if err != nil {
		t.Fatalf("resolving durable enrollment: %v", err)
	}
	progress, err := f.repository.ProgressForLesson(context.Background(), enrollment.ID, f.lessonID)
	if err != nil || progress.MaxPositionSeconds != 54 || !progress.Completed {
		t.Fatalf("durable resume state = %+v err=%v", progress, err)
	}
	var rows int
	var completingVersion string
	if err := f.pool.QueryRow(context.Background(), `
		SELECT completing_asset_version_id::text,
		       (SELECT count(*) FROM progress WHERE enrollment_id = $1::uuid AND course_lesson_identity_id = $2::uuid)
		FROM progress WHERE enrollment_id = $1::uuid AND course_lesson_identity_id = $2::uuid
	`, enrollment.ID, f.lessonID).Scan(&completingVersion, &rows); err != nil {
		t.Fatalf("reading completion record: %v", err)
	}
	if rows != 1 || completingVersion != f.versionID {
		t.Fatalf("completion record = rows %d version %q", rows, completingVersion)
	}
}

func TestProgressDelayedAfterRevocationIsRefusedBeforeMutation(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	reached := make(chan struct{})
	release := make(chan struct{})
	f.foundation.beforeProgressMutation = func(context.Context) {
		close(reached)
		<-release
	}
	responseDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		responseDone <- f.request(http.MethodPut, "/api/v1/learn/lessons/"+f.lessonID+"/progress", `{"position_seconds":30,"asset_version_id":"`+f.versionID+`"}`)
	}()
	select {
	case <-reached:
	case <-time.After(5 * time.Second):
		t.Fatal("request did not reach the post-initial-authorization pre-mutation barrier")
	}
	if _, err := f.pool.Exec(context.Background(), `UPDATE entitlements SET state = 'REVOKED', revoked_at = now() WHERE student_account_id = $1::uuid AND course_id = $2::uuid`, f.studentID, f.courseID); err != nil {
		t.Fatalf("revoking entitlement while request is paused: %v", err)
	}
	close(release)
	select {
	case response := <-responseDone:
		if response.Code != http.StatusNotFound {
			t.Fatalf("revoked delayed progress = %d %s, want uniform protected-unavailable", response.Code, response.Body.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("paused request did not finish after release")
	}
	var rows int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM progress`).Scan(&rows); err != nil {
		t.Fatalf("counting rejected Progress rows: %v", err)
	}
	if rows != 0 {
		t.Fatalf("revoked delayed request wrote %d Progress rows", rows)
	}
}

// TestProgressWriteReturnsCanonicalStateForTheRenderedSurfaces is the
// server half of "visible progress updates without a refresh".
//
// The write used to answer 204. That was correct as a persistence contract and
// wrong as a product contract: the browser learned only that the request had
// been accepted, so every surface showing completion or a course percentage
// kept rendering whatever it had at page load. Nothing short of a reload could
// correct it, because nothing else told the page what the server now believed.
//
// What is asserted here is that the response carries the state the server
// computed — not the state the request claimed. The completion rule and the
// aggregate stay server-side; the browser renders them.
func TestProgressWriteReturnsCanonicalStateForTheRenderedSurfaces(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	playbackPath := "/api/v1/learn/lessons/" + f.lessonID + "/playback"
	if response := f.request(http.MethodPost, playbackPath, ""); response.Code != http.StatusOK {
		t.Fatalf("protected playback = %d %s", response.Code, response.Body.String())
	}
	progressPath := "/api/v1/learn/lessons/" + f.lessonID + "/progress"

	type confirmation struct {
		LessonProgress struct {
			PositionSeconds float64 `json:"position_seconds"`
			Completed       bool    `json:"completed"`
		} `json:"lesson_progress"`
		CourseProgress *struct {
			CompletedLessons int `json:"completed_lessons"`
			TotalLessons     int `json:"total_lessons"`
			Percent          int `json:"percent"`
		} `json:"course_progress"`
	}
	confirm := func(t *testing.T, seconds string) confirmation {
		t.Helper()
		response := f.request(http.MethodPut, progressPath,
			`{"position_seconds":`+seconds+`,"asset_version_id":"`+f.versionID+`"}`)
		if response.Code != http.StatusOK {
			t.Fatalf("progress write = %d %s", response.Code, response.Body.String())
		}
		if response.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("progress confirmation Cache-Control = %q", response.Header().Get("Cache-Control"))
		}
		var body confirmation
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatalf("progress confirmation is not JSON: %v (%s)", err, response.Body.String())
		}
		if body.CourseProgress == nil {
			t.Fatal("progress confirmation carries no course aggregate")
		}
		return body
	}

	partial := confirm(t, "12")
	if partial.LessonProgress.Completed {
		t.Fatal("a partial position was reported as completed")
	}
	if partial.LessonProgress.PositionSeconds != 12 {
		t.Fatalf("confirmed position = %v, want 12", partial.LessonProgress.PositionSeconds)
	}
	if partial.CourseProgress.CompletedLessons != 0 || partial.CourseProgress.TotalLessons == 0 {
		t.Fatalf("partial course aggregate = %+v", *partial.CourseProgress)
	}

	// Past the completion threshold. The server decides this, and the response
	// is how the browser finds out.
	finished := confirm(t, "54")
	if !finished.LessonProgress.Completed {
		t.Fatal("a completing position was not confirmed as completed")
	}
	if finished.CourseProgress.CompletedLessons != 1 {
		t.Fatalf("completed course aggregate = %+v", *finished.CourseProgress)
	}
	if finished.CourseProgress.Percent == partial.CourseProgress.Percent {
		t.Fatalf("course percent did not move on completion: %d", finished.CourseProgress.Percent)
	}

	// A rewind after completion must not un-complete the Lesson, and the
	// browser must be told the completion stands rather than inferring it.
	rewound := confirm(t, "5")
	if !rewound.LessonProgress.Completed {
		t.Fatal("rewinding after completion reported the Lesson as incomplete")
	}
	if rewound.CourseProgress.CompletedLessons != 1 {
		t.Fatalf("rewound course aggregate = %+v", *rewound.CourseProgress)
	}
	if rewound.LessonProgress.PositionSeconds != 5 {
		t.Fatalf("rewound position = %v, want 5", rewound.LessonProgress.PositionSeconds)
	}
}
