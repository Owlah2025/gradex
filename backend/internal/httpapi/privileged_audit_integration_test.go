//go:build integration

package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/catalog"
)

type privilegedAuditFixture struct {
	ctx                                 context.Context
	client                              *http.Client
	engine                              *gin.Engine
	baseURL                             string
	pool                                *pgxpool.Pool
	repo                                *catalog.Repository
	adminID, instructorID               string
	adminToken, instructorToken         string
	courseID, revisionID                string
	sectionID, lessonID, fileID, termID string
	majorID, subjectID, videoID         string
}

type privilegedAuditExpectation struct {
	status     int
	action     string
	targetType string
	targetID   string
	committed  func(*testing.T, *privilegedAuditFixture)
}

type privilegedAuditScenario func(*testing.T, *privilegedAuditFixture, gin.RouteInfo) privilegedAuditExpectation

func TestProductionPrivilegedMutationRoutesCommitAuditEvidence(t *testing.T) {
	fixture := newPrivilegedAuditFixture(t)
	routes := privilegedCatalogMutationRoutes(fixture.engine)
	if len(routes) == 0 {
		t.Fatal("no privileged S2 mutation routes were derived from the production router")
	}

	scenarios := privilegedAuditScenarios()
	seen := make(map[string]bool, len(routes))
	for _, route := range routes {
		key := route.Method + " " + route.Path
		scenario, ok := scenarios[key]
		if !ok {
			t.Fatalf("privileged production route %s has no executable audit scenario", key)
		}
		seen[key] = true

		t.Run(key, func(t *testing.T) {
			f := newPrivilegedAuditFixture(t)
			expectation := scenario(t, f, route)
			f.assertAudit(t, expectation)
			if expectation.committed != nil {
				expectation.committed(t, f)
			}
		})
	}
	for key := range scenarios {
		if !seen[key] {
			t.Fatalf("audit scenario %s does not correspond to a privileged production route", key)
		}
	}
}

type instructorAuditScenario struct {
	prepare    func(*testing.T, *privilegedAuditFixture)
	body       func(*privilegedAuditFixture) string
	status     int
	action     string
	targetType string
}

// TestProductionInstructorMutationRoutesCommitAuditEvidence closes the other
// half of the live S2 mutation table. Together with the Admin proof above,
// every registered Course mutation is executed through the production router.
func TestProductionInstructorMutationRoutesCommitAuditEvidence(t *testing.T) {
	fixture := newPrivilegedAuditFixture(t)
	scenarios := instructorAuditScenarios()
	seen := make(map[string]bool)
	for _, route := range catalogMutationRoutes(fixture.engine) {
		if !strings.HasPrefix(route.Path, "/api/v1/courses") {
			continue
		}
		key := route.Method + " " + route.Path
		scenario, ok := scenarios[key]
		if !ok {
			t.Fatalf("instructor production route %s has no executable audit scenario", key)
		}
		seen[key] = true
		t.Run(key, func(t *testing.T) {
			f := newPrivilegedAuditFixture(t)
			if scenario.prepare != nil {
				scenario.prepare(t, f)
			}
			before := f.auditCount(t, scenario.action)
			response := f.instructorRequest(t, route, scenario.body(f))
			defer response.Body.Close()
			if response.StatusCode != scenario.status {
				t.Fatalf("%s status = %d, want %d", key, response.StatusCode, scenario.status)
			}
			f.assertNewInstructorAudit(t, scenario.action, scenario.targetType, before)
		})
	}
	for key := range scenarios {
		if !seen[key] {
			t.Fatalf("instructor audit scenario %s does not correspond to a production route", key)
		}
	}
}

