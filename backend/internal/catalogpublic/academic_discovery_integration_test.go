//go:build integration

package catalogpublic

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// T6 — academic discovery, against real PostgreSQL.
//
// These are database tests on purpose. The whole point of the audience rule is
// what happens when one Subject is mapped into several curricula: an
// implementation that joins instead of using EXISTS returns the same Course
// several times, and only a real query against real mapping rows can show that.

type discoveryFixture struct {
	ctx        context.Context
	pool       *pgxpool.Pool
	repository *Repository
	instructor string

	kuwait  string // institution id
	gulf    string // a second, unrelated institution
	compEng string // program id, Kuwait
	compSci string // program id, Kuwait
	// dataStructures is mapped into BOTH Kuwait programs' curricula. It is the
	// duplicate-prevention case.
	dataStructures string
	networks       string
	retiredSubject string
	gulfSubject    string
}

func newDiscoveryFixture(t *testing.T) *discoveryFixture {
	t.Helper()
	freshCatalogPublicSchema(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)
	pool, err := pgxpool.New(ctx, catalogPublicTestDSN)
	if err != nil {
		t.Fatalf("opening test database: %v", err)
	}
	t.Cleanup(pool.Close)
	repository, err := NewRepository(pool, PublishedOnly)
	if err != nil {
		t.Fatalf("constructing repository: %v", err)
	}

	f := &discoveryFixture{ctx: ctx, pool: pool, repository: repository}
	scan := func(dest *string, sql string, args ...any) {
		t.Helper()
		if err := pool.QueryRow(ctx, sql, args...).Scan(dest); err != nil {
			t.Fatalf("seeding: %v", err)
		}
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seeding: %v", err)
		}
	}

	scan(&f.instructor, `INSERT INTO accounts (normalized_email, email, role, status, display_name)
		VALUES ('t6@example.test', 't6@example.test', 'INSTRUCTOR', 'ACTIVE', 'T6 Instructor')
		RETURNING id::text`)
	scan(&f.kuwait, `INSERT INTO institutions (country_code, slug, name_ar, name_en)
		VALUES ('KW', 'kuwait-university', 'جامعة الكويت', 'Kuwait University') RETURNING id::text`)
	scan(&f.gulf, `INSERT INTO institutions (country_code, slug, name_ar, name_en)
		VALUES ('KW', 'gulf-university', 'جامعة الخليج', 'Gulf University') RETURNING id::text`)

	f.compEng = f.program(t, f.kuwait, "computer-engineering", "هندسة الحاسوب", "Computer Engineering")
	f.compSci = f.program(t, f.kuwait, "computer-science", "علوم الحاسوب", "Computer Science")
	gulfProgram := f.program(t, f.gulf, "gulf-computing", "حوسبة", "Gulf Computing")

	f.dataStructures = f.subject(t, f.kuwait, "0418-201", "هياكل البيانات", "Data Structures & Algorithms")
	f.networks = f.subject(t, f.kuwait, "0418-330", "الشبكات", "Computer Networks")
	f.gulfSubject = f.subject(t, f.gulf, "GU-101", "مقدمة", "Gulf Introduction")
	scan(&f.retiredSubject, `INSERT INTO subjects (institution_id, official_code, title_ar, title_en, retired_at)
		VALUES ($1::uuid, '0418-900', 'متقاعدة', 'Retired Subject', now()) RETURNING id::text`, f.kuwait)

	engCurriculum := f.curriculum(t, f.compEng, f.kuwait, "2024-eng")
	sciCurriculum := f.curriculum(t, f.compSci, f.kuwait, "2024-sci")
	gulfCurriculum := f.curriculum(t, gulfProgram, f.gulf, "2024-gulf")

	// The duplicate-prevention setup: ONE Subject in TWO curricula.
	f.mapSubject(t, engCurriculum, f.dataStructures, f.kuwait)
	f.mapSubject(t, sciCurriculum, f.dataStructures, f.kuwait)
	f.mapSubject(t, engCurriculum, f.networks, f.kuwait)
	f.mapSubject(t, gulfCurriculum, f.gulfSubject, f.gulf)

	exec(`SELECT 1`)
	return f
}

