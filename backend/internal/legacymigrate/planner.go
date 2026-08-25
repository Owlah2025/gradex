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
	// OutcomeNoRevision is a legacy Course carrying no Course Revision at all.
	//
	// It has no legacy taxonomy to translate, because the legacy taxonomy lives
	// on revisions, and it cannot receive audience targets, because those are
	// revision-scoped. There is nothing to migrate and nothing may be
	// fabricated, so the Course is reported and left exactly as it is. This
	// outcome exists because the alternative — the Course silently missing from
	// the report — made the summary lie about the size of the corpus.
	OutcomeNoRevision Outcome = "NO_REVISION"
	// OutcomeFounderMappingRequired is a legacy term the mapping file records
	// as an open Founder decision, with candidates.
	//
	// It is deliberately distinct from UNMAPPED. UNMAPPED means the tool found
	// nothing; this means the tool found several defensible answers and refuses
	// to pick, which is a different instruction to the reader and a different
	// action to take.
	OutcomeFounderMappingRequired Outcome = "FOUNDER_MAPPING_REQUIRED"
	// OutcomeDrift is an already-Academic Course whose canonical Subject is not
	// the Subject the current mapping file would choose for its legacy code.
	//
	// It is never repaired. Overwriting it would either undo a deliberate
	// Admin correction or silently re-decide identity for a Course a Student
	// may already own; both are exactly what this tool exists not to do.
	OutcomeDrift Outcome = "DRIFT"
)

// Step is one Course's decision, with the reason stated in product terms.
//
// Every field is either an identifier the Founder already owns or a label
// already visible in the product. No secret, no account, and no Student datum
// reaches the report.
type Step struct {
	CourseID string  `json:"course_id"`
	TitleEn  string  `json:"title_en"`
	Outcome  Outcome `json:"outcome"`
	Detail   string  `json:"detail"`
	// Classification is the Course's classification_model as it stands BEFORE
	// this run. It makes a report row self-describing rather than something the
	// reader has to infer from the outcome.
	Classification string `json:"classification"`
	// LegacyCode and LegacyLabel are the legacy taxonomy identity the Course
	// carries. The label is reported for recognition and is never matched on.
	LegacyCode  string `json:"legacy_subject_code,omitempty"`
	LegacyLabel string `json:"legacy_subject_label,omitempty"`
	// CurrentSubject is the canonical Subject the Course already holds, which is
	// set only for Courses that are already on the Academic Catalog.
	CurrentSubject string `json:"current_subject_code,omitempty"`
	// SubjectCode is the canonical Subject this run would assign, when one
	// resolves.
	SubjectCode string `json:"target_subject_code,omitempty"`
	// MappingSource names the authority that produced SubjectCode. There is
	// exactly one automatic authority — the Founder mapping file, matched on
	// normalized code — so this is never a similarity or a heuristic.
	MappingSource string `json:"mapping_source,omitempty"`
	// Candidates are the Subject codes a pending Founder decision offers.
	Candidates []string `json:"founder_candidates,omitempty"`
	// Disposition, DecidedOn, and ResolutionRequires report what the Founder has
	// concluded about a pending term. They exist so a reader can tell an open
	// question from a closed one that resolved to "keep this unresolved"; the
	// migrator treats both identically and writes neither.
	Disposition        string   `json:"founder_disposition,omitempty"`
	DecidedOn          string   `json:"founder_decided_on,omitempty"`
	ResolutionRequires []string `json:"founder_resolution_requires,omitempty"`
	ProgramWord        string   `json:"program_targets,omitempty"`
	// WouldMutate answers the only question an operator has before running
	// --apply: does this row get written? It is true for exactly the MIGRATE
	// rows and for nothing else.
	WouldMutate bool `json:"would_mutate"`
}

