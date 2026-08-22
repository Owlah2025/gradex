package manifest

import (
	"errors"
	"strings"
	"testing"
)

// Validation runs without a database on purpose: a curation mistake must be
// caught in review, not halfway through an import transaction.

func TestKuwaitUniversityLaunchManifestValidates(t *testing.T) {
	pkg, err := Load("kuwait-university-launch-v1")
	if err != nil {
		t.Fatalf("the shipped Kuwait University manifest does not validate: %v", err)
	}
	m := pkg.Manifest
	if m.Institution.Slug != "kuwait-university" || m.Institution.CountryCode != "KW" {
		t.Fatalf("institution = %s/%s, want kuwait-university/KW", m.Institution.Slug, m.Institution.CountryCode)
	}
	// Five credit-derived levels, from the official Student Manual. Not four.
	if m.Institution.MaxAcademicLevel != 5 {
		t.Fatalf("max_academic_level = %d, want 5", m.Institution.MaxAcademicLevel)
	}
	// Kuwait University admits into a College and assigns sub-majors later; that
	// is an enrollment status, not a foundation programme.
	if m.Institution.HasFoundationStage {
		t.Fatal("Kuwait University must not claim a foundation stage; no source supports one")
	}
}

func TestOnlyKuwaitUniversityShipsAsLaunchData(t *testing.T) {
	ids, err := Available()
	if err != nil {
		t.Fatalf("listing manifests: %v", err)
	}
	if len(ids) != 1 || ids[0] != "kuwait-university-launch-v1" {
		t.Fatalf("shipped manifests = %v; D-091 makes Kuwait University the only launch institution", ids)
	}
}

func TestUnknownManifestIsRefused(t *testing.T) {
	if _, err := Load("aasu-launch-v1"); err == nil {
		t.Fatal("an unknown manifest identifier must be refused, never defaulted")
	}
}

func TestSharedSubjectIsOneManifestEntryAcrossCurricula(t *testing.T) {
	pkg, err := Load("kuwait-university-launch-v1")
	if err != nil {
		t.Fatalf("loading manifest: %v", err)
	}
	// 0410-101 Calculus I is required verbatim by Computer Science, Computer
	// Engineering, and Electrical Engineering. It must be one Subject.
	declared := 0
	for _, subject := range pkg.Manifest.Subjects {
		if subject.OfficialCode == "0410-101" {
			declared++
		}
	}
	if declared != 1 {
		t.Fatalf("0410-101 is declared %d times; it must be one canonical Subject", declared)
	}
	curricula := map[string]bool{}
	for _, mapping := range pkg.Manifest.Mappings {
		if mapping.SubjectKey == "ku-0410-101" {
			curricula[mapping.CurriculumKey] = true
		}
	}
	// Computer Science, Cybersecurity, Computer Engineering, and Electrical
	// Engineering all require it. One Subject, four curricula.
	if len(curricula) < 4 {
		t.Fatalf("0410-101 maps into %d curricula, want at least 4 (CS, Cybersecurity, CpE, EE)", len(curricula))
	}
}

func TestNoSubjectCodeCollidesAfterNormalization(t *testing.T) {
	pkg, err := Load("kuwait-university-launch-v1")
	if err != nil {
		t.Fatalf("loading manifest: %v", err)
	}
	seen := map[string]string{}
	for _, subject := range pkg.Manifest.Subjects {
		if subject.OfficialCode == "" {
			continue
		}
		// Display format keeps the dash a Student would recognise on a plan.
		if !strings.Contains(subject.OfficialCode, "-") {
			t.Errorf("subject %s stores %q; official display formatting must be preserved",
				subject.Key, subject.OfficialCode)
		}
		normalized := normalizeForTest(subject.OfficialCode)
		if previous, clash := seen[normalized]; clash {
			t.Fatalf("subjects %s and %s both normalize to %q", previous, subject.Key, normalized)
		}
		seen[normalized] = subject.Key
	}
}

func normalizeForTest(code string) string {
	var b strings.Builder
	for _, r := range code {
		if (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') {
			b.WriteRune(r)
		} else if r >= 'a' && r <= 'z' {
			b.WriteRune(r - 32)
		}
	}
	return b.String()
}