func (f *discoveryFixture) program(t *testing.T, institution, slug, nameAr, nameEn string) string {
	t.Helper()
	var id string
	if err := f.pool.QueryRow(f.ctx, `
		INSERT INTO programs (institution_id, slug, name_ar, name_en, degree_kind)
		VALUES ($1::uuid, $2, $3, $4, 'BSC') RETURNING id::text`,
		institution, slug, nameAr, nameEn).Scan(&id); err != nil {
		t.Fatalf("seeding program: %v", err)
	}
	return id
}

func (f *discoveryFixture) subject(t *testing.T, institution, code, titleAr, titleEn string) string {
	t.Helper()
	var id string
	if err := f.pool.QueryRow(f.ctx, `
		INSERT INTO subjects (institution_id, official_code, title_ar, title_en)
		VALUES ($1::uuid, $2, $3, $4) RETURNING id::text`,
		institution, code, titleAr, titleEn).Scan(&id); err != nil {
		t.Fatalf("seeding subject: %v", err)
	}
	return id
}

func (f *discoveryFixture) curriculum(t *testing.T, program, institution, label string) string {
	t.Helper()
	var id string
	if err := f.pool.QueryRow(f.ctx, `
		INSERT INTO curricula (program_id, institution_id, version_label, status)
		VALUES ($1::uuid, $2::uuid, $3, 'ACTIVE') RETURNING id::text`,
		program, institution, label).Scan(&id); err != nil {
		t.Fatalf("seeding curriculum: %v", err)
	}
	return id
}

func (f *discoveryFixture) mapSubject(t *testing.T, curriculum, subject, institution string) {
	t.Helper()
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO curriculum_subjects (curriculum_id, subject_id, institution_id, requirement_kind)
		VALUES ($1::uuid, $2::uuid, $3::uuid, 'MAJOR_CORE')`,
		curriculum, subject, institution); err != nil {
		t.Fatalf("mapping subject: %v", err)
	}
}

// academicCourse builds a real Academic Course. lifecycle drives publication, so
// the visibility tests use the same seeding path as the discoverable ones.
func (f *discoveryFixture) academicCourse(
	t *testing.T, titleEn, lifecycle, institution, subject string,
) (string, string) {
	t.Helper()
	var courseID, revisionID string
	if err := f.pool.QueryRow(f.ctx, `
		INSERT INTO courses (owner_account_id, lifecycle, classification_model, institution_id, subject_id)
		VALUES ($1::uuid, 'DRAFT', 'ACADEMIC_CATALOG', $2::uuid, $3::uuid) RETURNING id::text`,
		f.instructor, institution, subject).Scan(&courseID); err != nil {
		t.Fatalf("seeding academic course: %v", err)
	}
	if err := f.pool.QueryRow(f.ctx, `
		INSERT INTO course_revisions (course_id, state, revision_number, title_ar, title_en,
		                              description_ar, description_en)
		VALUES ($1::uuid, 'APPROVED', 1, $2, $2, 'وصف', 'Description') RETURNING id::text`,
		courseID, titleEn).Scan(&revisionID); err != nil {
		t.Fatalf("seeding revision: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `
		UPDATE courses SET lifecycle = $1, live_revision_id = $2::uuid WHERE id = $3::uuid`,
		lifecycle, revisionID, courseID); err != nil {
		t.Fatalf("publishing: %v", err)
	}
	return courseID, revisionID
}

func (f *discoveryFixture) targetProgram(t *testing.T, courseID, revisionID, programID, institution string) {
	t.Helper()
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO course_program_targets (revision_id, course_id, program_id, institution_id)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid)`,
		revisionID, courseID, programID, institution); err != nil {
		t.Fatalf("seeding program target: %v", err)
	}
}

