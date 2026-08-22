package legacymigrate

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Outcome is what the planner decided about one legacy Course.
//
// Only MIGRATE is ever acted on. Every other outcome leaves the Course exactly
// as it is — still LEGACY_TAXONOMY, still fully operational — and is reported so
// a human can decide. Absence of a mapping is never treated as permission to
// guess (D-091 §9).
type Outcome string

const (
	// OutcomeMigrate is an unambiguous translation: one Subject, one
	// Institution, eligible in every respect.
	OutcomeMigrate Outcome = "MIGRATE"
	// OutcomeUnmapped means the legacy taxonomy carries nothing the mapping
	// file translates — no SUBJECT term, no academic code, or no entry.
	OutcomeUnmapped Outcome = "UNMAPPED"
	// OutcomeAmbiguous means the Course's own revisions disagree about which
	// legacy Subject it teaches. Identity cannot be chosen for the Founder.
	OutcomeAmbiguous Outcome = "AMBIGUOUS"
	// OutcomeIneligible means the translation resolves but the target cannot
	// legally be assigned: retired Subject, or the mapped Subject is absent
	// from the Institution.
	OutcomeIneligible Outcome = "INELIGIBLE"
	// OutcomeAlreadyAcademic is a Course a previous run migrated. It keeps
	// reruns honest rather than silently invisible.
	OutcomeAlreadyAcademic Outcome = "ALREADY_ACADEMIC"
)

// Step is one Course's decision, with the reason stated in product terms.
type Step struct {
	CourseID    string  `json:"course_id"`
	TitleEn     string  `json:"title_en"`
	Outcome     Outcome `json:"outcome"`
	Detail      string  `json:"detail"`
	SubjectCode string  `json:"subject_code,omitempty"`
	ProgramWord string  `json:"program_targets,omitempty"`
}

type Counts struct {
	Migrate         int `json:"migrate"`
	Unmapped        int `json:"unmapped"`
	Ambiguous       int `json:"ambiguous"`
	Ineligible      int `json:"ineligible"`
	AlreadyAcademic int `json:"already_academic"`
}

// Plan is the deterministic description of what an apply would do. Report
// returns it without writing; Apply returns the plan it actually executed.
type Plan struct {
	MappingID       string `json:"mapping_id"`
	MappingVersion  string `json:"mapping_version"`
	InstitutionSlug string `json:"institution_slug"`
	Steps           []Step `json:"steps"`
	Counts          Counts `json:"counts"`
	Applied         bool   `json:"applied"`
}

func (p *Plan) add(step Step) {
	p.Steps = append(p.Steps, step)
	switch step.Outcome {
	case OutcomeMigrate:
		p.Counts.Migrate++
	case OutcomeUnmapped:
		p.Counts.Unmapped++
	case OutcomeAmbiguous:
		p.Counts.Ambiguous++
	case OutcomeIneligible:
		p.Counts.Ineligible++
	case OutcomeAlreadyAcademic:
		p.Counts.AlreadyAcademic++
	}
}

// Migrator plans and applies the legacy cutover.
type Migrator struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) (*Migrator, error) {
	if pool == nil {
		return nil, errors.New("legacy migrator requires a database pool")
	}
	return &Migrator{pool: pool}, nil
}

// Options controls one run.
type Options struct {
	// ActorDescriptor is audited on every migrated Course.
	ActorDescriptor string
	// Apply false performs a report: the same plan is computed inside a
	// transaction that is always rolled back, so a report provably cannot
	// write. This is the same guarantee the Academic Catalog importer gives.
	Apply bool
}

// legacyCourse is one row of the workset.
type legacyCourse struct {
	id      string
	titleEn string
	// subjectCodes are the distinct legacy SUBJECT academic codes across every
	// revision of this Course. More than one means the Course disagrees with
	// itself about what it teaches.
	subjectCodes []string
	majorLabels  []string
	hasSubject   bool
}