type Counts struct {
	// Total is every Course this run considered, legacy and already-academic
	// alike. It is emitted so a reader can check the report against the corpus
	// instead of trusting that nothing was dropped.
	Total                  int `json:"total"`
	Migrate                int `json:"migrate"`
	Unmapped               int `json:"unmapped"`
	Ambiguous              int `json:"ambiguous"`
	Ineligible             int `json:"ineligible"`
	AlreadyAcademic        int `json:"already_academic"`
	NoRevision             int `json:"no_revision"`
	FounderMappingRequired int `json:"founder_mapping_required"`
	Drift                  int `json:"drift"`
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
	case OutcomeNoRevision:
		p.Counts.NoRevision++
	case OutcomeFounderMappingRequired:
		p.Counts.FounderMappingRequired++
	case OutcomeDrift:
		p.Counts.Drift++
	}
	p.Counts.Total++
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
	// subjectLabels are reported so a human recognises the record. They are
	// never matched on, in either direction: neither a legacy Subject label nor
	// a Course title is academic identity.
	subjectLabels []string
	majorLabels   []string
	hasSubject    bool
	// hasRevision is false for a Course with no Course Revision at all. Such a
	// Course used to be dropped by an inner join and is now explicit.
	hasRevision bool
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

	// Courses a previous run already moved, reported by id so a rerun is
	// legible and so drift is visible rather than assumed absent.
	migrated, err := loadAlreadyAcademic(ctx, tx, institutionID)
	if err != nil {
		return nil, err
	}
	for _, course := range migrated {
		plan.add(describeAcademic(course, mapping))
	}

	courses, err := loadLegacyCourses(ctx, tx)
	if err != nil {
		return nil, err
	}

	for _, course := range courses {
		step := m.decide(course, mapping, subjectByCode)
		// Exactly the MIGRATE rows are written, and an operator can read that
		// off the report before deciding to run --apply at all.
		step.WouldMutate = step.Outcome == OutcomeMigrate
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
	step := Step{
		CourseID:       course.id,
		TitleEn:        course.titleEn,
		Classification: "LEGACY_TAXONOMY",
		LegacyLabel:    strings.Join(course.subjectLabels, " / "),
	}
	if len(course.subjectCodes) == 1 {
		step.LegacyCode = course.subjectCodes[0]
	}

	// Checked first, and before anything reads the legacy taxonomy, because the
	// legacy taxonomy lives on revisions: a Course with none has no taxonomy to
	// read rather than an empty one. Fail closed — never fabricate a revision to
	// carry the audience a migration would want to attach.
	if !course.hasRevision {
		step.Outcome = OutcomeNoRevision
		step.Detail = "the Course has no Course Revision, so it carries no legacy taxonomy to translate and can hold no audience target"
		return step
	}

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
	// A recorded Founder decision outranks "not in the mapping": the file has
	// something to say about this term, and what it says is "do not choose".
	//
	// Both dispositions are equally fail-closed — a decision to leave a record
	// unresolved is still a decision not to migrate it — so only the reported
	// reason differs. It differs because "someone still has to answer this" and
	// "this has been answered, and the answer is to wait for evidence" are
	// different instructions to whoever reads the report.
	if pending, waiting := mapping.PendingFor(legacyCode); waiting {
		step.Outcome = OutcomeFounderMappingRequired
		step.Candidates = pending.CandidateCodes()
		step.Disposition = string(pending.Disposition())
		step.DecidedOn = pending.DecidedOn
		step.ResolutionRequires = pending.ResolutionRequires
		if pending.Disposition() == DispositionKeepUnresolved {
			step.MappingSource = "founder-decision-keep-unresolved"
			step.Detail = fmt.Sprintf(
				"legacy Subject code %s is intentionally unresolved by Founder decision of %s; "+
					"none of the %d canonical candidates may be chosen without authoritative evidence: %s",
				legacyCode, pending.DecidedOn, len(step.Candidates), pending.Why)
			return step
		}
		step.MappingSource = "founder-decision-pending"
		step.Detail = fmt.Sprintf(
			"legacy Subject code %s awaits a Founder decision between %d canonical candidates: %s",
			legacyCode, len(step.Candidates), pending.Why)
		return step
	}
	subjectCode, mapped := mapping.SubjectFor(legacyCode)
	if !mapped {
		step.Outcome = OutcomeUnmapped
		step.Detail = fmt.Sprintf("legacy Subject code %s is not in the mapping", legacyCode)
		return step
	}
	step.SubjectCode = subjectCode
	step.MappingSource = "founder-mapping:" + mappingSourceNote

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

// mappingSourceNote names the single automatic authority a target Subject can
// come from. It is a constant rather than a computed string precisely so that
// no second, softer authority can be introduced without editing this line.
const mappingSourceNote = "normalized-code"

// describeAcademic reports one Course that is already on the Academic Catalog.
//
// It is a read, never a repair. When the current mapping file would send the
// Course's legacy code to a different Subject than the one it actually holds,
// that is reported as DRIFT and nothing is written: the Course may have been
// corrected deliberately by an Admin, or the mapping may have been edited after
// the cutover, and this tool is not entitled to decide which.
func describeAcademic(course academicCourse, mapping *Mapping) Step {
	step := Step{
		CourseID:       course.id,
		TitleEn:        course.titleEn,
		Classification: "ACADEMIC_CATALOG",
		CurrentSubject: course.subjectCode,
		Outcome:        OutcomeAlreadyAcademic,
		Detail:         "already on the Academic Catalog; this run does not touch it",
	}
	if len(course.legacySubjectCodes) == 1 {
		step.LegacyCode = course.legacySubjectCodes[0]
	}
	// Drift is only decidable when the Course carries exactly one legacy code
	// and the file has an opinion about it. Anything else is not drift, it is
	// absence of evidence, and is reported as the ordinary already-academic row.
	if step.LegacyCode == "" {
		return step
	}
	mapped, present := mapping.SubjectFor(step.LegacyCode)
	if !present {
		return step
	}
	step.SubjectCode = mapped
	if NormalizeCode(mapped) == NormalizeCode(course.subjectCode) {
		return step
	}
	step.Outcome = OutcomeDrift
	step.MappingSource = "founder-mapping:" + mappingSourceNote
	step.Detail = fmt.Sprintf(
		"Course holds canonical Subject %s but the mapping sends legacy code %s to %s; left unchanged for a Founder decision",
		displayCode(course.subjectCode), step.LegacyCode, mapped)
	return step
}

func displayCode(code string) string {
	if strings.TrimSpace(code) == "" {
		return "(no code)"
	}
	return code
}
