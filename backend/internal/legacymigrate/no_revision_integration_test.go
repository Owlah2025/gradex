//go:build integration

package legacymigrate

import "testing"

// T5 report completeness, against real PostgreSQL.
//
// These cases exist because of a specific, proven defect: loadLegacyCourses
// joined course_revisions with an INNER JOIN, so a legacy Course carrying no
// revision at all did not merely fail to migrate — it never appeared in the
// report and was not counted. A migration tool whose summary silently describes
// a smaller corpus than the corpus cannot be used to declare a cutover complete.

// legacyCourseWithoutRevision builds the record the old inner join dropped: a
// real LEGACY_TAXONOMY Course row with zero course_revisions.
func (f *fixture) legacyCourseWithoutRevision(t *testing.T) string {
	t.Helper()
	var courseID string
	if err := f.pool.QueryRow(f.ctx, `
		INSERT INTO courses (owner_account_id, lifecycle, classification_model)
		VALUES ($1::uuid, 'DRAFT', 'LEGACY_TAXONOMY') RETURNING id::text`,
		f.instructorID).Scan(&courseID); err != nil {
		t.Fatalf("seeding revision-less legacy course: %v", err)
	}
	return courseID
}

func (f *fixture) countLegacyCourses(t *testing.T) int {
	t.Helper()
	var count int
	if err := f.pool.QueryRow(f.ctx,
		`SELECT count(*) FROM courses WHERE classification_model = 'LEGACY_TAXONOMY'`).Scan(&count); err != nil {
		t.Fatalf("counting legacy courses: %v", err)
	}
	return count
}

func stepFor(plan *Plan, courseID string) (Step, bool) {
	for _, step := range plan.Steps {
		if step.CourseID == courseID {
			return step, true
		}
	}
	return Step{}, false
}