// Placement is allowed only where Kuwait University publishes a study plan.
// Two curricula qualify: the Computer Science 2024 Suggested Study Plan and the
// Data Science and AI 8-Semester Plan. Every placed row must cite the plan that
// places it, and no other curriculum may claim a placement at all.
func TestPlacementExistsOnlyWhereAnOfficialPlanPublishesIt(t *testing.T) {
	pkg, err := Load("kuwait-university-launch-v1")
	if err != nil {
		t.Fatalf("loading manifest: %v", err)
	}
	planFor := map[string]string{
		"ku-cs-2024":      "ku-cs-suggested-study-plan",
		"ku-dsai-current": "ku-dsai-8-semester-plan",
	}
	placedPerCurriculum := map[string]int{}
	placed := 0
	for _, mapping := range pkg.Manifest.Mappings {
		if mapping.Level == nil && mapping.Semester == nil {
			continue
		}
		placed++
		placedPerCurriculum[mapping.CurriculumKey]++
		planSource, published := planFor[mapping.CurriculumKey]
		if !published {
			t.Errorf("%s/%s asserts a placement, but no Kuwait University plan places that curriculum",
				mapping.CurriculumKey, mapping.SubjectKey)
			continue
		}
		cites := false
		for _, source := range mapping.Sources {
			if source == planSource {
				cites = true
			}
		}
		if !cites {
			t.Errorf("%s/%s asserts a placement without citing the official study plan",
				mapping.CurriculumKey, mapping.SubjectKey)
		}
		// The plan names four years and two terms; anything outside that is not
		// something the plan could have said.
		if mapping.Level != nil && (*mapping.Level < 1 || *mapping.Level > 4) {
			t.Errorf("%s/%s level %d is outside the plan's FRESHMAN-SENIOR range",
				mapping.CurriculumKey, mapping.SubjectKey, *mapping.Level)
		}
		if mapping.Semester != nil && (*mapping.Semester < 1 || *mapping.Semester > 2) {
			t.Errorf("%s/%s semester %d is outside the plan's Fall/Spring range",
				mapping.CurriculumKey, mapping.SubjectKey, *mapping.Semester)
		}
	}
	if placed == 0 {
		t.Fatal("no placement at all; the official study plans are not being used")
	}
	// Both published plans must actually be used, not just one.
	for curriculum := range planFor {
		if placedPerCurriculum[curriculum] == 0 {
			t.Errorf("curriculum %s has an official study plan but no placement was applied", curriculum)
		}
	}
	// Both plans hold unnamed elective slots, so some rows must stay unplaced.
	if placed == len(pkg.Manifest.Mappings) {
		t.Fatal("every mapping is placed; the plans do not name a course for every requirement")
	}
}

// T2.2: the Data Science and AI degree is real, is conferred by the Department
// of Information Science in the College of Life Sciences, and must not be
// confused with a Gradex "Data Science" label or relocated to another college.
func TestDataScienceProgramMatchesOfficialStructure(t *testing.T) {
	pkg, err := Load("kuwait-university-launch-v1")
	if err != nil {
		t.Fatalf("loading manifest: %v", err)
	}
	found := 0
	var program Program
	for _, candidate := range pkg.Manifest.Programs {
		if candidate.Key == "ku-data-science-ai" {
			found++
			program = candidate
		}
	}
	if found != 1 {
		t.Fatalf("the DSAI program appears %d times, want exactly 1", found)
	}
	if program.NameEn != "Data Science and Artificial Intelligence" {
		t.Errorf("DSAI English name = %q", program.NameEn)
	}
	// Kuwait University publishes this Arabic itself, on the Arabic major sheet.
	if program.NameArSource != SourceOfficial {
		t.Error("the DSAI Arabic name is the university's own and must be marked official")
	}
	if program.OwningUnit != "ku-information-science-dept" {
		t.Fatalf("DSAI owning unit = %q, want the Information Science department", program.OwningUnit)
	}
	units := map[string]Unit{}
	for _, unit := range pkg.Manifest.Units {
		units[unit.Key] = unit
	}
	department, ok := units["ku-information-science-dept"]
	if !ok {
		t.Fatal("the Information Science department is missing")
	}
	if department.Kind != "DEPARTMENT" || department.ParentKey != "ku-life-sciences" {
		t.Fatalf("Information Science is %s under %q, want a DEPARTMENT under the College of Life Sciences",
			department.Kind, department.ParentKey)
	}
	college, ok := units["ku-life-sciences"]
	if !ok || college.Kind != "COLLEGE" || college.ParentKey != "" {
		t.Fatal("the College of Life Sciences must be a top-level COLLEGE")
	}
	// No synthetic computing hierarchy was invented to host the programme.
	for _, unit := range pkg.Manifest.Units {
		for _, invented := range []string{"Data Science", "Artificial Intelligence", "Computing", "AI"} {
			if unit.NameEn == invented {
				t.Errorf("academic unit %q is invented; Kuwait University publishes no such unit", unit.NameEn)
			}
		}
	}
}

