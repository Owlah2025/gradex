package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DeviceCutoverCounts describes historical authority that must be gone before
// the automatic-admission API is enabled. OutstandingOTP includes expired rows
// that remain live in the action-secret chain.
type DeviceCutoverCounts struct {
	LegacySessions  int `json:"legacy_sessions"`
	PendingSessions int `json:"pending_sessions"`
	OutstandingOTP  int `json:"outstanding_otp"`
	UnexpiredOTP    int `json:"unexpired_otp"`
}

type cutoverQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func ReadDeviceCutoverCounts(ctx context.Context, pool *pgxpool.Pool) (DeviceCutoverCounts, error) {
	return readDeviceCutoverCounts(ctx, pool)
}

func readDeviceCutoverCounts(ctx context.Context, query cutoverQuerier) (DeviceCutoverCounts, error) {
	var counts DeviceCutoverCounts
	if err := query.QueryRow(ctx,
		`SELECT
		   count(*) FILTER (WHERE device_trust_state = 'LEGACY_UNBOUND'),
		   count(*) FILTER (WHERE device_trust_state = 'PENDING_DEVICE_TRUST')
		  FROM sessions WHERE state = 'ACTIVE'`,
	).Scan(&counts.LegacySessions, &counts.PendingSessions); err != nil {
		return DeviceCutoverCounts{}, fmt.Errorf("counting historical device sessions: %w", err)
	}
	if err := query.QueryRow(ctx,
		`SELECT count(*), count(*) FILTER (WHERE expires_at > clock_timestamp())
		  FROM identity_action_secrets
		  WHERE purpose = 'DEVICE_TRUST_OTP'
		    AND consumed_at IS NULL AND superseded_at IS NULL`,
	).Scan(&counts.OutstandingOTP, &counts.UnexpiredOTP); err != nil {
		return DeviceCutoverCounts{}, fmt.Errorf("counting device challenges: %w", err)
	}
	return counts, nil
}

