package catalog

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5"
)

type ReorderSectionsRequest struct {
	CourseID       string
	RevisionID     string
	OwnerAccountID string
	SectionIDs     []string
}

type ReorderLessonsRequest struct {
	CourseID       string
	RevisionID     string
	SectionID      string
	OwnerAccountID string
	LessonIDs      []string
}

func validateExactOrder(authoritative, requested []string) error {
	if len(authoritative) != len(requested) {
		return ErrInvalidOrder
	}
	want := make(map[string]struct{}, len(authoritative))
	for _, id := range authoritative {
		want[id] = struct{}{}
	}
	seen := make(map[string]struct{}, len(requested))
	for _, id := range requested {
		if _, duplicate := seen[id]; duplicate {
			return ErrInvalidOrder
		}
		if _, exists := want[id]; !exists {
			return ErrInvalidOrder
		}
		seen[id] = struct{}{}
	}
	return nil
}

func lockedIdentityOrder(
	ctx context.Context,
	tx pgx.Tx,
	query string,
	args ...any,
) ([]string, int, error) {
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	ids := make([]string, 0)
	maxPosition := -1
	for rows.Next() {
		var id string
		var position int
		if err := rows.Scan(&id, &position); err != nil {
			return nil, 0, err
		}
		ids = append(ids, id)
		if position > maxPosition {
			maxPosition = position
		}
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return ids, maxPosition, nil
}

type positionRewrite struct {
	table, identityColumn, scopeColumn, scopeID string
	authoritative, requested                    []string
	maxPosition                                 int
}

func rewritePositions(ctx context.Context, tx pgx.Tx, rewrite positionRewrite) error {
	if len(rewrite.requested) == 0 {
		return nil
	}
	// Existing unique constraints are immediate. Move every row to a disjoint
	// range first, then assign the canonical zero-based order. Both statements
	// run inside the caller's transaction and the authoritative rows are locked.
	temporaryBase := int64(rewrite.maxPosition) + 1
	if temporaryBase+int64(len(rewrite.authoritative))-1 > math.MaxInt32 {
		return errors.New("position space exhausted")
	}
	temporaryQuery := fmt.Sprintf(`
		WITH ordered(id, position) AS (
			SELECT value::uuid, $1::bigint + ordinality - 1
			FROM unnest($2::text[]) WITH ORDINALITY AS input(value, ordinality)
		)
		UPDATE %s item
		SET position = ordered.position
		FROM ordered
		WHERE item.%s = ordered.id AND item.%s = $3::uuid
	`, rewrite.table, rewrite.identityColumn, rewrite.scopeColumn)
	if _, err := tx.Exec(ctx, temporaryQuery, temporaryBase, rewrite.authoritative, rewrite.scopeID); err != nil {
		return fmt.Errorf("assigning temporary positions: %w", err)
	}

	canonicalQuery := fmt.Sprintf(`
		WITH ordered(id, position) AS (
			SELECT value::uuid, ordinality - 1
			FROM unnest($1::text[]) WITH ORDINALITY AS input(value, ordinality)
		)
		UPDATE %s item
		SET position = ordered.position, updated_at = now()
		FROM ordered
		WHERE item.%s = ordered.id AND item.%s = $2::uuid
	`, rewrite.table, rewrite.identityColumn, rewrite.scopeColumn)
	if _, err := tx.Exec(ctx, canonicalQuery, rewrite.requested, rewrite.scopeID); err != nil {
		return fmt.Errorf("assigning canonical positions: %w", err)
	}
	return nil
}

func (r *Repository) ReorderSections(
	ctx context.Context,
	req ReorderSectionsRequest,
	actorDescriptor string,
) (*CourseRevision, error) {
	if req.CourseID == "" || req.RevisionID == "" || req.OwnerAccountID == "" {
		return nil, ErrCourseNotFound
	}

	var canonical *CourseRevision
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		courseRow, err := r.LockCourse(ctx, tx, req.CourseID)
		if err != nil {
			return err
		}
		if courseRow.OwnerAccountID != req.OwnerAccountID {
			return ErrCourseNotFound
		}
		if err := r.checkOwnerActive(ctx, tx, req.OwnerAccountID); err != nil {
			return err
		}
		revision, err := r.LockCandidate(ctx, tx, req.CourseID, req.RevisionID)
		if err != nil {
			return err
		}

		authoritative, maxPosition, err := lockedIdentityOrder(ctx, tx, `
			SELECT section_identity_id::text, position
			FROM course_sections
			WHERE revision_id = $1::uuid
			ORDER BY position, id
			FOR UPDATE
		`, revision.ID)
		if err != nil {
			return fmt.Errorf("locking sections for reorder: %w", err)
		}
		if err := validateExactOrder(authoritative, req.SectionIDs); err != nil {
			return err
		}
		if err := rewritePositions(ctx, tx, positionRewrite{
			table: "course_sections", identityColumn: "section_identity_id", scopeColumn: "revision_id",
			scopeID: revision.ID, authoritative: authoritative, requested: req.SectionIDs, maxPosition: maxPosition,
		}); err != nil {
			return err
		}
		if err := writeInstructorAudit(ctx, tx, instructorAuditRequest{
			accountID: req.OwnerAccountID, actorDescriptor: actorDescriptor,
			action: "SECTIONS_REORDERED", targetType: "COURSE_REVISION", targetID: revision.ID,
			reason: "Course sections reordered", metadata: map[string]any{
				"course_id": req.CourseID, "section_ids": req.SectionIDs,
			},
		}); err != nil {
			return err
		}
		canonical, err = r.loadRevisionGraphByIDTx(ctx, tx, revision.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return canonical, nil
}

func (r *Repository) ReorderLessons(
	ctx context.Context,
	req ReorderLessonsRequest,
	actorDescriptor string,
) (*CourseRevision, error) {
	if req.CourseID == "" || req.RevisionID == "" || req.SectionID == "" || req.OwnerAccountID == "" {
		return nil, ErrCourseNotFound
	}

	var canonical *CourseRevision
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		courseRow, err := r.LockCourse(ctx, tx, req.CourseID)
		if err != nil {
			return err
		}
		if courseRow.OwnerAccountID != req.OwnerAccountID {
			return ErrCourseNotFound
		}
		if err := r.checkOwnerActive(ctx, tx, req.OwnerAccountID); err != nil {
			return err
		}
		revision, err := r.LockCandidate(ctx, tx, req.CourseID, req.RevisionID)
		if err != nil {
			return err
		}

		var sectionRowID string
		err = tx.QueryRow(ctx, `
			SELECT id::text
			FROM course_sections
			WHERE revision_id = $1::uuid AND section_identity_id = $2::uuid
			FOR UPDATE
		`, revision.ID, req.SectionID).Scan(&sectionRowID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidOrder
		}
		if err != nil {
			return fmt.Errorf("locking lesson section for reorder: %w", err)
		}

		authoritative, maxPosition, err := lockedIdentityOrder(ctx, tx, `
			SELECT lesson_identity_id::text, position
			FROM course_lessons
			WHERE section_id = $1::uuid
			ORDER BY position, id
			FOR UPDATE
		`, sectionRowID)
		if err != nil {
			return fmt.Errorf("locking lessons for reorder: %w", err)
		}
		if err := validateExactOrder(authoritative, req.LessonIDs); err != nil {
			return err
		}
		if err := rewritePositions(ctx, tx, positionRewrite{
			table: "course_lessons", identityColumn: "lesson_identity_id", scopeColumn: "section_id",
			scopeID: sectionRowID, authoritative: authoritative, requested: req.LessonIDs, maxPosition: maxPosition,
		}); err != nil {
			return err
		}
		if err := writeInstructorAudit(ctx, tx, instructorAuditRequest{
			accountID: req.OwnerAccountID, actorDescriptor: actorDescriptor,
			action: "LESSONS_REORDERED", targetType: "SECTION", targetID: req.SectionID,
			reason: "Section lessons reordered", metadata: map[string]any{
				"course_id": req.CourseID, "revision_id": revision.ID, "lesson_ids": req.LessonIDs,
			},
		}); err != nil {
			return err
		}
		canonical, err = r.loadRevisionGraphByIDTx(ctx, tx, revision.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return canonical, nil
}