// The DSAI plan reuses canonical Subjects rather than minting parallel copies.
func TestDataScienceReusesCanonicalSubjects(t *testing.T) {
	pkg, err := Load("kuwait-university-launch-v1")
	if err != nil {
		t.Fatalf("loading manifest: %v", err)
	}
	declared := map[string]int{}
	for _, subject := range pkg.Manifest.Subjects {
		if subject.OfficialCode != "" {
			declared[subject.OfficialCode]++
		}
	}
	// Each of these is required by DSAI *and* by at least one other programme.
	for _, shared := range []string{"0410-101", "0410-111", "0330-100"} {
		if declared[shared] != 1 {
			t.Fatalf("%s is declared %d times; a shared Subject must exist once", shared, declared[shared])
		}
		curricula := map[string]bool{}
		for _, mapping := range pkg.Manifest.Mappings {
			if mapping.SubjectKey == "ku-"+shared {
				curricula[mapping.CurriculumKey] = true
			}
		}
		if !curricula["ku-dsai-current"] {
			t.Errorf("%s is not mapped into the DSAI curriculum", shared)
		}
		if len(curricula) < 2 {
			t.Errorf("%s serves only %d curriculum; it is meant to be shared", shared, len(curricula))
		}
	}
	// Every DSAI mapping must resolve to a declared Subject of this institution.
	subjectKeys := map[string]bool{}
	for _, subject := range pkg.Manifest.Subjects {
		subjectKeys[subject.Key] = true
	}
	dsai := 0
	for _, mapping := range pkg.Manifest.Mappings {
		if mapping.CurriculumKey != "ku-dsai-current" {
			continue
		}
		dsai++
		if !subjectKeys[mapping.SubjectKey] {
			t.Errorf("DSAI maps %s, which is not a declared Subject", mapping.SubjectKey)
		}
	}
	if dsai == 0 {
		t.Fatal("the DSAI curriculum has no mappings; this detector would pass vacuously")
	}
}

// Founder Decision 2: Mathematics majors are academically real but outside
// current commercial targeting. Their absence is product scope, not ignorance.
func TestMathematicsProgramsRemainOutOfLaunchScope(t *testing.T) {
	pkg, err := Load("kuwait-university-launch-v1")
	if err != nil {
		t.Fatalf("loading manifest: %v", err)
	}
	names := map[string]bool{}
	units := map[string]bool{}
	for _, program := range pkg.Manifest.Programs {
		names[program.NameEn] = true
	}
	for _, unit := range pkg.Manifest.Units {
		units[unit.NameEn] = true
	}
	for _, excluded := range []string{"Mathematics", "Financial Mathematics"} {
		if names[excluded] {
			t.Errorf("%q is a launch Program; the Founder scoped Mathematics majors out", excluded)
		}
	}
	// The department and its Subjects stay: Subject ownership is not audience.
	if !units["Mathematics"] {
		t.Error("the Mathematics department must remain; it owns the shared Math Subjects")
	}
}

// The launch Program selector is deliberate, and exactly these five.
func TestLaunchProgramSetIsExactlyTheFounderSet(t *testing.T) {
	pkg, err := Load("kuwait-university-launch-v1")
	if err != nil {
		t.Fatalf("loading manifest: %v", err)
	}
	want := map[string]bool{
		"Computer Science": true,
		"Cybersecurity":    true,
		"Data Science and Artificial Intelligence": true,
		"Computer Engineering":                     true,
		"Electrical Engineering":                   true,
	}
	got := map[string]bool{}
	for _, program := range pkg.Manifest.Programs {
		got[program.NameEn] = true
	}
	if len(got) != len(want) {
		t.Fatalf("launch Programs = %v, want exactly the five Founder-approved Programs", got)
	}
	for name := range want {
		if !got[name] {
			t.Errorf("launch Program %q is missing", name)
		}
	}
}