// Run computes the plan and, when Options.Apply is set, executes it.
//
// The workset is exactly `classification_model = 'LEGACY_TAXONOMY'`, which is
// what makes the run idempotent: a Course this tool migrates leaves the workset
// permanently, so a rerun neither re-migrates it nor has to remember it.
func (m *Migrator) Run(ctx context.Context, mapping *Mapping, options Options) (*Plan, error) {
	if mapping == nil {
		return nil, errors.New("legacy migration requires a mapping")
	}
	if err := mapping.Validate(); err != nil {
		return nil, err
	}
	plan := &Plan{
		MappingID: mapping.ID, MappingVersion: mapping.Version,
		InstitutionSlug: mapping.InstitutionSlug, Steps: []Step{},
	}

	tx, err := m.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("beginning legacy migration: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	institutionID, err := resolveInstitution(ctx, tx, mapping.InstitutionSlug)
	if err != nil {
		return nil, err
	}
	subjectByCode, err := loadSubjects(ctx, tx, institutionID)
	if err != nil {
		return nil, err
	}
	programBySlug, err := loadPrograms(ctx, tx, institutionID)
	if err != nil {
		return nil, err
	}

	// Courses a previous run already moved, reported so a rerun is legible.
	migrated, err := countAlreadyAcademic(ctx, tx, institutionID)
	if err != nil {
		return nil, err
	}
	for i := 0; i < migrated; i++ {
		plan.add(Step{Outcome: OutcomeAlreadyAcademic, Detail: "already on the Academic Catalog"})
	}

	courses, err := loadLegacyCourses(ctx, tx)
	if err != nil {
		return nil, err
	}

	for _, course := range courses {
		step := m.decide(course, mapping, subjectByCode)
		if step.Outcome != OutcomeMigrate || !options.Apply {
			plan.add(step)
			continue
		}
		targets := resolveProgramTargets(course, mapping, programBySlug)
		if err := migrateCourse(ctx, tx, course, institutionID,
			subjectByCode[NormalizeCode(step.SubjectCode)].id, targets, options.ActorDescriptor); err != nil {
			return nil, fmt.Errorf("migrating course %s: %w", course.id, err)
		}
		if len(targets) > 0 {
			step.ProgramWord = strings.Join(targetSlugs(course, mapping), ", ")
		}
		plan.add(step)
	}

	if options.Apply {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("committing legacy migration: %w", err)
		}
		plan.Applied = true
	}
	return plan, nil
}

// decide is the whole classification rule, in one place and with no writes.
func (m *Migrator) decide(
	course legacyCourse, mapping *Mapping, subjects map[string]subjectRow,
) Step {
	step := Step{CourseID: course.id, TitleEn: course.titleEn}

	if !course.hasSubject {
		step.Outcome = OutcomeUnmapped
		step.Detail = "no legacy Subject term on any revision"
		return step
	}
	// The Course names a legacy Subject term, but that term carries no academic
	// code — so there is nothing to match a canonical Subject on. This is the
	// codeless-term case and it must be checked BEFORE indexing the codes.
	if len(course.subjectCodes) == 0 {
		step.Outcome = OutcomeUnmapped
		step.Detail = "legacy Subject term carries no academic code"
		return step
	}
	// A Course whose revisions name different legacy Subjects has no single
	// academic identity to migrate to. Choosing one would be inventing identity.
	if len(course.subjectCodes) > 1 {
		step.Outcome = OutcomeAmbiguous
		step.Detail = fmt.Sprintf("revisions name %d different legacy Subject codes", len(course.subjectCodes))
		return step
	}
	legacyCode := course.subjectCodes[0]
	if NormalizeCode(legacyCode) == "" {
		step.Outcome = OutcomeUnmapped
		step.Detail = "legacy Subject term carries no usable academic code"
		return step
	}
	subjectCode, mapped := mapping.SubjectFor(legacyCode)
	if !mapped {
		step.Outcome = OutcomeUnmapped
		step.Detail = fmt.Sprintf("legacy Subject code %s is not in the mapping", legacyCode)
		return step
	}
	step.SubjectCode = subjectCode

	target, present := subjects[NormalizeCode(subjectCode)]
	if !present {
		step.Outcome = OutcomeIneligible
		step.Detail = fmt.Sprintf("mapped Subject %s does not exist in this Institution", subjectCode)
		return step
	}
	if target.retired {
		step.Outcome = OutcomeIneligible
		step.Detail = fmt.Sprintf("mapped Subject %s is retired", subjectCode)
		return step
	}
	step.Outcome = OutcomeMigrate
	step.Detail = fmt.Sprintf("legacy Subject %s becomes canonical Subject %s", legacyCode, subjectCode)
	return step
}

func targetSlugs(course legacyCourse, mapping *Mapping) []string {
	var slugs []string
	for _, label := range course.majorLabels {
		slugs = append(slugs, mapping.ProgramsFor(label)...)
	}
	sort.Strings(slugs)
	return slugs
}

func resolveProgramTargets(
	course legacyCourse, mapping *Mapping, programs map[string]string,
) []string {
	seen := map[string]struct{}{}
	var ids []string
	for _, slug := range targetSlugs(course, mapping) {
		id, ok := programs[slug]
		if !ok {
			// A Program the catalog does not have is skipped rather than
			// invented. The Course keeps the automatic audience its Subject
			// implies, which is a truthful fallback.
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
