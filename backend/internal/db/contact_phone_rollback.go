package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// CheckStudentContactPhoneRollbackSafety is the preflight for 58 -> 57.
//
// 0058 down refuses on its own while any Student number or request contact
// snapshot exists, but golang-migrate marks the target version dirty before
// running the SQL, so a refusal there would leave a dirty marker over an
// intact schema 58. Refusing here first keeps the marker exactly as it was.
func CheckStudentContactPhoneRollbackSafety(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return errors.New("database pool is required")
	}
	var numbers, snapshots bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM accounts WHERE contact_phone IS NOT NULL),
		       EXISTS (SELECT 1 FROM purchase_requests WHERE contact_phone IS NOT NULL)
	`).Scan(&numbers, &snapshots); err != nil {
		return fmt.Errorf("checking contact phone rollback safety: %w", err)
	}
	if numbers || snapshots {
		return errors.New("cannot roll back 0058: Student contact numbers or request contact snapshots exist; " +
			"retain schema 58, or export and clear them through an approved, audited operation first")
	}
	return nil
}