func instructorAuditScenarios() map[string]instructorAuditScenario {
	return map[string]instructorAuditScenario{
		http.MethodPost + " /api/v1/courses": {body: func(*privilegedAuditFixture) string {
			return `{"title_ar":"دورة تدقيق","title_en":"Audit Course","description_ar":"وصف","description_en":"Description"}`
		}, status: http.StatusCreated, action: "COURSE_CREATED", targetType: "COURSE"},
		http.MethodPut + " /api/v1/courses/:id/candidate": {prepare: preparePublishedCourse, body: emptyAuditBody, status: http.StatusOK, action: "COURSE_CANDIDATE_CREATED", targetType: "COURSE_REVISION"},
		http.MethodPatch + " /api/v1/courses/:id/revisions/:revisionId": {body: func(*privilegedAuditFixture) string {
			return `{"title_ar":"عنوان","title_en":"Title","description_ar":"وصف","description_en":"Description"}`
		}, status: http.StatusOK, action: "COURSE_REVISION_UPDATED", targetType: "COURSE_REVISION"},
		http.MethodPost + " /api/v1/courses/:id/revisions/:revisionId/sections": {body: func(*privilegedAuditFixture) string { return `{"title_ar":"قسم","title_en":"Section"}` }, status: http.StatusCreated, action: "SECTION_CREATED", targetType: "SECTION"},
		http.MethodPatch + " /api/v1/courses/:id/revisions/:revisionId/sections/:sectionId": {body: func(*privilegedAuditFixture) string {
			return `{"title_ar":"قسم محدث","title_en":"Updated Section"}`
		}, status: http.StatusOK, action: "SECTION_UPDATED", targetType: "SECTION"},
		http.MethodDelete + " /api/v1/courses/:id/revisions/:revisionId/sections/:sectionId":       {body: emptyAuditBody, status: http.StatusNoContent, action: "SECTION_DELETED", targetType: "SECTION"},
		http.MethodPost + " /api/v1/courses/:id/revisions/:revisionId/sections/:sectionId/lessons": {body: func(*privilegedAuditFixture) string { return `{"title_ar":"درس","title_en":"Lesson"}` }, status: http.StatusCreated, action: "LESSON_CREATED", targetType: "LESSON"},
		http.MethodPatch + " /api/v1/courses/:id/revisions/:revisionId/lessons/:lessonId": {prepare: prepareAuditLesson, body: func(*privilegedAuditFixture) string {
			return `{"title_ar":"درس محدث","title_en":"Updated Lesson"}`
		}, status: http.StatusOK, action: "LESSON_UPDATED", targetType: "LESSON"},
		http.MethodDelete + " /api/v1/courses/:id/revisions/:revisionId/lessons/:lessonId":    {prepare: prepareAuditLesson, body: emptyAuditBody, status: http.StatusNoContent, action: "LESSON_DELETED", targetType: "LESSON"},
		http.MethodPut + " /api/v1/courses/:id/revisions/:revisionId/lessons/:lessonId/video": {prepare: prepareAuditLesson, body: func(f *privilegedAuditFixture) string { return fmt.Sprintf(`{"video_asset_version_id":%q}`, f.videoID) }, status: http.StatusOK, action: "LESSON_VIDEO_ATTACHED", targetType: "LESSON"},
		http.MethodPut + " /api/v1/courses/:id/revisions/:revisionId/lessons/:lessonId/files": {prepare: prepareAuditLesson, body: func(f *privilegedAuditFixture) string {
			return fmt.Sprintf(`{"kind":"RESOURCE","asset_version_id":%q,"display_name_ar":"ملف","display_name_en":"File"}`, f.videoID)
		}, status: http.StatusCreated, action: "LESSON_FILE_ATTACHED", targetType: "LESSON_FILE"},
		http.MethodDelete + " /api/v1/courses/:id/revisions/:revisionId/lessons/:lessonId/files": {prepare: prepareAuditFile, body: func(f *privilegedAuditFixture) string { return `{"file_id":"` + f.fileID + `"}` }, status: http.StatusNoContent, action: "LESSON_FILE_DELETED", targetType: "LESSON_FILE"},
		http.MethodPut + " /api/v1/courses/:id/revisions/:revisionId/preview": {body: func(f *privilegedAuditFixture) string {
			return fmt.Sprintf(`{"preview_asset_version_id":%q}`, f.videoID)
		}, status: http.StatusOK, action: "PREVIEW_ASSET_SET", targetType: "COURSE_REVISION"},
		http.MethodDelete + " /api/v1/courses/:id/revisions/:revisionId/preview": {prepare: prepareAuditPreview, body: emptyAuditBody, status: http.StatusOK, action: "PREVIEW_ASSET_CLEARED", targetType: "COURSE_REVISION"},
		http.MethodPost + " /api/v1/courses/:id/revisions/:revisionId/submit":    {prepare: prepareSubmittableCourse, body: emptyAuditBody, status: http.StatusOK, action: "COURSE_SUBMITTED", targetType: "COURSE"},
	}
}

func emptyAuditBody(*privilegedAuditFixture) string { return "" }

