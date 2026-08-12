package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	ErrInvalidPrice = errors.New("price must be non-negative")
)

type PriceChange struct {
	ID                 string    `json:"id"`
	CourseID           string    `json:"course_id"`
	SectionID          *string   `json:"section_id,omitempty"`
	OldValueMinorUnits *int64    `json:"old_value_minor_units"`
	NewValueMinorUnits int64     `json:"new_value_minor_units"`
	ChangedByAccountID string    `json:"changed_by_account_id"`
	Reason             string    `json:"reason"`
	ChangedAt          time.Time `json:"changed_at"`
}

type SetCoursePriceRequest struct {
	CourseID        string
	AdminAccountID  string
	ActorDescriptor string
	PriceMinorUnits int64
	Reason          string
}

type SetSectionPriceRequest struct {
	CourseID          string
	SectionIdentityID string
	AdminAccountID    string
	ActorDescriptor   string
	PriceMinorUnits   int64
	Reason            string
}

// SetCoursePrice appends a course price change record atomically with audit logging inside a transaction.
func (r *Repository) SetCoursePrice(ctx context.Context, req SetCoursePriceRequest) (*PriceChange, error) {
	if req.CourseID == "" {
		return nil, ErrCourseNotFound
	}
	if req.AdminAccountID == "" {
		return nil, errors.New("admin account ID is required")
	}
	if req.PriceMinorUnits < 0 {
		return nil, ErrInvalidPrice
	}
	if strings.TrimSpace(req.Reason) == "" {
		return nil, ErrReasonRequired
	}

	var pc PriceChange
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		courseRow, err := r.LockCourse(ctx, tx, req.CourseID)
		if err != nil {
			return err
		}

		var oldVal *int64
		err = tx.QueryRow(ctx, `
			SELECT new_value_minor_units
			FROM course_price_changes
			WHERE course_id = $1::uuid AND section_id IS NULL
			ORDER BY changed_at DESC, id DESC
			LIMIT 1
		`, courseRow.ID).Scan(&oldVal)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("querying latest course price: %w", err)
		}

		err = tx.QueryRow(ctx, `
			INSERT INTO course_price_changes (
				course_id, section_id, old_value_minor_units, new_value_minor_units,
				changed_by_account_id, reason, changed_at
			) VALUES (
				$1::uuid, NULL, $2, $3,
				$4::uuid, $5,
				GREATEST(
					clock_timestamp(),
					COALESCE(
						(SELECT MAX(changed_at) + interval '1 microsecond'
						 FROM course_price_changes
						 WHERE course_id = $1::uuid AND section_id IS NULL),
						clock_timestamp()
					)
				)
			)
			RETURNING id, course_id, section_id, old_value_minor_units, new_value_minor_units,
			          changed_by_account_id, reason, changed_at
		`, courseRow.ID, oldVal, req.PriceMinorUnits, req.AdminAccountID, req.Reason).Scan(
			&pc.ID, &pc.CourseID, &pc.SectionID, &pc.OldValueMinorUnits, &pc.NewValueMinorUnits,
			&pc.ChangedByAccountID, &pc.Reason, &pc.ChangedAt,
		)
		if err != nil {
			return fmt.Errorf("inserting course price change: %w", err)
		}

		actorID := req.AdminAccountID
		if err := WriteAuditEvent(ctx, tx, AuditEvent{
			ActorAccountID:  &actorID,
			ActorRole:       "ADMIN",
			ActorDescriptor: req.ActorDescriptor,
			Action:          "COURSE_PRICE_CHANGED",
			TargetType:      "COURSE",
			TargetID:        courseRow.ID,
			Reason:          req.Reason,
			Metadata: map[string]any{
				"old_value_minor_units": oldVal,
				"new_value_minor_units": req.PriceMinorUnits,
			},
		}); err != nil {
			return fmt.Errorf("writing price change audit event: %w", err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return &pc, nil
}

// courseHasLaunchPrice reports whether an Admin has set a Course-level catalog
// price. The current price is the newest `course_price_changes` row with a NULL
// section_id, exactly as the public read model resolves it, so the existence of
// any such row is the existence of a price. A Section price is never a
// substitute: Section is not an acquirable scope and its price is not displayed
// (D-045 resolved question 2).
//
// Amount is deliberately not constrained beyond the authoritative rule that a
// price is a non-negative integer amount in fils (ErrInvalidPrice, BR-019). No
// repository authority establishes a positive minimum, so zero is a price.
//
// The caller must already hold the course row lock. SetCoursePrice takes the
// same lock before appending, so price writes and approval serialize.
func courseHasLaunchPrice(ctx context.Context, tx pgx.Tx, courseID string) (bool, error) {
	var priced bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM course_price_changes
			WHERE course_id = $1::uuid AND section_id IS NULL
		)
	`, courseID).Scan(&priced)
	if err != nil {
		return false, fmt.Errorf("checking course launch price: %w", err)
	}
	return priced, nil
}

// SetSectionPrice appends a stable section price change record atomically with audit logging inside a transaction.
func (r *Repository) SetSectionPrice(ctx context.Context, req SetSectionPriceRequest) (*PriceChange, error) {
	if req.CourseID == "" || req.SectionIdentityID == "" {
		return nil, ErrCourseNotFound
	}
	if req.AdminAccountID == "" {
		return nil, errors.New("admin account ID is required")
	}
	if req.PriceMinorUnits < 0 {
		return nil, ErrInvalidPrice
	}
	if strings.TrimSpace(req.Reason) == "" {
		return nil, ErrReasonRequired
	}

	var pc PriceChange
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		courseRow, err := r.LockCourse(ctx, tx, req.CourseID)
		if err != nil {
			return err
		}

		var sectionExists bool
		err = tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM course_section_identities
				WHERE id = $1::uuid AND course_id = $2::uuid
			)
		`, req.SectionIdentityID, courseRow.ID).Scan(&sectionExists)
		if err != nil {
			return fmt.Errorf("verifying section membership: %w", err)
		}
		if !sectionExists {
			return ErrCourseNotFound
		}

		var oldVal *int64
		err = tx.QueryRow(ctx, `
			SELECT new_value_minor_units
			FROM course_price_changes
			WHERE course_id = $1::uuid AND section_id = $2::uuid
			ORDER BY changed_at DESC, id DESC
			LIMIT 1
		`, courseRow.ID, req.SectionIdentityID).Scan(&oldVal)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("querying latest section price: %w", err)
		}

		err = tx.QueryRow(ctx, `
			INSERT INTO course_price_changes (
				course_id, section_id, old_value_minor_units, new_value_minor_units,
				changed_by_account_id, reason, changed_at
			) VALUES (
				$1::uuid, $2::uuid, $3, $4,
				$5::uuid, $6,
				GREATEST(
					clock_timestamp(),
					COALESCE(
						(SELECT MAX(changed_at) + interval '1 microsecond'
						 FROM course_price_changes
						 WHERE course_id = $1::uuid AND section_id = $2::uuid),
						clock_timestamp()
					)
				)
			)
			RETURNING id, course_id, section_id, old_value_minor_units, new_value_minor_units,
			          changed_by_account_id, reason, changed_at
		`, courseRow.ID, req.SectionIdentityID, oldVal, req.PriceMinorUnits, req.AdminAccountID, req.Reason).Scan(
			&pc.ID, &pc.CourseID, &pc.SectionID, &pc.OldValueMinorUnits, &pc.NewValueMinorUnits,
			&pc.ChangedByAccountID, &pc.Reason, &pc.ChangedAt,
		)
		if err != nil {
			return fmt.Errorf("inserting section price change: %w", err)
		}

		actorID := req.AdminAccountID
		if err := WriteAuditEvent(ctx, tx, AuditEvent{
			ActorAccountID:  &actorID,
			ActorRole:       "ADMIN",
			ActorDescriptor: req.ActorDescriptor,
			Action:          "COURSE_PRICE_CHANGED",
			TargetType:      "SECTION",
			TargetID:        req.SectionIdentityID,
			Reason:          req.Reason,
			Metadata: map[string]any{
				"course_id":             courseRow.ID,
				"section_id":            req.SectionIdentityID,
				"old_value_minor_units": oldVal,
				"new_value_minor_units": req.PriceMinorUnits,
			},
		}); err != nil {
			return fmt.Errorf("writing section price change audit event: %w", err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return &pc, nil
}

// GetCoursePriceHistory returns append-only price history for a course and its sections.
func (r *Repository) GetCoursePriceHistory(ctx context.Context, courseID string) ([]PriceChange, error) {
	if courseID == "" {
		return nil, ErrCourseNotFound
	}

	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM courses WHERE id = $1::uuid)`, courseID).Scan(&exists)
	if err != nil {
		return nil, fmt.Errorf("checking course existence: %w", err)
	}
	if !exists {
		return nil, ErrCourseNotFound
	}

	rows, err := r.pool.Query(ctx, `
		SELECT id, course_id, section_id, old_value_minor_units, new_value_minor_units,
		       changed_by_account_id, reason, changed_at
		FROM course_price_changes
		WHERE course_id = $1::uuid
		ORDER BY changed_at DESC, id DESC
	`, courseID)
	if err != nil {
		return nil, fmt.Errorf("querying price history: %w", err)
	}
	defer rows.Close()

	var history []PriceChange
	for rows.Next() {
		var pc PriceChange
		if err := rows.Scan(
			&pc.ID, &pc.CourseID, &pc.SectionID, &pc.OldValueMinorUnits, &pc.NewValueMinorUnits,
			&pc.ChangedByAccountID, &pc.Reason, &pc.ChangedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning price change: %w", err)
		}
		history = append(history, pc)
	}
	if history == nil {
		history = []PriceChange{}
	}
	return history, rows.Err()
}
