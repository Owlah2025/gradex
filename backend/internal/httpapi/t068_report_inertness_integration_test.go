//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Owlah2025/gradex/backend/internal/learning"
	"github.com/Owlah2025/gradex/backend/internal/ratelimit"
)

// T068: a report is an inert signal (FR-031, SC-011, BR-146).
//
// Filing a report must not hide, retire, alter, reorder, or mark the content it names. The risk this
// guards against is not a line of code someone wrote on purpose — it is the ordinary product
// instinct that a reported item should be dimmed, flagged, or pushed down while someone looks at it.
// S5 has no moderation at all; the queue is S8's, and until then the content a Student reported must
// look exactly as it did a moment earlier, to them and to everyone else.
//
// The evidence is therefore comparative rather than negative: a full row-level snapshot of the
// Course graph, its media, and the Student's learning state, plus the rendered read models, captured
// before and after a real submission through the production router.

// t068Graph is the ordered graph a reordering would disturb. A single-Lesson fixture cannot detect
// promotion, demotion, or regrouping, so this adds a second Section and extra Lessons.
type t068Graph struct {
	sectionIdentityA string
	sectionIdentityB string
	// lessonOrder is every stable Lesson identity in authored order across both Sections.
	lessonOrder []string
	resource    string
	lab         string
}

// seedT068OrderedGraph extends the accepted fixture with a second Section and two more Lessons, so
// order is observable. It uses the same live revision the fixture published.
func seedT068OrderedGraph(t *testing.T, f learningIntegrationFixture) t068Graph {
	t.Helper()
	ctx := context.Background()

	var revisionID, sectionIdentityA, sectionRowA string
	if err := f.pool.QueryRow(ctx, `
		SELECT cs.revision_id::text, cs.section_identity_id::text, cs.id::text
		FROM course_sections cs WHERE cs.course_id = $1::uuid
	`, f.courseID).Scan(&revisionID, &sectionIdentityA, &sectionRowA); err != nil {
		t.Fatalf("resolving seeded section: %v", err)
	}

	graph := t068Graph{sectionIdentityA: sectionIdentityA, sectionIdentityB: uuid.NewString()}
	sectionRowB := uuid.NewString()
	lessonA2, lessonB1, lessonB2 := uuid.NewString(), uuid.NewString(), uuid.NewString()

	statements := []struct {
		query string
		args  []any
	}{
		// A second Section, authored after the first.
		{`INSERT INTO course_section_identities (id, course_id) VALUES ($1::uuid, $2::uuid)`,
			[]any{graph.sectionIdentityB, f.courseID}},
		{`INSERT INTO course_sections (id, revision_id, course_id, section_identity_id, title_ar, title_en, position)
		  VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'القسم الثاني', 'Section Two', 1)`,
			[]any{sectionRowB, revisionID, f.courseID, graph.sectionIdentityB}},
		// A second Lesson in Section One, after the fixture's.
		{`INSERT INTO course_lesson_identities (id, course_id, section_identity_id) VALUES ($1::uuid, $2::uuid, $3::uuid)`,
			[]any{lessonA2, f.courseID, sectionIdentityA}},
		{`INSERT INTO course_lessons (id, section_id, course_id, section_identity_id, lesson_identity_id, title_ar, title_en, position)
		  VALUES (gen_random_uuid(), $1::uuid, $2::uuid, $3::uuid, $4::uuid, 'الدرس الثاني', 'Lesson A2', 1)`,
			[]any{sectionRowA, f.courseID, sectionIdentityA, lessonA2}},
		// Two Lessons in Section Two.
		{`INSERT INTO course_lesson_identities (id, course_id, section_identity_id) VALUES ($1::uuid, $2::uuid, $3::uuid)`,
			[]any{lessonB1, f.courseID, graph.sectionIdentityB}},
		{`INSERT INTO course_lessons (id, section_id, course_id, section_identity_id, lesson_identity_id, title_ar, title_en, position)
		  VALUES (gen_random_uuid(), $1::uuid, $2::uuid, $3::uuid, $4::uuid, 'الدرس الثالث', 'Lesson B1', 0)`,
			[]any{sectionRowB, f.courseID, graph.sectionIdentityB, lessonB1}},
		{`INSERT INTO course_lesson_identities (id, course_id, section_identity_id) VALUES ($1::uuid, $2::uuid, $3::uuid)`,
			[]any{lessonB2, f.courseID, graph.sectionIdentityB}},
		{`INSERT INTO course_lessons (id, section_id, course_id, section_identity_id, lesson_identity_id, title_ar, title_en, position)
		  VALUES (gen_random_uuid(), $1::uuid, $2::uuid, $3::uuid, $4::uuid, 'الدرس الرابع', 'Lesson B2', 1)`,
			[]any{sectionRowB, f.courseID, graph.sectionIdentityB, lessonB2}},
	}
	for _, statement := range statements {
		if _, err := f.pool.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seeding ordered graph: %v\n%s", err, statement.query)
		}
	}

	graph.lessonOrder = []string{f.lessonID, lessonA2, lessonB1, lessonB2}
	graph.resource = attachLessonMaterial(t, f, "RESOURCE", "t068-resource")
	graph.lab = attachLessonMaterial(t, f, "LAB_MATERIAL", "t068-lab")
	return graph
}