func newPrivilegedAuditFixture(t *testing.T) *privilegedAuditFixture {
	t.Helper()
	ts, p, adminID, instructorID, courseID, sectionID, adminToken, instToken := setupAdminPricingAPIServer(t)
	engine, ok := ts.Config.Handler.(*gin.Engine)
	if !ok {
		t.Fatal("test server does not expose the production Gin router")
	}
	ctx := context.Background()
	var revisionID string
	if err := p.QueryRow(ctx, `SELECT id::text FROM course_revisions WHERE course_id = $1::uuid`, courseID).Scan(&revisionID); err != nil {
		t.Fatalf("loading fixture revision: %v", err)
	}
	if _, err := p.Exec(ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name)
		VALUES ('10000000-0000-0000-0000-000000000003', 'second@example.com', 'second@example.com', 'INSTRUCTOR', 'ACTIVE', 'Second Instructor')
	`); err != nil {
		t.Fatalf("seeding reassignment owner: %v", err)
	}
	if _, err := p.Exec(ctx, `INSERT INTO sections (id, course_id, title, "order") VALUES ('10000000-0000-0000-0000-000000000010', $1::uuid, 'Asset source', 1)`, courseID); err != nil {
		t.Fatalf("seeding ready video fixture: %v", err)
	}
	if _, err := p.Exec(ctx, `INSERT INTO lessons (id, section_id, title, "order") VALUES ('10000000-0000-0000-0000-000000000011', '10000000-0000-0000-0000-000000000010', 'Asset source', 1)`); err != nil {
		t.Fatalf("seeding ready video fixture: %v", err)
	}
	if _, err := p.Exec(ctx, `INSERT INTO videos (id, lesson_id, status) VALUES ('10000000-0000-0000-0000-000000000012', '10000000-0000-0000-0000-000000000011', 'READY')`); err != nil {
		t.Fatalf("seeding ready video fixture: %v", err)
	}
	repo, err := catalog.NewRepository(p, testWriterForAuthoring(t))
	if err != nil {
		t.Fatalf("catalog.NewRepository: %v", err)
	}
	return &privilegedAuditFixture{
		ctx: ctx, client: ts.Client(), engine: engine, baseURL: ts.URL, pool: p, repo: repo,
		adminID: adminID, instructorID: instructorID, adminToken: adminToken,
		instructorToken: instToken,
		courseID:        courseID, revisionID: revisionID, sectionID: sectionID,
		videoID: "10000000-0000-0000-0000-000000000012",
	}
}

func privilegedCatalogMutationRoutes(engine *gin.Engine) []gin.RouteInfo {
	var routes []gin.RouteInfo
	for _, route := range engine.Routes() {
		if route.Method == http.MethodGet || !isPrivilegedCatalogMutationPath(route.Path) {
			continue
		}
		routes = append(routes, route)
	}
	return routes
}

func isPrivilegedCatalogMutationPath(path string) bool {
	return strings.HasPrefix(path, "/api/v1/admin/review/") ||
		strings.HasPrefix(path, "/api/v1/admin/courses/") ||
		strings.HasPrefix(path, "/api/v1/admin/taxonomy/terms")
}

func privilegedAuditScenarios() map[string]privilegedAuditScenario {
	return map[string]privilegedAuditScenario{
		http.MethodPost + " /api/v1/admin/review/courses/:id/revisions/:revisionId/approve":           approveAuditScenario,
		http.MethodPost + " /api/v1/admin/review/courses/:id/revisions/:revisionId/request-changes":   requestChangesAuditScenario,
		http.MethodPost + " /api/v1/admin/review/courses/:id/revisions/:revisionId/preview/:lessonId": previewAuditScenario,
		http.MethodPut + " /api/v1/admin/courses/:id/price":                                           coursePriceAuditScenario,
		http.MethodPut + " /api/v1/admin/courses/:id/sections/:sectionId/price":                       sectionPriceAuditScenario,
		http.MethodPost + " /api/v1/admin/courses/:id/delist":                                         lifecycleAuditScenario(catalog.LifecycleDelisted, "COURSE_DELISTED"),
		http.MethodPost + " /api/v1/admin/courses/:id/relist":                                         relistAuditScenario,
		http.MethodPost + " /api/v1/admin/courses/:id/retire":                                         retireAuditScenario,
		http.MethodPost + " /api/v1/admin/courses/:id/archive":                                        lifecycleAuditScenario(catalog.LifecycleArchived, "COURSE_ARCHIVED"),
		http.MethodDelete + " /api/v1/admin/courses/:id":                                              deleteAuditScenario,
		http.MethodPost + " /api/v1/admin/courses/:id/owner":                                          ownerAuditScenario,
		http.MethodPost + " /api/v1/admin/courses/:id/access-suspension":                              suspendAuditScenario,
		http.MethodDelete + " /api/v1/admin/courses/:id/access-suspension":                            restoreAuditScenario,
		http.MethodPut + " /api/v1/admin/courses/:id/taxonomy":                                        taxonomyAssignmentAuditScenario,
		http.MethodPost + " /api/v1/admin/taxonomy/terms":                                             createTermAuditScenario,
		http.MethodPatch + " /api/v1/admin/taxonomy/terms/:id":                                        renameTermAuditScenario,
		http.MethodPost + " /api/v1/admin/taxonomy/terms/:id/retire":                                  retireTermAuditScenario,
		http.MethodDelete + " /api/v1/admin/taxonomy/terms/:id":                                       deleteTermAuditScenario,
	}
}

func (f *privilegedAuditFixture) request(t *testing.T, route gin.RouteInfo, body string) *http.Response {
	t.Helper()
	path := route.Path
	if strings.HasPrefix(path, "/api/v1/admin/taxonomy/terms/") {
		path = strings.ReplaceAll(path, ":id", f.termID)
	} else {
		path = strings.ReplaceAll(path, ":id", f.courseID)
	}
	path = strings.ReplaceAll(path, ":revisionId", f.revisionID)
	path = strings.ReplaceAll(path, ":sectionId", f.sectionID)
	path = strings.ReplaceAll(path, ":lessonId", f.lessonID)
	return doPricingRequest(t, f.client, route.Method, f.baseURL+path, f.adminToken, "https://gradex.example", f.adminToken, []byte(body))
}

func (f *privilegedAuditFixture) instructorRequest(t *testing.T, route gin.RouteInfo, body string) *http.Response {
	t.Helper()
	path := strings.ReplaceAll(route.Path, ":id", f.courseID)
	path = strings.ReplaceAll(path, ":revisionId", f.revisionID)
	path = strings.ReplaceAll(path, ":sectionId", f.sectionID)
	path = strings.ReplaceAll(path, ":lessonId", f.lessonID)
	return doPricingRequest(t, f.client, route.Method, f.baseURL+path, f.instructorToken, "https://gradex.example", f.instructorToken, []byte(body))
}

func (f *privilegedAuditFixture) auditCount(t *testing.T, action string) int {
	t.Helper()
	var count int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM audit_events WHERE actor_account_id = $1::uuid AND action = $2`, f.instructorID, action).Scan(&count); err != nil {
		t.Fatalf("counting %s audit events: %v", action, err)
	}
	return count
}

