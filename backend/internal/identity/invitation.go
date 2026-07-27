package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

var (
	ErrInvitationNotFound    = errors.New("staff invitation not found")
	ErrInvitationInvalid     = errors.New("staff invitation is invalid")
	ErrInvitationExpired     = errors.New("staff invitation is expired")
	ErrInvitationAlreadyUsed = errors.New("staff invitation has already been used")
	ErrInvitationSuperseded  = errors.New("staff invitation was superseded")
	ErrInvitationRevoked     = errors.New("staff invitation was revoked")
	ErrAccountAlreadyExists  = errors.New("an account already exists for this email")
	ErrInviterUnauthorized   = errors.New("inviter lacks required authority")
)

type StaffInvitationState string

const (
	InvitationPending    StaffInvitationState = "PENDING"
	InvitationConsumed   StaffInvitationState = "CONSUMED"
	InvitationSuperseded StaffInvitationState = "SUPERSEDED"
	InvitationExpired    StaffInvitationState = "EXPIRED"
	InvitationRevoked    StaffInvitationState = "REVOKED"
)

type StaffInvitation struct {
	ID               string
	NormalizedEmail  string
	Email            string
	InvitedRole      Role
	InviterAccountID string
	State            StaffInvitationState
	ActionSecretID   string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type CreateStaffInvitationRequest struct {
	ActorPrincipal   Principal
	ActorSession     Session
	RecentAuthWindow time.Duration
	Email            string
	Role             Role
	// Locale addresses the invitation notification. The invitee has no Account
	// yet, so unlike recovery — which reads the locale off the Account — it is
	// supplied by the inviting Admin. Empty means the platform default, Arabic,
	// matching the Accept-Language fallback the admission surface already uses.
	Locale    Locale
	TTL       time.Duration
	Now       time.Time
	RequestID string
	Outbox    *outbox.Writer
}

type IssuedStaffInvitation struct {
	Invitation  StaffInvitation
	Bearer      config.Secret
	BearerToken string
}

type StaffInvitationPreview struct {
	InvitedRole Role
	State       StaffInvitationState
}

type CompleteStaffInvitationRequest struct {
	Bearer      string
	DisplayName string
	Password    config.Secret
	Compromised CompromisedRangeSource
	Now         time.Time
	RequestID   string
}

type CompleteStaffInvitationResult struct {
	AccountID   string
	InvitedRole Role
}

type RevokeStaffInvitationRequest struct {
	ActorPrincipal   Principal
	ActorSession     Session
	RecentAuthWindow time.Duration
	InvitationID     string
	Now              time.Time
	RequestID        string
}

// CreateStaffInvitation creates or supersedes a staff invitation.
func CreateStaffInvitation(
	ctx context.Context,
	tx pgx.Tx,
	req CreateStaffInvitationRequest,
) (IssuedStaffInvitation, error) {
	decision := Authorize(req.ActorPrincipal, CapAdminOperations)
	if !decision.Allowed {
		return IssuedStaffInvitation{}, fmt.Errorf("%w: %s", ErrUnauthorized, decision.Reason)
	}

	if err := CheckRecentAuthentication(req.ActorSession, req.RecentAuthWindow, req.Now); err != nil {
		return IssuedStaffInvitation{}, fmt.Errorf("%w: recent authentication check failed", ErrRecentAuthRequired)
	}

	if req.Role != RoleInstructor && req.Role != RoleAdmin {
		return IssuedStaffInvitation{}, fmt.Errorf("invalid invited role %q: only INSTRUCTOR or ADMIN can be invited", req.Role)
	}

	if req.Outbox == nil {
		return IssuedStaffInvitation{}, errors.New("outbox writer is required to create a staff invitation")
	}

	locale := req.Locale
	if locale == "" {
		locale = LocaleArabic
	}
	if !locale.Valid() {
		return IssuedStaffInvitation{}, ErrInvalidLocale
	}

	normalizedEmail, err := NormalizeEmail(req.Email)
	if err != nil {
		return IssuedStaffInvitation{}, fmt.Errorf("normalizing invitation email: %w", err)
	}

	// M3: Advisory lock on normalized email to serialize concurrent invitation creation for the same email
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, normalizedEmail); err != nil {
		return IssuedStaffInvitation{}, fmt.Errorf("locking normalized invitation email: %w", err)
	}

	// BR-009: Reject if email address already belongs to any Account.
	var exists bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM accounts WHERE normalized_email = $1)`,
		normalizedEmail,
	).Scan(&exists); err != nil {
		return IssuedStaffInvitation{}, fmt.Errorf("checking existing account: %w", err)
	}
	if exists {
		return IssuedStaffInvitation{}, ErrAccountAlreadyExists
	}

	if req.TTL <= 0 {
		req.TTL = 7 * 24 * time.Hour // default 7 days TTL
	}

	issuedSecret, err := newActionSecret(actionSecretOptions{
		Purpose: ActionStaffInvitation,
		Now:     req.Now,
		TTL:     req.TTL,
	})
	if err != nil {
		return IssuedStaffInvitation{}, fmt.Errorf("minting invitation action secret: %w", err)
	}

	// Supersede existing PENDING invitation for this email if present.
	var existingInvID, existingSecretID string
	err = tx.QueryRow(ctx,
		`SELECT id::text, action_secret_id::text
		   FROM staff_invitations
		  WHERE normalized_email = $1 AND state = 'PENDING'
		    FOR UPDATE`,
		normalizedEmail,
	).Scan(&existingInvID, &existingSecretID)

	if err == nil {
		if _, err := tx.Exec(ctx,
			`UPDATE staff_invitations SET state = 'SUPERSEDED', updated_at = $2 WHERE id = $1::uuid`,
			existingInvID, req.Now,
		); err != nil {
			return IssuedStaffInvitation{}, fmt.Errorf("superseding existing staff invitation: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`UPDATE identity_action_secrets SET superseded_at = $2, superseded_by_id = $3::uuid WHERE id = $1::uuid`,
			existingSecretID, req.Now, issuedSecret.ID,
		); err != nil {
			return IssuedStaffInvitation{}, fmt.Errorf("superseding existing action secret: %w", err)
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return IssuedStaffInvitation{}, fmt.Errorf("checking pending invitations: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO identity_action_secrets
		   (id, account_id, purpose, secret_digest, issued_at, expires_at)
		 VALUES ($1::uuid, NULL, $2, $3, $4, $5)`,
		issuedSecret.ID, string(ActionStaffInvitation), issuedSecret.Digest, issuedSecret.IssuedAt, issuedSecret.ExpiresAt,
	); err != nil {
		return IssuedStaffInvitation{}, fmt.Errorf("inserting action secret: %w", err)
	}

	invitationID := uuid.NewString()
	if _, err := tx.Exec(ctx,
		`INSERT INTO staff_invitations
		   (id, normalized_email, email, invited_role, inviter_account_id, state, action_secret_id, created_at, updated_at)
		 VALUES ($1::uuid, $2, $3, $4, $5::uuid, 'PENDING', $6::uuid, $7, $7)`,
		invitationID, normalizedEmail, strings.TrimSpace(req.Email), string(req.Role), req.ActorPrincipal.AccountID, issuedSecret.ID, req.Now,
	); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return IssuedStaffInvitation{}, ErrAccountAlreadyExists
		}
		return IssuedStaffInvitation{}, fmt.Errorf("inserting staff invitation: %w", err)
	}

	evidence := map[string]any{
		"invitation_id":    invitationID,
		"invited_email":    normalizedEmail,
		"invited_role":     string(req.Role),
		"action_secret_id": issuedSecret.ID,
	}

	if err := appendIdentitySecurityEvent(ctx, tx, securityEventAppend{
		eventType: "STAFF_INVITATION_CREATED",
		accountID: req.ActorPrincipal.AccountID,
		revision:  1,
		requestID: req.RequestID,
		evidence:  evidence,
	}); err != nil {
		return IssuedStaffInvitation{}, fmt.Errorf("writing STAFF_INVITATION_CREATED evidence: %w", err)
	}

	// The delivery intent is co-committed with the evidence above, inside this
	// transaction. It is part of the invariant, not an optional extra: an
	// invitation that commits without one can never be delivered and nothing
	// downstream would report it.
	_, err = req.Outbox.Append(ctx, tx, outbox.Event{
		Type:              "identity.staff_invitation_created",
		SchemaVersion:     1,
		SourceModule:      "IDENTITY_AND_ACCESS",
		AggregateType:     "STAFF_INVITATION",
		AggregateID:       invitationID,
		AggregateRevision: 1,
		CorrelationID:     req.RequestID,
		SafePayload: map[string]any{
			"action_secret_id":  issuedSecret.ID,
			"purpose":           string(ActionStaffInvitation),
			"invited_role":      string(req.Role),
			"locale":            string(locale),
			"secret_expires_at": issuedSecret.ExpiresAt,
		},
	}, outbox.VerificationDelivery{
		Destination:       normalizedEmail,
		Locale:            string(locale),
		TemplateContract:  "staff-invitation-v1",
		VerificationToken: issuedSecret.Bearer.Expose(),
		ExpiresAt:         issuedSecret.ExpiresAt,
	})
	if err != nil {
		return IssuedStaffInvitation{}, fmt.Errorf("writing staff invitation outbox intent: %w", err)
	}

	invitation := StaffInvitation{
		ID:               invitationID,
		NormalizedEmail:  normalizedEmail,
		Email:            strings.TrimSpace(req.Email),
		InvitedRole:      req.Role,
		InviterAccountID: req.ActorPrincipal.AccountID,
		State:            InvitationPending,
		ActionSecretID:   issuedSecret.ID,
		CreatedAt:        req.Now,
		UpdatedAt:        req.Now,
	}

	return IssuedStaffInvitation{
		Invitation:  invitation,
		Bearer:      issuedSecret.Bearer,
		BearerToken: issuedSecret.Bearer.Expose(),
	}, nil
}