// t068ContentSnapshot captures every row that could hide, retire, alter, reorder, or mark the
// content — whole rows, not selected columns, so an in-place change to any field is caught.
func t068ContentSnapshot(t *testing.T, f learningIntegrationFixture) map[string]string {
	t.Helper()
	queries := map[string]string{
		"courses":            `SELECT COALESCE(jsonb_agg(to_jsonb(c) ORDER BY c.id), '[]'::jsonb)::text FROM courses c WHERE c.id = $1::uuid`,
		"revisions":          `SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY r.id), '[]'::jsonb)::text FROM course_revisions r WHERE r.course_id = $1::uuid`,
		"section_identities": `SELECT COALESCE(jsonb_agg(to_jsonb(s) ORDER BY s.id), '[]'::jsonb)::text FROM course_section_identities s WHERE s.course_id = $1::uuid`,
		"sections":           `SELECT COALESCE(jsonb_agg(to_jsonb(s) ORDER BY s.id), '[]'::jsonb)::text FROM course_sections s WHERE s.course_id = $1::uuid`,
		"lesson_identities":  `SELECT COALESCE(jsonb_agg(to_jsonb(i) ORDER BY i.id), '[]'::jsonb)::text FROM course_lesson_identities i WHERE i.course_id = $1::uuid`,
		"lessons":            `SELECT COALESCE(jsonb_agg(to_jsonb(l) ORDER BY l.id), '[]'::jsonb)::text FROM course_lessons l WHERE l.course_id = $1::uuid`,
		"lesson_files":       `SELECT COALESCE(jsonb_agg(to_jsonb(lf) ORDER BY lf.lesson_id, lf.kind), '[]'::jsonb)::text FROM lesson_files lf JOIN course_lessons cl ON cl.id = lf.lesson_id WHERE cl.course_id = $1::uuid`,
		"media_assets":       `SELECT COALESCE(jsonb_agg(to_jsonb(a) ORDER BY a.id), '[]'::jsonb)::text FROM media_assets a WHERE a.course_id = $1::uuid`,
		"asset_versions":     `SELECT COALESCE(jsonb_agg(to_jsonb(v) ORDER BY v.id), '[]'::jsonb)::text FROM media_asset_versions v JOIN media_assets a ON a.id = v.logical_asset_id WHERE a.course_id = $1::uuid`,
		"renditions":         `SELECT COALESCE(jsonb_agg(to_jsonb(vr) ORDER BY vr.asset_version_id, vr.name), '[]'::jsonb)::text FROM video_renditions vr JOIN media_asset_versions v ON v.id = vr.asset_version_id JOIN media_assets a ON a.id = v.logical_asset_id WHERE a.course_id = $1::uuid`,
		"accounts": `SELECT COALESCE(jsonb_agg(to_jsonb(ac) ORDER BY ac.id), '[]'::jsonb)::text FROM accounts ac
		             WHERE ac.id IN (SELECT owner_account_id FROM courses WHERE id = $1::uuid)
		                OR ac.id IN (SELECT student_account_id FROM enrollments WHERE course_id = $1::uuid)`,
		"enrollments":  `SELECT COALESCE(jsonb_agg(to_jsonb(e) ORDER BY e.id), '[]'::jsonb)::text FROM enrollments e WHERE e.course_id = $1::uuid`,
		"entitlements": `SELECT COALESCE(jsonb_agg(to_jsonb(en) ORDER BY en.id), '[]'::jsonb)::text FROM entitlements en WHERE en.course_id = $1::uuid`,
		"progress":     `SELECT COALESCE(jsonb_agg(to_jsonb(p) ORDER BY p.enrollment_id, p.course_lesson_identity_id), '[]'::jsonb)::text FROM progress p JOIN enrollments e ON e.id = p.enrollment_id WHERE e.course_id = $1::uuid`,
	}
	snapshot := make(map[string]string, len(queries))
	for name, query := range queries {
		var value string
		if err := f.pool.QueryRow(context.Background(), query, f.courseID).Scan(&value); err != nil {
			t.Fatalf("snapshotting %s: %v", name, err)
		}
		snapshot[name] = value
	}
	return snapshot
}

