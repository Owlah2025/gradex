package legacymigrate

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type subjectRow struct {
	id      string
	retired bool
}

func resolveInstitution(ctx context.Context, tx pgx.Tx, slug string) (string, error) {
	var id string
	err := tx.QueryRow(ctx,
		`SELECT id::text FROM institutions WHERE slug = $1 AND retired_at IS NULL`, slug).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("mapping institution %q is not in the Academic Catalog", slug)
	}
	if err != nil {
		return "", fmt.Errorf("resolving mapping institution: %w", err)
	}
	return id, nil
}

// loadSubjects keys by normalized code, which is the canonical identity a code
// carries (D-093 §7). Retired Subjects are loaded rather than filtered so the
// planner can say "retired" instead of "missing".
func loadSubjects(ctx context.Context, tx pgx.Tx, institutionID string) (map[string]subjectRow, error) {
	rows, err := tx.Query(ctx, `
		SELECT code_normalized, id::text, retired_at IS NOT NULL
		FROM subjects
		WHERE institution_id = $1::uuid AND code_normalized IS NOT NULL`, institutionID)
	if err != nil {
		return nil, fmt.Errorf("loading canonical subjects: %w", err)
	}
	defer rows.Close()
	out := map[string]subjectRow{}
	for rows.Next() {
		var code string
		var row subjectRow
		if err := rows.Scan(&code, &row.id, &row.retired); err != nil {
			return nil, fmt.Errorf("scanning canonical subject: %w", err)
		}
		out[code] = row
	}
	return out, rows.Err()
}