// Gradex teaching areas are commercial labels. Only a Program Kuwait University
// actually confers may appear here.
func TestOnlySourceBackedProgramsExist(t *testing.T) {
	pkg, err := Load("kuwait-university-launch-v1")
	if err != nil {
		t.Fatalf("loading manifest: %v", err)
	}
	// Kuwait University confers a B.Sc. in Cybersecurity through the Computer
	// Science department, so Cybersecurity is a real Program and is present.
	// It confers no degree named for these, so none may be invented.
	// "Data Science" alone is a Gradex teaching label; Kuwait University's degree
	// is named "Data Science and Artificial Intelligence" and is present.
	forbidden := map[string]bool{
		"Software": true, "Software Engineering": true,
		"Data Science": true, "Programming": true, "Cybersecurity Engineering": true,
	}
	for _, program := range pkg.Manifest.Programs {
		if forbidden[program.NameEn] {
			t.Errorf("program %q is a Gradex teaching area, not a Kuwait University degree", program.NameEn)
		}
		if len(program.Sources) == 0 {
			t.Errorf("program %q cites no source", program.NameEn)
		}
	}
	byName := map[string]bool{}
	for _, program := range pkg.Manifest.Programs {
		byName[program.NameEn] = true
	}
	if !byName["Cybersecurity"] {
		t.Error("Kuwait University confers a B.Sc. in Cybersecurity; it belongs in launch scope")
	}
	if !byName["Data Science and Artificial Intelligence"] {
		t.Error("Kuwait University confers a B.Sc. in Data Science and Artificial Intelligence; it belongs in launch scope")
	}
}

// A curriculum label Gradex invented must say so, so it can never reach a
// Student looking like the university's own version name.
func TestPlaceholderCurriculumLabelsDeclareThemselves(t *testing.T) {
	pkg, err := Load("kuwait-university-launch-v1")
	if err != nil {
		t.Fatalf("loading manifest: %v", err)
	}
	official, placeholder := 0, 0
	for _, curriculum := range pkg.Manifest.Curricula {
		switch curriculum.VersionLabelSource {
		case SourceOfficial:
			official++
			if curriculum.VersionLabel == "current" {
				t.Errorf("curriculum %s claims %q is an official Kuwait University label", curriculum.Key, curriculum.VersionLabel)
			}
		case SourceGradexTranslation:
			placeholder++
			if curriculum.VersionLabelNote == "" {
				t.Errorf("curriculum %s uses a placeholder label without explaining it", curriculum.Key)
			}
		default:
			t.Errorf("curriculum %s declares no version-label provenance", curriculum.Key)
		}
	}
	if official == 0 || placeholder == 0 {
		t.Fatalf("official=%d placeholder=%d; this detector would pass vacuously", official, placeholder)
	}
}

func TestEveryArabicStringDeclaresItsProvenance(t *testing.T) {
	pkg, err := Load("kuwait-university-launch-v1")
	if err != nil {
		t.Fatalf("loading manifest: %v", err)
	}
	if !pkg.Manifest.Institution.NameArSource.Valid() {
		t.Error("the institution Arabic name declares no provenance")
	}
	for _, unit := range pkg.Manifest.Units {
		// College and department Arabic names come from the official Student
		// Manual, so claiming anything else here would understate the evidence.
		if unit.NameArSource != SourceOfficial {
			t.Errorf("unit %s claims %q for its Arabic name; the Student Manual publishes it",
				unit.Key, unit.NameArSource)
		}
	}
	// A Subject may claim official Arabic only if it cites a source that actually
	// publishes Arabic titles. Kuwait University publishes English-only pages for
	// the College of Science and engineering Subjects, so those stay Gradex
	// translations; it publishes a full Arabic major sheet for Data Science and
	// AI, so those are the university's own wording.
	arabicPublishing := map[string]bool{"ku-dsai-major-sheet-ar": true}
	official, translated := 0, 0
	for _, subject := range pkg.Manifest.Subjects {
		if !subject.TitleArSource.Valid() {
			t.Errorf("subject %s declares no Arabic provenance", subject.Key)
			continue
		}
		if subject.TitleArSource != SourceOfficial {
			translated++
			continue
		}
		official++
		cites := false
		for _, source := range subject.Sources {
			if arabicPublishing[source] {
				cites = true
			}
		}
		if !cites {
			t.Errorf("subject %s claims official Arabic without citing a source that publishes Arabic titles",
				subject.Key)
		}
	}
	if official == 0 || translated == 0 {
		t.Fatalf("official=%d translated=%d; this detector would pass vacuously", official, translated)
	}
}