func assertContentUnchanged(t *testing.T, label string, before, after map[string]string) {
	t.Helper()
	for relation, want := range before {
		if after[relation] != want {
			t.Fatalf("%s changed %s:\nbefore %s\nafter  %s", label, relation, want, after[relation])
		}
	}
	if len(before) != len(after) {
		t.Fatalf("%s changed the snapshot shape", label)
	}
}

// stripReportContexts removes the opaque tokens and nothing else.
//
// A context is freshly minted per render with a random nonce, so two reads of identical content
// legitimately differ there and only there. Every authored value, status, order, and availability
// field is left in place, because those are exactly what must not change.
func stripReportContexts(t *testing.T, body []byte) string {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decoding read model: %v", err)
	}
	if _, present := decoded["report_context"]; present {
		decoded["report_context"] = "<context>"
	}
	if _, present := decoded["report_contexts"]; present {
		decoded["report_contexts"] = "<contexts>"
	}
	normalized, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("re-encoding read model: %v", err)
	}
	return string(normalized)
}

// t068ReadSnapshot renders every protected read a Student can reach, through the production router.
func t068ReadSnapshot(t *testing.T, f learningIntegrationFixture, graph t068Graph) map[string]string {
	t.Helper()
	snapshot := make(map[string]string, len(graph.lessonOrder)+2)

	home := f.request(http.MethodGet, "/api/v1/learn/courses/"+f.courseID, "")
	if home.Code != http.StatusOK {
		t.Fatalf("course home = %d %s", home.Code, home.Body.String())
	}
	snapshot["course_home"] = stripReportContexts(t, home.Body.Bytes())

	dashboard := f.request(http.MethodGet, "/api/v1/learn/dashboard", "")
	if dashboard.Code != http.StatusOK {
		t.Fatalf("dashboard = %d", dashboard.Code)
	}
	snapshot["dashboard"] = dashboard.Body.String()

	for _, lessonID := range graph.lessonOrder {
		response := f.request(http.MethodGet, "/api/v1/learn/courses/"+f.courseID+"/lessons/"+lessonID, "")
		if response.Code != http.StatusOK {
			t.Fatalf("lesson %s = %d %s", lessonID, response.Code, response.Body.String())
		}
		snapshot["lesson:"+lessonID] = stripReportContexts(t, response.Body.Bytes())
	}
	return snapshot
}

func assertReadsUnchanged(t *testing.T, label string, before, after map[string]string) {
	t.Helper()
	if len(before) != len(after) {
		t.Fatalf("%s changed which reads are available: %d before, %d after", label, len(before), len(after))
	}
	for surface, want := range before {
		if after[surface] != want {
			t.Fatalf("%s changed %s:\nbefore %s\nafter  %s", label, surface, want, after[surface])
		}
	}
}