func (f *discoveryFixture) browse(t *testing.T, filters Filters) ListResult {
	t.Helper()
	result, err := f.repository.Browse(f.ctx, false, 1, 50, "", false, filters)
	if err != nil {
		t.Fatalf("browse: %v", err)
	}
	return result
}

func titlesOf(result ListResult) []string {
	titles := make([]string, 0, len(result.Items))
	for _, item := range result.Items {
		titles = append(titles, item.Title)
	}
	return titles
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func countOf(values []string, want string) int {
	total := 0
	for _, value := range values {
		if value == want {
			total++
		}
	}
	return total
}

// A. Institution filtering.
func TestT6InstitutionFilterNarrowsToOneUniversity(t *testing.T) {
	f := newDiscoveryFixture(t)
	f.academicCourse(t, "Kuwait Course", "PUBLISHED", f.kuwait, f.dataStructures)
	f.academicCourse(t, "Gulf Course", "PUBLISHED", f.gulf, f.gulfSubject)

	kuwait := titlesOf(f.browse(t, Filters{InstitutionSlug: "kuwait-university"}))
	if !contains(kuwait, "Kuwait Course") || contains(kuwait, "Gulf Course") {
		t.Fatalf("kuwait-university results = %v", kuwait)
	}
	gulf := titlesOf(f.browse(t, Filters{InstitutionSlug: "gulf-university"}))
	if !contains(gulf, "Gulf Course") || contains(gulf, "Kuwait Course") {
		t.Fatalf("gulf-university results = %v", gulf)
	}
	// An unfiltered catalogue still shows both. Filtering narrows, never widens.
	if all := titlesOf(f.browse(t, Filters{})); len(all) != 2 {
		t.Fatalf("unfiltered results = %v, want both Courses", all)
	}
	// A retired or unknown Institution is an empty catalogue, not an error.
	if unknown := f.browse(t, Filters{InstitutionSlug: "no-such-university"}); unknown.Total != 0 {
		t.Fatalf("unknown institution total = %d, want 0", unknown.Total)
	}
}

// B. Program filtering — automatic audience.
//
// The Course has ZERO explicit targets. Its Subject is mapped into both Kuwait
// programs' curricula, so it must surface under both.
func TestT6ProgramFilterUsesInferredAudienceWhenNoTargetsExist(t *testing.T) {
	f := newDiscoveryFixture(t)
	f.academicCourse(t, "Inferred Course", "PUBLISHED", f.kuwait, f.dataStructures)
	f.academicCourse(t, "Networks Course", "PUBLISHED", f.kuwait, f.networks)

	engineering := titlesOf(f.browse(t, Filters{ProgramSlug: "computer-engineering"}))
	if !contains(engineering, "Inferred Course") {
		t.Fatalf("computer-engineering results = %v; the Subject's curriculum mapping must imply audience", engineering)
	}
	science := titlesOf(f.browse(t, Filters{ProgramSlug: "computer-science"}))
	if !contains(science, "Inferred Course") {
		t.Fatalf("computer-science results = %v", science)
	}
	// Networks is mapped only into the engineering curriculum.
	if contains(science, "Networks Course") {
		t.Fatalf("computer-science results leaked an unmapped Subject: %v", science)
	}
	if !contains(engineering, "Networks Course") {
		t.Fatalf("computer-engineering results = %v", engineering)
	}
}

// C. Program filtering — explicit targets override inference.
func TestT6ExplicitProgramTargetsOverrideInference(t *testing.T) {
	f := newDiscoveryFixture(t)
	// The Subject maps into BOTH programs, but the revision explicitly targets
	// only Computer Engineering.
	course, revision := f.academicCourse(t, "Targeted Course", "PUBLISHED", f.kuwait, f.dataStructures)
	f.targetProgram(t, course, revision, f.compEng, f.kuwait)

	engineering := titlesOf(f.browse(t, Filters{ProgramSlug: "computer-engineering"}))
	if !contains(engineering, "Targeted Course") {
		t.Fatalf("the explicitly targeted Program must find the Course: %v", engineering)
	}
	science := titlesOf(f.browse(t, Filters{ProgramSlug: "computer-science"}))
	if contains(science, "Targeted Course") {
		t.Fatalf("an explicit audience was widened by inference: %v", science)
	}
	// It is still in the unfiltered catalogue. Audience narrows discovery by
	// Program, it does not remove a Course from the catalogue.
	if all := titlesOf(f.browse(t, Filters{})); !contains(all, "Targeted Course") {
		t.Fatalf("unfiltered catalogue = %v", all)
	}
}

// D. Subject filtering matches the canonical Course-level Subject only.
func TestT6SubjectFilterMatchesCanonicalCourseSubject(t *testing.T) {
	f := newDiscoveryFixture(t)
	f.academicCourse(t, "Structures Course", "PUBLISHED", f.kuwait, f.dataStructures)
	f.academicCourse(t, "Networks Course", "PUBLISHED", f.kuwait, f.networks)

	byCode := titlesOf(f.browse(t, Filters{Subject: "0418-201"}))
	if !contains(byCode, "Structures Course") || contains(byCode, "Networks Course") {
		t.Fatalf("subject filter results = %v", byCode)
	}
	// Normalization is the identity authority, so a differently punctuated code
	// is the same Subject.
	if loose := titlesOf(f.browse(t, Filters{Subject: "0418 201"})); !contains(loose, "Structures Course") {
		t.Fatalf("normalized code did not match: %v", loose)
	}
	if unknown := f.browse(t, Filters{Subject: "9999-999"}); unknown.Total != 0 {
		t.Fatalf("unknown subject total = %d, want 0", unknown.Total)
	}
}

// E. Combined filters compose.
func TestT6CombinedFiltersCompose(t *testing.T) {
	f := newDiscoveryFixture(t)
	f.academicCourse(t, "Match", "PUBLISHED", f.kuwait, f.dataStructures)
	f.academicCourse(t, "Wrong Subject", "PUBLISHED", f.kuwait, f.networks)
	f.academicCourse(t, "Wrong Institution", "PUBLISHED", f.gulf, f.gulfSubject)

	result := f.browse(t, Filters{
		InstitutionSlug: "kuwait-university",
		ProgramSlug:     "computer-engineering",
		Subject:         "0418-201",
	})
	titles := titlesOf(result)
	if len(titles) != 1 || titles[0] != "Match" {
		t.Fatalf("combined filter results = %v, want exactly [Match]", titles)
	}
	if result.Total != 1 {
		t.Fatalf("total = %d, want 1; the count must describe the same set as the page", result.Total)
	}
	// A combination that cannot match is an empty catalogue, not an error.
	empty := f.browse(t, Filters{InstitutionSlug: "kuwait-university", Subject: "GU-101"})
	if empty.Total != 0 {
		t.Fatalf("cross-institution combination total = %d, want 0", empty.Total)
	}
}

// F. Publication state. Academic filters must not reach a non-public Course.
func TestT6AcademicFiltersNeverExposeNonPublicCourses(t *testing.T) {
	f := newDiscoveryFixture(t)
	f.academicCourse(t, "Published", "PUBLISHED", f.kuwait, f.dataStructures)
	for _, lifecycle := range []string{"DRAFT", "PENDING_REVIEW", "CHANGES_REQUESTED", "DELISTED", "ARCHIVED"} {
		f.academicCourse(t, lifecycle+" Course", lifecycle, f.kuwait, f.dataStructures)
	}
	suspended, _ := f.academicCourse(t, "Suspended", "PUBLISHED", f.kuwait, f.dataStructures)
	if _, err := f.pool.Exec(f.ctx,
		`UPDATE courses SET access_suspended_at = now(), access_suspension_reason = 'emergency'
		 WHERE id = $1::uuid`, suspended); err != nil {
		t.Fatalf("suspending: %v", err)
	}

	for _, filters := range []Filters{
		{},
		{InstitutionSlug: "kuwait-university"},
		{ProgramSlug: "computer-engineering"},
		{Subject: "0418-201"},
		{InstitutionSlug: "kuwait-university", ProgramSlug: "computer-engineering", Subject: "0418-201"},
	} {
		titles := titlesOf(f.browse(t, filters))
		if len(titles) != 1 || titles[0] != "Published" {
			t.Fatalf("filters %+v exposed non-public Courses: %v", filters, titles)
		}
	}
}

// G. Duplicate prevention — the reason the audience rule uses EXISTS.
func TestT6SubjectInManyCurriculaNeverDuplicatesACourse(t *testing.T) {
	f := newDiscoveryFixture(t)
	// A third curriculum for the SAME program, also mapping the same Subject.
	// Under a join this Course would appear three times.
	// Inserted SUPERSEDED from the start: only one ACTIVE Curriculum per Program
	// is representable, which is itself part of why a join would be wrong here.
	var extra string
	if err := f.pool.QueryRow(f.ctx, `
		INSERT INTO curricula (program_id, institution_id, version_label, status)
		VALUES ($1::uuid, $2::uuid, '2019-eng', 'SUPERSEDED') RETURNING id::text`,
		f.compEng, f.kuwait).Scan(&extra); err != nil {
		t.Fatalf("seeding superseded curriculum: %v", err)
	}
	f.mapSubject(t, extra, f.dataStructures, f.kuwait)

	f.academicCourse(t, "Shared Subject Course", "PUBLISHED", f.kuwait, f.dataStructures)

	result := f.browse(t, Filters{ProgramSlug: "computer-engineering"})
	titles := titlesOf(result)
	if got := countOf(titles, "Shared Subject Course"); got != 1 {
		t.Fatalf("Course appeared %d times; a Subject in several curricula must not duplicate it: %v", got, titles)
	}
	if result.Total != 1 {
		t.Fatalf("total = %d, want 1", result.Total)
	}
}

// H. Retired academic entities are not offered for new selection.
func TestT6RetiredAcademicEntitiesAreNotOfferedAsFilters(t *testing.T) {
	f := newDiscoveryFixture(t)
	if _, err := f.pool.Exec(f.ctx,
		`UPDATE programs SET retired_at = now() WHERE id = $1::uuid`, f.compSci); err != nil {
		t.Fatalf("retiring program: %v", err)
	}

	institutions, err := f.repository.ListInstitutionFilters(f.ctx)
	if err != nil {
		t.Fatalf("institution options: %v", err)
	}
	if len(institutions) != 2 {
		t.Fatalf("institution options = %d, want 2 active", len(institutions))
	}

	programs, err := f.repository.ListProgramFilters(f.ctx, "kuwait-university")
	if err != nil {
		t.Fatalf("program options: %v", err)
	}
	for _, program := range programs {
		if program.Slug == "computer-science" {
			t.Fatal("a retired Program was offered as a discovery filter")
		}
	}
	if len(programs) != 1 || programs[0].Slug != "computer-engineering" {
		t.Fatalf("program options = %+v", programs)
	}

	subjects, err := f.repository.ListSubjectFilters(f.ctx, "kuwait-university", "")
	if err != nil {
		t.Fatalf("subject options: %v", err)
	}
	for _, subject := range subjects {
		if subject.Code == "0418-900" {
			t.Fatal("a retired Subject was offered as a discovery filter")
		}
	}

	// Narrowed to a Program, the options come from that Program's active
	// curriculum only.
	scoped, err := f.repository.ListSubjectFilters(f.ctx, "kuwait-university", "computer-engineering")
	if err != nil {
		t.Fatalf("scoped subject options: %v", err)
	}
	if len(scoped) != 2 {
		t.Fatalf("scoped subject options = %+v, want the two mapped Subjects", scoped)
	}
	for _, subject := range scoped {
		if subject.Value == "" {
			t.Fatal("every option must carry a shareable filter value")
		}
	}
}

// I. Profile relevance ranks; it never filters.
func TestT6ProfileRelevanceRanksWithoutRemovingCourses(t *testing.T) {
	f := newDiscoveryFixture(t)
	// Seeded so that plain c.id ordering would not already produce the expected
	// order: the unrelated Course is created first.
	f.academicCourse(t, "Other University Course", "PUBLISHED", f.gulf, f.gulfSubject)
	f.academicCourse(t, "Same Institution Course", "PUBLISHED", f.kuwait, f.networks)
	targeted, revision := f.academicCourse(t, "Explicitly Targeted", "PUBLISHED", f.kuwait, f.dataStructures)
	f.targetProgram(t, targeted, revision, f.compEng, f.kuwait)

	result := f.browse(t, Filters{RelevantProgramSlug: "computer-engineering"})
	titles := titlesOf(result)

	// Nothing is removed. Ranking is an ordering, not a filter.
	if len(titles) != 3 {
		t.Fatalf("ranked results = %v; relevance must not remove Courses", titles)
	}
	if titles[0] != "Explicitly Targeted" {
		t.Fatalf("ranked results = %v; an explicit Program target must rank first", titles)
	}
	if titles[len(titles)-1] != "Other University Course" {
		t.Fatalf("ranked results = %v; an unrelated Institution must rank last", titles)
	}
}

// J. Access isolation. A catalogue read is a read.
func TestT6DiscoveryCreatesNoAccessOrAudienceRows(t *testing.T) {
	f := newDiscoveryFixture(t)
	f.academicCourse(t, "Browsed Course", "PUBLISHED", f.kuwait, f.dataStructures)

	before := f.writeCounts(t)
	for _, filters := range []Filters{
		{InstitutionSlug: "kuwait-university"},
		{ProgramSlug: "computer-engineering"},
		{Subject: "0418-201"},
		{RelevantProgramSlug: "computer-engineering"},
	} {
		f.browse(t, filters)
	}
	if _, err := f.repository.ListInstitutionFilters(f.ctx); err != nil {
		t.Fatalf("institution options: %v", err)
	}
	after := f.writeCounts(t)

	for table, want := range before {
		if got := after[table]; got != want {
			t.Fatalf("browsing wrote to %s: %d -> %d", table, want, got)
		}
	}
}

func (f *discoveryFixture) writeCounts(t *testing.T) map[string]int {
	t.Helper()
	counts := map[string]int{}
	for _, table := range []string{
		"entitlements", "enrollments", "course_access_invitations", "purchase_requests",
		"course_program_targets", "courses", "course_revisions", "audit_events",
	} {
		var count int
		if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM `+table).Scan(&count); err != nil {
			t.Fatalf("counting %s: %v", table, err)
		}
		counts[table] = count
	}
	return counts
}

// Course detail names the Programs a Course reaches, in the reader's language,
// by the same explicit-or-inferred rule the filters use.
func TestT6CourseDetailReportsLocalizedProgramAudience(t *testing.T) {
	f := newDiscoveryFixture(t)
	inferred, _ := f.academicCourse(t, "Inferred", "PUBLISHED", f.kuwait, f.dataStructures)
	targeted, revision := f.academicCourse(t, "Targeted", "PUBLISHED", f.kuwait, f.dataStructures)
	f.targetProgram(t, targeted, revision, f.compEng, f.kuwait)

	detail, err := f.repository.Detail(f.ctx, inferred, false)
	if err != nil || detail == nil {
		t.Fatalf("detail: %v %v", detail, err)
	}
	if len(detail.ProgramAudience) != 2 {
		t.Fatalf("inferred audience = %v, want both Programs the Subject maps into", detail.ProgramAudience)
	}

	detail, err = f.repository.Detail(f.ctx, targeted, false)
	if err != nil || detail == nil {
		t.Fatalf("detail: %v %v", detail, err)
	}
	if len(detail.ProgramAudience) != 1 || detail.ProgramAudience[0] != "Computer Engineering" {
		t.Fatalf("explicit audience = %v, want only the targeted Program", detail.ProgramAudience)
	}

	arabic, err := f.repository.Detail(f.ctx, targeted, true)
	if err != nil || arabic == nil {
		t.Fatalf("arabic detail: %v %v", arabic, err)
	}
	if len(arabic.ProgramAudience) != 1 || arabic.ProgramAudience[0] != "هندسة الحاسوب" {
		t.Fatalf("arabic audience = %v; names must be localized", arabic.ProgramAudience)
	}
}

// Academic level. It is canonical study-plan data, never derived from a Course.
func TestT6LevelFilterUsesRecordedCurriculumLevels(t *testing.T) {
	f := newDiscoveryFixture(t)

	// Record levels the way the launch manifest does: some Subjects carry one,
	// some do not.
	if _, err := f.pool.Exec(f.ctx, `
		UPDATE curriculum_subjects SET recommended_level = 2 WHERE subject_id = $1::uuid`,
		f.dataStructures); err != nil {
		t.Fatalf("recording level: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `
		UPDATE curriculum_subjects SET recommended_level = NULL WHERE subject_id = $1::uuid`,
		f.networks); err != nil {
		t.Fatalf("clearing level: %v", err)
	}

	f.academicCourse(t, "Level Two Course", "PUBLISHED", f.kuwait, f.dataStructures)
	f.academicCourse(t, "Unlevelled Course", "PUBLISHED", f.kuwait, f.networks)

	levelTwo := titlesOf(f.browse(t, Filters{InstitutionSlug: "kuwait-university", Level: "2"}))
	if !contains(levelTwo, "Level Two Course") {
		t.Fatalf("level 2 results = %v", levelTwo)
	}
	// A Subject with no recorded level must not be given an invented one.
	if contains(levelTwo, "Unlevelled Course") {
		t.Fatalf("a Subject with no recorded level matched a level filter: %v", levelTwo)
	}
	if other := f.browse(t, Filters{Level: "3"}); other.Total != 0 {
		t.Fatalf("level 3 total = %d, want 0", other.Total)
	}

	// The same Subject recorded at level 2 by BOTH Programs' curricula still
	// returns one row.
	combined := f.browse(t, Filters{
		InstitutionSlug: "kuwait-university", ProgramSlug: "computer-engineering", Level: "2",
	})
	if got := countOf(titlesOf(combined), "Level Two Course"); got != 1 || combined.Total != 1 {
		t.Fatalf("level filter duplicated a Course: %d occurrences, total %d", got, combined.Total)
	}

	// A malformed level is an unfiltered catalogue, never an error or a
	// surprise: it simply contributes no clause.
	for _, malformed := range []string{"", "abc", "-1", "0", "99", "2; DROP TABLE courses"} {
		result, err := f.repository.Browse(f.ctx, false, 1, 50, "", false, Filters{Level: malformed})
		if err != nil {
			t.Fatalf("malformed level %q errored: %v", malformed, err)
		}
		if result.Total != 2 {
			t.Fatalf("malformed level %q narrowed the catalogue: %d", malformed, result.Total)
		}
	}

	// Only recorded levels are offered as choices.
	levels, err := f.repository.ListLevelFilters(f.ctx, "kuwait-university", "")
	if err != nil {
		t.Fatalf("level options: %v", err)
	}
	if len(levels) != 1 || levels[0] != 2 {
		t.Fatalf("level options = %v, want only the recorded level", levels)
	}
	if none, err := f.repository.ListLevelFilters(f.ctx, "gulf-university", ""); err != nil ||
		len(none) != 0 {
		t.Fatalf("gulf level options = %v (%v), want none recorded", none, err)
	}
}
