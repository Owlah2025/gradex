// Package importer applies a version-controlled Academic Catalog manifest to
// the database.
//
// Three properties define it:
//
//   - Idempotent. Manifest keys resolve to database rows through natural keys
//     (institution slug, unit slug, program slug, curriculum version label,
//     normalized subject code or title). Database identifiers are never part of
//     the manifest, so re-importing unchanged data is a no-op even though every
//     UUID is generated fresh.
//   - Atomic. One manifest import is one transaction. A failure at entity N
//     leaves nothing behind, so an institution is never half-imported.
//   - Non-destructive. Absence from a manifest is drift to be reported, never a
//     deletion. Retirement requires explicit intent, which this manifest format
//     does not yet express.
package importer

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/Owlah2025/gradex/backend/internal/academic"
	"github.com/Owlah2025/gradex/backend/internal/academic/manifest"
)

// Action is what the plan intends to do with one entity.
type Action string

const (
	ActionCreate Action = "CREATE"
	ActionUpdate Action = "UPDATE"
	ActionNoop   Action = "NOOP"
	// ActionDrift marks a database row the manifest does not describe. It is
	// reported and never acted on: absence is not deletion.
	ActionDrift Action = "DRIFT"
)

type Step struct {
	Entity string `json:"entity"`
	Key    string `json:"key"`
	Action Action `json:"action"`
	Detail string `json:"detail,omitempty"`
}

// Plan is the deterministic description of what an apply would do. dry-run
// returns it without writing; apply returns the plan it actually executed.
type Plan struct {
	ManifestID      string `json:"manifest_id"`
	ManifestVersion string `json:"manifest_version"`
	InstitutionSlug string `json:"institution_slug"`
	Steps           []Step `json:"steps"`
	Counts          Counts `json:"counts"`
	Applied         bool   `json:"applied"`
}

type Counts struct {
	Create int `json:"create"`
	Update int `json:"update"`
	Noop   int `json:"noop"`
	Drift  int `json:"drift"`
}

func (p *Plan) add(entity, key string, action Action, detail string) {
	p.Steps = append(p.Steps, Step{Entity: entity, Key: key, Action: action, Detail: detail})
	switch action {
	case ActionCreate:
		p.Counts.Create++
	case ActionUpdate:
		p.Counts.Update++
	case ActionNoop:
		p.Counts.Noop++
	case ActionDrift:
		p.Counts.Drift++
	}
}

var (
	// ErrIdentityRebind is the fail-closed refusal for an update that would move
	// an entity to a different Institution or Program. A manifest key edit must
	// never silently relocate real academic data; that needs a human decision.
	ErrIdentityRebind = errors.New("manifest would rebind an existing entity to a different owner")
	ErrImportConflict = errors.New("import conflicts with existing catalog data")
)

// Importer owns no state beyond its repository handle.
type Importer struct {
	repo *academic.Repository
}

func New(repo *academic.Repository) (*Importer, error) {
	if repo == nil {
		return nil, errors.New("importer requires an academic repository")
	}
	return &Importer{repo: repo}, nil
}

// Options configures one import run.
type Options struct {
	// Actor is audited on every mutation. The CLI supplies a SYSTEM principal;
	// the HTTP path supplies the authenticated Admin.
	Actor academic.Actor
	// Apply false performs a dry run: the same plan is computed inside a
	// transaction that is always rolled back, so a dry run provably cannot write.
	Apply bool
}

// Run computes the plan and, when Options.Apply is set, executes it. Both modes
// run in one transaction; dry run rolls back unconditionally.
func (i *Importer) Run(ctx context.Context, pkg *manifest.Package, options Options) (*Plan, error) {
	if pkg == nil || pkg.Manifest == nil {
		return nil, errors.New("importer requires a manifest package")
	}
	// Validate again at the boundary. Load already validates, but an in-process
	// caller could construct a package directly.
	if err := pkg.Manifest.Validate(pkg.Sources); err != nil {
		return nil, err
	}
	if err := options.Actor.Validate(); err != nil {
		return nil, err
	}

	m := pkg.Manifest
	plan := &Plan{
		ManifestID:      m.ID,
		ManifestVersion: m.Version,
		InstitutionSlug: m.Institution.Slug,
		Applied:         options.Apply,
	}

	// errDryRun unwinds the transaction after a successful dry run. It never
	// escapes: Run swallows exactly this sentinel.
	errDryRun := errors.New("dry run rollback")

	err := i.repo.ExecTx(ctx, func(tx pgx.Tx) error {
		if err := i.importInto(ctx, tx, m, plan, options); err != nil {
			return err
		}
		if !options.Apply {
			return errDryRun
		}
		return nil
	})
	if err != nil && !errors.Is(err, errDryRun) {
		return nil, err
	}
	return plan, nil
}