// visibleOrder reads the rendered Course Home ordering — the order a Student actually sees, not the
// authored integers behind it.
func visibleOrder(t *testing.T, f learningIntegrationFixture) []string {
	t.Helper()
	response := f.request(http.MethodGet, "/api/v1/learn/courses/"+f.courseID, "")
	if response.Code != http.StatusOK {
		t.Fatalf("course home = %d", response.Code)
	}
	var body struct {
		Sections []struct {
			SectionID string `json:"section_id"`
			Title     string `json:"title"`
			Lessons   []struct {
				LessonID  string                   `json:"lesson_id"`
				Title     string                   `json:"title"`
				Materials []map[string]interface{} `json:"materials"`
			} `json:"lessons"`
		} `json:"sections"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding course home: %v", err)
	}
	order := make([]string, 0)
	for _, section := range body.Sections {
		order = append(order, "section:"+section.SectionID+"="+section.Title)
		for _, lesson := range section.Lessons {
			kinds := make([]string, 0, len(lesson.Materials))
			for _, material := range lesson.Materials {
				kinds = append(kinds, material["kind"].(string))
			}
			order = append(order, "  lesson:"+lesson.LessonID+"="+lesson.Title+" materials="+strings.Join(kinds, ","))
		}
	}
	return order
}

// TestReportingDoesNotHideRetireAlterReorderOrMarkContent is T068's core matrix: one report per
// target kind, each against a fully captured graph, each followed by a whole-graph comparison.
func TestReportingDoesNotHideRetireAlterReorderOrMarkContent(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	graph := seedT068OrderedGraph(t, f)

	contexts := lessonContextsOf(t, f)
	kinds := []struct {
		name  string
		token string
	}{
		{"COURSE", courseContextOf(t, f)},
		{"LESSON", contexts.Lesson},
		{"VIDEO", contexts.Video},
		{"RESOURCE", contexts.Resource},
		{"LAB_MATERIAL", contexts.LabMaterial},
	}

	for index, kind := range kinds {
		t.Run(kind.name, func(t *testing.T) {
			if kind.token == "" {
				t.Fatalf("%s issued no context; the fixture cannot exercise this kind", kind.name)
			}
			contentBefore := t068ContentSnapshot(t, f)
			readsBefore := t068ReadSnapshot(t, f, graph)
			orderBefore := visibleOrder(t, f)
			reportsBefore := reportCount(t, f)

			response := submitReport(f, kind.token, "inappropriate")
			if response.Code != http.StatusCreated {
				t.Fatalf("%s report = %d %s", kind.name, response.Code, response.Body.String())
			}

			// Exactly one row was added, and it is the report.
			if got := reportCount(t, f); got != reportsBefore+1 {
				t.Fatalf("%s created %d report rows, want exactly 1", kind.name, got-reportsBefore)
			}
			if got := len(reportRowsFor(t, f, kind.name)); got != 1 {
				t.Fatalf("%s produced %d rows of its own kind", kind.name, got)
			}

			// Nothing else moved: not a row, not a field, not an order, not a rendered byte.
			assertContentUnchanged(t, kind.name+" report", contentBefore, t068ContentSnapshot(t, f))
			assertReadsUnchanged(t, kind.name+" report", readsBefore, t068ReadSnapshot(t, f, graph))
			if orderAfter := visibleOrder(t, f); !reflect.DeepEqual(orderBefore, orderAfter) {
				t.Fatalf("%s report reordered content:\nbefore %v\nafter  %v", kind.name, orderBefore, orderAfter)
			}

			// The fixture must actually be able to detect a reorder.
			if len(orderBefore) < 6 {
				t.Fatalf("the ordering fixture is too small to detect a reorder: %v", orderBefore)
			}
			_ = index
		})
	}

	// Five kinds, five reports — and a Course whose graph is byte-identical to where it started.
	if got := reportCount(t, f); got != len(kinds) {
		t.Fatalf("report rows = %d, want one per kind", got)
	}
}

// TestReportedContentCarriesNoReportDerivedMarker proves nothing was added for a marker to live in,
// and nothing projects one into a read.
func TestReportedContentCarriesNoReportDerivedMarker(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	graph := seedT068OrderedGraph(t, f)

	if response := submitReport(f, courseContextOf(t, f), "inappropriate"); response.Code != http.StatusCreated {
		t.Fatalf("report = %d %s", response.Code, response.Body.String())
	}

	// No learning table gained a report-derived column. `content_reports` is relationally separate:
	// no other table references it, so no read can join a marker out of it.
	rows, err := f.pool.Query(context.Background(), `
		SELECT table_name, column_name FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name IN ('courses','course_revisions','course_sections','course_lessons',
		                     'course_section_identities','course_lesson_identities','lesson_files',
		                     'media_assets','media_asset_versions','enrollments','entitlements','progress')
	`)
	if err != nil {
		t.Fatalf("reading learning columns: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var table, column string
		if err := rows.Scan(&table, &column); err != nil {
			t.Fatalf("scanning column: %v", err)
		}
		for _, marker := range []string{"report", "flag", "under_review", "moderat", "hidden", "takedown"} {
			if strings.Contains(column, marker) {
				t.Fatalf("%s.%s looks like a report-derived marker", table, column)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating columns: %v", err)
	}

	var referencing int
	if err := f.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM information_schema.table_constraints tc
		JOIN information_schema.constraint_column_usage ccu ON ccu.constraint_name = tc.constraint_name
		WHERE tc.constraint_type = 'FOREIGN KEY' AND ccu.table_name = 'content_reports'
	`).Scan(&referencing); err != nil {
		t.Fatalf("reading foreign keys onto content_reports: %v", err)
	}
	if referencing != 0 {
		t.Fatalf("%d tables reference content_reports; a report could become content state", referencing)
	}

	// And no rendered read mentions a report at all beyond the contexts D-065 issues.
	for surface, body := range t068ReadSnapshot(t, f, graph) {
		lowered := strings.ToLower(body)
		for _, marker := range []string{
			"reported", "flagged", "under_review", "has_reports", "report_count",
			"moderation", "queue", "warning", "takedown", "hidden",
		} {
			if strings.Contains(lowered, marker) {
				t.Fatalf("%s exposed %q after a report: %s", surface, marker, body)
			}
		}
	}
}