func (f *privilegedAuditFixture) assertNewInstructorAudit(t *testing.T, action, targetType string, before int) {
	t.Helper()
	var actorRole, module, targetID, reason string
	if err := f.pool.QueryRow(f.ctx, `
		SELECT actor_role, module, target_id, reason
		FROM audit_events
		WHERE actor_account_id = $1::uuid AND action = $2
		ORDER BY occurred_at DESC LIMIT 1
	`, f.instructorID, action).Scan(&actorRole, &module, &targetID, &reason); err != nil {
		t.Fatalf("loading %s audit event: %v", action, err)
	}
	if count := f.auditCount(t, action); count != before+1 || actorRole != "INSTRUCTOR" || module != "CATALOG_AND_AUTHORING" || targetID == "" || reason == "" {
		t.Fatalf("%s audit = count %d role %q module %q target %q reason %q", action, count, actorRole, module, targetID, reason)
	}
	var actualTargetType string
	if err := f.pool.QueryRow(f.ctx, `SELECT target_type FROM audit_events WHERE actor_account_id = $1::uuid AND action = $2 ORDER BY occurred_at DESC LIMIT 1`, f.instructorID, action).Scan(&actualTargetType); err != nil || actualTargetType != targetType {
		t.Fatalf("%s target type = %q (err=%v), want %q", action, actualTargetType, err, targetType)
	}
}

func preparePublishedCourse(t *testing.T, f *privilegedAuditFixture) { f.preparePublished(t) }

func prepareAuditLesson(t *testing.T, f *privilegedAuditFixture) {
	t.Helper()
	if f.lessonID != "" {
		return
	}
	lesson, err := f.repo.AddLesson(f.ctx, catalog.AddLessonRequest{CourseID: f.courseID, RevisionID: f.revisionID, SectionID: f.sectionID, OwnerAccountID: f.instructorID, TitleAr: "درس", TitleEn: "Lesson"}, f.instructorID)
	if err != nil {
		t.Fatalf("creating audit lesson: %v", err)
	}
	f.lessonID = lesson.LessonIdentityID
}

func prepareAuditFile(t *testing.T, f *privilegedAuditFixture) {
	t.Helper()
	prepareAuditLesson(t, f)
	file, err := f.repo.AddLessonFile(f.ctx, catalog.NewDBAssetVersionValidator(f.pool), catalog.LessonFileRequest{CourseID: f.courseID, RevisionID: f.revisionID, LessonID: f.lessonID, OwnerAccountID: f.instructorID, Kind: catalog.FileKindResource, AssetVersionID: f.videoID, DisplayNameAr: "ملف", DisplayNameEn: "File"}, f.instructorID)
	if err != nil {
		t.Fatalf("creating audit file: %v", err)
	}
	f.fileID = file.ID
}

func prepareAuditPreview(t *testing.T, f *privilegedAuditFixture) {
	t.Helper()
	if _, err := f.repo.SetPreviewAsset(f.ctx, catalog.NewDBAssetVersionValidator(f.pool), catalog.PreviewAssetRequest{CourseID: f.courseID, RevisionID: f.revisionID, PreviewAssetVersionID: f.videoID, OwnerAccountID: f.instructorID}, f.instructorID); err != nil {
		t.Fatalf("setting audit preview: %v", err)
	}
}

func prepareSubmittableCourse(t *testing.T, f *privilegedAuditFixture) {
	t.Helper()
	f.ensureTerms(t)
	year := catalog.StudyYear("YEAR_1")
	if _, err := f.repo.UpdateCourseRevision(f.ctx, catalog.NewDBAssetVersionValidator(f.pool), catalog.UpdateRevisionRequest{CourseID: f.courseID, RevisionID: f.revisionID, OwnerAccountID: f.instructorID, MajorTermID: &f.majorID, SubjectTermID: &f.subjectID, StudyYear: &year}, f.instructorID); err != nil {
		t.Fatalf("making audit revision complete: %v", err)
	}
	prepareAuditLesson(t, f)
	if _, err := f.repo.SetLessonVideo(f.ctx, catalog.NewDBAssetVersionValidator(f.pool), catalog.SetVideoRequest{CourseID: f.courseID, RevisionID: f.revisionID, LessonID: f.lessonID, OwnerAccountID: f.instructorID, VideoAssetVersionID: f.videoID}, f.instructorID); err != nil {
		t.Fatalf("attaching audit video: %v", err)
	}
}