// PreviewStaffInvitation inspects an invitation bearer and returns the invited role and state.
// Non-enumerating: returns invited role and state only; NEVER the email.
func PreviewStaffInvitation(
	ctx context.Context,
	tx pgx.Tx,
	bearer string,
	now time.Time,
) (StaffInvitationPreview, error) {
	digest, err := DigestActionSecret(bearer)
	if err != nil {
		return StaffInvitationPreview{}, ErrInvitationInvalid
	}

	var state string
	var expiresAt time.Time
	var invitedRole string

	err = tx.QueryRow(ctx,
		`SELECT si.state, ias.expires_at, si.invited_role
		   FROM identity_action_secrets ias
		   JOIN staff_invitations si ON si.action_secret_id = ias.id
		  WHERE ias.secret_digest = $1 AND ias.purpose = $2`,
		digest, string(ActionStaffInvitation),
	).Scan(&state, &expiresAt, &invitedRole)

	if errors.Is(err, pgx.ErrNoRows) {
		return StaffInvitationPreview{}, ErrInvitationInvalid
	}
	if err != nil {
		return StaffInvitationPreview{}, fmt.Errorf("previewing staff invitation: %w", err)
	}

	invState := StaffInvitationState(state)
	if invState == InvitationPending && !now.Before(expiresAt) {
		invState = InvitationExpired
	}

	return StaffInvitationPreview{
		InvitedRole: Role(invitedRole),
		State:       invState,
	}, nil
}