// TestReportingChangesNothingForAnotherStudent is SC-011 from the other side: a report is one
// Student's private signal, and a second Student's view of the same Course is untouched by it.
func TestReportingChangesNothingForAnotherStudent(t *testing.T) {
	reporter := newLearningIntegrationFixture(t)
	graph := seedT068OrderedGraph(t, reporter)

	// A second Student, independently enrolled and entitled in the same database and Course.
	observer := newLearningIntegrationFixtureWith(t, learningFixtureOptions{pool: reporter.pool})
	observerReads := func() map[string]string {
		return t068ReadSnapshot(t, observer, t068Graph{lessonOrder: []string{observer.lessonID}})
	}

	// The observer also reads the reporter's Course, which is the view that must not change.
	sharedCourse := func() string {
		response := observer.request(http.MethodGet, "/api/v1/learn/courses/"+reporter.courseID, "")
		// The observer is enrolled in their own Course, not the reporter's, so this is the uniform
		// refusal — and it must stay the uniform refusal, not become "reported".
		return response.Body.String() + "|" + http.StatusText(response.Code)
	}

	beforeOwn := observerReads()
	beforeShared := sharedCourse()
	beforeContent := t068ContentSnapshot(t, reporter)

	for _, token := range []string{courseContextOf(t, reporter), lessonContextsOf(t, reporter).Lesson} {
		if response := submitReport(reporter, token, "inaccurate"); response.Code != http.StatusCreated {
			t.Fatalf("reporter submission = %d %s", response.Code, response.Body.String())
		}
	}

	if after := observerReads(); !reflect.DeepEqual(beforeOwn, after) {
		t.Fatal("another Student's own reads changed after an unrelated report")
	}
	if after := sharedCourse(); after != beforeShared {
		t.Fatalf("another Student's view of the reported Course changed:\nbefore %s\nafter  %s", beforeShared, after)
	}
	assertContentUnchanged(t, "reports by one Student", beforeContent, t068ContentSnapshot(t, reporter))

	// The reporter still sees their own content unchanged too, and the graph still renders.
	if order := visibleOrder(t, reporter); len(order) < 6 {
		t.Fatalf("the reporter's Course stopped rendering its full graph: %v", order)
	}
	_ = graph
}