func (f *privilegedAuditFixture) assertAudit(t *testing.T, want privilegedAuditExpectation) {
	t.Helper()
	var action, actorID, actorRole, targetType, targetID, module string
	var occurredAt time.Time
	err := f.pool.QueryRow(f.ctx, `
		SELECT action, actor_account_id::text, actor_role, target_type, target_id, module, occurred_at
		FROM audit_events
		WHERE action = $1 AND actor_account_id = $2::uuid AND target_type = $3 AND target_id = $4
		LIMIT 1
	`, want.action, f.adminID, want.targetType, want.targetID).Scan(&action, &actorID, &actorRole, &targetType, &targetID, &module, &occurredAt)
	if err != nil {
		t.Fatalf("loading committed audit %s for %s/%s: %v", want.action, want.targetType, want.targetID, err)
	}
	if action != want.action || actorID != f.adminID || actorRole != "ADMIN" || targetType != want.targetType || targetID != want.targetID || module != "CATALOG_AND_AUTHORING" || occurredAt.IsZero() {
		t.Fatalf("audit row = action=%s actor=%s/%s target=%s/%s module=%s occurred_at=%s", action, actorID, actorRole, targetType, targetID, module, occurredAt)
	}
}

func (f *privilegedAuditFixture) ensureTerms(t *testing.T) {
	t.Helper()
	if f.majorID != "" {
		return
	}
	if err := f.pool.QueryRow(f.ctx, `INSERT INTO taxonomy_terms (kind, label_ar, label_en) VALUES ('MAJOR', 'علوم', 'Science') RETURNING id::text`).Scan(&f.majorID); err != nil {
		t.Fatalf("creating major term: %v", err)
	}
	if err := f.pool.QueryRow(f.ctx, `INSERT INTO taxonomy_terms (kind, label_ar, label_en, academic_code) VALUES ('SUBJECT', 'فيزياء', 'Physics', 'PHY101') RETURNING id::text`).Scan(&f.subjectID); err != nil {
		t.Fatalf("creating subject term: %v", err)
	}
}

func (f *privilegedAuditFixture) preparePendingReview(t *testing.T) {
	t.Helper()
	if f.lessonID != "" {
		return
	}
	f.ensureTerms(t)
	year := catalog.StudyYear("YEAR_1")
	if _, err := f.repo.UpdateCourseRevision(f.ctx, catalog.NewDBAssetVersionValidator(f.pool), catalog.UpdateRevisionRequest{
		CourseID: f.courseID, RevisionID: f.revisionID, OwnerAccountID: f.instructorID,
		MajorTermID: &f.majorID, SubjectTermID: &f.subjectID, StudyYear: &year,
	}, f.instructorID); err != nil {
		t.Fatalf("making revision complete: %v", err)
	}
	lesson, err := f.repo.AddLesson(f.ctx, catalog.AddLessonRequest{
		CourseID: f.courseID, RevisionID: f.revisionID, SectionID: f.sectionID, OwnerAccountID: f.instructorID,
		TitleAr: "درس", TitleEn: "Lesson",
	}, f.instructorID)
	if err != nil {
		t.Fatalf("adding fixture lesson: %v", err)
	}
	f.lessonID = lesson.LessonIdentityID
	if _, err := f.repo.SetLessonVideo(f.ctx, catalog.NewDBAssetVersionValidator(f.pool), catalog.SetVideoRequest{
		CourseID: f.courseID, RevisionID: f.revisionID, LessonID: f.lessonID, OwnerAccountID: f.instructorID, VideoAssetVersionID: f.videoID,
	}, f.instructorID); err != nil {
		t.Fatalf("attaching fixture video: %v", err)
	}
	if _, err := f.repo.SubmitCourse(f.ctx, catalog.NewDBAssetVersionValidator(f.pool), catalog.SubmitCourseRequest{CourseID: f.courseID, RevisionID: f.revisionID, OwnerAccountID: f.instructorID, ActorDescriptor: f.instructorID}); err != nil {
		t.Fatalf("submitting fixture course: %v", err)
	}
}

func (f *privilegedAuditFixture) preparePublished(t *testing.T) {
	t.Helper()
	f.preparePendingReview(t)
	if _, err := f.repo.ApproveCourse(f.ctx, catalog.NewDBAssetVersionValidator(f.pool), catalog.ApproveCourseRequest{
		CourseID: f.courseID, RevisionID: f.revisionID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID,
	}); err != nil {
		t.Fatalf("publishing fixture course: %v", err)
	}
}

func (f *privilegedAuditFixture) execute(t *testing.T, route gin.RouteInfo, body string, want privilegedAuditExpectation) privilegedAuditExpectation {
	t.Helper()
	response := f.request(t, route, body)
	defer response.Body.Close()
	if response.StatusCode != want.status {
		t.Fatalf("%s %s status = %d, want %d", route.Method, route.Path, response.StatusCode, want.status)
	}
	return want
}

