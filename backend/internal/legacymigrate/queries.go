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

func countAlreadyAcademic(ctx context.Context, tx pgx.Tx, institutionID string) (int, error) {
	var count int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM courses
		WHERE classification_model = 'ACADEMIC_CATALOG' AND institution_id = $1::uuid`,
		institutionID).Scan(&count); err != nil {
		return 0, fmt.Errorf("counting migrated courses: %w", err)
	}
	return count, nil
}

// loadLegacyCourses is the workset: every Course still on the legacy
// classification, with the distinct legacy Subject codes and Major labels its
// revisions carry.
//
// Aggregating across ALL revisions is deliberate. A Course whose revisions
// disagree about their legacy Subject has no single identity to migrate to, and
// the planner must be able to see that rather than silently taking the live one.
func loadLegacyCourses(ctx context.Context, tx pgx.Tx) ([]legacyCourse, error) {
	rows, err := tx.Query(ctx, `
		SELECT c.id::text,
		       COALESCE(live.title_en, latest.title_en, '') AS title_en,
		       COALESCE(array_agg(DISTINCT subject_term.academic_code)
		                FILTER (WHERE subject_term.academic_code IS NOT NULL), '{}') AS subject_codes,
		       COALESCE(array_agg(DISTINCT major_term.label_en)
		                FILTER (WHERE major_term.label_en IS NOT NULL), '{}') AS major_labels,
		       bool_or(r.subject_term_id IS NOT NULL) AS has_subject
		FROM courses c
		JOIN course_revisions r ON r.course_id = c.id
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
			&course.subjectCodes, &course.majorLabels, &course.hasSubject); err != nil {
			return nil, fmt.Errorf("scanning legacy course: %w", err)
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