// The regression itself: the revision-less Course must be visible, explicit,
// and untouched by --apply.
func TestT5RevisionLessCourseIsReportedAndNeverMigrated(t *testing.T) {
	f := newFixture(t)

	coded := f.legacyTerm(t, "SUBJECT", "Principles", "0418-320")
	migratable, _ := f.legacyCourseFixture(t, "Migratable", &coded, nil)
	orphan := f.legacyCourseWithoutRevision(t)

	mapping := mappingFor([]SubjectMapping{
		{TermCode: "0418-320", TermLabelEn: "Principles", SubjectCode: "0418-320"},
	}, nil)

	migrator, err := New(f.pool)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	plan, err := migrator.Run(f.ctx, mapping, Options{})
	if err != nil {
		t.Fatalf("report: %v", err)
	}

	step, present := stepFor(plan, orphan)
	if !present {
		t.Fatal("the revision-less Course is missing from the report; this is the defect under test")
	}
	if step.Outcome != OutcomeNoRevision {
		t.Fatalf("outcome = %s (%s), want %s", step.Outcome, step.Detail, OutcomeNoRevision)
	}
	if step.Detail == "" {
		t.Fatal("the report must explain why the Course cannot migrate")
	}
	if step.WouldMutate {
		t.Fatal("a revision-less Course must never be marked as something --apply writes")
	}
	if plan.Counts.NoRevision != 1 {
		t.Fatalf("no_revision count = %d, want 1", plan.Counts.NoRevision)
	}

	// --apply must intentionally skip it while still migrating the eligible one.
	if _, err := migrator.Run(f.ctx, mapping, Options{Apply: true}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	model, institution, subject := f.classification(t, orphan)
	if model != "LEGACY_TAXONOMY" || institution != nil || subject != nil {
		t.Fatalf("apply mutated the revision-less Course: model=%s institution=%v subject=%v",
			model, institution, subject)
	}
	var revisions int
	if err := f.pool.QueryRow(f.ctx,
		`SELECT count(*) FROM course_revisions WHERE course_id = $1::uuid`, orphan).Scan(&revisions); err != nil {
		t.Fatalf("counting revisions: %v", err)
	}
	if revisions != 0 {
		t.Fatalf("apply fabricated %d revision(s) for a revision-less Course", revisions)
	}
	if model, _, _ := f.classification(t, migratable); model != "ACADEMIC_CATALOG" {
		t.Fatalf("the eligible Course did not migrate: %s", model)
	}
}

// The summary must be checkable against the corpus. Every legacy Course in the
// database appears exactly once, and Total reconciles with the rows.
func TestT5ReportAccountsForEveryLegacyCourse(t *testing.T) {
	f := newFixture(t)

	coded := f.legacyTerm(t, "SUBJECT", "Principles", "0418-320")
	unknown := f.legacyTerm(t, "SUBJECT", "Unknown", "9999-999")
	codeless := f.legacyTerm(t, "SUBJECT", "Codeless", "")

	f.legacyCourseFixture(t, "Migratable", &coded, nil)
	f.legacyCourseFixture(t, "Unmapped", &unknown, nil)
	f.legacyCourseFixture(t, "Codeless", &codeless, nil)
	f.legacyCourseFixture(t, "No Subject", nil, nil)
	f.legacyCourseWithoutRevision(t)
	f.legacyCourseWithoutRevision(t)

	mapping := mappingFor([]SubjectMapping{
		{TermCode: "0418-320", TermLabelEn: "Principles", SubjectCode: "0418-320"},
	}, nil)

	migrator, err := New(f.pool)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	plan, err := migrator.Run(f.ctx, mapping, Options{})
	if err != nil {
		t.Fatalf("report: %v", err)
	}

	legacyInDatabase := f.countLegacyCourses(t)
	seen := map[string]int{}
	for _, step := range plan.Steps {
		if step.CourseID == "" {
			t.Fatal("every report row must name a Course")
		}
		seen[step.CourseID]++
	}
	if len(seen) != legacyInDatabase {
		t.Fatalf("report covers %d Courses but the database holds %d legacy Courses",
			len(seen), legacyInDatabase)
	}
	for courseID, count := range seen {
		if count != 1 {
			t.Fatalf("Course %s appears %d times in the report", courseID, count)
		}
	}
	if plan.Counts.Total != len(plan.Steps) {
		t.Fatalf("total=%d but the report has %d rows", plan.Counts.Total, len(plan.Steps))
	}
}

// The real Kuwait University record: a legacy SUBJECT term with no canonical
// counterpart and several defensible candidates. It must fail closed with an
// outcome distinct from UNMAPPED, and no flag may migrate it.
func TestT5PendingFounderDecisionFailsClosed(t *testing.T) {
	f := newFixture(t)

	pendingTerm := f.legacyTerm(t, "SUBJECT", "Software Engineering", "SWE101")
	course, _ := f.legacyCourseFixture(t, "Introduction to Algorithms", &pendingTerm, nil)

	mapping := mappingFor(nil, nil)
	mapping.PendingDecisions = []PendingDecision{{
		TermCode: "SWE101", TermLabelEn: "Software Engineering",
		CourseTitleEn: "Introduction to Algorithms",
		Why:           "the legacy Subject label and the Course title name different subject areas",
		Candidates: []SubjectCandidate{
			{SubjectCode: "0418-320", Note: "matches the legacy label"},
			{SubjectCode: "0418-999", Note: "matches the Course title"},
		},
	}}

	migrator, err := New(f.pool)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	plan, err := migrator.Run(f.ctx, mapping, Options{Apply: true})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}

	step, present := stepFor(plan, course)
	if !present {
		t.Fatal("a Course awaiting a Founder decision must still be reported")
	}
	if step.Outcome != OutcomeFounderMappingRequired {
		t.Fatalf("outcome = %s (%s), want %s", step.Outcome, step.Detail, OutcomeFounderMappingRequired)
	}
	if len(step.Candidates) != 2 {
		t.Fatalf("the report must carry the candidates the Founder chooses between; got %v", step.Candidates)
	}
	if step.SubjectCode != "" {
		t.Fatalf("no target Subject may be chosen for a pending decision; got %q", step.SubjectCode)
	}
	if step.WouldMutate {
		t.Fatal("a pending Founder decision must never be marked as something --apply writes")
	}
	// --apply ran. The Course must be exactly as it was.
	if model, institution, subject := f.classification(t, course); model != "LEGACY_TAXONOMY" ||
		institution != nil || subject != nil {
		t.Fatalf("apply migrated a Course awaiting a Founder decision: %s %v %v", model, institution, subject)
	}
	if plan.Counts.FounderMappingRequired != 1 {
		t.Fatalf("founder_mapping_required = %d, want 1", plan.Counts.FounderMappingRequired)
	}
	// An undecided entry reports as still awaiting an answer.
	if step.Disposition != string(DispositionAwaitingDecision) {
		t.Fatalf("disposition = %q, want %q", step.Disposition, DispositionAwaitingDecision)
	}
}

