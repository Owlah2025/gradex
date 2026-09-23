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

// ExpiredStaffLegacyCleanupCounts separates the narrowly authorized cleanup
// targets from historical states that require an explicit disposition.
type ExpiredStaffLegacyCleanupCounts struct {
	EligibleExpiredLegacy int `json:"eligible_expired_legacy"`
	NonExpiredLegacy      int `json:"non_expired_legacy"`
	PendingDeviceTrust    int `json:"pending_device_trust"`
	UnexpectedRole        int `json:"unexpected_role"`
}

type expiredStaffLegacySession struct {
	id                string
	accountID         string
	role              string
	absoluteExpiresAt time.Time
}

type expiredStaffLegacyCleanupBatch struct {
	request      ExpiredStaffLegacyCleanupRequest
	decisionTime time.Time
	targets      []expiredStaffLegacySession
}

const expiredStaffLegacyCleanupCountsSQL = `
	SELECT
	  count(*) FILTER (WHERE a.role IN ('ADMIN', 'INSTRUCTOR')
	    AND s.device_trust_state = 'LEGACY_UNBOUND'
	    AND s.absolute_expires_at <= $1),
	  count(*) FILTER (WHERE a.role IN ('ADMIN', 'INSTRUCTOR')
	    AND s.device_trust_state = 'LEGACY_UNBOUND'
	    AND s.absolute_expires_at > $1),
	  count(*) FILTER (WHERE a.role IN ('ADMIN', 'INSTRUCTOR')
	    AND s.device_trust_state = 'PENDING_DEVICE_TRUST'),
	  count(*) FILTER (WHERE a.role NOT IN ('STUDENT', 'ADMIN', 'INSTRUCTOR'))
	  FROM sessions s
	  JOIN accounts a ON a.id = s.account_id
	 WHERE s.state = 'ACTIVE'
	   AND s.device_trust_state IN ('LEGACY_UNBOUND', 'PENDING_DEVICE_TRUST')`

const lockExpiredStaffLegacySessionsSQL = `
	SELECT s.id::text, s.account_id::text, a.role::text, s.absolute_expires_at
	  FROM sessions s
	  JOIN accounts a ON a.id = s.account_id
	 WHERE a.role IN ('ADMIN', 'INSTRUCTOR')
	   AND s.state = 'ACTIVE'
	   AND s.device_trust_state = 'LEGACY_UNBOUND'
	   AND s.absolute_expires_at <= $1
	 ORDER BY s.account_id, s.id
	 FOR UPDATE OF s`

const expiredStaffLegacyCleanupAuditSQL = `
	INSERT INTO audit_events
	  (actor_role, actor_descriptor, action, module, target_type, target_id,
	   reason, metadata, correlation_id)
	 VALUES ('RELEASE_OPERATOR', $1, 'EXPIRED_LEGACY_SESSION_CLEANUP',
         'IDENTITY_AND_ACCESS', 'SESSION', $2,
         'One-time cleanup of an expired non-Student legacy session family',
         jsonb_build_object('account_id', $3::text, 'account_role', $4::text,
                            'device_trust_state', 'LEGACY_UNBOUND',
                            'absolute_expires_at', $5::timestamptz), $6)`

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

// ReadExpiredStaffLegacyCleanupCounts reports only non-Student historical
// sessions relevant to the separate, one-time Staff cleanup gate.
func ReadExpiredStaffLegacyCleanupCounts(
	ctx context.Context, pool *pgxpool.Pool,
) (ExpiredStaffLegacyCleanupCounts, error) {
	decisionTime, err := readDatabaseDecisionTime(ctx, pool)
	if err != nil {
		return ExpiredStaffLegacyCleanupCounts{}, err
	}
	return readExpiredStaffLegacyCleanupCounts(ctx, pool, decisionTime)
}

