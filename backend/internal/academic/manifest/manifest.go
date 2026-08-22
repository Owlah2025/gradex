// Package manifest defines the version-controlled Academic Catalog launch data
// format and its validation.
//
// Manifests are curation data, not code: institution records live in checked-in
// YAML under manifests/, never hardcoded in Go. The importer resolves the
// manifest's stable semantic keys to database identities, so re-importing an
// unchanged manifest is a no-op even though every database identifier is a
// freshly generated UUID.
//
// This package performs no database work and reads nothing from the filesystem
// at runtime beyond the embedded manifests, so `validate` is a pure function of
// checked-in data.
package manifest

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/Owlah2025/gradex/backend/internal/academic"
)

// TitleSource records where a piece of bilingual text came from. Gradex must
// never present its own translation as the institution's official wording, so
// every localized field declares its provenance and validation requires it.
type TitleSource string

const (
	// SourceOfficial means the institution itself publishes this exact wording.
	SourceOfficial TitleSource = "official"
	// SourceGradexTranslation means Gradex supplied the wording because the
	// institution publishes the field in only one language.
	SourceGradexTranslation TitleSource = "gradex_translation"
)

func (s TitleSource) Valid() bool {
	return s == SourceOfficial || s == SourceGradexTranslation
}

// Manifest is one institution's launch catalog.
type Manifest struct {
	// ID is the import package identifier an Admin selects. It is the only
	// caller-supplied selector the HTTP import accepts, which is why no
	// filesystem path or URL is ever accepted from a client.
	ID          string `yaml:"id"`
	Version     string `yaml:"version"`
	Description string `yaml:"description"`

	Institution Institution `yaml:"institution"`

	Units     []Unit       `yaml:"academic_units"`
	Programs  []Program    `yaml:"programs"`
	Curricula []Curriculum `yaml:"curricula"`
	Subjects  []Subject    `yaml:"subjects"`
	Mappings  []Mapping    `yaml:"curriculum_subjects"`
}

type Institution struct {
	Key                string      `yaml:"key"`
	Slug               string      `yaml:"slug"`
	CountryCode        string      `yaml:"country_code"`
	NameAr             string      `yaml:"name_ar"`
	NameEn             string      `yaml:"name_en"`
	NameArSource       TitleSource `yaml:"name_ar_source"`
	MaxAcademicLevel   int         `yaml:"max_academic_level"`
	HasFoundationStage bool        `yaml:"has_foundation_stage"`
	Sources            []string    `yaml:"sources"`
}

type Unit struct {
	Key          string      `yaml:"key"`
	ParentKey    string      `yaml:"parent_key"`
	Kind         string      `yaml:"kind"`
	Slug         string      `yaml:"slug"`
	NameAr       string      `yaml:"name_ar"`
	NameEn       string      `yaml:"name_en"`
	NameArSource TitleSource `yaml:"name_ar_source"`
	Sources      []string    `yaml:"sources"`
}

type Program struct {
	Key          string      `yaml:"key"`
	OwningUnit   string      `yaml:"owning_unit_key"`
	Slug         string      `yaml:"slug"`
	NameAr       string      `yaml:"name_ar"`
	NameEn       string      `yaml:"name_en"`
	NameArSource TitleSource `yaml:"name_ar_source"`
	DegreeKind   string      `yaml:"degree_kind"`
	Sources      []string    `yaml:"sources"`
}

type Curriculum struct {
	Key          string `yaml:"key"`
	ProgramKey   string `yaml:"program_key"`
	VersionLabel string `yaml:"version_label"`
	// VersionLabelSource states whether the institution itself publishes this
	// plan label. A Gradex placeholder must never reach a Student looking like
	// the university's own version name.
	VersionLabelSource TitleSource `yaml:"version_label_source"`
	VersionLabelNote   string      `yaml:"version_label_note"`
	EffectiveFromYear  *int        `yaml:"effective_from_year"`
	Sources            []string    `yaml:"sources"`
}

type Subject struct {
	Key           string      `yaml:"key"`
	OwningUnit    string      `yaml:"owning_unit_key"`
	OfficialCode  string      `yaml:"official_code"`
	TitleAr       string      `yaml:"title_ar"`
	TitleEn       string      `yaml:"title_en"`
	TitleArSource TitleSource `yaml:"title_ar_source"`
	Sources       []string    `yaml:"sources"`
}

type Mapping struct {
	CurriculumKey string `yaml:"curriculum_key"`
	SubjectKey    string `yaml:"subject_key"`
	Requirement   string `yaml:"requirement_kind"`
	// Level, Semester, and Credits are pointers so "absent" and "zero" are
	// distinguishable. Absent is the correct value whenever the institution
	// publishes no placement — a fabricated level is worse than a null one,
	// because discovery ranking already degrades gracefully from
	// "my curriculum at my level" to "my curriculum at any level".
	Level    *int     `yaml:"recommended_level"`
	Semester *int     `yaml:"recommended_semester"`
	Credits  *float64 `yaml:"credits"`
	Sources  []string `yaml:"sources"`
}