// A Founder decision to KEEP the record unresolved is a decision, not the
// absence of one. It must change what the report says and nothing about what
// the migrator does: the Course stays fail-closed under --apply exactly as it
// did while the question was open.
func TestT5IntentionallyUnresolvedRecordStaysFailClosed(t *testing.T) {
	f := newFixture(t)

	pendingTerm := f.legacyTerm(t, "SUBJECT", "Software Engineering", "SWE101")
	course, revision := f.legacyCourseFixture(t, "Introduction to Algorithms", &pendingTerm, nil)
	f.publishWithCommerce(t, course, revision)
	before := f.commercialSnapshot(t, course)

	mapping := mappingFor(nil, nil)
	mapping.PendingDecisions = []PendingDecision{{
		TermCode: "SWE101", TermLabelEn: "Software Engineering",
		CourseTitleEn: "Introduction to Algorithms",
		Why:           "the legacy Subject label and the Course title name different subject areas",
		Candidates: []SubjectCandidate{
			{SubjectCode: "0418-320", Note: "matches the legacy label"},
			{SubjectCode: "0418-999", Note: "matches the Course title"},
		},
		Decision:           DispositionKeepUnresolved,
		DecidedOn:          "2026-08-23",
		ResolutionRequires: []string{"the official Kuwait University subject code"},
	}}

	migrator, err := New(f.pool)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	plan, err := migrator.Run(f.ctx, mapping, Options{Apply: true, ActorDescriptor: "t5-governance"})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}

	step, present := stepFor(plan, course)
	if !present {
		t.Fatal("an intentionally unresolved Course must still be reported")
	}
	if step.Outcome != OutcomeFounderMappingRequired {
		t.Fatalf("outcome = %s, want %s", step.Outcome, OutcomeFounderMappingRequired)
	}
	if step.Disposition != string(DispositionKeepUnresolved) {
		t.Fatalf("disposition = %q, want %q", step.Disposition, DispositionKeepUnresolved)
	}
	if step.DecidedOn != "2026-08-23" {
		t.Fatalf("decided_on = %q", step.DecidedOn)
	}
	if len(step.ResolutionRequires) == 0 {
		t.Fatal("the report must state what would reopen the decision")
	}
	if step.WouldMutate {
		t.Fatal("an intentionally unresolved record must never be marked as something --apply writes")
	}
	if step.SubjectCode != "" {
		t.Fatalf("no canonical Subject may be chosen; got %q", step.SubjectCode)
	}

	// Nothing was written. Not the classification, not identity, not commerce.
	model, institution, subject := f.classification(t, course)
	if model != "LEGACY_TAXONOMY" || institution != nil || subject != nil {
		t.Fatalf("apply mutated an intentionally unresolved Course: %s %v %v", model, institution, subject)
	}
	for key, want := range before {
		if got := f.commercialSnapshot(t, course)[key]; got != want {
			t.Fatalf("apply changed %s: %q -> %q", key, want, got)
		}
	}
	var targets, audits int
	if err := f.pool.QueryRow(f.ctx,
		`SELECT count(*) FROM course_program_targets WHERE course_id = $1::uuid`, course).Scan(&targets); err != nil {
		t.Fatalf("counting targets: %v", err)
	}
	if err := f.pool.QueryRow(f.ctx,
		`SELECT count(*) FROM audit_events WHERE target_id = $1`, course).Scan(&audits); err != nil {
		t.Fatalf("counting audits: %v", err)
	}
	if targets != 0 || audits != 0 {
		t.Fatalf("apply wrote %d target(s) and %d audit row(s) for an unresolved Course", targets, audits)
	}
}