func approveAuditScenario(t *testing.T, f *privilegedAuditFixture, route gin.RouteInfo) privilegedAuditExpectation {
	f.preparePendingReview(t)
	return f.execute(t, route, "", privilegedAuditExpectation{status: http.StatusOK, action: "COURSE_PUBLISHED", targetType: "COURSE", targetID: f.courseID, committed: func(t *testing.T, f *privilegedAuditFixture) {
		assertCourseValue(t, f, `SELECT lifecycle::text FROM courses WHERE id = $1::uuid`, "PUBLISHED")
	}})
}

func requestChangesAuditScenario(t *testing.T, f *privilegedAuditFixture, route gin.RouteInfo) privilegedAuditExpectation {
	f.preparePendingReview(t)
	return f.execute(t, route, `{"reason":"Audit proof requires changes"}`, privilegedAuditExpectation{status: http.StatusOK, action: "COURSE_CHANGES_REQUESTED", targetType: "COURSE", targetID: f.courseID, committed: func(t *testing.T, f *privilegedAuditFixture) {
		assertCourseValue(t, f, `SELECT lifecycle::text FROM courses WHERE id = $1::uuid`, "CHANGES_REQUESTED")
	}})
}

func previewAuditScenario(t *testing.T, f *privilegedAuditFixture, route gin.RouteInfo) privilegedAuditExpectation {
	f.preparePendingReview(t)
	return f.execute(t, route, "", privilegedAuditExpectation{status: http.StatusOK, action: "ADMIN_CONTENT_PREVIEWED", targetType: "LESSON", targetID: f.lessonID, committed: func(t *testing.T, f *privilegedAuditFixture) {
		var video string
		if err := f.pool.QueryRow(f.ctx, `SELECT video_asset_version_id::text FROM course_lessons WHERE lesson_identity_id = $1::uuid`, f.lessonID).Scan(&video); err != nil || video != f.videoID {
			t.Fatalf("preview fixture video = %q (err=%v), want %q", video, err, f.videoID)
		}
	}})
}

func coursePriceAuditScenario(t *testing.T, f *privilegedAuditFixture, route gin.RouteInfo) privilegedAuditExpectation {
	return f.execute(t, route, `{"price_minor_units":25000,"reason":"Audit proof course price"}`, privilegedAuditExpectation{status: http.StatusOK, action: "COURSE_PRICE_CHANGED", targetType: "COURSE", targetID: f.courseID, committed: func(t *testing.T, f *privilegedAuditFixture) {
		assertPriceChange(t, f, f.courseID, "", 25000)
	}})
}

func sectionPriceAuditScenario(t *testing.T, f *privilegedAuditFixture, route gin.RouteInfo) privilegedAuditExpectation {
	return f.execute(t, route, `{"price_minor_units":10000,"reason":"Audit proof section price"}`, privilegedAuditExpectation{status: http.StatusOK, action: "COURSE_PRICE_CHANGED", targetType: "SECTION", targetID: f.sectionID, committed: func(t *testing.T, f *privilegedAuditFixture) {
		assertPriceChange(t, f, f.courseID, f.sectionID, 10000)
	}})
}

func lifecycleAuditScenario(target catalog.CourseLifecycle, action string) privilegedAuditScenario {
	return func(t *testing.T, f *privilegedAuditFixture, route gin.RouteInfo) privilegedAuditExpectation {
		f.preparePublished(t)
		return f.execute(t, route, "", privilegedAuditExpectation{status: http.StatusOK, action: action, targetType: "COURSE", targetID: f.courseID, committed: func(t *testing.T, f *privilegedAuditFixture) {
			assertCourseValue(t, f, `SELECT lifecycle::text FROM courses WHERE id = $1::uuid`, string(target))
		}})
	}
}

func relistAuditScenario(t *testing.T, f *privilegedAuditFixture, route gin.RouteInfo) privilegedAuditExpectation {
	f.preparePublished(t)
	if _, err := f.repo.TransitionCourseLifecycle(f.ctx, catalog.LifecycleMutation{CourseID: f.courseID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID, Target: catalog.LifecycleDelisted}); err != nil {
		t.Fatalf("preparing delisted course: %v", err)
	}
	return f.execute(t, route, "", privilegedAuditExpectation{status: http.StatusOK, action: "COURSE_RELISTED", targetType: "COURSE", targetID: f.courseID, committed: func(t *testing.T, f *privilegedAuditFixture) {
		assertCourseValue(t, f, `SELECT lifecycle::text FROM courses WHERE id = $1::uuid`, "PUBLISHED")
	}})
}

func retireAuditScenario(t *testing.T, f *privilegedAuditFixture, route gin.RouteInfo) privilegedAuditExpectation {
	f.preparePublished(t)
	return f.execute(t, route, "", privilegedAuditExpectation{status: http.StatusOK, action: "COURSE_RETIRED", targetType: "COURSE", targetID: f.courseID, committed: func(t *testing.T, f *privilegedAuditFixture) {
		var retired bool
		if err := f.pool.QueryRow(f.ctx, `SELECT retired_at IS NOT NULL FROM courses WHERE id = $1::uuid`, f.courseID).Scan(&retired); err != nil || !retired {
			t.Fatalf("retired course state = %t (err=%v), want true", retired, err)
		}
	}})
}