type resolved struct {
	institutionID string
	units         map[string]string
	programs      map[string]string
	curricula     map[string]string
	subjects      map[string]string
}

func (i *Importer) importInto(
	ctx context.Context, tx pgx.Tx, m *manifest.Manifest, plan *Plan, options Options,
) error {
	state := resolved{
		units:     map[string]string{},
		programs:  map[string]string{},
		curricula: map[string]string{},
		subjects:  map[string]string{},
	}

	// Serialize concurrent imports of the same institution. Two importers racing
	// on the same manifest would otherwise both see "absent" and both insert.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`,
		"academic-import:"+m.Institution.Slug); err != nil {
		return fmt.Errorf("acquiring import lock: %w", err)
	}

	if err := i.upsertInstitution(ctx, tx, m, plan, options, &state); err != nil {
		return err
	}
	if err := i.upsertUnits(ctx, tx, m, plan, options, &state); err != nil {
		return err
	}
	if err := i.upsertPrograms(ctx, tx, m, plan, options, &state); err != nil {
		return err
	}
	if err := i.upsertCurricula(ctx, tx, m, plan, options, &state); err != nil {
		return err
	}
	if err := i.upsertSubjects(ctx, tx, m, plan, options, &state); err != nil {
		return err
	}
	if err := i.upsertMappings(ctx, tx, m, plan, options, &state); err != nil {
		return err
	}
	return i.reportDrift(ctx, tx, m, plan, &state)
}

func (i *Importer) audit(
	ctx context.Context, tx pgx.Tx, options Options, action, targetType, targetID, reason string,
	metadata map[string]any,
) error {
	if !options.Apply {
		return nil
	}
	return academic.WriteAuditEvent(ctx, tx, options.Actor, academic.AuditEvent{
		Action: action, TargetType: targetType, TargetID: targetID,
		Reason: reason, Metadata: metadata,
	})
}