// Mapping is on normalized code alone. A legacy term whose LABEL is character
// for character a canonical Subject title still does not migrate, because a
// label is prose and not identity.
func TestT5MappingNeverFallsBackToTitleSimilarity(t *testing.T) {
	f := newFixture(t)

	// The canonical Subject 0418-320 is titled "Principles of Computer Systems".
	twin := f.legacyTerm(t, "SUBJECT", "Principles of Computer Systems", "ZZZ-000")
	course, _ := f.legacyCourseFixture(t, "Principles of Computer Systems", &twin, nil)

	mapping := mappingFor([]SubjectMapping{
		{TermCode: "0418-320", TermLabelEn: "Principles", SubjectCode: "0418-320"},
	}, nil)

	migrator, err := New(f.pool)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	plan, err := migrator.Run(f.ctx, mapping, Options{Apply: true})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	got, detail := outcomeFor(plan, course)
	if got != OutcomeUnmapped {
		t.Fatalf("outcome = %s (%s); an identical title must not resolve identity", got, detail)
	}
	if model, _, _ := f.classification(t, course); model != "LEGACY_TAXONOMY" {
		t.Fatal("a title match migrated a Course; matching must be on normalized code only")
	}
}

// A Course already on the Academic Catalog whose legacy code the current
// mapping now sends somewhere else is reported as drift and left alone.
func TestT5DriftIsReportedAndNeverOverwritten(t *testing.T) {
	f := newFixture(t)

	var alternativeID string
	if err := f.pool.QueryRow(f.ctx, `
		INSERT INTO subjects (institution_id, official_code, title_ar, title_en)
		VALUES ($1::uuid, '0418-777', 'بديل', 'Alternative Subject') RETURNING id::text`,
		f.institutionID).Scan(&alternativeID); err != nil {
		t.Fatalf("seeding alternative subject: %v", err)
	}

	coded := f.legacyTerm(t, "SUBJECT", "Principles", "0418-320")
	course, _ := f.legacyCourseFixture(t, "Drifting", &coded, nil)

	migrator, err := New(f.pool)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	original := mappingFor([]SubjectMapping{
		{TermCode: "0418-320", TermLabelEn: "Principles", SubjectCode: "0418-320"},
	}, nil)
	if _, err := migrator.Run(f.ctx, original, Options{Apply: true}); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	_, _, migratedSubject := f.classification(t, course)
	if migratedSubject == nil || *migratedSubject != f.subjectID {
		t.Fatalf("first apply did not assign the mapped Subject: %v", migratedSubject)
	}

	// The Founder edits the mapping after the cutover.
	edited := mappingFor([]SubjectMapping{
		{TermCode: "0418-320", TermLabelEn: "Principles", SubjectCode: "0418-777"},
	}, nil)
	plan, err := migrator.Run(f.ctx, edited, Options{Apply: true})
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}

	step, present := stepFor(plan, course)
	if !present {
		t.Fatal("a migrated Course must still appear in the report")
	}
	if step.Outcome != OutcomeDrift {
		t.Fatalf("outcome = %s (%s), want %s", step.Outcome, step.Detail, OutcomeDrift)
	}
	if step.CurrentSubject != "0418-320" || step.SubjectCode != "0418-777" {
		t.Fatalf("drift must state both sides: current=%q mapping=%q", step.CurrentSubject, step.SubjectCode)
	}
	if step.WouldMutate {
		t.Fatal("drift must never be marked as something --apply writes")
	}
	_, _, after := f.classification(t, course)
	if after == nil || *after != f.subjectID {
		t.Fatalf("apply overwrote a canonical Subject on drift: %v", after)
	}
	if plan.Counts.Drift != 1 {
		t.Fatalf("drift = %d, want 1", plan.Counts.Drift)
	}
}