func deleteAuditScenario(t *testing.T, f *privilegedAuditFixture, route gin.RouteInfo) privilegedAuditExpectation {
	f.replaceWithDeletableCourse(t)
	return f.execute(t, route, "", privilegedAuditExpectation{status: http.StatusNoContent, action: "COURSE_DELETED", targetType: "COURSE", targetID: f.courseID, committed: func(t *testing.T, f *privilegedAuditFixture) {
		var exists bool
		if err := f.pool.QueryRow(f.ctx, `SELECT EXISTS (SELECT 1 FROM courses WHERE id = $1::uuid)`, f.courseID).Scan(&exists); err != nil || exists {
			t.Fatalf("deleted course exists = %t (err=%v), want false", exists, err)
		}
	}})
}

func (f *privilegedAuditFixture) replaceWithDeletableCourse(t *testing.T) {
	t.Helper()
	course, err := f.repo.CreateCourse(f.ctx, catalog.CreateCourseRequest{
		OwnerAccountID: f.instructorID, TitleAr: "دورة حذف", TitleEn: "Deletable course", DescriptionAr: "وصف", DescriptionEn: "Description",
	}, f.instructorID)
	if err != nil {
		t.Fatalf("creating deletable fixture course: %v", err)
	}
	f.courseID = course.ID
	f.revisionID = course.EditableRevision.ID
}

func ownerAuditScenario(t *testing.T, f *privilegedAuditFixture, route gin.RouteInfo) privilegedAuditExpectation {
	f.preparePublished(t)
	const newOwnerID = "10000000-0000-0000-0000-000000000003"
	return f.execute(t, route, `{"owner_account_id":"`+newOwnerID+`"}`, privilegedAuditExpectation{status: http.StatusOK, action: "COURSE_OWNER_REASSIGNED", targetType: "COURSE", targetID: f.courseID, committed: func(t *testing.T, f *privilegedAuditFixture) {
		assertCourseValue(t, f, `SELECT owner_account_id::text FROM courses WHERE id = $1::uuid`, newOwnerID)
	}})
}

func suspendAuditScenario(t *testing.T, f *privilegedAuditFixture, route gin.RouteInfo) privilegedAuditExpectation {
	return f.execute(t, route, `{"cause":"SECURITY","reason":"Audit proof suspension"}`, privilegedAuditExpectation{status: http.StatusOK, action: "COURSE_ACCESS_SUSPENDED", targetType: "COURSE", targetID: f.courseID, committed: func(t *testing.T, f *privilegedAuditFixture) {
		var suspended bool
		if err := f.pool.QueryRow(f.ctx, `SELECT access_suspended_at IS NOT NULL FROM courses WHERE id = $1::uuid`, f.courseID).Scan(&suspended); err != nil || !suspended {
			t.Fatalf("suspended course state = %t (err=%v), want true", suspended, err)
		}
	}})
}

func restoreAuditScenario(t *testing.T, f *privilegedAuditFixture, route gin.RouteInfo) privilegedAuditExpectation {
	if _, err := f.repo.SuspendCourseAccess(f.ctx, catalog.SuspendCourseAccessRequest{CourseID: f.courseID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID, Cause: catalog.SuspensionCauseSecurity, Reason: "Fixture suspension"}); err != nil {
		t.Fatalf("preparing suspended course: %v", err)
	}
	return f.execute(t, route, `{"reason":"Audit proof restoration"}`, privilegedAuditExpectation{status: http.StatusOK, action: "COURSE_ACCESS_RESTORED", targetType: "COURSE", targetID: f.courseID, committed: func(t *testing.T, f *privilegedAuditFixture) {
		var suspended bool
		if err := f.pool.QueryRow(f.ctx, `SELECT access_suspended_at IS NOT NULL FROM courses WHERE id = $1::uuid`, f.courseID).Scan(&suspended); err != nil || suspended {
			t.Fatalf("restored course suspended state = %t (err=%v), want false", suspended, err)
		}
	}})
}

func taxonomyAssignmentAuditScenario(t *testing.T, f *privilegedAuditFixture, route gin.RouteInfo) privilegedAuditExpectation {
	f.ensureTerms(t)
	body := fmt.Sprintf(`{"revision_id":%q,"major_term_id":%q,"subject_term_id":%q}`, f.revisionID, f.majorID, f.subjectID)
	return f.execute(t, route, body, privilegedAuditExpectation{status: http.StatusOK, action: "COURSE_REVISION_UPDATED", targetType: "COURSE", targetID: f.courseID, committed: func(t *testing.T, f *privilegedAuditFixture) {
		var major, subject string
		if err := f.pool.QueryRow(f.ctx, `SELECT major_term_id::text, subject_term_id::text FROM course_revisions WHERE id = $1::uuid`, f.revisionID).Scan(&major, &subject); err != nil || major != f.majorID || subject != f.subjectID {
			t.Fatalf("taxonomy assignment = %s/%s (err=%v), want %s/%s", major, subject, err, f.majorID, f.subjectID)
		}
	}})
}