func readExpiredStaffLegacyCleanupCounts(
	ctx context.Context, query cutoverQuerier, decisionTime time.Time,
) (ExpiredStaffLegacyCleanupCounts, error) {
	var counts ExpiredStaffLegacyCleanupCounts
	err := query.QueryRow(ctx, expiredStaffLegacyCleanupCountsSQL, decisionTime).Scan(&counts.EligibleExpiredLegacy, &counts.NonExpiredLegacy,
		&counts.PendingDeviceTrust, &counts.UnexpectedRole)
	if err != nil {
		return ExpiredStaffLegacyCleanupCounts{}, fmt.Errorf("counting expired Staff legacy sessions: %w", err)
	}
	return counts, nil
}

func readDatabaseDecisionTime(ctx context.Context, query cutoverQuerier) (time.Time, error) {
	var decisionTime time.Time
	if err := query.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&decisionTime); err != nil {
		return time.Time{}, fmt.Errorf("reading database time for Staff cleanup: %w", err)
	}
	return decisionTime, nil
}

type ExpiredStaffLegacyCleanupRequest struct {
	ExpectedEligible int
	Operator         string
	RequestID        string
}

// ApplyExpiredStaffLegacyCleanup revokes only expired Admin/Instructor
// LEGACY_UNBOUND families. The table lock prevents a concurrent session write
// from changing the preflight set while the exact row set and expected count
// are checked and committed.
func ApplyExpiredStaffLegacyCleanup(
	ctx context.Context, pool *pgxpool.Pool, request ExpiredStaffLegacyCleanupRequest,
) (ExpiredStaffLegacyCleanupCounts, error) {
	if err := validateExpiredStaffLegacyCleanupRequest(request); err != nil {
		return ExpiredStaffLegacyCleanupCounts{}, err
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ExpiredStaffLegacyCleanupCounts{}, fmt.Errorf("beginning expired Staff cleanup: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	actual, batch, err := lockAndCheckExpiredStaffCleanupSet(ctx, tx, request)
	if err != nil {
		return ExpiredStaffLegacyCleanupCounts{}, err
	}
	if err := commitExpiredStaffCleanup(ctx, tx, batch); err != nil {
		return ExpiredStaffLegacyCleanupCounts{}, err
	}
	return actual, nil
}

func validateExpiredStaffLegacyCleanupRequest(request ExpiredStaffLegacyCleanupRequest) error {
	if strings.TrimSpace(request.Operator) == "" || strings.TrimSpace(request.RequestID) == "" ||
		request.ExpectedEligible < 0 {
		return errors.New("expired Staff cleanup requires an operator, request ID, and nonnegative expected count")
	}
	return nil
}

func lockAndCheckExpiredStaffCleanupSet(
	ctx context.Context, tx pgx.Tx, request ExpiredStaffLegacyCleanupRequest,
) (ExpiredStaffLegacyCleanupCounts, expiredStaffLegacyCleanupBatch, error) {
	decisionTime, err := lockExpiredStaffCleanupWriteSet(ctx, tx)
	if err != nil {
		return ExpiredStaffLegacyCleanupCounts{}, expiredStaffLegacyCleanupBatch{}, err
	}
	actual, targets, err := readExpiredStaffCleanupSnapshot(ctx, tx, decisionTime)
	if err != nil {
		return ExpiredStaffLegacyCleanupCounts{}, expiredStaffLegacyCleanupBatch{}, err
	}
	if err := validateExpiredStaffLegacyCleanupState(request, actual, len(targets)); err != nil {
		return ExpiredStaffLegacyCleanupCounts{}, expiredStaffLegacyCleanupBatch{}, err
	}
	return actual, expiredStaffLegacyCleanupBatch{
		request: request, decisionTime: decisionTime, targets: targets,
	}, nil
}

func readExpiredStaffCleanupSnapshot(
	ctx context.Context, tx pgx.Tx, decisionTime time.Time,
) (ExpiredStaffLegacyCleanupCounts, []expiredStaffLegacySession, error) {
	targets, err := lockedExpiredStaffLegacySessions(ctx, tx, decisionTime)
	if err != nil {
		return ExpiredStaffLegacyCleanupCounts{}, nil, err
	}
	actual, err := readExpiredStaffLegacyCleanupCounts(ctx, tx, decisionTime)
	if err != nil {
		return ExpiredStaffLegacyCleanupCounts{}, nil, err
	}
	return actual, targets, nil
}

func lockExpiredStaffCleanupWriteSet(ctx context.Context, tx pgx.Tx) (time.Time, error) {
	if _, err := tx.Exec(ctx, `LOCK TABLE sessions IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return time.Time{}, fmt.Errorf("locking session families for expired Staff cleanup: %w", err)
	}
	return readDatabaseDecisionTime(ctx, tx)
}

func validateExpiredStaffLegacyCleanupState(
	request ExpiredStaffLegacyCleanupRequest, actual ExpiredStaffLegacyCleanupCounts, targetCount int,
) error {
	if actual.NonExpiredLegacy != 0 || actual.PendingDeviceTrust != 0 || actual.UnexpectedRole != 0 {
		return fmt.Errorf("expired Staff cleanup refused: non_expired_legacy=%d pending_device_trust=%d unexpected_role=%d",
			actual.NonExpiredLegacy, actual.PendingDeviceTrust, actual.UnexpectedRole)
	}
	if actual.EligibleExpiredLegacy != request.ExpectedEligible || targetCount != actual.EligibleExpiredLegacy {
		return fmt.Errorf("expired Staff cleanup count changed: eligible=%d locked=%d expected=%d",
			actual.EligibleExpiredLegacy, targetCount, request.ExpectedEligible)
	}
	return nil
}

func revokeExpiredStaffLegacyTargets(
	ctx context.Context, tx pgx.Tx, batch expiredStaffLegacyCleanupBatch,
) error {
	for _, target := range batch.targets {
		if err := revokeSessionFamily(ctx, tx, target.id, RevokedByAdmin, batch.decisionTime); err != nil {
			return err
		}
		if err := appendExpiredStaffLegacyCleanupAudit(ctx, tx, batch.request, target); err != nil {
			return err
		}
	}
	return nil
}

func commitExpiredStaffCleanup(
	ctx context.Context, tx pgx.Tx, batch expiredStaffLegacyCleanupBatch,
) error {
	if err := revokeExpiredStaffLegacyTargets(ctx, tx, batch); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing expired Staff cleanup: %w", err)
	}
	return nil
}

func lockedExpiredStaffLegacySessions(
	ctx context.Context, tx pgx.Tx, decisionTime time.Time,
) ([]expiredStaffLegacySession, error) {
	rows, err := tx.Query(ctx, lockExpiredStaffLegacySessionsSQL, decisionTime)
	if err != nil {
		return nil, fmt.Errorf("locking expired Staff legacy session targets: %w", err)
	}
	defer rows.Close()
	return scanExpiredStaffLegacySessions(rows)
}

func scanExpiredStaffLegacySessions(rows pgx.Rows) ([]expiredStaffLegacySession, error) {
	targets := make([]expiredStaffLegacySession, 0)
	for rows.Next() {
		var target expiredStaffLegacySession
		if err := rows.Scan(&target.id, &target.accountID, &target.role, &target.absoluteExpiresAt); err != nil {
			return nil, fmt.Errorf("scanning expired Staff legacy session target: %w", err)
		}
		targets = append(targets, target)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating expired Staff legacy session targets: %w", err)
	}
	return targets, nil
}

func appendExpiredStaffLegacyCleanupAudit(
	ctx context.Context, tx pgx.Tx, request ExpiredStaffLegacyCleanupRequest,
	target expiredStaffLegacySession,
) error {
	_, err := tx.Exec(ctx, expiredStaffLegacyCleanupAuditSQL,
		request.Operator, target.id, target.accountID, target.role,
		target.absoluteExpiresAt, request.RequestID)
	if err != nil {
		return fmt.Errorf("auditing expired Staff legacy session cleanup: %w", err)
	}
	return nil
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
