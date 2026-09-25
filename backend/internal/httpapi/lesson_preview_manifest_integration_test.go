//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
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

// TestLessonPreviewManifestURLIsRetrievableThroughTheMountedRouter follows the
// anonymous Lesson preview the way a player does: it asks the real
// authorization route for a capability, takes the manifest_url that route
// returned, and fetches exactly that URL through the real mounted router.
//
// # WHY THIS EXISTS
//
// cmd/api's route inventory already pins the mounted route TEMPLATES for this
// feature as strings. That is necessary and it is not sufficient: a template
// can be mounted correctly while the URL the issuer hands out still cannot be
// retrieved. The schema46 application rollback drill found exactly that — the
// POST returned 200 with a manifest_url and the GET of that same URL answered
// 404 — and no test in the tree failed, because none of them joined the two
// halves over HTTP.
//
// The second half of the test pins the reason that 404 happened. Master
// manifest generation re-derives every persisted rendition against the compiled
// HLS ladder and refuses any row that disagrees with it, which surfaces as an
// inventory-safe 404. Preview ISSUANCE does not re-derive: it only asks whether
// renditions exist. That asymmetry is deliberate — issuance must not do the
// work of delivery — but it means off-ladder rendition rows produce a preview
// that authorizes and then will not play, and nothing but a joined request
// catches it.
func TestLessonPreviewManifestURLIsRetrievableThroughTheMountedRouter(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	seedPreviewableLessonVideo(t, f, 1400)
	router := newLessonPreviewRouter(t, f)

	authorizationPath := "/api/v1/media/courses/" + f.courseID +
		"/lessons/" + f.lessonID + "/preview-authorizations"

	authorized := httptest.NewRecorder()
	router.ServeHTTP(authorized, httptest.NewRequest(http.MethodPost, authorizationPath, strings.NewReader("{}")))
	if authorized.Code != http.StatusOK {
		t.Fatalf("preview authorization status=%d body=%q", authorized.Code, authorized.Body.String())
	}
	var issued struct {
		PreviewSession string `json:"preview_session"`
		ManifestURL    string `json:"manifest_url"`
	}
	if err := json.Unmarshal(authorized.Body.Bytes(), &issued); err != nil {
		t.Fatalf("decoding preview authorization: %v", err)
	}
	if issued.PreviewSession == "" || issued.ManifestURL == "" {
		t.Fatalf("preview authorization carried no capability: %q", authorized.Body.String())
	}

	// The URL the SERVER issued, fetched verbatim. Not a reconstruction of it:
	// reconstructing it here would re-implement the issuer's own mistake and the
	// test would agree with whatever the issuer did.
	master := httptest.NewRecorder()
	router.ServeHTTP(master, httptest.NewRequest(http.MethodGet, issued.ManifestURL, nil))
	if master.Code != http.StatusOK {
		t.Fatalf("GET of the issued manifest_url status=%d url=%q body=%q",
			master.Code, issued.ManifestURL, master.Body.String())
	}
	body := master.Body.String()
	if !strings.HasPrefix(body, "#EXTM3U") {
		t.Fatalf("issued manifest is not an HLS playlist: %q", body)
	}

	// The master must name a rendition manifest underneath the SAME capability,
	// which is what a player follows next.
	//
	// Fetching that URL is deliberately NOT asserted here. The rendition leg
	// rewrites the stored media playlist and signs every segment reference, so
	// it needs real playlist objects in object storage; this fixture writes
	// database rows only, and under it the rendition route answers 404 for that
	// reason rather than for a routing or authorization one. Asserting on it
	// here would pin the harness instead of the product. The rendition leg is
	// therefore NOT covered by this test and is called out as such.
	rendition := firstRenditionURL(t, body)
	if !strings.HasPrefix(rendition, "/api/v1/media/lesson-previews/"+issued.PreviewSession+"/renditions/") {
		t.Fatalf("the master names a rendition outside the issued capability: %q", rendition)
	}

	// Now the regression the drill actually found: a persisted rendition that
	// does not match the compiled ladder. Authorization must still succeed and
	// the manifest must fail closed, which is what makes this defect invisible
	// to an issuance-only test.
	//
	// video_renditions is append-only, so the off-ladder case gets its own Asset
	// Version and the Lesson is re-pointed at it. Mutating the passing rows
	// would require defeating an append-only trigger, which this test will not
	// do.
	seedPreviewableLessonVideo(t, f, 1401)

	offLadder := httptest.NewRecorder()
	router.ServeHTTP(offLadder, httptest.NewRequest(http.MethodPost, authorizationPath, strings.NewReader("{}")))
	if offLadder.Code != http.StatusOK {
		t.Fatalf("off-ladder preview authorization status=%d, want 200: issuance does not re-derive the ladder", offLadder.Code)
	}
	var reissued struct {
		ManifestURL string `json:"manifest_url"`
	}
	if err := json.Unmarshal(offLadder.Body.Bytes(), &reissued); err != nil {
		t.Fatalf("decoding the off-ladder authorization: %v", err)
	}
	denied := httptest.NewRecorder()
	router.ServeHTTP(denied, httptest.NewRequest(http.MethodGet, reissued.ManifestURL, nil))
	if denied.Code != http.StatusNotFound {
		t.Fatalf("an off-ladder rendition must fail closed, got status=%d body=%q", denied.Code, denied.Body.String())
	}
}