// A rerun must name the Courses it is skipping, not just count them.
func TestT5AlreadyAcademicRowsCarryCourseIdentity(t *testing.T) {
	f := newFixture(t)

	coded := f.legacyTerm(t, "SUBJECT", "Principles", "0418-320")
	course, _ := f.legacyCourseFixture(t, "Migratable", &coded, nil)

	mapping := mappingFor([]SubjectMapping{
		{TermCode: "0418-320", TermLabelEn: "Principles", SubjectCode: "0418-320"},
	}, nil)
	migrator, err := New(f.pool)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := migrator.Run(f.ctx, mapping, Options{Apply: true}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	plan, err := migrator.Run(f.ctx, mapping, Options{})
	if err != nil {
		t.Fatalf("rerun report: %v", err)
	}
	step, present := stepFor(plan, course)
	if !present {
		t.Fatal("a rerun must report the migrated Course by id")
	}
	if step.Outcome != OutcomeAlreadyAcademic {
		t.Fatalf("outcome = %s, want %s", step.Outcome, OutcomeAlreadyAcademic)
	}
	if step.CurrentSubject != "0418-320" {
		t.Fatalf("current Subject = %q, want the canonical code it holds", step.CurrentSubject)
	}
	if step.Classification != "ACADEMIC_CATALOG" {
		t.Fatalf("classification = %q", step.Classification)
	}
	if step.WouldMutate {
		t.Fatal("an already-academic Course must never be marked as something --apply writes")
	}
}

// The complete commercial invariant. T5 is classification migration, not Course
// recreation, so everything a Student may already have bought must survive it
// byte for byte: the same Course id, the same revisions, the same sections and
// lessons, the same price, the same entitlement, and the same progress.
func TestT5ApplyPreservesEveryCommercialInvariant(t *testing.T) {
	f := newFixture(t)

	coded := f.legacyTerm(t, "SUBJECT", "Principles", "0418-320")
	major := f.legacyTerm(t, "MAJOR", "Computer Science", "")
	course, revision := f.legacyCourseFixture(t, "Purchased Legacy Course", &coded, &major)
	f.publishWithCommerce(t, course, revision)

	before := f.commercialSnapshot(t, course)

	migrator, err := New(f.pool)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mapping := mappingFor(
		[]SubjectMapping{{TermCode: "0418-320", SubjectCode: "0418-320"}},
		[]MajorMapping{{TermLabelEn: "Computer Science", ProgramSlugs: []string{"computer-science"}}},
	)
	plan, err := migrator.Run(f.ctx, mapping, Options{Apply: true, ActorDescriptor: "t5-invariants"})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if plan.Counts.Migrate != 1 {
		t.Fatalf("migrate = %d, want 1", plan.Counts.Migrate)
	}

	after := f.commercialSnapshot(t, course)
	for key, want := range before {
		if got := after[key]; got != want {
			t.Fatalf("migration changed %s: %q -> %q", key, want, got)
		}
	}

	// Only the classification-related fields moved.
	model, institution, subject := f.classification(t, course)
	if model != "ACADEMIC_CATALOG" || institution == nil || subject == nil {
		t.Fatalf("classification did not move: %s %v %v", model, institution, subject)
	}

	// And a second apply changes nothing further — idempotent against a Course
	// that carries real commercial state.
	if _, err := migrator.Run(f.ctx, mapping, Options{Apply: true, ActorDescriptor: "t5-invariants"}); err != nil {
		t.Fatalf("rerun apply: %v", err)
	}
	rerun := f.commercialSnapshot(t, course)
	for key, want := range after {
		if got := rerun[key]; got != want {
			t.Fatalf("rerun changed %s: %q -> %q", key, want, got)
		}
	}
	var targets int
	if err := f.pool.QueryRow(f.ctx,
		`SELECT count(*) FROM course_program_targets WHERE course_id = $1::uuid`, course).Scan(&targets); err != nil {
		t.Fatalf("counting targets: %v", err)
	}
	if targets != 1 {
		t.Fatalf("a rerun duplicated audience targets: %d", targets)
	}
}

// publishWithCommerce gives a legacy Course the full state a real purchased
// Course carries: published revision, structure, price, entitlement, progress.
func (f *fixture) publishWithCommerce(t *testing.T, courseID, revisionID string) {
	t.Helper()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := f.pool.Exec(f.ctx, sql, args...); err != nil {
			t.Fatalf("seeding commerce: %v", err)
		}
	}
	scan := func(dest *string, sql string, args ...any) {
		t.Helper()
		if err := f.pool.QueryRow(f.ctx, sql, args...).Scan(dest); err != nil {
			t.Fatalf("seeding commerce: %v", err)
		}
	}

	exec(`UPDATE course_revisions SET state = 'APPROVED' WHERE id = $1::uuid`, revisionID)
	exec(`UPDATE courses SET live_revision_id = $1::uuid, lifecycle = 'PUBLISHED' WHERE id = $2::uuid`,
		revisionID, courseID)

	var sectionIdentity, lessonIdentity string
	scan(&sectionIdentity, `INSERT INTO course_section_identities (course_id) VALUES ($1::uuid) RETURNING id::text`, courseID)
	scan(&lessonIdentity, `INSERT INTO course_lesson_identities (course_id, section_identity_id)
		VALUES ($1::uuid, $2::uuid) RETURNING id::text`, courseID, sectionIdentity)
	var sectionID string
	scan(&sectionID, `INSERT INTO course_sections (revision_id, course_id, section_identity_id, title_ar, title_en, position)
		VALUES ($1::uuid, $2::uuid, $3::uuid, 'قسم', 'Section One', 1) RETURNING id::text`,
		revisionID, courseID, sectionIdentity)
	exec(`INSERT INTO course_lessons (section_id, course_id, section_identity_id, lesson_identity_id,
	                                  title_ar, title_en, position)
	      VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'درس', 'Lesson One', 1)`,
		sectionID, courseID, sectionIdentity, lessonIdentity)

	exec(`INSERT INTO course_price_changes (course_id, new_value_minor_units, changed_by_account_id, reason)
	      VALUES ($1::uuid, 25000, $2::uuid, 'launch price')`, courseID, f.instructorID)

	var studentID string
	scan(&studentID, `INSERT INTO accounts (normalized_email, email, role, status, display_name)
		VALUES ('t5student@example.test', 't5student@example.test', 'STUDENT', 'ACTIVE', 'T5 Student')
		RETURNING id::text`)
	var enrollmentID string
	scan(&enrollmentID, `INSERT INTO enrollments (student_account_id, course_id)
		VALUES ($1::uuid, $2::uuid) RETURNING id::text`, studentID, courseID)
	// A manual entitlement must cite the invitation it came from, so the
	// fixture builds the real access chain rather than a synthetic row.
	var invitationID string
	scan(&invitationID, `INSERT INTO course_access_invitations
		(normalized_email, email, course_id, created_by_account_id, state,
		 accepted_by_account_id, decided_by_account_id)
		VALUES ('t5student@example.test', 't5student@example.test', $1::uuid, $2::uuid, 'APPROVED',
		        $3::uuid, $2::uuid)
		RETURNING id::text`, courseID, f.instructorID, studentID)
	exec(`INSERT INTO entitlements (student_account_id, scope_kind, scope_id, course_id, grant_source,
	                                source_invitation_id,
	                                original_access_ends_at, access_ends_at, retirement_eligibility_at)
	      VALUES ($1::uuid, 'COURSE', $2::uuid, $2::uuid, 'MANUAL_INVITATION', $3::uuid,
	              now() + interval '365 days', now() + interval '365 days', now() + interval '400 days')`,
		studentID, courseID, invitationID)
	exec(`INSERT INTO progress (enrollment_id, course_lesson_identity_id)
	      VALUES ($1::uuid, $2::uuid)`, enrollmentID, lessonIdentity)
}