func retireOutstandingDeviceOTPsForAccount(ctx context.Context, tx pgx.Tx, accountID string) error {
	rows, err := tx.Query(ctx,
		`SELECT id::text FROM identity_action_secrets
		  WHERE account_id = $1::uuid AND purpose = 'DEVICE_TRUST_OTP'
		    AND consumed_at IS NULL AND superseded_at IS NULL
		  ORDER BY id FOR UPDATE`, accountID)
	if err != nil {
		return fmt.Errorf("loading device challenges for recovery: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("scanning device challenge for recovery: %w", err)
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return fmt.Errorf("iterating device challenges for recovery: %w", err)
	}
	for _, id := range ids {
		if err := retireDeviceOTPByID(ctx, tx, id); err != nil {
			return err
		}
	}
	return nil
}

func retireDeviceOTPByID(ctx context.Context, tx pgx.Tx, id string) error {
	tag, err := tx.Exec(ctx,
		`UPDATE identity_action_secrets
		   SET superseded_at = GREATEST(issued_at, clock_timestamp()),
		       superseded_by_id = id
		  WHERE id = $1::uuid AND consumed_at IS NULL AND superseded_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("invalidating device challenge: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return errors.New("device challenge changed during invalidation")
	}
	return nil
}

type DeviceCutoverRequest struct {
	Expected  DeviceCutoverCounts
	Operator  string
	RequestID string
}

// ApplyDeviceCutover revokes historical families through the canonical session
// revoker and self-supersedes their outstanding OTPs in one transaction. The
// caller must stop old API writers and supply exact preflight counts.
func ApplyDeviceCutover(
	ctx context.Context, pool *pgxpool.Pool, request DeviceCutoverRequest,
) (DeviceCutoverCounts, error) {
	if strings.TrimSpace(request.Operator) == "" || strings.TrimSpace(request.RequestID) == "" ||
		request.Expected.LegacySessions < 0 || request.Expected.PendingSessions < 0 || request.Expected.OutstandingOTP < 0 {
		return DeviceCutoverCounts{}, errors.New("device cutover requires an operator, request ID, and nonnegative expected counts")
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return DeviceCutoverCounts{}, fmt.Errorf("beginning device cutover: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	families, err := lockedCutoverIDs(ctx, tx,
		`SELECT s.id::text FROM sessions s
		  JOIN accounts a ON a.id = s.account_id
		  WHERE a.role = 'STUDENT' AND s.state = 'ACTIVE'
		    AND s.device_trust_state IN ('LEGACY_UNBOUND', 'PENDING_DEVICE_TRUST')
		  ORDER BY s.account_id, s.id FOR UPDATE OF s`)
	if err != nil {
		return DeviceCutoverCounts{}, err
	}
	otpIDs, err := lockedCutoverIDs(ctx, tx,
		`SELECT id::text FROM identity_action_secrets
		  WHERE purpose = 'DEVICE_TRUST_OTP'
		    AND consumed_at IS NULL AND superseded_at IS NULL
		  ORDER BY id FOR UPDATE`)
	if err != nil {
		return DeviceCutoverCounts{}, err
	}
	actual, err := readDeviceCutoverCounts(ctx, tx)
	if err != nil {
		return DeviceCutoverCounts{}, err
	}
	if !cutoverCountsMatch(request.Expected, actual, len(families), len(otpIDs)) {
		return DeviceCutoverCounts{}, fmt.Errorf("device cutover counts changed: legacy=%d pending=%d otp=%d",
			actual.LegacySessions, actual.PendingSessions, actual.OutstandingOTP)
	}

	now := time.Now().UTC()
	for _, id := range families {
		if err := revokeSessionFamily(ctx, tx, id, RevokedByAdmin, now); err != nil {
			return DeviceCutoverCounts{}, err
		}
		if err := appendCutoverTargetAudit(ctx, tx, request, cutoverTargetAudit{
			action: "STUDENT_DEVICE_CUTOVER_SESSION_REVOKED", targetType: "SESSION", targetID: id,
		}); err != nil {
			return DeviceCutoverCounts{}, err
		}
	}
	for _, id := range otpIDs {
		if err := retireDeviceOTPByID(ctx, tx, id); err != nil {
			return DeviceCutoverCounts{}, err
		}
		if err := appendCutoverTargetAudit(ctx, tx, request, cutoverTargetAudit{
			action: "DEVICE_TRUST_OTP_INVALIDATED", targetType: "ACTION_SECRET", targetID: id,
		}); err != nil {
			return DeviceCutoverCounts{}, err
		}
	}
	if err := appendDeviceCutoverAudit(ctx, tx, request, actual); err != nil {
		return DeviceCutoverCounts{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return DeviceCutoverCounts{}, fmt.Errorf("committing device cutover: %w", err)
	}
	return actual, nil
}

func lockedCutoverIDs(ctx context.Context, tx pgx.Tx, statement string) ([]string, error) {
	rows, err := tx.Query(ctx, statement)
	if err != nil {
		return nil, fmt.Errorf("locking device cutover targets: %w", err)
	}
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scanning device cutover target: %w", err)
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, fmt.Errorf("iterating device cutover targets: %w", err)
	}
	return ids, nil
}

func cutoverCountsMatch(expected, actual DeviceCutoverCounts, families, otps int) bool {
	return actual.LegacySessions == expected.LegacySessions &&
		actual.PendingSessions == expected.PendingSessions &&
		actual.OutstandingOTP == expected.OutstandingOTP &&
		families == actual.LegacySessions+actual.PendingSessions &&
		otps == actual.OutstandingOTP
}

type cutoverTargetAudit struct {
	action     string
	targetType string
	targetID   string
}

func appendCutoverTargetAudit(
	ctx context.Context, tx pgx.Tx, request DeviceCutoverRequest, target cutoverTargetAudit,
) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO audit_events
		  (actor_role, actor_descriptor, action, module, target_type, target_id,
		   reason, metadata, correlation_id)
		 VALUES ('RELEASE_OPERATOR', $1, $2, 'IDENTITY_AND_ACCESS', $3, $4,
		         'Historical Student device-authority cutover',
		         '{}'::jsonb, $5)`,
		request.Operator, target.action, target.targetType, target.targetID, request.RequestID)
	if err != nil {
		return fmt.Errorf("auditing device cutover target: %w", err)
	}
	return nil
}

func appendDeviceCutoverAudit(
	ctx context.Context, tx pgx.Tx, request DeviceCutoverRequest, actual DeviceCutoverCounts,
) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO audit_events
		   (actor_role, actor_descriptor, action, module, target_type, target_id,
		    reason, metadata, correlation_id)
		 VALUES ('RELEASE_OPERATOR', $1, 'STUDENT_DEVICE_CUTOVER', 'IDENTITY_AND_ACCESS',
		         'DEVICE_POLICY_CUTOVER', $2, 'Retire historical device sessions and challenges',
		         jsonb_build_object('legacy_sessions', $3::int,
		                            'pending_sessions', $4::int,
		                            'invalidated_device_otps', $5::int), $2)`,
		request.Operator, request.RequestID,
		actual.LegacySessions, actual.PendingSessions, actual.OutstandingOTP)
	if err != nil {
		return fmt.Errorf("auditing device cutover: %w", err)
	}
	return nil
}