// newLessonPreviewRouter composes the production router with the media
// foundation mounted, so the preview routes under test are the ones cmd/api
// serves rather than a bespoke subset.
func newLessonPreviewRouter(t *testing.T, f learningIntegrationFixture) http.Handler {
	t.Helper()
	writer, err := outbox.NewWriter("lesson-preview-http", []byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatalf("creating media outbox: %v", err)
	}
	scannerSource, err := media.NewUnavailableScanner("the Lesson preview router test performs no scanning")
	if err != nil {
		t.Fatalf("creating scanner source: %v", err)
	}
	scanner, err := media.NewScannerAdapter(scannerSource)
	if err != nil {
		t.Fatalf("creating scanner adapter: %v", err)
	}
	// The media Service needs the wider ObjectStore surface; delivery does not.
	// d064PipelineStore is the existing wrapper that supplies it.
	service, err := media.NewService(media.ServiceOptions{
		DB: f.pool, Store: &d064PipelineStore{learningIntegrationStore: f.store},
		Outbox: writer, Scanner: scanner,
		UploadURLExpiry: time.Minute, MaxUploadBytes: 1024,
	})
	if err != nil {
		t.Fatalf("creating media service: %v", err)
	}
	delivery, err := media.NewDeliveryService(media.DeliveryOptions{
		DB: f.pool, Store: f.store, Evaluator: f.evaluator,
		SignatureLifetime: time.Minute, BuyerTagKey: []byte("01234567890123456789012345678901"),
		Now: f.clock.Now, Playback: testPlaybackCoordinator(t),
	})
	if err != nil {
		t.Fatalf("creating delivery service: %v", err)
	}
	mediaFoundation, err := NewMediaFoundation(MediaFoundationOptions{Service: service, Delivery: delivery})
	if err != nil {
		t.Fatalf("creating media foundation: %v", err)
	}
	cfg, err := config.LoadFrom(config.MapLookup(map[string]string{
		"APP_ENV": "development", "REDIS_ADDR": "localhost:6379",
		"S3_ENDPOINT": "http://localhost:9000", "S3_BUCKET": "gradex-test",
	}), config.MapSecretResolver{
		"DATABASE_URL": "postgres://x", "S3_ACCESS_KEY": "a", "S3_SECRET_KEY": "b", "PLAYBACK_TOKEN_SECRET": "c",
	})
	if err != nil {
		t.Fatalf("loading configuration: %v", err)
	}
	router, err := NewRouter(cfg,
		logging.New(&syncBuffer{}, "lesson-preview-test", "development", logging.LevelFromString("info")),
		health.New(time.Second), learningIntegrationAuth{studentID: f.studentID},
		identity.NewDBPrincipalResolver(f.pool),
		WithMediaFoundation(mediaFoundation), WithLearningFoundation(f.foundation))
	if err != nil {
		t.Fatalf("composing the production router: %v", err)
	}
	return router
}