func (i *Importer) upsertInstitution(
	ctx context.Context, tx pgx.Tx, m *manifest.Manifest, plan *Plan, options Options, state *resolved,
) error {
	institution := m.Institution
	var (
		id                      string
		nameAr, nameEn, country string
		maxLevel                int
		foundation              bool
	)
	err := tx.QueryRow(ctx, `
		SELECT id::text, name_ar, name_en, country_code, max_academic_level, has_foundation_stage
		FROM institutions WHERE slug = $1 AND retired_at IS NULL`, institution.Slug).
		Scan(&id, &nameAr, &nameEn, &country, &maxLevel, &foundation)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if !options.Apply {
			// A dry run still needs a real row to plan children against, so it
			// inserts inside the transaction that is guaranteed to roll back.
			plan.add("institution", institution.Key, ActionCreate, institution.Slug)
		} else {
			plan.add("institution", institution.Key, ActionCreate, institution.Slug)
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO institutions (country_code, slug, name_ar, name_en, max_academic_level, has_foundation_stage)
			VALUES ($1, $2, $3, $4, $5, $6) RETURNING id::text`,
			institution.CountryCode, institution.Slug, institution.NameAr, institution.NameEn,
			institution.MaxAcademicLevel, institution.HasFoundationStage).Scan(&id); err != nil {
			return fmt.Errorf("creating institution %s: %w", institution.Key, err)
		}
		state.institutionID = id
		return i.audit(ctx, tx, options, "ACADEMIC_INSTITUTION_CREATED", "ACADEMIC_INSTITUTION", id,
			"Academic Institution created by catalog import", map[string]any{
				"manifest_id": m.ID, "manifest_version": m.Version,
				"slug": institution.Slug, "country_code": institution.CountryCode,
			})
	case err != nil:
		return fmt.Errorf("loading institution %s: %w", institution.Slug, err)
	}

	state.institutionID = id
	// Country code is identity-shaped: a manifest that moves an institution to
	// another country is a curation error, not an update.
	if country != institution.CountryCode {
		return fmt.Errorf("%w: institution %s is %s in the database and %s in the manifest",
			ErrIdentityRebind, institution.Slug, country, institution.CountryCode)
	}
	if nameAr == institution.NameAr && nameEn == institution.NameEn &&
		maxLevel == institution.MaxAcademicLevel && foundation == institution.HasFoundationStage {
		plan.add("institution", institution.Key, ActionNoop, institution.Slug)
		return nil
	}
	plan.add("institution", institution.Key, ActionUpdate, institution.Slug)
	if _, err := tx.Exec(ctx, `
		UPDATE institutions SET name_ar = $1, name_en = $2, max_academic_level = $3,
			has_foundation_stage = $4, updated_at = now() WHERE id = $5::uuid`,
		institution.NameAr, institution.NameEn, institution.MaxAcademicLevel,
		institution.HasFoundationStage, id); err != nil {
		return fmt.Errorf("updating institution %s: %w", institution.Key, err)
	}
	return i.audit(ctx, tx, options, "ACADEMIC_INSTITUTION_UPDATED", "ACADEMIC_INSTITUTION", id,
		"Academic Institution updated by catalog import", map[string]any{
			"manifest_id": m.ID, "manifest_version": m.Version, "slug": institution.Slug,
		})
}

// unitsInDependencyOrder returns units parents-first, so a child never resolves
// before its parent exists. Manifest validation has already refused cycles.
func unitsInDependencyOrder(units []manifest.Unit) []manifest.Unit {
	byKey := map[string]manifest.Unit{}
	for _, unit := range units {
		byKey[unit.Key] = unit
	}
	ordered := make([]manifest.Unit, 0, len(units))
	placed := map[string]bool{}
	var place func(manifest.Unit)
	place = func(unit manifest.Unit) {
		if placed[unit.Key] {
			return
		}
		if unit.ParentKey != "" {
			if parent, ok := byKey[unit.ParentKey]; ok {
				place(parent)
			}
		}
		placed[unit.Key] = true
		ordered = append(ordered, unit)
	}
	for _, unit := range units {
		place(unit)
	}
	return ordered
}

func (i *Importer) upsertUnits(
	ctx context.Context, tx pgx.Tx, m *manifest.Manifest, plan *Plan, options Options, state *resolved,
) error {
	for _, unit := range unitsInDependencyOrder(m.Units) {
		var parentID *string
		if unit.ParentKey != "" {
			resolvedParent, ok := state.units[unit.ParentKey]
			if !ok {
				return fmt.Errorf("unit %s references parent %s that was not imported", unit.Key, unit.ParentKey)
			}
			parentID = &resolvedParent
		}

		var (
			id                   string
			nameAr, nameEn, kind string
			existingParent       *string
			existingInstitution  string
		)
		err := tx.QueryRow(ctx, `
			SELECT id::text, institution_id::text, parent_unit_id::text, kind, name_ar, name_en
			FROM academic_units WHERE institution_id = $1::uuid AND slug = $2 AND retired_at IS NULL`,
			state.institutionID, unit.Slug).
			Scan(&id, &existingInstitution, &existingParent, &kind, &nameAr, &nameEn)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			plan.add("academic_unit", unit.Key, ActionCreate, unit.Slug)
			if err := tx.QueryRow(ctx, `
				INSERT INTO academic_units (institution_id, parent_unit_id, kind, slug, name_ar, name_en)
				VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6) RETURNING id::text`,
				state.institutionID, parentID, unit.Kind, unit.Slug, unit.NameAr, unit.NameEn).Scan(&id); err != nil {
				return fmt.Errorf("creating academic unit %s: %w", unit.Key, err)
			}
			state.units[unit.Key] = id
			if err := i.audit(ctx, tx, options, "ACADEMIC_UNIT_CREATED", "ACADEMIC_UNIT", id,
				"Academic Unit created by catalog import", map[string]any{
					"manifest_id": m.ID, "slug": unit.Slug, "kind": unit.Kind,
					"institution_id": state.institutionID,
				}); err != nil {
				return err
			}
			continue
		case err != nil:
			return fmt.Errorf("loading academic unit %s: %w", unit.Slug, err)
		}

		state.units[unit.Key] = id
		// Re-parenting an existing unit through a manifest edit is an
		// identity-shaped change: it silently moves every Program and Subject
		// beneath it. Fail closed.
		if !samePointer(existingParent, parentID) {
			return fmt.Errorf("%w: academic unit %s would be re-parented by import; resolve this explicitly",
				ErrIdentityRebind, unit.Slug)
		}
		if nameAr == unit.NameAr && nameEn == unit.NameEn && kind == unit.Kind {
			plan.add("academic_unit", unit.Key, ActionNoop, unit.Slug)
			continue
		}
		plan.add("academic_unit", unit.Key, ActionUpdate, unit.Slug)
		if _, err := tx.Exec(ctx, `
			UPDATE academic_units SET name_ar = $1, name_en = $2, kind = $3, updated_at = now()
			WHERE id = $4::uuid`, unit.NameAr, unit.NameEn, unit.Kind, id); err != nil {
			return fmt.Errorf("updating academic unit %s: %w", unit.Key, err)
		}
		if err := i.audit(ctx, tx, options, "ACADEMIC_UNIT_UPDATED", "ACADEMIC_UNIT", id,
			"Academic Unit updated by catalog import", map[string]any{
				"manifest_id": m.ID, "slug": unit.Slug,
			}); err != nil {
			return err
		}
	}
	return nil
}

func samePointer(a, b *string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func (i *Importer) upsertPrograms(
	ctx context.Context, tx pgx.Tx, m *manifest.Manifest, plan *Plan, options Options, state *resolved,
) error {
	for _, program := range m.Programs {
		var owningUnit *string
		if program.OwningUnit != "" {
			unitID, ok := state.units[program.OwningUnit]
			if !ok {
				return fmt.Errorf("program %s references unit %s that was not imported", program.Key, program.OwningUnit)
			}
			owningUnit = &unitID
		}

		var (
			id                     string
			nameAr, nameEn, degree string
			existingUnit           *string
		)
		err := tx.QueryRow(ctx, `
			SELECT id::text, owning_unit_id::text, name_ar, name_en, degree_kind
			FROM programs WHERE institution_id = $1::uuid AND slug = $2 AND retired_at IS NULL`,
			state.institutionID, program.Slug).Scan(&id, &existingUnit, &nameAr, &nameEn, &degree)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			plan.add("program", program.Key, ActionCreate, program.Slug)
			if err := tx.QueryRow(ctx, `
				INSERT INTO programs (institution_id, owning_unit_id, slug, name_ar, name_en, degree_kind)
				VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6) RETURNING id::text`,
				state.institutionID, owningUnit, program.Slug, program.NameAr,
				program.NameEn, program.DegreeKind).Scan(&id); err != nil {
				return fmt.Errorf("creating program %s: %w", program.Key, err)
			}
			state.programs[program.Key] = id
			if err := i.audit(ctx, tx, options, "ACADEMIC_PROGRAM_CREATED", "ACADEMIC_PROGRAM", id,
				"Academic Program created by catalog import", map[string]any{
					"manifest_id": m.ID, "slug": program.Slug, "degree_kind": program.DegreeKind,
					"institution_id": state.institutionID,
				}); err != nil {
				return err
			}
			continue
		case err != nil:
			return fmt.Errorf("loading program %s: %w", program.Slug, err)
		}

		state.programs[program.Key] = id
		if !samePointer(existingUnit, owningUnit) {
			return fmt.Errorf("%w: program %s would move to a different owning unit; resolve this explicitly",
				ErrIdentityRebind, program.Slug)
		}
		if nameAr == program.NameAr && nameEn == program.NameEn && degree == program.DegreeKind {
			plan.add("program", program.Key, ActionNoop, program.Slug)
			continue
		}
		plan.add("program", program.Key, ActionUpdate, program.Slug)
		if _, err := tx.Exec(ctx, `
			UPDATE programs SET name_ar = $1, name_en = $2, degree_kind = $3, updated_at = now()
			WHERE id = $4::uuid`, program.NameAr, program.NameEn, program.DegreeKind, id); err != nil {
			return fmt.Errorf("updating program %s: %w", program.Key, err)
		}
		if err := i.audit(ctx, tx, options, "ACADEMIC_PROGRAM_UPDATED", "ACADEMIC_PROGRAM", id,
			"Academic Program updated by catalog import", map[string]any{
				"manifest_id": m.ID, "slug": program.Slug,
			}); err != nil {
			return err
		}
	}
	return nil
}

func (i *Importer) upsertCurricula(
	ctx context.Context, tx pgx.Tx, m *manifest.Manifest, plan *Plan, options Options, state *resolved,
) error {
	for _, curriculum := range m.Curricula {
		programID, ok := state.programs[curriculum.ProgramKey]
		if !ok {
			return fmt.Errorf("curriculum %s references program %s that was not imported",
				curriculum.Key, curriculum.ProgramKey)
		}

		var (
			id            string
			effectiveYear *int
			status        string
		)
		err := tx.QueryRow(ctx, `
			SELECT id::text, effective_from_year, status::text FROM curricula
			WHERE program_id = $1::uuid AND version_label = $2`,
			programID, curriculum.VersionLabel).Scan(&id, &effectiveYear, &status)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			plan.add("curriculum", curriculum.Key, ActionCreate, curriculum.VersionLabel)
			if err := tx.QueryRow(ctx, `
				INSERT INTO curricula (program_id, institution_id, version_label, effective_from_year, status)
				VALUES ($1::uuid, $2::uuid, $3, $4, 'ACTIVE') RETURNING id::text`,
				programID, state.institutionID, curriculum.VersionLabel,
				curriculum.EffectiveFromYear).Scan(&id); err != nil {
				return fmt.Errorf("creating curriculum %s: %w", curriculum.Key, err)
			}
			state.curricula[curriculum.Key] = id
			if err := i.audit(ctx, tx, options, "ACADEMIC_CURRICULUM_CREATED", "ACADEMIC_CURRICULUM", id,
				"Academic Curriculum created by catalog import", map[string]any{
					"manifest_id": m.ID, "version_label": curriculum.VersionLabel,
					"program_id": programID, "institution_id": state.institutionID,
				}); err != nil {
				return err
			}
			continue
		case err != nil:
			return fmt.Errorf("loading curriculum %s: %w", curriculum.Key, err)
		}

		state.curricula[curriculum.Key] = id
		if samePointerInt(effectiveYear, curriculum.EffectiveFromYear) {
			plan.add("curriculum", curriculum.Key, ActionNoop, curriculum.VersionLabel)
			continue
		}
		plan.add("curriculum", curriculum.Key, ActionUpdate, curriculum.VersionLabel)
		if _, err := tx.Exec(ctx, `
			UPDATE curricula SET effective_from_year = $1, updated_at = now() WHERE id = $2::uuid`,
			curriculum.EffectiveFromYear, id); err != nil {
			return fmt.Errorf("updating curriculum %s: %w", curriculum.Key, err)
		}
		if err := i.audit(ctx, tx, options, "ACADEMIC_CURRICULUM_UPDATED", "ACADEMIC_CURRICULUM", id,
			"Academic Curriculum updated by catalog import", map[string]any{
				"manifest_id": m.ID, "version_label": curriculum.VersionLabel,
			}); err != nil {
			return err
		}
	}
	return nil
}

func samePointerInt(a, b *int) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func (i *Importer) upsertSubjects(
	ctx context.Context, tx pgx.Tx, m *manifest.Manifest, plan *Plan, options Options, state *resolved,
) error {
	for _, subject := range m.Subjects {
		var owningUnit *string
		if subject.OwningUnit != "" {
			unitID, ok := state.units[subject.OwningUnit]
			if !ok {
				return fmt.Errorf("subject %s references unit %s that was not imported", subject.Key, subject.OwningUnit)
			}
			owningUnit = &unitID
		}
		code := strings.TrimSpace(subject.OfficialCode)

		// Identity lookup mirrors the T1 indexes exactly: normalized code where a
		// code exists, normalized title otherwise. This is what makes a repeated
		// import a no-op rather than a duplicate.
		var (
			id               string
			existingCode     *string
			titleAr, titleEn string
			existingUnit     *string
		)
		var err error
		if code != "" {
			err = tx.QueryRow(ctx, `
				SELECT id::text, official_code, title_ar, title_en, owning_unit_id::text FROM subjects
				WHERE institution_id = $1::uuid AND code_normalized = academic_normalize_code($2)
				  AND retired_at IS NULL`,
				state.institutionID, code).Scan(&id, &existingCode, &titleAr, &titleEn, &existingUnit)
		} else {
			err = tx.QueryRow(ctx, `
				SELECT id::text, official_code, title_ar, title_en, owning_unit_id::text FROM subjects
				WHERE institution_id = $1::uuid AND code_normalized IS NULL AND retired_at IS NULL
				  AND (title_ar_normalized = catalog_normalize_ar($2) OR title_en_normalized = catalog_normalize_ar($3))`,
				state.institutionID, subject.TitleAr, subject.TitleEn).
				Scan(&id, &existingCode, &titleAr, &titleEn, &existingUnit)
		}

		var codeArg *string
		if code != "" {
			codeArg = &code
		}

		switch {
		case errors.Is(err, pgx.ErrNoRows):
			plan.add("subject", subject.Key, ActionCreate, code)
			if err := tx.QueryRow(ctx, `
				INSERT INTO subjects (institution_id, owning_unit_id, official_code, title_ar, title_en)
				VALUES ($1::uuid, $2::uuid, $3, $4, $5) RETURNING id::text`,
				state.institutionID, owningUnit, codeArg, subject.TitleAr, subject.TitleEn).Scan(&id); err != nil {
				return fmt.Errorf("creating subject %s: %w", subject.Key, err)
			}
			state.subjects[subject.Key] = id
			if err := i.audit(ctx, tx, options, "ACADEMIC_SUBJECT_CREATED", "ACADEMIC_SUBJECT", id,
				"Academic Subject created by catalog import", map[string]any{
					"manifest_id": m.ID, "code": code, "institution_id": state.institutionID,
					// Recorded so an investigator can tell an official title from a
					// Gradex-supplied translation without opening the manifest.
					"title_ar_source": string(subject.TitleArSource),
				}); err != nil {
				return err
			}
			continue
		case err != nil:
			return fmt.Errorf("loading subject %s: %w", subject.Key, err)
		}

		state.subjects[subject.Key] = id
		if !samePointer(existingUnit, owningUnit) {
			return fmt.Errorf("%w: subject %s would move to a different owning unit; resolve this explicitly",
				ErrIdentityRebind, subject.Key)
		}
		// Re-formatting the official code is allowed while normalized identity
		// holds; changing it to a different canonical code is not, and the
		// identity lookup above already guarantees that.
		if titleAr == subject.TitleAr && titleEn == subject.TitleEn && sameCode(existingCode, codeArg) {
			plan.add("subject", subject.Key, ActionNoop, code)
			continue
		}
		plan.add("subject", subject.Key, ActionUpdate, code)
		if _, err := tx.Exec(ctx, `
			UPDATE subjects SET title_ar = $1, title_en = $2, official_code = $3, updated_at = now()
			WHERE id = $4::uuid`, subject.TitleAr, subject.TitleEn, codeArg, id); err != nil {
			return fmt.Errorf("updating subject %s: %w", subject.Key, err)
		}
		if err := i.audit(ctx, tx, options, "ACADEMIC_SUBJECT_UPDATED", "ACADEMIC_SUBJECT", id,
			"Academic Subject updated by catalog import", map[string]any{
				"manifest_id": m.ID, "code": code, "title_ar_source": string(subject.TitleArSource),
			}); err != nil {
			return err
		}
	}
	return nil
}

func sameCode(a, b *string) bool { return samePointer(a, b) }

func (i *Importer) upsertMappings(
	ctx context.Context, tx pgx.Tx, m *manifest.Manifest, plan *Plan, options Options, state *resolved,
) error {
	for _, mapping := range m.Mappings {
		curriculumID, ok := state.curricula[mapping.CurriculumKey]
		if !ok {
			return fmt.Errorf("mapping references curriculum %s that was not imported", mapping.CurriculumKey)
		}
		subjectID, ok := state.subjects[mapping.SubjectKey]
		if !ok {
			return fmt.Errorf("mapping references subject %s that was not imported", mapping.SubjectKey)
		}
		key := mapping.CurriculumKey + "/" + mapping.SubjectKey

		var (
			id              string
			requirement     string
			level, semester *int
			credits         *float64
		)
		err := tx.QueryRow(ctx, `
			SELECT id::text, requirement_kind::text, recommended_level, recommended_semester, credits
			FROM curriculum_subjects WHERE curriculum_id = $1::uuid AND subject_id = $2::uuid`,
			curriculumID, subjectID).Scan(&id, &requirement, &level, &semester, &credits)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			plan.add("curriculum_subject", key, ActionCreate, mapping.Requirement)
			if err := tx.QueryRow(ctx, `
				INSERT INTO curriculum_subjects (
					curriculum_id, subject_id, institution_id, requirement_kind,
					recommended_level, recommended_semester, credits)
				VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7) RETURNING id::text`,
				curriculumID, subjectID, state.institutionID, mapping.Requirement,
				mapping.Level, mapping.Semester, mapping.Credits).Scan(&id); err != nil {
				return fmt.Errorf("mapping subject %s into curriculum %s: %w",
					mapping.SubjectKey, mapping.CurriculumKey, err)
			}
			if err := i.audit(ctx, tx, options, "ACADEMIC_CURRICULUM_SUBJECT_MAPPED", "ACADEMIC_CURRICULUM",
				curriculumID, "Subject mapped into Curriculum by catalog import", map[string]any{
					"manifest_id": m.ID, "subject_id": subjectID,
					"requirement_kind": mapping.Requirement, "institution_id": state.institutionID,
				}); err != nil {
				return err
			}
			continue
		case err != nil:
			return fmt.Errorf("loading mapping %s: %w", key, err)
		}

		if requirement == mapping.Requirement && samePointerInt(level, mapping.Level) &&
			samePointerInt(semester, mapping.Semester) && sameCredits(credits, mapping.Credits) {
			plan.add("curriculum_subject", key, ActionNoop, mapping.Requirement)
			continue
		}
		plan.add("curriculum_subject", key, ActionUpdate, mapping.Requirement)
		if _, err := tx.Exec(ctx, `
			UPDATE curriculum_subjects SET requirement_kind = $1, recommended_level = $2,
				recommended_semester = $3, credits = $4, updated_at = now() WHERE id = $5::uuid`,
			mapping.Requirement, mapping.Level, mapping.Semester, mapping.Credits, id); err != nil {
			return fmt.Errorf("updating mapping %s: %w", key, err)
		}
		if err := i.audit(ctx, tx, options, "ACADEMIC_CURRICULUM_SUBJECT_MAPPED", "ACADEMIC_CURRICULUM",
			curriculumID, "Curriculum mapping updated by catalog import", map[string]any{
				"manifest_id": m.ID, "subject_id": subjectID, "requirement_kind": mapping.Requirement,
			}); err != nil {
			return err
		}
	}
	return nil
}

func sameCredits(a, b *float64) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

// reportDrift lists catalog rows the manifest does not describe. Nothing is
// retired or deleted: absence from a manifest is not an instruction to remove
// real academic data, and this format expresses no retirement intent.
func (i *Importer) reportDrift(
	ctx context.Context, tx pgx.Tx, m *manifest.Manifest, plan *Plan, state *resolved,
) error {
	known := map[string]bool{}
	for _, unit := range m.Units {
		known["unit:"+unit.Slug] = true
	}
	for _, program := range m.Programs {
		known["program:"+program.Slug] = true
	}
	for _, subject := range m.Subjects {
		if code := strings.TrimSpace(subject.OfficialCode); code != "" {
			known["subject:"+academic.NormalizeCode(code)] = true
		}
	}

	drift := []string{}
	rows, err := tx.Query(ctx, `
		SELECT 'unit:' || slug FROM academic_units WHERE institution_id = $1::uuid AND retired_at IS NULL
		UNION ALL
		SELECT 'program:' || slug FROM programs WHERE institution_id = $1::uuid AND retired_at IS NULL
		UNION ALL
		SELECT 'subject:' || code_normalized FROM subjects
		WHERE institution_id = $1::uuid AND retired_at IS NULL AND code_normalized IS NOT NULL`,
		state.institutionID)
	if err != nil {
		return fmt.Errorf("reporting drift: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var identifier string
		if err := rows.Scan(&identifier); err != nil {
			return fmt.Errorf("scanning drift row: %w", err)
		}
		if !known[identifier] {
			drift = append(drift, identifier)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	sort.Strings(drift)
	for _, identifier := range drift {
		plan.add("drift", identifier, ActionDrift,
			"present in the database, absent from the manifest; retained")
	}
	return nil
}