// TestReportingNeitherRetiresTheReportedInstanceNorDisturbsTheCurrentOne is the exact-visible half
// of FR-031: reporting the instance a stale page rendered must not retire it, resurrect it, or move
// the live pointer.
func TestReportingNeitherRetiresTheReportedInstanceNorDisturbsTheCurrentOne(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	graph := seedT068OrderedGraph(t, f)

	revisionA := liveRevisionOf(t, f)
	staleCourse := courseContextOf(t, f)
	staleLesson := lessonContextsOf(t, f)

	// B becomes current while the page sits open.
	f.replaceLiveRevision(t)
	revisionB := liveRevisionOf(t, f)
	if revisionA == revisionB {
		t.Fatal("revision B did not become live")
	}

	contentBefore := t068ContentSnapshot(t, f)
	readsBefore := t068ReadSnapshot(t, f, t068Graph{lessonOrder: []string{f.lessonID}})
	playbackBefore := f.request(http.MethodPost, "/api/v1/learn/lessons/"+f.lessonID+"/playback", "")
	if playbackBefore.Code != http.StatusOK {
		t.Fatalf("playback before = %d %s", playbackBefore.Code, playbackBefore.Body.String())
	}

	// Report the stale instances.
	for name, token := range map[string]string{"COURSE": staleCourse, "LESSON": staleLesson.Lesson, "VIDEO": staleLesson.Video} {
		if response := submitReport(f, token, "broken_unavailable"); response.Code != http.StatusCreated {
			t.Fatalf("%s report on the stale page = %d %s", name, response.Code, response.Body.String())
		}
	}

	// Revision A is stored by the reports and is neither retired nor re-promoted.
	var storedRefs []string
	rows, err := f.pool.Query(context.Background(), `SELECT target_revision_ref::text FROM content_reports ORDER BY target_kind`)
	if err != nil {
		t.Fatalf("reading reports: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			t.Fatalf("scanning ref: %v", err)
		}
		storedRefs = append(storedRefs, ref)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating reports: %v", err)
	}
	sort.Strings(storedRefs)
	if len(storedRefs) != 3 {
		t.Fatalf("stored refs = %v, want three", storedRefs)
	}

	// The live pointer still names B: a report never makes the reported instance current again.
	if live := liveRevisionOf(t, f); live != revisionB {
		t.Fatalf("live revision = %s after reporting A, want B %s", live, revisionB)
	}
	// Both revisions keep their state — A is not retired for having been reported.
	for name, revision := range map[string]string{"A": revisionA, "B": revisionB} {
		var state string
		if err := f.pool.QueryRow(context.Background(),
			`SELECT state::text FROM course_revisions WHERE id = $1::uuid`, revision).Scan(&state); err != nil {
			t.Fatalf("reading revision %s: %v", name, err)
		}
		if state != "APPROVED" {
			t.Fatalf("revision %s state = %s after a report", name, state)
		}
	}
	// Media the reported instance referenced is still READY: reporting retires nothing.
	var readyVersions int
	if err := f.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM media_asset_versions v JOIN media_assets a ON a.id = v.logical_asset_id
		WHERE a.course_id = $1::uuid AND v.state = 'READY'
	`, f.courseID).Scan(&readyVersions); err != nil {
		t.Fatalf("counting ready versions: %v", err)
	}
	if readyVersions != 3 {
		t.Fatalf("READY asset versions = %d after reporting, want the seeded 3", readyVersions)
	}

	assertContentUnchanged(t, "stale-instance reports", contentBefore, t068ContentSnapshot(t, f))
	assertReadsUnchanged(t, "stale-instance reports", readsBefore, t068ReadSnapshot(t, f, t068Graph{lessonOrder: []string{f.lessonID}}))

	// S4 still resolves the same exact playable version afterwards.
	playbackAfter := f.request(http.MethodPost, "/api/v1/learn/lessons/"+f.lessonID+"/playback", "")
	if playbackAfter.Code != http.StatusOK {
		t.Fatalf("playback after = %d %s", playbackAfter.Code, playbackAfter.Body.String())
	}
	beforeVersion := playbackVersionOf(t, playbackBefore.Body.Bytes())
	afterVersion := playbackVersionOf(t, playbackAfter.Body.Bytes())
	if beforeVersion != afterVersion {
		t.Fatalf("playback resolved %s before the report and %s after", beforeVersion, afterVersion)
	}
	_ = graph
}

func playbackVersionOf(t *testing.T, body []byte) string {
	t.Helper()
	var decoded struct {
		AssetVersionID string `json:"asset_version_id"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decoding playback: %v", err)
	}
	if decoded.AssetVersionID == "" {
		t.Fatal("playback carried no asset version")
	}
	return decoded.AssetVersionID
}