func createTermAuditScenario(t *testing.T, f *privilegedAuditFixture, route gin.RouteInfo) privilegedAuditExpectation {
	const label = "Audit-created term"
	want := privilegedAuditExpectation{status: http.StatusCreated, action: "TAXONOMY_TERM_CREATED", targetType: "TAXONOMY_TERM"}
	response := f.request(t, route, `{"kind":"MAJOR","label_ar":"مصطلح تدقيق","label_en":"`+label+`"}`)
	defer response.Body.Close()
	if response.StatusCode != want.status {
		t.Fatalf("%s %s status = %d, want %d", route.Method, route.Path, response.StatusCode, want.status)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT id::text FROM taxonomy_terms WHERE label_en = $1`, label).Scan(&want.targetID); err != nil {
		t.Fatalf("loading created taxonomy term: %v", err)
	}
	want.committed = func(t *testing.T, f *privilegedAuditFixture) {
		var count int
		if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM taxonomy_terms WHERE id = $1::uuid`, want.targetID).Scan(&count); err != nil || count != 1 {
			t.Fatalf("created taxonomy term count = %d (err=%v), want 1", count, err)
		}
	}
	return want
}

func renameTermAuditScenario(t *testing.T, f *privilegedAuditFixture, route gin.RouteInfo) privilegedAuditExpectation {
	f.ensureTerms(t)
	f.termID = f.majorID
	return f.execute(t, route, `{"label_ar":"علوم محدثة","label_en":"Updated Science"}`, privilegedAuditExpectation{status: http.StatusOK, action: "TAXONOMY_TERM_RENAMED", targetType: "TAXONOMY_TERM", targetID: f.termID, committed: func(t *testing.T, f *privilegedAuditFixture) {
		var label string
		if err := f.pool.QueryRow(f.ctx, `SELECT label_en FROM taxonomy_terms WHERE id = $1::uuid`, f.termID).Scan(&label); err != nil || label != "Updated Science" {
			t.Fatalf("renamed taxonomy label = %q (err=%v)", label, err)
		}
	}})
}

func retireTermAuditScenario(t *testing.T, f *privilegedAuditFixture, route gin.RouteInfo) privilegedAuditExpectation {
	f.ensureTerms(t)
	f.termID = f.majorID
	return f.execute(t, route, "", privilegedAuditExpectation{status: http.StatusOK, action: "TAXONOMY_TERM_RETIRED", targetType: "TAXONOMY_TERM", targetID: f.termID, committed: func(t *testing.T, f *privilegedAuditFixture) {
		var retired bool
		if err := f.pool.QueryRow(f.ctx, `SELECT retired_at IS NOT NULL FROM taxonomy_terms WHERE id = $1::uuid`, f.termID).Scan(&retired); err != nil || !retired {
			t.Fatalf("retired taxonomy state = %t (err=%v), want true", retired, err)
		}
	}})
}

func deleteTermAuditScenario(t *testing.T, f *privilegedAuditFixture, route gin.RouteInfo) privilegedAuditExpectation {
	f.ensureTerms(t)
	f.termID = f.majorID
	return f.execute(t, route, "", privilegedAuditExpectation{status: http.StatusNoContent, action: "TAXONOMY_TERM_DELETED", targetType: "TAXONOMY_TERM", targetID: f.termID, committed: func(t *testing.T, f *privilegedAuditFixture) {
		var exists bool
		if err := f.pool.QueryRow(f.ctx, `SELECT EXISTS (SELECT 1 FROM taxonomy_terms WHERE id = $1::uuid)`, f.termID).Scan(&exists); err != nil || exists {
			t.Fatalf("deleted taxonomy term exists = %t (err=%v), want false", exists, err)
		}
	}})
}

func assertCourseValue(t *testing.T, f *privilegedAuditFixture, query, want string) {
	t.Helper()
	var got string
	if err := f.pool.QueryRow(f.ctx, query, f.courseID).Scan(&got); err != nil || got != want {
		t.Fatalf("committed course value = %q (err=%v), want %q", got, err, want)
	}
}

func assertPriceChange(t *testing.T, f *privilegedAuditFixture, courseID, sectionID string, want int64) {
	t.Helper()
	var got int64
	query := `SELECT new_value_minor_units FROM course_price_changes WHERE course_id = $1::uuid AND section_id IS NULL`
	args := []any{courseID}
	if sectionID != "" {
		query = `SELECT new_value_minor_units FROM course_price_changes WHERE course_id = $1::uuid AND section_id = $2::uuid`
		args = append(args, sectionID)
	}
	if err := f.pool.QueryRow(f.ctx, query, args...).Scan(&got); err != nil || got != want {
		t.Fatalf("committed price change = %d (err=%v), want %d", got, err, want)
	}
}
