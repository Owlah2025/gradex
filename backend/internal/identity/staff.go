package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

// StaffService provides production database transaction wrapping for staff
// lifecycle, invitation, and suspension operations.
type StaffService struct {
	pool        *pgxpool.Pool
	outbox      *outbox.Writer
	compromised CompromisedRangeSource
}

// NewStaffService constructs a production StaffService.
func NewStaffService(
	pool *pgxpool.Pool,
	writer *outbox.Writer,
	compromised CompromisedRangeSource,
) (*StaffService, error) {
	if pool == nil {
		return nil, errors.New("pgx pool is required for StaffService")
	}
	// The invitation's delivery intent is co-committed with its Identity
	// evidence, so the writer is part of the invariant rather than an optional
	// dependency. A nil writer would commit invitations that can never be
	// delivered, silently.
	if writer == nil {
		return nil, errors.New("outbox writer is required for StaffService")
	}
	return &StaffService{
		pool:        pool,
		outbox:      writer,
		compromised: compromised,
	}, nil
}

func (s *StaffService) CreateStaffInvitation(
	ctx context.Context,
	req CreateStaffInvitationRequest,
) (IssuedStaffInvitation, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return IssuedStaffInvitation{}, fmt.Errorf("beginning create staff invitation transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	req.Outbox = s.outbox

	res, err := CreateStaffInvitation(ctx, tx, req)
	if err != nil {
		return IssuedStaffInvitation{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return IssuedStaffInvitation{}, fmt.Errorf("committing create staff invitation transaction: %w", err)
	}

	return res, nil
}

func (s *StaffService) PreviewStaffInvitation(
	ctx context.Context,
	bearer string,
	now time.Time,
) (StaffInvitationPreview, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return StaffInvitationPreview{}, fmt.Errorf("beginning preview staff invitation transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	res, err := PreviewStaffInvitation(ctx, tx, bearer, now)
	if err != nil {
		return StaffInvitationPreview{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return StaffInvitationPreview{}, fmt.Errorf("committing preview staff invitation transaction: %w", err)
	}

	return res, nil
}

func (s *StaffService) CompleteStaffInvitation(
	ctx context.Context,
	req CompleteStaffInvitationRequest,
) (CompleteStaffInvitationResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return CompleteStaffInvitationResult{}, fmt.Errorf("beginning complete staff invitation transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if req.Compromised == nil {
		req.Compromised = s.compromised
	}

	res, err := CompleteStaffInvitation(ctx, tx, req)
	if err != nil {
		return CompleteStaffInvitationResult{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return CompleteStaffInvitationResult{}, fmt.Errorf("committing complete staff invitation transaction: %w", err)
	}

	return res, nil
}

func (s *StaffService) RevokeStaffInvitation(
	ctx context.Context,
	req RevokeStaffInvitationRequest,
) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning revoke staff invitation transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := RevokeStaffInvitation(ctx, tx, req); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing revoke staff invitation transaction: %w", err)
	}

	return nil
}

func (s *StaffService) SuspendAccount(
	ctx context.Context,
	req SuspendAccountRequest,
) (SuspendAccountResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return SuspendAccountResult{}, fmt.Errorf("beginning suspend account transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	res, err := SuspendAccount(ctx, tx, req)
	if err != nil {
		return SuspendAccountResult{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return SuspendAccountResult{}, fmt.Errorf("committing suspend account transaction: %w", err)
	}

	return res, nil
}

func (s *StaffService) ReinstateAccount(
	ctx context.Context,
	req ReinstateAccountRequest,
) (ReinstateAccountResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ReinstateAccountResult{}, fmt.Errorf("beginning reinstate account transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	res, err := ReinstateAccount(ctx, tx, req)
	if err != nil {
		return ReinstateAccountResult{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return ReinstateAccountResult{}, fmt.Errorf("committing reinstate account transaction: %w", err)
	}

	return res, nil
}

func (s *StaffService) ListPendingInvitations(
	ctx context.Context,
	principal Principal,
) ([]StaffInvitation, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("beginning list pending invitations transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	res, err := ListPendingInvitations(ctx, tx, principal)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("committing list pending invitations transaction: %w", err)
	}

	return res, nil
}

func (s *StaffService) ListInstructorAccounts(ctx context.Context, principal Principal) ([]InstructorAccount, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("beginning list Instructor accounts transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	accounts, err := ListInstructorAccounts(ctx, tx, principal)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("committing list Instructor accounts transaction: %w", err)
	}
	return accounts, nil
}