// seedPreviewableLessonVideo gives the fixture's live Lesson a READY video with
// ladder-conformant renditions and marks it publicly previewable.
//
// The media version is walked through the real state machine rather than
// inserted READY, because the database enforces that machine and the exact
// per-version scan and processing evidence with triggers. Nothing is relaxed.
func seedPreviewableLessonVideo(t *testing.T, f learningIntegrationFixture, bitrate480 int) string {
	t.Helper()
	ctx := context.Background()
	var lessonRow, ownerID string
	if err := f.pool.QueryRow(ctx,
		`SELECT cl.id::text, c.owner_account_id::text
		   FROM course_lessons cl JOIN courses c ON c.id = cl.course_id
		  WHERE cl.lesson_identity_id = $1::uuid`, f.lessonID).Scan(&lessonRow, &ownerID); err != nil {
		t.Fatalf("resolving the live lesson row: %v", err)
	}
	assetID, versionID := uuid.NewString(), uuid.NewString()
	scanID, attemptID := uuid.NewString(), uuid.NewString()
	prefix := "media/preview-http/" + versionID

	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO media_assets (id, kind, owner_account_id, course_id, visibility)
		  VALUES ($1::uuid, 'VIDEO', $2::uuid, $3::uuid, 'PROTECTED')`, []any{assetID, ownerID, f.courseID}},
		{`INSERT INTO media_asset_versions
		    (id, logical_asset_id, kind, state, storage_object_key, storage_object_version,
		     content_type, size_bytes, sha256_hex, trusted_duration_ms)
		  VALUES ($1::uuid, $2::uuid, 'VIDEO', 'UPLOADED', $3, 'v1', 'video/mp4', 4096, repeat('a', 64), 480000)`,
			[]any{versionID, assetID, prefix + "/source.mp4"}},
		{`UPDATE media_asset_versions SET state = 'QUARANTINED' WHERE id = $1::uuid`, []any{versionID}},
		{`UPDATE media_asset_versions SET state = 'SCANNING' WHERE id = $1::uuid`, []any{versionID}},
		{`INSERT INTO scan_attempts
		    (id, asset_version_id, attempt_number, work_id, storage_object_version, outcome, scanner_identity)
		  VALUES ($1::uuid, $2::uuid, 1, $3, 'v1', 'PASSED', 'lesson-preview-fixture')`,
			[]any{scanID, versionID, "scan:" + versionID}},
		{`UPDATE media_asset_versions SET successful_scan_attempt_id = $1::uuid, state = 'SCAN_PASSED' WHERE id = $2::uuid`,
			[]any{scanID, versionID}},
		{`UPDATE media_asset_versions SET state = 'PROCESSING' WHERE id = $1::uuid`, []any{versionID}},
		{`INSERT INTO processing_attempts
		    (id, asset_version_id, operation_id, state, attempt_kind, rendition_count, trusted_duration_ms, output_prefix)
		  VALUES ($1::uuid, $2::uuid, $3, 'SUCCEEDED', 'FULL', 2, 480000, $4)`,
			[]any{attemptID, versionID, "op:" + versionID, prefix + "/hls"}},
		// Width, height and video bitrate must equal the compiled ladder rung of
		// the same name. 720p is 1280x720 at 2800 kbps and 480p is 854x480 at
		// 1400 kbps; anything else is refused at master generation.
		{`INSERT INTO video_renditions
		    (asset_version_id, name, storage_object_key, width, height, bitrate_kbps, duration_ms, processing_operation_id)
		  VALUES ($1::uuid, '480p', $2, 854, 480, $5, 480000, $4),
		         ($1::uuid, '720p', $3, 1280, 720, 2800, 480000, $4)`,
			[]any{versionID, prefix + "/hls/480p/playlist.m3u8", prefix + "/hls/720p/playlist.m3u8", "op:" + versionID, bitrate480}},
		{`UPDATE media_asset_versions SET state = 'READY', successful_processing_attempt_id = $1::uuid WHERE id = $2::uuid`,
			[]any{attemptID, versionID}},
		{`UPDATE course_lessons SET video_asset_version_id = $1::uuid, allow_public_preview = true WHERE id = $2::uuid`,
			[]any{versionID, lessonRow}},
	} {
		if _, err := f.pool.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seeding the previewable Lesson video: %v", err)
		}
	}
	return versionID
}

// firstRenditionURL returns the first rendition manifest the master names.
func firstRenditionURL(t *testing.T, master string) string {
	t.Helper()
	for _, line := range strings.Split(master, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			return line
		}
	}
	t.Fatalf("the master playlist names no rendition manifest: %q", master)
	return ""
}