func loadPrograms(ctx context.Context, tx pgx.Tx, institutionID string) (map[string]string, error) {
	rows, err := tx.Query(ctx, `
		SELECT slug, id::text FROM programs
		WHERE institution_id = $1::uuid AND retired_at IS NULL`, institutionID)
	if err != nil {
		return nil, fmt.Errorf("loading programs: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var slug, id string
		if err := rows.Scan(&slug, &id); err != nil {
			return nil, fmt.Errorf("scanning program: %w", err)
		}
		out[slug] = id
	}
	return out, rows.Err()
}

// loadLegacyCourses is the workset: EVERY Course still on the legacy
// classification, with the distinct legacy Subject codes and Major labels its
// revisions carry.
//
// Aggregating across ALL revisions is deliberate. A Course whose revisions
// disagree about their legacy Subject has no single identity to migrate to, and
// the planner must be able to see that rather than silently taking the live one.
//
// The join to course_revisions is a LEFT JOIN, and that is the entire point of
// this comment. It was an INNER JOIN, which meant a Course carrying no revision
// at all did not merely fail to migrate — it vanished from the report, so the
// summary counted a corpus smaller than the corpus. A migration tool that
// cannot see a record cannot be trusted to say the migration is complete. With
// the LEFT JOIN such a Course produces one all-NULL revision row, has_revision
// is false, and the planner classifies it NO_REVISION.
func loadLegacyCourses(ctx context.Context, tx pgx.Tx) ([]legacyCourse, error) {
	rows, err := tx.Query(ctx, `
		SELECT c.id::text,
		       COALESCE(live.title_en, latest.title_en, '') AS title_en,
		       COALESCE(array_agg(DISTINCT subject_term.academic_code)
		                FILTER (WHERE subject_term.academic_code IS NOT NULL), '{}') AS subject_codes,
		       COALESCE(array_agg(DISTINCT subject_term.label_en)
		                FILTER (WHERE subject_term.label_en IS NOT NULL), '{}') AS subject_labels,
		       COALESCE(array_agg(DISTINCT major_term.label_en)
		                FILTER (WHERE major_term.label_en IS NOT NULL), '{}') AS major_labels,
		       COALESCE(bool_or(r.subject_term_id IS NOT NULL), FALSE) AS has_subject,
		       COALESCE(bool_or(r.id IS NOT NULL), FALSE) AS has_revision
		FROM courses c
		LEFT JOIN course_revisions r ON r.course_id = c.id
		LEFT JOIN taxonomy_terms subject_term ON subject_term.id = r.subject_term_id
		LEFT JOIN taxonomy_terms major_term ON major_term.id = r.major_term_id
		LEFT JOIN course_revisions live ON live.id = c.live_revision_id
		LEFT JOIN LATERAL (
			SELECT title_en FROM course_revisions
			WHERE course_id = c.id ORDER BY revision_number DESC LIMIT 1
		) latest ON TRUE
		WHERE c.classification_model = 'LEGACY_TAXONOMY'
		GROUP BY c.id, live.title_en, latest.title_en
		ORDER BY c.id`)
	if err != nil {
		return nil, fmt.Errorf("loading legacy courses: %w", err)
	}
	defer rows.Close()

	var out []legacyCourse
	for rows.Next() {
		var course legacyCourse
		if err := rows.Scan(&course.id, &course.titleEn,
			&course.subjectCodes, &course.subjectLabels, &course.majorLabels,
			&course.hasSubject, &course.hasRevision); err != nil {
			return nil, fmt.Errorf("scanning legacy course: %w", err)
		}
		out = append(out, course)
	}
	return out, rows.Err()
}

// academicCourse is one Course a previous run (or ordinary Instructor authoring
// since T4-B) already placed on the Academic Catalog.
type academicCourse struct {
	id           string
	titleEn      string
	subjectCode  string
	subjectTitle string
	// legacySubjectCodes are the legacy codes the Course's revisions still
	// carry. The cutover never clears them, so they remain available for drift
	// detection: a mapping that now points somewhere else than where the Course
	// actually landed is a fact the Founder must see, not a write to perform.
	legacySubjectCodes []string
}

// loadAlreadyAcademic replaces a bare count with the real rows.
//
// A count told a rerun "5 already academic" without saying which five, so the
// report could not be diffed against the corpus and drift was invisible. Every
// Course now appears in the report by id, exactly like a legacy one.
func loadAlreadyAcademic(ctx context.Context, tx pgx.Tx, institutionID string) ([]academicCourse, error) {
	rows, err := tx.Query(ctx, `
		SELECT c.id::text,
		       COALESCE(live.title_en, latest.title_en, '') AS title_en,
		       COALESCE(s.official_code, '') AS subject_code,
		       COALESCE(s.title_en, '') AS subject_title,
		       COALESCE(array_agg(DISTINCT subject_term.academic_code)
		                FILTER (WHERE subject_term.academic_code IS NOT NULL), '{}') AS legacy_subject_codes
		FROM courses c
		LEFT JOIN subjects s ON s.id = c.subject_id
		LEFT JOIN course_revisions r ON r.course_id = c.id
		LEFT JOIN taxonomy_terms subject_term ON subject_term.id = r.subject_term_id
		LEFT JOIN course_revisions live ON live.id = c.live_revision_id
		LEFT JOIN LATERAL (
			SELECT title_en FROM course_revisions
			WHERE course_id = c.id ORDER BY revision_number DESC LIMIT 1
		) latest ON TRUE
		WHERE c.classification_model = 'ACADEMIC_CATALOG' AND c.institution_id = $1::uuid
		GROUP BY c.id, live.title_en, latest.title_en, s.official_code, s.title_en
		ORDER BY c.id`, institutionID)
	if err != nil {
		return nil, fmt.Errorf("loading migrated courses: %w", err)
	}
	defer rows.Close()
	var out []academicCourse
	for rows.Next() {
		var course academicCourse
		if err := rows.Scan(&course.id, &course.titleEn, &course.subjectCode,
			&course.subjectTitle, &course.legacySubjectCodes); err != nil {
			return nil, fmt.Errorf("scanning migrated course: %w", err)
		}
		out = append(out, course)
	}
	return out, rows.Err()
}

// migrateCourse performs the cutover for one Course.
//
// Classification, Institution, and Subject move in ONE statement. That is
// required, not stylistic: 0025's CHECK constraints forbid a legacy Course from
// holding academic identity and forbid an Academic Course from lacking an
// Institution, so any partial write is refused by the database. The Course row
// is never replaced and its id never changes, so every entitlement, invitation,
// grant, price, section, lesson, and progress row that references it is
// untouched.
//
// The post-publication Subject trigger permits this: it fires only when the
// Course ALREADY has a Subject, and a legacy Course by definition has none. A
// published legacy Course therefore receives its first canonical Subject and is
// immutable from that moment on.
func migrateCourse(
	ctx context.Context,
	tx pgx.Tx,
	course legacyCourse,
	institutionID, subjectID string,
	programIDs []string,
	actorDescriptor string,
) error {
	command, err := tx.Exec(ctx, `
		UPDATE courses
		SET classification_model = 'ACADEMIC_CATALOG',
		    institution_id = $1::uuid,
		    subject_id = $2::uuid,
		    updated_at = now()
		WHERE id = $3::uuid
		  AND classification_model = 'LEGACY_TAXONOMY'`,
		institutionID, subjectID, course.id)
	if err != nil {
		return err
	}
	// Compare-and-set: a Course another process moved between the plan and the
	// apply is left alone rather than written over.
	if command.RowsAffected() != 1 {
		return fmt.Errorf("course %s left the legacy workset during the run", course.id)
	}

	// Audience targets attach to the Course's CURRENT live or editable revision
	// only. Historical superseded revisions keep the audience they published
	// with, which is what makes revision history readable after the cutover.
	if len(programIDs) > 0 {
		var revisionID string
		err := tx.QueryRow(ctx, `
			SELECT COALESCE(c.live_revision_id, (
				SELECT id FROM course_revisions
				WHERE course_id = c.id AND state IN ('DRAFT', 'CHANGES_REQUESTED', 'PENDING_REVIEW')
				ORDER BY revision_number DESC LIMIT 1
			))::text
			FROM courses c WHERE c.id = $1::uuid`, course.id).Scan(&revisionID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("resolving target revision: %w", err)
		}
		if revisionID != "" {
			for _, programID := range programIDs {
				if _, err := tx.Exec(ctx, `
					INSERT INTO course_program_targets (revision_id, course_id, program_id, institution_id)
					VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid)
					ON CONFLICT (revision_id, program_id) DO NOTHING`,
					revisionID, course.id, programID, institutionID); err != nil {
					return fmt.Errorf("attaching audience target: %w", err)
				}
			}
		}
	}

	return writeMigrationAudit(ctx, tx, course.id, subjectID, institutionID, actorDescriptor)
}

func writeMigrationAudit(
	ctx context.Context, tx pgx.Tx, courseID, subjectID, institutionID, descriptor string,
) error {
	if descriptor == "" {
		descriptor = "catalog-migrate"
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO audit_events (
			actor_role, actor_descriptor, action, module,
			target_type, target_id, reason, metadata
		) VALUES (
			'ADMIN', $1, 'COURSE_TAXONOMY_MIGRATED', 'CATALOG_AND_AUTHORING',
			'COURSE', $2, $3, $4::jsonb
		)`,
		descriptor, courseID,
		"Course migrated from the legacy taxonomy onto the Academic Catalog",
		fmt.Sprintf(`{"institution_id":%q,"subject_id":%q}`, institutionID, subjectID))
	if err != nil {
		return fmt.Errorf("writing migration audit: %w", err)
	}
	return nil
}