// TestRefusedDuplicateAndThrottledSubmissionsChangeNoContent completes the picture: a submission
// that never becomes a report must not change content either. Only limiter-owned state may move.
func TestRefusedDuplicateAndThrottledSubmissionsChangeNoContent(t *testing.T) {
	f := throttledFixture(t)
	graph := seedT068OrderedGraph(t, f)

	// One accepted report, so a duplicate is reachable.
	first := courseContextOf(t, f)
	if response := submitReport(f, first, "inaccurate"); response.Code != http.StatusCreated {
		t.Fatalf("first report = %d %s", response.Code, response.Body.String())
	}

	contentBefore := t068ContentSnapshot(t, f)
	readsBefore := t068ReadSnapshot(t, f, graph)
	reportsBefore := reportCount(t, f)

	// Duplicate: the Student's own open report for the same target.
	duplicate := submitReport(f, first, "inappropriate")
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate = %d %s", duplicate.Code, duplicate.Body.String())
	}

	// Protected refusal: a cryptographically valid context naming a target that is not coherent.
	foreign := fixtureContext(t, f, learning.ReportContextRequest{
		TargetKind: learning.ReportTargetCourse, CourseID: f.courseID,
		StableTargetID: f.courseID, VisibleCourseRevisionID: uuid.NewString(),
	})
	assertProtectedUnavailable(t, submitReport(f, foreign, "inaccurate"))

	// Throttle: three attempts are already spent — the accepted report, the duplicate, and the
	// protected refusal, since every authenticated submission costs one (T064). The remaining
	// fillers are refusals too, so exhausting the quota creates no report of its own.
	for spent := 3; spent < int(ratelimit.ProtectedLearningReportsPerHour); spent++ {
		filler := fixtureContext(t, f, learning.ReportContextRequest{
			TargetKind: learning.ReportTargetCourse, CourseID: f.courseID,
			StableTargetID: f.courseID, VisibleCourseRevisionID: uuid.NewString(),
		})
		assertProtectedUnavailable(t, submitReport(f, filler, "inaccurate"))
	}
	assertThrottled(t, submitReport(f, first, "inaccurate"))

	// No refusal created a report, and none touched the content.
	if got := reportCount(t, f); got != reportsBefore {
		t.Fatalf("refused submissions changed report rows from %d to %d", reportsBefore, got)
	}
	assertContentUnchanged(t, "refused submissions", contentBefore, t068ContentSnapshot(t, f))
	assertReadsUnchanged(t, "refused submissions", readsBefore, t068ReadSnapshot(t, f, graph))
}