// commercialSnapshot reads exactly the facts a migration must not change.
func (f *fixture) commercialSnapshot(t *testing.T, courseID string) map[string]string {
	t.Helper()
	snapshot := map[string]string{}
	for key, query := range map[string]string{
		"course_id":     `SELECT id::text FROM courses WHERE id = $1::uuid`,
		"owner":         `SELECT owner_account_id::text FROM courses WHERE id = $1::uuid`,
		"slug":          `SELECT COALESCE(slug, '-') FROM courses WHERE id = $1::uuid`,
		"lifecycle":     `SELECT lifecycle::text FROM courses WHERE id = $1::uuid`,
		"live_revision": `SELECT COALESCE(live_revision_id::text, '-') FROM courses WHERE id = $1::uuid`,
		"suspended":     `SELECT COALESCE(access_suspended_at::text, '-') FROM courses WHERE id = $1::uuid`,
		"retired":       `SELECT COALESCE(retired_at::text, '-') FROM courses WHERE id = $1::uuid`,
		"revisions":     `SELECT COALESCE(string_agg(id::text || ':' || state::text, ',' ORDER BY id), '-') FROM course_revisions WHERE course_id = $1::uuid`,
		"legacy_terms":  `SELECT COALESCE(string_agg(COALESCE(subject_term_id::text, '-') || '/' || COALESCE(major_term_id::text, '-'), ',' ORDER BY id), '-') FROM course_revisions WHERE course_id = $1::uuid`,
		"sections":      `SELECT COALESCE(string_agg(id::text || ':' || title_en, ',' ORDER BY id), '-') FROM course_sections WHERE course_id = $1::uuid`,
		"lessons":       `SELECT COALESCE(string_agg(id::text || ':' || title_en, ',' ORDER BY id), '-') FROM course_lessons WHERE course_id = $1::uuid`,
		"price":         `SELECT COALESCE(string_agg(new_value_minor_units::text, ',' ORDER BY id), '-') FROM course_price_changes WHERE course_id = $1::uuid`,
		"entitlements":  `SELECT COALESCE(string_agg(id::text || ':' || state::text || ':' || access_ends_at::text, ',' ORDER BY id), '-') FROM entitlements WHERE course_id = $1::uuid`,
		"enrollments":   `SELECT COALESCE(string_agg(id::text, ',' ORDER BY id), '-') FROM enrollments WHERE course_id = $1::uuid`,
		"progress":      `SELECT COALESCE(count(*)::text, '0') FROM progress p JOIN enrollments e ON e.id = p.enrollment_id WHERE e.course_id = $1::uuid`,
		"invitations":   `SELECT COALESCE(count(*)::text, '0') FROM course_access_invitations WHERE course_id = $1::uuid`,
		"media_assets":  `SELECT COALESCE(count(*)::text, '0') FROM media_assets WHERE course_id = $1::uuid`,
		"purchase_reqs": `SELECT COALESCE(count(*)::text, '0') FROM purchase_requests WHERE course_id = $1::uuid`,
	} {
		var value string
		if err := f.pool.QueryRow(f.ctx, query, courseID).Scan(&value); err != nil {
			t.Fatalf("snapshotting %s: %v", key, err)
		}
		snapshot[key] = value
	}
	return snapshot
}