func TestEveryClaimCitesADefinedSource(t *testing.T) {
	pkg, err := Load("kuwait-university-launch-v1")
	if err != nil {
		t.Fatalf("loading manifest: %v", err)
	}
	if len(pkg.Sources.Sources) == 0 {
		t.Fatal("no sources are defined; this detector would pass vacuously")
	}
	for _, source := range pkg.Sources.Sources {
		if !strings.HasPrefix(source.URL, "https://") {
			t.Errorf("source %s is not an https primary source", source.ID)
		}
		// Competitor and aggregator sources may inform discovery vocabulary but
		// never academic fact.
		for _, forbidden := range []string{"baims.com", "wikipedia.org", "unirank", "standyou", "edarabia"} {
			if strings.Contains(source.URL, forbidden) {
				t.Errorf("source %s (%s) is not an academic authority", source.ID, source.URL)
			}
		}
	}
}

const validSources = `
sources:
  - id: s1
    url: https://example.test/a
    title: A
    type: official_university_page
    retrieved_at: "2026-08-21"
`

func fixture(body string) ([]byte, []byte) {
	return []byte(body), []byte(validSources)
}

const minimalHeader = `
id: fixture-manifest
version: "1.0.0"
institution:
  key: fx
  slug: fixture-university
  country_code: KW
  name_ar: جامعة
  name_en: Fixture University
  name_ar_source: official
  max_academic_level: 4
  sources: [s1]
`

func expectInvalid(t *testing.T, body, wantFragment string) {
	t.Helper()
	_, err := ParsePackage(fixture(body))
	if err == nil {
		t.Fatalf("manifest was accepted; expected a validation error mentioning %q", wantFragment)
	}
	if !errors.Is(err, ErrInvalidManifest) {
		t.Fatalf("error %v does not unwrap to ErrInvalidManifest", err)
	}
	if !strings.Contains(err.Error(), wantFragment) {
		t.Fatalf("error %q does not mention %q", err.Error(), wantFragment)
	}
}

func TestMalformedManifestIsRejected(t *testing.T) {
	if _, err := ParsePackage([]byte("id: [not-a-string"), []byte(validSources)); err == nil {
		t.Fatal("unparseable YAML was accepted")
	}
	// Strict decoding means a typo in a field name fails loudly rather than
	// silently dropping curated data.
	if _, err := ParsePackage([]byte(minimalHeader+"\nunknown_top_level: 1\n"), []byte(validSources)); err == nil {
		t.Fatal("an unknown manifest field was silently ignored")
	}
}

func TestUnknownParentKeyIsRejected(t *testing.T) {
	expectInvalid(t, minimalHeader+`
academic_units:
  - {key: a, parent_key: nowhere, kind: DEPARTMENT, slug: a, name_ar: أ, name_en: A, name_ar_source: official, sources: [s1]}
`, "references unknown unit")
}

func TestManifestCycleIsRejected(t *testing.T) {
	expectInvalid(t, minimalHeader+`
academic_units:
  - {key: a, parent_key: c, kind: COLLEGE, slug: a, name_ar: أ, name_en: A, name_ar_source: official, sources: [s1]}
  - {key: b, parent_key: a, kind: DEPARTMENT, slug: b, name_ar: ب, name_en: B, name_ar_source: official, sources: [s1]}
  - {key: c, parent_key: b, kind: DEPARTMENT, slug: c, name_ar: ج, name_en: C, name_ar_source: official, sources: [s1]}
`, "hierarchy cycle")
}