// CompleteStaffInvitation completes staff onboarding. Accepts bearer, display name, and password. Accepts NO role field.
func CompleteStaffInvitation(
	ctx context.Context,
	tx pgx.Tx,
	req CompleteStaffInvitationRequest,
) (CompleteStaffInvitationResult, error) {
	digest, err := DigestActionSecret(req.Bearer)
	if err != nil {
		return CompleteStaffInvitationResult{}, ErrInvitationInvalid
	}

	// 1. Lock identity_action_secret
	var secretID string
	var expiresAt time.Time
	var consumedAt, supersededAt *time.Time

	err = tx.QueryRow(ctx,
		`SELECT id::text, expires_at, consumed_at, superseded_at
		   FROM identity_action_secrets
		  WHERE secret_digest = $1 AND purpose = $2
		    FOR UPDATE`,
		digest, string(ActionStaffInvitation),
	).Scan(&secretID, &expiresAt, &consumedAt, &supersededAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return CompleteStaffInvitationResult{}, ErrInvitationInvalid
	}
	if err != nil {
		return CompleteStaffInvitationResult{}, fmt.Errorf("locking invitation action secret: %w", err)
	}

	if consumedAt != nil {
		return CompleteStaffInvitationResult{}, ErrInvitationAlreadyUsed
	}
	if supersededAt != nil {
		return CompleteStaffInvitationResult{}, ErrInvitationSuperseded
	}
	if !req.Now.Before(expiresAt) {
		return CompleteStaffInvitationResult{}, ErrInvitationExpired
	}

	// 2. Lock staff_invitations row
	var invID, normalizedEmail, email, invitedRole, inviterID, invState string

	err = tx.QueryRow(ctx,
		`SELECT id::text, normalized_email, email, invited_role, inviter_account_id::text, state
		   FROM staff_invitations
		  WHERE action_secret_id = $1::uuid
		    FOR UPDATE`,
		secretID,
	).Scan(&invID, &normalizedEmail, &email, &invitedRole, &inviterID, &invState)

	if errors.Is(err, pgx.ErrNoRows) {
		return CompleteStaffInvitationResult{}, ErrInvitationInvalid
	}
	if err != nil {
		return CompleteStaffInvitationResult{}, fmt.Errorf("locking staff invitation row: %w", err)
	}

	if invState == string(InvitationConsumed) {
		return CompleteStaffInvitationResult{}, ErrInvitationAlreadyUsed
	}
	if invState == string(InvitationSuperseded) {
		return CompleteStaffInvitationResult{}, ErrInvitationSuperseded
	}
	if invState == string(InvitationRevoked) {
		return CompleteStaffInvitationResult{}, ErrInvitationRevoked
	}
	if invState != string(InvitationPending) {
		return CompleteStaffInvitationResult{}, ErrInvitationInvalid
	}

	// 3. Verify inviter authority at completion time (I4 & inviter authority check).
	var inviterStatus string
	var inviterRole string
	err = tx.QueryRow(ctx,
		`SELECT status, role::text FROM accounts WHERE id = $1::uuid FOR UPDATE`,
		inviterID,
	).Scan(&inviterStatus, &inviterRole)

	if errors.Is(err, pgx.ErrNoRows) || inviterStatus != string(StatusActive) {
		return CompleteStaffInvitationResult{}, ErrInviterUnauthorized
	}
	if inviterRole != string(RoleAdmin) {
		return CompleteStaffInvitationResult{}, ErrInviterUnauthorized
	}

	// 4. Check if an account already exists for normalizedEmail (or if target suspended I9).
	var existingStatus string
	err = tx.QueryRow(ctx,
		`SELECT status FROM accounts WHERE normalized_email = $1 FOR UPDATE`,
		normalizedEmail,
	).Scan(&existingStatus)

	if err == nil {
		if existingStatus == string(StatusSuspended) {
			return CompleteStaffInvitationResult{}, fmt.Errorf("%w: invitation target is suspended", ErrInvitationInvalid)
		}
		return CompleteStaffInvitationResult{}, ErrAccountAlreadyExists
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return CompleteStaffInvitationResult{}, fmt.Errorf("checking existing target account: %w", err)
	}

	// 5. Validate display name
	displayName, err := ValidateDisplayName(req.DisplayName)
	if err != nil {
		return CompleteStaffInvitationResult{}, fmt.Errorf("invalid display name: %w", err)
	}

	// 6. Validate password, screen for compromised password, and hash inside credential boundary
	hash, err := PrepareNewCredential(ctx, req.Password, req.Compromised)
	if err != nil {
		return CompleteStaffInvitationResult{}, fmt.Errorf("preparing credential: %w", err)
	}

	// 7. Create Account
	newAccountID := uuid.NewString()
	if _, err := tx.Exec(ctx,
		`INSERT INTO accounts
		   (id, normalized_email, email, role, status, display_name, email_verified_at, created_at, updated_at)
		 VALUES ($1::uuid, $2, $3, $4, 'ACTIVE', $5, $6, $6, $6)`,
		newAccountID, normalizedEmail, email, invitedRole, displayName, req.Now,
	); err != nil {
		return CompleteStaffInvitationResult{}, fmt.Errorf("inserting account: %w", err)
	}

	// 8. Create password_credentials
	if _, err := tx.Exec(ctx,
		`INSERT INTO password_credentials
		   (account_id, password_hash, state, created_at)
		 VALUES ($1::uuid, $2, 'ACTIVE', $3)`,
		newAccountID, hash.Expose(), req.Now,
	); err != nil {
		return CompleteStaffInvitationResult{}, fmt.Errorf("inserting password credentials: %w", err)
	}

	// 9. Consume invitation & action secret
	if _, err := tx.Exec(ctx,
		`UPDATE staff_invitations SET state = 'CONSUMED', updated_at = $2 WHERE id = $1::uuid`,
		invID, req.Now,
	); err != nil {
		return CompleteStaffInvitationResult{}, fmt.Errorf("updating invitation state to CONSUMED: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`UPDATE identity_action_secrets
		    SET consumed_at = $2,
		        first_attempt_at = COALESCE(first_attempt_at, $2),
		        last_attempt_at = $2,
		        attempt_count = attempt_count + 1
		  WHERE id = $1::uuid`,
		secretID, req.Now,
	); err != nil {
		return CompleteStaffInvitationResult{}, fmt.Errorf("consuming action secret: %w", err)
	}

	// 10. Write evidence (STAFF_INVITATION_COMPLETED)
	evidence := map[string]any{
		"invitation_id":      invID,
		"invited_role":       invitedRole,
		"inviter_account_id": inviterID,
		"created_account_id": newAccountID,
	}

	if err := appendIdentitySecurityEvent(ctx, tx, securityEventAppend{
		eventType: "STAFF_INVITATION_COMPLETED",
		accountID: newAccountID,
		revision:  1,
		requestID: req.RequestID,
		evidence:  evidence,
	}); err != nil {
		return CompleteStaffInvitationResult{}, fmt.Errorf("writing STAFF_INVITATION_COMPLETED evidence: %w", err)
	}

	return CompleteStaffInvitationResult{
		AccountID:   newAccountID,
		InvitedRole: Role(invitedRole),
	}, nil
}

// RevokeStaffInvitation revokes a pending staff invitation.
func RevokeStaffInvitation(
	ctx context.Context,
	tx pgx.Tx,
	req RevokeStaffInvitationRequest,
) error {
	decision := Authorize(req.ActorPrincipal, CapAdminOperations)
	if !decision.Allowed {
		return fmt.Errorf("%w: %s", ErrUnauthorized, decision.Reason)
	}

	if err := CheckRecentAuthentication(req.ActorSession, req.RecentAuthWindow, req.Now); err != nil {
		return fmt.Errorf("%w: recent authentication check failed", ErrRecentAuthRequired)
	}

	var secretID, state, normalizedEmail, invitedRole string
	err := tx.QueryRow(ctx,
		`SELECT action_secret_id::text, state, normalized_email, invited_role
		   FROM staff_invitations
		  WHERE id = $1::uuid
		    FOR UPDATE`,
		req.InvitationID,
	).Scan(&secretID, &state, &normalizedEmail, &invitedRole)

	if errors.Is(err, pgx.ErrNoRows) {
		return ErrInvitationNotFound
	}
	if err != nil {
		return fmt.Errorf("locking staff invitation for revocation: %w", err)
	}

	if state != string(InvitationPending) {
		// Revocation of non-pending invitation is idempotent or no-op
		return nil
	}

	if _, err := tx.Exec(ctx,
		`UPDATE staff_invitations SET state = 'REVOKED', updated_at = $2 WHERE id = $1::uuid`,
		req.InvitationID, req.Now,
	); err != nil {
		return fmt.Errorf("updating invitation state to REVOKED: %w", err)
	}

	evidence := map[string]any{
		"invitation_id":    req.InvitationID,
		"normalized_email": normalizedEmail,
		"invited_role":     invitedRole,
	}

	if err := appendIdentitySecurityEvent(ctx, tx, securityEventAppend{
		eventType: "STAFF_INVITATION_REVOKED",
		accountID: req.ActorPrincipal.AccountID,
		revision:  1,
		requestID: req.RequestID,
		evidence:  evidence,
	}); err != nil {
		return fmt.Errorf("writing STAFF_INVITATION_REVOKED evidence: %w", err)
	}

	return nil
}

// ListPendingInvitations returns all PENDING invitations. Authorization is the caller's responsibility.
func ListPendingInvitations(
	ctx context.Context,
	tx pgx.Tx,
	actorPrincipal Principal,
) ([]StaffInvitation, error) {
	decision := Authorize(actorPrincipal, CapAdminOperations)
	if !decision.Allowed {
		return nil, fmt.Errorf("%w: %s", ErrUnauthorized, decision.Reason)
	}

	rows, err := tx.Query(ctx,
		`SELECT id::text, normalized_email, email, invited_role, inviter_account_id::text, state, created_at, updated_at
		   FROM staff_invitations
		  WHERE state = 'PENDING'
		  ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("listing pending invitations: %w", err)
	}
	defer rows.Close()

	var invitations []StaffInvitation
	for rows.Next() {
		var inv StaffInvitation
		var role, state string
		if err := rows.Scan(&inv.ID, &inv.NormalizedEmail, &inv.Email, &role, &inv.InviterAccountID, &state, &inv.CreatedAt, &inv.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning invitation row: %w", err)
		}
		inv.InvitedRole = Role(role)
		inv.State = StaffInvitationState(state)
		invitations = append(invitations, inv)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating invitation rows: %w", err)
	}
	return invitations, nil
}