// SourceRecord is one provenance entry. Provenance is curation metadata: it is
// never consulted by authorization or by any runtime decision.
type SourceRecord struct {
	ID          string `yaml:"id"`
	URL         string `yaml:"url"`
	Title       string `yaml:"title"`
	Type        string `yaml:"type"`
	RetrievedAt string `yaml:"retrieved_at"`
	// Supports is free-form curation notes describing what this source backs.
	// It is documentation for a human reviewer, never read by any logic.
	Supports []map[string]any `yaml:"supports"`
}

type SourceCatalog struct {
	Sources []SourceRecord `yaml:"sources"`
}

var (
	ErrInvalidManifest = errors.New("manifest is invalid")

	keyPattern  = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	datePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

// ValidationError names the exact field that failed, so a curation mistake is
// actionable without reading the importer.
type ValidationError struct {
	Entity string
	Key    string
	Field  string
	Reason string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("%s %q: %s %s", e.Entity, e.Key, e.Field, e.Reason)
}

func (e ValidationError) Unwrap() error { return ErrInvalidManifest }

type ValidationErrors []ValidationError

func (e ValidationErrors) Error() string {
	parts := make([]string, 0, len(e))
	for _, item := range e {
		parts = append(parts, item.Error())
	}
	return fmt.Sprintf("manifest is invalid (%d problems): %s", len(e), strings.Join(parts, "; "))
}

func (e ValidationErrors) Unwrap() error { return ErrInvalidManifest }

// Validate performs every check that does not need a database. It returns all
// problems at once rather than the first, because a curator fixing a manifest
// wants the whole list.
func (m *Manifest) Validate(sources *SourceCatalog) error {
	var problems ValidationErrors
	add := func(entity, key, field, reason string) {
		problems = append(problems, ValidationError{Entity: entity, Key: key, Field: field, Reason: reason})
	}

	knownSources := map[string]bool{}
	if sources != nil {
		for _, source := range sources.Sources {
			if source.ID == "" {
				add("source", "", "id", "is required")
				continue
			}
			if knownSources[source.ID] {
				add("source", source.ID, "id", "is duplicated")
			}
			knownSources[source.ID] = true
			if strings.TrimSpace(source.URL) == "" {
				add("source", source.ID, "url", "is required")
			}
			if strings.TrimSpace(source.Title) == "" {
				add("source", source.ID, "title", "is required")
			}
			if !datePattern.MatchString(source.RetrievedAt) {
				add("source", source.ID, "retrieved_at", "must be YYYY-MM-DD")
			}
		}
	}
	// Every factual claim must cite a source the catalog actually defines.
	// A dangling citation is a curation defect, not a cosmetic one.
	citations := func(entity, key string, refs []string) {
		if len(refs) == 0 {
			add(entity, key, "sources", "must cite at least one source")
			return
		}
		for _, ref := range refs {
			if sources != nil && !knownSources[ref] {
				add(entity, key, "sources", fmt.Sprintf("cites unknown source %q", ref))
			}
		}
	}
	localized := func(entity, key, field string, source TitleSource) {
		if !source.Valid() {
			add(entity, key, field, `must be "official" or "gradex_translation"`)
		}
	}

	if !keyPattern.MatchString(m.ID) {
		add("manifest", m.ID, "id", "must be a lowercase hyphenated key")
	}
	if strings.TrimSpace(m.Version) == "" {
		add("manifest", m.ID, "version", "is required")
	}

	institution := m.Institution
	if !keyPattern.MatchString(institution.Key) {
		add("institution", institution.Key, "key", "must be a lowercase hyphenated key")
	}
	if !slugPattern.MatchString(institution.Slug) {
		add("institution", institution.Key, "slug", "must be a lowercase hyphenated slug")
	}
	if !regexp.MustCompile(`^[A-Z]{2}$`).MatchString(institution.CountryCode) {
		add("institution", institution.Key, "country_code", "must be a two-letter uppercase code")
	}
	if strings.TrimSpace(institution.NameAr) == "" || strings.TrimSpace(institution.NameEn) == "" {
		add("institution", institution.Key, "name", "requires both Arabic and English")
	}
	localized("institution", institution.Key, "name_ar_source", institution.NameArSource)
	if institution.MaxAcademicLevel < 1 || institution.MaxAcademicLevel > 12 {
		add("institution", institution.Key, "max_academic_level", "must be between 1 and 12")
	}
	citations("institution", institution.Key, institution.Sources)

	unitKeys := map[string]Unit{}
	for _, unit := range m.Units {
		if !keyPattern.MatchString(unit.Key) {
			add("academic_unit", unit.Key, "key", "must be a lowercase hyphenated key")
			continue
		}
		if _, seen := unitKeys[unit.Key]; seen {
			add("academic_unit", unit.Key, "key", "is duplicated")
		}
		unitKeys[unit.Key] = unit
		if !academic.UnitKind(unit.Kind).Valid() {
			add("academic_unit", unit.Key, "kind", "must be COLLEGE, DEPARTMENT, or SERVICE_UNIT")
		}
		if !slugPattern.MatchString(unit.Slug) {
			add("academic_unit", unit.Key, "slug", "must be a lowercase hyphenated slug")
		}
		if strings.TrimSpace(unit.NameAr) == "" || strings.TrimSpace(unit.NameEn) == "" {
			add("academic_unit", unit.Key, "name", "requires both Arabic and English")
		}
		localized("academic_unit", unit.Key, "name_ar_source", unit.NameArSource)
		citations("academic_unit", unit.Key, unit.Sources)
	}
	for _, unit := range m.Units {
		if unit.ParentKey == "" {
			continue
		}
		if unit.ParentKey == unit.Key {
			add("academic_unit", unit.Key, "parent_key", "cannot be its own parent")
			continue
		}
		if _, ok := unitKeys[unit.ParentKey]; !ok {
			add("academic_unit", unit.Key, "parent_key", fmt.Sprintf("references unknown unit %q", unit.ParentKey))
		}
	}
	// Cycles are refused in the manifest as well as in the database, so a bad
	// manifest fails validation before anything opens a transaction.
	for _, unit := range m.Units {
		seen := map[string]bool{unit.Key: true}
		cursor := unit.ParentKey
		for cursor != "" {
			if seen[cursor] {
				add("academic_unit", unit.Key, "parent_key", "forms a hierarchy cycle")
				break
			}
			seen[cursor] = true
			next, ok := unitKeys[cursor]
			if !ok {
				break
			}
			cursor = next.ParentKey
		}
	}

	programKeys := map[string]bool{}
	for _, program := range m.Programs {
		if !keyPattern.MatchString(program.Key) {
			add("program", program.Key, "key", "must be a lowercase hyphenated key")
			continue
		}
		if programKeys[program.Key] {
			add("program", program.Key, "key", "is duplicated")
		}
		programKeys[program.Key] = true
		if !slugPattern.MatchString(program.Slug) {
			add("program", program.Key, "slug", "must be a lowercase hyphenated slug")
		}
		if program.OwningUnit != "" {
			if _, ok := unitKeys[program.OwningUnit]; !ok {
				add("program", program.Key, "owning_unit_key",
					fmt.Sprintf("references unknown unit %q", program.OwningUnit))
			}
		}
		if strings.TrimSpace(program.NameAr) == "" || strings.TrimSpace(program.NameEn) == "" {
			add("program", program.Key, "name", "requires both Arabic and English")
		}
		localized("program", program.Key, "name_ar_source", program.NameArSource)
		if !regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`).MatchString(program.DegreeKind) {
			add("program", program.Key, "degree_kind", "must be an uppercase identifier")
		}
		citations("program", program.Key, program.Sources)
	}

	curriculumKeys := map[string]Curriculum{}
	activePerProgram := map[string]string{}
	for _, curriculum := range m.Curricula {
		if !keyPattern.MatchString(curriculum.Key) {
			add("curriculum", curriculum.Key, "key", "must be a lowercase hyphenated key")
			continue
		}
		if _, seen := curriculumKeys[curriculum.Key]; seen {
			add("curriculum", curriculum.Key, "key", "is duplicated")
		}
		curriculumKeys[curriculum.Key] = curriculum
		if !programKeys[curriculum.ProgramKey] {
			add("curriculum", curriculum.Key, "program_key",
				fmt.Sprintf("references unknown program %q", curriculum.ProgramKey))
		}
		if strings.TrimSpace(curriculum.VersionLabel) == "" {
			add("curriculum", curriculum.Key, "version_label", "is required")
		}
		if !curriculum.VersionLabelSource.Valid() {
			add("curriculum", curriculum.Key, "version_label_source",
				`must be "official" or "gradex_translation"`)
		}
		// A placeholder label is only acceptable when it explains itself.
		if curriculum.VersionLabelSource == SourceGradexTranslation &&
			strings.TrimSpace(curriculum.VersionLabelNote) == "" {
			add("curriculum", curriculum.Key, "version_label_note",
				"is required when the label is a Gradex placeholder")
		}
		// One ACTIVE curriculum per program is a database invariant; catching it
		// here turns a constraint violation into a curation message.
		if previous, clash := activePerProgram[curriculum.ProgramKey]; clash {
			add("curriculum", curriculum.Key, "program_key",
				fmt.Sprintf("program already has an active curriculum %q in this manifest", previous))
		}
		activePerProgram[curriculum.ProgramKey] = curriculum.Key
		if curriculum.EffectiveFromYear != nil &&
			(*curriculum.EffectiveFromYear < 1900 || *curriculum.EffectiveFromYear > 2200) {
			add("curriculum", curriculum.Key, "effective_from_year", "is out of range")
		}
		citations("curriculum", curriculum.Key, curriculum.Sources)
	}

	subjectKeys := map[string]bool{}
	byNormalizedCode := map[string]string{}
	byNormalizedTitle := map[string]string{}
	for _, subject := range m.Subjects {
		if !keyPattern.MatchString(subject.Key) {
			add("subject", subject.Key, "key", "must be a lowercase hyphenated key")
			continue
		}
		if subjectKeys[subject.Key] {
			add("subject", subject.Key, "key", "is duplicated")
		}
		subjectKeys[subject.Key] = true
		if subject.OwningUnit != "" {
			if _, ok := unitKeys[subject.OwningUnit]; !ok {
				add("subject", subject.Key, "owning_unit_key",
					fmt.Sprintf("references unknown unit %q", subject.OwningUnit))
			}
		}
		if strings.TrimSpace(subject.TitleAr) == "" || strings.TrimSpace(subject.TitleEn) == "" {
			add("subject", subject.Key, "title", "requires both Arabic and English")
		}
		localized("subject", subject.Key, "title_ar_source", subject.TitleArSource)
		citations("subject", subject.Key, subject.Sources)

		// Two manifest keys that normalize to the same Subject identity would be
		// refused by the database mid-import. Catching it here keeps the failure
		// in review rather than in production.
		if code := strings.TrimSpace(subject.OfficialCode); code != "" {
			normalized := academic.NormalizeCode(code)
			if normalized == "" {
				add("subject", subject.Key, "official_code", "normalizes to an empty identity")
				continue
			}
			if previous, clash := byNormalizedCode[normalized]; clash {
				add("subject", subject.Key, "official_code",
					fmt.Sprintf("normalizes to %q, which subject %q already claims", normalized, previous))
			}
			byNormalizedCode[normalized] = subject.Key
			continue
		}
		// Code-less subjects are identified by title, matching the T1 indexes.
		for _, title := range []string{subject.TitleAr, subject.TitleEn} {
			normalized := strings.ToLower(strings.Join(strings.Fields(title), " "))
			if previous, clash := byNormalizedTitle[normalized]; clash && previous != subject.Key {
				add("subject", subject.Key, "title",
					fmt.Sprintf("normalizes to the same identity as code-less subject %q", previous))
			}
			byNormalizedTitle[normalized] = subject.Key
		}
	}

	seenMapping := map[string]bool{}
	for _, mapping := range m.Mappings {
		id := mapping.CurriculumKey + "|" + mapping.SubjectKey
		if seenMapping[id] {
			add("curriculum_subject", id, "pair", "is duplicated")
		}
		seenMapping[id] = true
		curriculum, ok := curriculumKeys[mapping.CurriculumKey]
		if !ok {
			add("curriculum_subject", id, "curriculum_key",
				fmt.Sprintf("references unknown curriculum %q", mapping.CurriculumKey))
		}
		if !subjectKeys[mapping.SubjectKey] {
			add("curriculum_subject", id, "subject_key",
				fmt.Sprintf("references unknown subject %q", mapping.SubjectKey))
		}
		if !academic.RequirementKind(mapping.Requirement).Valid() {
			add("curriculum_subject", id, "requirement_kind", "is not a supported requirement kind")
		}
		if mapping.Level != nil {
			if *mapping.Level < 1 {
				add("curriculum_subject", id, "recommended_level", "must be at least 1")
			}
			if *mapping.Level > institution.MaxAcademicLevel {
				add("curriculum_subject", id, "recommended_level",
					fmt.Sprintf("exceeds the institution maximum of %d", institution.MaxAcademicLevel))
			}
			// A level claim is a factual claim about the institution's plan, so
			// it needs its own citation rather than inheriting one.
			if len(mapping.Sources) == 0 {
				add("curriculum_subject", id, "recommended_level",
					"asserts a placement, so it must cite a source")
			}
		}
		if mapping.Semester != nil {
			if *mapping.Semester < 1 || *mapping.Semester > 3 {
				add("curriculum_subject", id, "recommended_semester", "must be between 1 and 3")
			}
			if len(mapping.Sources) == 0 {
				add("curriculum_subject", id, "recommended_semester",
					"asserts a placement, so it must cite a source")
			}
		}
		if mapping.Credits != nil && (*mapping.Credits < 0 || *mapping.Credits > 30) {
			add("curriculum_subject", id, "credits", "is out of range")
		}
		_ = curriculum
	}

	if len(problems) > 0 {
		return problems
	}
	return nil
}