func TestDuplicateManifestKeyIsRejected(t *testing.T) {
	expectInvalid(t, minimalHeader+`
academic_units:
  - {key: a, kind: COLLEGE, slug: a1, name_ar: أ, name_en: A, name_ar_source: official, sources: [s1]}
  - {key: a, kind: COLLEGE, slug: a2, name_ar: أ, name_en: A, name_ar_source: official, sources: [s1]}
`, "is duplicated")
}

func TestTwoKeysNormalizingToOneSubjectIdentityAreRejected(t *testing.T) {
	// This is the release-blocking invariant: the same canonical Subject must
	// never enter the catalog twice under two manifest keys.
	expectInvalid(t, minimalHeader+`
subjects:
  - {key: s-a, official_code: 0410-101, title_ar: أ, title_en: Calculus I, title_ar_source: gradex_translation, sources: [s1]}
  - {key: s-b, official_code: "0410 101", title_ar: ب, title_en: Calculus One, title_ar_source: gradex_translation, sources: [s1]}
`, "normalizes to")
}

func TestCodelessSubjectsDeduplicateByTitle(t *testing.T) {
	expectInvalid(t, minimalHeader+`
subjects:
  - {key: s-a, title_ar: ندوة, title_en: Seminar, title_ar_source: gradex_translation, sources: [s1]}
  - {key: s-b, title_ar: أخرى, title_en: Seminar, title_ar_source: gradex_translation, sources: [s1]}
`, "same identity as code-less subject")
}

func TestRecommendedLevelAboveInstitutionMaximumIsRejected(t *testing.T) {
	expectInvalid(t, minimalHeader+`
academic_units:
  - {key: u, kind: COLLEGE, slug: u, name_ar: أ, name_en: U, name_ar_source: official, sources: [s1]}
programs:
  - {key: p, owning_unit_key: u, slug: p, name_ar: ب, name_en: P, name_ar_source: official, degree_kind: BSC, sources: [s1]}
curricula:
  - {key: c, program_key: p, version_label: "2026", sources: [s1]}
subjects:
  - {key: s, official_code: 0410-101, title_ar: ج, title_en: S, title_ar_source: gradex_translation, sources: [s1]}
curriculum_subjects:
  - {curriculum_key: c, subject_key: s, requirement_kind: MAJOR_CORE, recommended_level: 9, sources: [s1]}
`, "exceeds the institution maximum")
}

func TestAssertedPlacementRequiresItsOwnCitation(t *testing.T) {
	expectInvalid(t, minimalHeader+`
academic_units:
  - {key: u, kind: COLLEGE, slug: u, name_ar: أ, name_en: U, name_ar_source: official, sources: [s1]}
programs:
  - {key: p, owning_unit_key: u, slug: p, name_ar: ب, name_en: P, name_ar_source: official, degree_kind: BSC, sources: [s1]}
curricula:
  - {key: c, program_key: p, version_label: "2026", sources: [s1]}
subjects:
  - {key: s, official_code: 0410-101, title_ar: ج, title_en: S, title_ar_source: gradex_translation, sources: [s1]}
curriculum_subjects:
  - {curriculum_key: c, subject_key: s, requirement_kind: MAJOR_CORE, recommended_level: 2}
`, "must cite a source")
}

func TestUnknownSourceCitationIsRejected(t *testing.T) {
	expectInvalid(t, minimalHeader+`
academic_units:
  - {key: u, kind: COLLEGE, slug: u, name_ar: أ, name_en: U, name_ar_source: official, sources: [does-not-exist]}
`, "cites unknown source")
}

func TestTwoActiveCurriculaForOneProgramAreRejected(t *testing.T) {
	expectInvalid(t, minimalHeader+`
academic_units:
  - {key: u, kind: COLLEGE, slug: u, name_ar: أ, name_en: U, name_ar_source: official, sources: [s1]}
programs:
  - {key: p, owning_unit_key: u, slug: p, name_ar: ب, name_en: P, name_ar_source: official, degree_kind: BSC, sources: [s1]}
curricula:
  - {key: c1, program_key: p, version_label: "2025", sources: [s1]}
  - {key: c2, program_key: p, version_label: "2026", sources: [s1]}
`, "already has an active curriculum")
}

func TestMissingArabicProvenanceIsRejected(t *testing.T) {
	expectInvalid(t, minimalHeader+`
subjects:
  - {key: s, official_code: 0410-101, title_ar: ج, title_en: S, sources: [s1]}
`, "title_ar_source")
}
