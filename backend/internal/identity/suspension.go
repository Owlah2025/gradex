package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	ErrAccountNotFound = errors.New("account not found")
	ErrUnauthorized    = errors.New("unauthorized capability")
)

type SuspendAccountRequest struct {
	ActorPrincipal   Principal
	ActorSession     Session
	RecentAuthWindow time.Duration
	SubjectAccountID string
	Reason           string
	Now              time.Time
	RequestID        string
}

type SuspendAccountResult struct {
	AlreadySuspended bool
	Revision         int
	Epoch            int
}

type ReinstateAccountRequest struct {
	ActorPrincipal   Principal
	ActorSession     Session
	RecentAuthWindow time.Duration
	SubjectAccountID string
	Reason           string
	Now              time.Time
	RequestID        string
}

type ReinstateAccountResult struct {
	AlreadyActive bool
	Revision      int
}

// SuspendAccount executes the Account suspension operation in a single database transaction.
func SuspendAccount(
	ctx context.Context,
	tx pgx.Tx,
	req SuspendAccountRequest,
) (SuspendAccountResult, error) {
	decision := Authorize(req.ActorPrincipal, CapSecurityOperations)
	if !decision.Allowed {
		return SuspendAccountResult{}, fmt.Errorf("%w: %s", ErrUnauthorized, decision.Reason)
	}

	if err := CheckRecentAuthentication(req.ActorSession, req.RecentAuthWindow, req.Now); err != nil {
		return SuspendAccountResult{}, fmt.Errorf("%w: recent authentication check failed", ErrRecentAuthRequired)
	}

	if req.SubjectAccountID == "" {
		return SuspendAccountResult{}, errors.New("subject account ID is required")
	}

	var currentStatus string
	var currentRevision int
	var currentEpoch int

	err := tx.QueryRow(ctx,
		`SELECT status, revision, session_epoch
		   FROM accounts
		  WHERE id = $1::uuid
		    FOR UPDATE`,
		req.SubjectAccountID,
	).Scan(&currentStatus, &currentRevision, &currentEpoch)

	if errors.Is(err, pgx.ErrNoRows) {
		return SuspendAccountResult{}, ErrAccountNotFound
	}
	if err != nil {
		return SuspendAccountResult{}, fmt.Errorf("locking account for suspension: %w", err)
	}

	if currentStatus == string(StatusSuspended) {
		return SuspendAccountResult{
			AlreadySuspended: true,
			Revision:         currentRevision,
			Epoch:            currentEpoch,
		}, nil
	}

	var newRevision int
	var newEpoch int
	err = tx.QueryRow(ctx,
		`UPDATE accounts
		    SET status = 'SUSPENDED',
		        session_epoch = session_epoch + 1,
		        revision = revision + 1,
		        updated_at = $2
		  WHERE id = $1::uuid
		RETURNING revision, session_epoch`,
		req.SubjectAccountID, req.Now,
	).Scan(&newRevision, &newEpoch)

	if err != nil {
		return SuspendAccountResult{}, fmt.Errorf("updating account status to SUSPENDED: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`UPDATE sessions
		    SET state = 'REVOKED',
		        revoked_at = $2,
		        revocation_reason = 'ACCOUNT_SUSPENDED',
		        updated_at = $2
		  WHERE account_id = $1::uuid
		    AND state = 'ACTIVE'`,
		req.SubjectAccountID, req.Now,
	); err != nil {
		return SuspendAccountResult{}, fmt.Errorf("revoking session families on account suspension: %w", err)
	}

	evidence := map[string]any{
		"actor_account_id": req.ActorPrincipal.AccountID,
		"reason":           req.Reason,
	}

	if err := appendIdentitySecurityEvent(ctx, tx, securityEventAppend{
		eventType: "ACCOUNT_SUSPENDED",
		accountID: req.SubjectAccountID,
		revision:  newRevision,
		requestID: req.RequestID,
		evidence:  evidence,
	}); err != nil {
		return SuspendAccountResult{}, fmt.Errorf("writing ACCOUNT_SUSPENDED security evidence: %w", err)
	}

	return SuspendAccountResult{
		AlreadySuspended: false,
		Revision:         newRevision,
		Epoch:            newEpoch,
	}, nil
}

// ReinstateAccount restores an Account to ACTIVE in a single database transaction.
func ReinstateAccount(
	ctx context.Context,
	tx pgx.Tx,
	req ReinstateAccountRequest,
) (ReinstateAccountResult, error) {
	decision := Authorize(req.ActorPrincipal, CapSecurityOperations)
	if !decision.Allowed {
		return ReinstateAccountResult{}, fmt.Errorf("%w: %s", ErrUnauthorized, decision.Reason)
	}

	if err := CheckRecentAuthentication(req.ActorSession, req.RecentAuthWindow, req.Now); err != nil {
		return ReinstateAccountResult{}, fmt.Errorf("%w: recent authentication check failed", ErrRecentAuthRequired)
	}

	if req.SubjectAccountID == "" {
		return ReinstateAccountResult{}, errors.New("subject account ID is required")
	}

	var currentStatus string
	var currentRevision int

	err := tx.QueryRow(ctx,
		`SELECT status, revision
		   FROM accounts
		  WHERE id = $1::uuid
		    FOR UPDATE`,
		req.SubjectAccountID,
	).Scan(&currentStatus, &currentRevision)

	if errors.Is(err, pgx.ErrNoRows) {
		return ReinstateAccountResult{}, ErrAccountNotFound
	}
	if err != nil {
		return ReinstateAccountResult{}, fmt.Errorf("locking account for reinstatement: %w", err)
	}

	if currentStatus == string(StatusActive) {
		return ReinstateAccountResult{
			AlreadyActive: true,
			Revision:      currentRevision,
		}, nil
	}

	var newRevision int
	err = tx.QueryRow(ctx,
		`UPDATE accounts
		    SET status = 'ACTIVE',
		        revision = revision + 1,
		        updated_at = $2
		  WHERE id = $1::uuid
		RETURNING revision`,
		req.SubjectAccountID, req.Now,
	).Scan(&newRevision)

	if err != nil {
		return ReinstateAccountResult{}, fmt.Errorf("updating account status to ACTIVE: %w", err)
	}

	evidence := map[string]any{
		"actor_account_id": req.ActorPrincipal.AccountID,
		"reason":           req.Reason,
	}

	if err := appendIdentitySecurityEvent(ctx, tx, securityEventAppend{
		eventType: "ACCOUNT_REINSTATED",
		accountID: req.SubjectAccountID,
		revision:  newRevision,
		requestID: req.RequestID,
		evidence:  evidence,
	}); err != nil {
		return ReinstateAccountResult{}, fmt.Errorf("writing ACCOUNT_REINSTATED security evidence: %w", err)
	}

	return ReinstateAccountResult{
		AlreadyActive: false,
		Revision:      newRevision,
	}, nil
}
