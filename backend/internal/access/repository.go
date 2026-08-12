package access

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

type Repository struct {
	pool         *pgxpool.Pool
	outboxWriter *outbox.Writer
}

func NewRepository(pool *pgxpool.Pool, outboxWriter *outbox.Writer) (*Repository, error) {
	if pool == nil {
		return nil, errors.New("pgxpool is required")
	}
	if outboxWriter == nil {
		return nil, errors.New("outbox writer is required")
	}
	return &Repository{pool: pool, outboxWriter: outboxWriter}, nil
}

type SetCourseDefaultAccessExpiryParams struct {
	CourseID            string
	AdminAccountID      string
	ActorDescriptor     string
	DefaultAccessEndsAt time.Time
	Reason              string
}

func (r *Repository) SetCourseDefaultAccessExpiry(ctx context.Context, params SetCourseDefaultAccessExpiryParams) error {
	if r == nil || r.pool == nil {
		return errors.New("repository is not initialized")
	}
	if strings.TrimSpace(params.CourseID) == "" {
		return ErrCourseNotFound
	}
	if params.DefaultAccessEndsAt.IsZero() {
		return ErrExpiryRequired
	}
	if strings.TrimSpace(params.Reason) == "" {
		return ErrReasonRequired
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var (
		existingCourseID string
		oldExpiry        *time.Time
	)
	err = tx.QueryRow(ctx, "SELECT id::text, default_access_ends_at FROM courses WHERE id = $1 FOR UPDATE", params.CourseID).Scan(&existingCourseID, &oldExpiry)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrCourseNotFound
	}
	if err != nil {
		return fmt.Errorf("locking course: %w", err)
	}

	tag, err := tx.Exec(ctx, "UPDATE courses SET default_access_ends_at = $1 WHERE id = $2", params.DefaultAccessEndsAt, params.CourseID)
	if err != nil {
		return fmt.Errorf("updating course default_access_ends_at: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrCourseNotFound
	}

	var oldExpiryStr *string
	if oldExpiry != nil && !oldExpiry.IsZero() {
		formatted := oldExpiry.UTC().Format(time.RFC3339)
		oldExpiryStr = &formatted
	}

	metadata, err := json.Marshal(map[string]any{
		"old_default_access_ends_at": oldExpiryStr,
		"new_default_access_ends_at": params.DefaultAccessEndsAt.UTC().Format(time.RFC3339),
	})
	if err != nil {
		return fmt.Errorf("marshaling audit metadata: %w", err)
	}

	actorID := params.AdminAccountID
	if actorID == "" {
		actorID = params.ActorDescriptor
	}

	auditQuery := `
		INSERT INTO audit_events (
			actor_account_id, actor_role, actor_descriptor,
			action, module, target_type, target_id,
			reason, metadata
		) VALUES (
			$1, 'ADMIN', $2,
			'COURSE_DEFAULT_ACCESS_EXPIRY_SET', 'CATALOG_AND_AUTHORING', 'COURSE', $3,
			$4, $5
		)
	`
	_, err = tx.Exec(ctx, auditQuery,
		params.AdminAccountID, params.ActorDescriptor, params.CourseID,
		params.Reason, metadata,
	)
	if err != nil {
		return fmt.Errorf("writing audit event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}

	return nil
}

type CreateInvitationParams struct {
	CourseID          string
	Email             string
	AdminAccountID    string
	AdminNote         *string
	ExternalReference *string
	Locale            identity.Locale
	Now               time.Time
	TTL               time.Duration
}

func (r *Repository) CreateInvitation(ctx context.Context, params CreateInvitationParams) (Invitation, string, error) {
	if r == nil || r.pool == nil || r.outboxWriter == nil {
		return Invitation{}, "", errors.New("repository is not initialized")
	}
	if strings.TrimSpace(params.CourseID) == "" {
		return Invitation{}, "", ErrCourseNotFound
	}
	normalizedEmail, err := identity.NormalizeEmail(params.Email)
	if err != nil {
		return Invitation{}, "", fmt.Errorf("%w: %v", ErrInvalidEmail, err)
	}
	requestedLocale := params.Locale
	if requestedLocale == "" {
		requestedLocale = identity.LocaleArabic
	}
	if !requestedLocale.Valid() {
		return Invitation{}, "", identity.ErrInvalidLocale
	}

	now := params.Now
	if now.IsZero() {
		now = time.Now()
	}
	ttl := params.TTL
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	expiresAt := now.Add(ttl)

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Invitation{}, "", fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Lock Course
	var courseID string
	err = tx.QueryRow(ctx, "SELECT id::text FROM courses WHERE id = $1 FOR SHARE", params.CourseID).Scan(&courseID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Invitation{}, "", ErrCourseNotFound
	}
	if err != nil {
		return Invitation{}, "", fmt.Errorf("locking course: %w", err)
	}

	// Authoritative recipient eligibility check inside transaction (FR-004, BR-082)
	var role string
	recipientLocale := requestedLocale
	err = tx.QueryRow(ctx, "SELECT role, locale FROM accounts WHERE normalized_email = $1", normalizedEmail).Scan(&role, &recipientLocale)
	if err == nil {
		if role != "STUDENT" {
			return Invitation{}, "", ErrIneligibleRecipient
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return Invitation{}, "", fmt.Errorf("checking account eligibility in transaction: %w", err)
	}

	// Generate action secret
	rawBytes := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, rawBytes); err != nil {
		return Invitation{}, "", fmt.Errorf("generating action secret: %w", err)
	}
	bearerToken := base64.RawURLEncoding.EncodeToString(rawBytes)
	digest := sha256.Sum256([]byte(bearerToken))

	actionSecretID := uuid.NewString()
	_, err = tx.Exec(ctx, `
		INSERT INTO identity_action_secrets (
			id, account_id, purpose, secret_digest, issued_at, expires_at
		) VALUES (
			$1::uuid, NULL, 'COURSE_ACCESS_INVITATION', $2, $3, $4
		)
	`, actionSecretID, digest[:], now, expiresAt)
	if err != nil {
		return Invitation{}, "", fmt.Errorf("inserting action secret: %w", err)
	}

	invitationID := uuid.NewString()
	var inv Invitation
	err = tx.QueryRow(ctx, `
		INSERT INTO course_access_invitations (
			id, normalized_email, email, course_id, created_by_account_id,
			state, admin_note, external_reference, action_secret_id, created_at
		) VALUES (
			$1::uuid, $2, $3, $4::uuid, $5::uuid,
			'PENDING_STUDENT_ACCEPTANCE', $6, $7, $8::uuid, $9
		) RETURNING id::text, normalized_email, email, course_id::text, created_by_account_id::text,
		            state, admin_note, external_reference, action_secret_id::text, created_at
	`, invitationID, normalizedEmail, strings.TrimSpace(params.Email), params.CourseID, params.AdminAccountID,
		params.AdminNote, params.ExternalReference, actionSecretID, now,
	).Scan(
		&inv.ID, &inv.NormalizedEmail, &inv.Email, &inv.CourseID, &inv.CreatedByAccountID,
		&inv.State, &inv.AdminNote, &inv.ExternalReference, &inv.ActionSecretID, &inv.CreatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			if pgErr.ConstraintName == "cai_one_non_terminal_per_pair" || pgErr.Code == "23505" {
				return Invitation{}, "", ErrDuplicateInvitation
			}
			if pgErr.ConstraintName == "cai_email_present" {
				return Invitation{}, "", ErrInvalidEmail
			}
		}
		return Invitation{}, "", fmt.Errorf("inserting invitation: %w", err)
	}

	// Write audit event
	metadata, err := json.Marshal(map[string]any{
		"course_id":        params.CourseID,
		"normalized_email": normalizedEmail,
		"action_secret_id": actionSecretID,
	})
	if err != nil {
		return Invitation{}, "", fmt.Errorf("marshaling audit metadata: %w", err)
	}

	auditQuery := `
		INSERT INTO audit_events (
			actor_account_id, actor_role, actor_descriptor,
			action, module, target_type, target_id, reason, metadata
		) VALUES (
			$1::uuid, 'ADMIN', $2,
			'COURSE_ACCESS_INVITATION_ISSUED', 'IDENTITY_AND_ACCESS', 'COURSE_ACCESS_INVITATION', $3,
			'Course access invitation created', $4
		)
	`
	_, err = tx.Exec(ctx, auditQuery, params.AdminAccountID, params.AdminAccountID, inv.ID, metadata)
	if err != nil {
		return Invitation{}, "", fmt.Errorf("writing audit event: %w", err)
	}

	// Write outbox intent
	event := outbox.Event{
		ID:                uuid.NewString(),
		Type:              "access.invitation_issued",
		SchemaVersion:     1,
		SourceModule:      "IDENTITY_AND_ACCESS",
		AggregateType:     "COURSE_ACCESS_INVITATION",
		AggregateID:       inv.ID,
		AggregateRevision: 1,
		CorrelationID:     uuid.NewString(),
		SafePayload: map[string]any{
			"action_secret_id":  actionSecretID,
			"purpose":           string(identity.ActionCourseAccessInvitation),
			"course_id":         params.CourseID,
			"secret_expires_at": expiresAt,
			"locale":            string(recipientLocale),
			"template_contract": "course-access-invitation-v1",
		},
	}
	delivery := outbox.VerificationDelivery{
		Destination:       normalizedEmail,
		Locale:            string(recipientLocale),
		TemplateContract:  "course-access-invitation-v1",
		VerificationToken: bearerToken,
		ExpiresAt:         expiresAt,
	}
	_, err = r.outboxWriter.Append(ctx, tx, event, delivery)
	if err != nil {
		return Invitation{}, "", fmt.Errorf("writing outbox intent: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Invitation{}, "", fmt.Errorf("committing transaction: %w", err)
	}

	return inv, bearerToken, nil
}

type AcceptInvitationParams struct {
	InvitationID    string
	AcceptanceToken string
	CallerAccountID string
	Now             time.Time
}

func (r *Repository) AcceptInvitation(ctx context.Context, params AcceptInvitationParams) (Invitation, error) {
	if r == nil || r.pool == nil {
		return Invitation{}, errors.New("repository is not initialized")
	}
	if strings.TrimSpace(params.InvitationID) == "" {
		return Invitation{}, ErrInvitationNotFound
	}
	if strings.TrimSpace(params.AcceptanceToken) == "" {
		return Invitation{}, ErrAcceptanceTokenExpired
	}

	now := params.Now
	if now.IsZero() {
		now = time.Now()
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Invitation{}, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Fetch caller's normalized email
	var callerNormalizedEmail string
	err = tx.QueryRow(ctx, "SELECT normalized_email FROM accounts WHERE id = $1", params.CallerAccountID).Scan(&callerNormalizedEmail)
	if errors.Is(err, pgx.ErrNoRows) {
		return Invitation{}, ErrInvitationNotFound
	}
	if err != nil {
		return Invitation{}, fmt.Errorf("fetching caller account: %w", err)
	}

	var (
		inv            Invitation
		actionSecretID *string
	)
	err = tx.QueryRow(ctx, `
		SELECT id::text, normalized_email, email, course_id::text, created_by_account_id::text,
		       decided_by_account_id::text, accepted_by_account_id::text, state, decision_reason,
		       admin_note, external_reference, action_secret_id::text, created_at, accepted_at,
		       decided_at, cancelled_at
		  FROM course_access_invitations
		 WHERE id = $1 FOR UPDATE
	`, params.InvitationID).Scan(
		&inv.ID, &inv.NormalizedEmail, &inv.Email, &inv.CourseID, &inv.CreatedByAccountID,
		&inv.DecidedByAccountID, &inv.AcceptedByAccountID, &inv.State, &inv.DecisionReason,
		&inv.AdminNote, &inv.ExternalReference, &actionSecretID, &inv.CreatedAt, &inv.AcceptedAt,
		&inv.DecidedAt, &inv.CancelledAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Invitation{}, ErrInvitationNotFound
	}
	if err != nil {
		return Invitation{}, fmt.Errorf("locking invitation: %w", err)
	}

	// Refuse wrong identity or mismatched email as byte-identical 404 (FR-008, FR-009)
	if inv.NormalizedEmail != callerNormalizedEmail {
		return Invitation{}, ErrInvitationNotFound
	}

	if inv.State != StatePendingStudentAcceptance {
		return Invitation{}, ErrInvitationStateConflict
	}

	if actionSecretID == nil {
		return Invitation{}, ErrAcceptanceTokenExpired
	}

	// Validate action secret
	computedDigest := sha256.Sum256([]byte(params.AcceptanceToken))

	var (
		storedDigest []byte
		expiresAt    time.Time
		consumedAt   *time.Time
		supersededAt *time.Time
	)
	err = tx.QueryRow(ctx, `
		SELECT secret_digest, expires_at, consumed_at, superseded_at
		  FROM identity_action_secrets
		 WHERE id = $1 AND purpose = 'COURSE_ACCESS_INVITATION' FOR UPDATE
	`, *actionSecretID).Scan(&storedDigest, &expiresAt, &consumedAt, &supersededAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Invitation{}, ErrAcceptanceTokenExpired
	}
	if err != nil {
		return Invitation{}, fmt.Errorf("locking action secret: %w", err)
	}

	if consumedAt != nil || supersededAt != nil || expiresAt.Before(now) || expiresAt.Equal(now) {
		return Invitation{}, ErrAcceptanceTokenExpired
	}
	if string(storedDigest) != string(computedDigest[:]) {
		return Invitation{}, ErrAcceptanceTokenExpired
	}

	// Consume action secret
	_, err = tx.Exec(ctx, `UPDATE identity_action_secrets SET consumed_at = $1 WHERE id = $2`, now, *actionSecretID)
	if err != nil {
		return Invitation{}, fmt.Errorf("consuming action secret: %w", err)
	}

	// Transition invitation state to PENDING_ADMIN_APPROVAL
	inv.State = StatePendingAdminApproval
	inv.AcceptedByAccountID = &params.CallerAccountID
	inv.AcceptedAt = &now

	_, err = tx.Exec(ctx, `
		UPDATE course_access_invitations
		   SET state = 'PENDING_ADMIN_APPROVAL',
		       accepted_by_account_id = $1::uuid,
		       accepted_at = $2
		 WHERE id = $3::uuid
	`, params.CallerAccountID, now, inv.ID)
	if err != nil {
		return Invitation{}, fmt.Errorf("updating invitation state: %w", err)
	}

	// Write audit event
	auditQuery := `
		INSERT INTO audit_events (
			actor_account_id, actor_role, actor_descriptor,
			action, module, target_type, target_id, reason
		) VALUES (
			$1::uuid, 'STUDENT', $2,
			'COURSE_ACCESS_INVITATION_ACCEPTED', 'IDENTITY_AND_ACCESS', 'COURSE_ACCESS_INVITATION', $3,
			'Course access invitation accepted'
		)
	`
	_, err = tx.Exec(ctx, auditQuery, params.CallerAccountID, params.CallerAccountID, inv.ID)
	if err != nil {
		return Invitation{}, fmt.Errorf("writing audit event: %w", err)
	}

	// CRITICAL INVARIANT: NO Entitlement or Enrollment row is created!

	if err := tx.Commit(ctx); err != nil {
		return Invitation{}, fmt.Errorf("committing transaction: %w", err)
	}

	return inv, nil
}

func (r *Repository) GetInvitationByID(ctx context.Context, id string) (Invitation, error) {
	if r == nil || r.pool == nil {
		return Invitation{}, errors.New("repository is not initialized")
	}
	var inv Invitation
	var actionSecretID *string
	err := r.pool.QueryRow(ctx, `
		SELECT id::text, normalized_email, email, course_id::text, created_by_account_id::text,
		       decided_by_account_id::text, accepted_by_account_id::text, state, decision_reason,
		       admin_note, external_reference, action_secret_id::text, created_at, accepted_at,
		       decided_at, cancelled_at
		  FROM course_access_invitations
		 WHERE id = $1
	`, id).Scan(
		&inv.ID, &inv.NormalizedEmail, &inv.Email, &inv.CourseID, &inv.CreatedByAccountID,
		&inv.DecidedByAccountID, &inv.AcceptedByAccountID, &inv.State, &inv.DecisionReason,
		&inv.AdminNote, &inv.ExternalReference, &actionSecretID, &inv.CreatedAt, &inv.AcceptedAt,
		&inv.DecidedAt, &inv.CancelledAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Invitation{}, ErrInvitationNotFound
	}
	if err != nil {
		return Invitation{}, fmt.Errorf("getting invitation: %w", err)
	}
	inv.ActionSecretID = actionSecretID
	return inv, nil
}

type ListAdminInvitationsFilter struct {
	State    *State
	CourseID *string
	Limit    int
	Offset   int
}

func (r *Repository) ListAdminInvitations(ctx context.Context, filter ListAdminInvitationsFilter) ([]Invitation, int, error) {
	if r == nil || r.pool == nil {
		return nil, 0, errors.New("repository is not initialized")
	}

	limit := filter.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	whereClauses := []string{"1=1"}
	args := []any{}
	argIdx := 1

	if filter.State != nil && filter.State.Valid() {
		whereClauses = append(whereClauses, fmt.Sprintf("i.state = $%d", argIdx))
		args = append(args, string(*filter.State))
		argIdx++
	}
	if filter.CourseID != nil && strings.TrimSpace(*filter.CourseID) != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("i.course_id = $%d::uuid", argIdx))
		args = append(args, *filter.CourseID)
		argIdx++
	}

	whereStmt := strings.Join(whereClauses, " AND ")

	countQuery := fmt.Sprintf("SELECT count(*) FROM course_access_invitations i WHERE %s", whereStmt)
	var total int
	if err := r.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("counting invitations: %w", err)
	}

	// The entitlement join carries the grant this Invitation produced, so the
	// Admin queue can open Entitlement Detail without anyone handling an ID.
	query := fmt.Sprintf(`
		SELECT i.id::text, i.normalized_email, i.email, i.course_id::text, i.created_by_account_id::text,
		       i.decided_by_account_id::text, i.accepted_by_account_id::text, i.state, i.decision_reason,
		       i.admin_note, i.external_reference, i.action_secret_id::text, i.created_at, i.accepted_at,
		       i.decided_at, i.cancelled_at, e.id::text
		  FROM course_access_invitations i
		  LEFT JOIN entitlements e ON e.source_invitation_id = i.id
		 WHERE %s
		 ORDER BY i.created_at DESC
		 LIMIT $%d OFFSET $%d
	`, whereStmt, argIdx, argIdx+1)

	args = append(args, limit, offset)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("querying invitations: %w", err)
	}
	defer rows.Close()

	var result []Invitation
	for rows.Next() {
		var inv Invitation
		var actionSecretID *string
		var entitlementID *string
		if err := rows.Scan(
			&inv.ID, &inv.NormalizedEmail, &inv.Email, &inv.CourseID, &inv.CreatedByAccountID,
			&inv.DecidedByAccountID, &inv.AcceptedByAccountID, &inv.State, &inv.DecisionReason,
			&inv.AdminNote, &inv.ExternalReference, &actionSecretID, &inv.CreatedAt, &inv.AcceptedAt,
			&inv.DecidedAt, &inv.CancelledAt, &entitlementID,
		); err != nil {
			return nil, 0, fmt.Errorf("scanning invitation: %w", err)
		}
		inv.ActionSecretID = actionSecretID
		inv.EntitlementID = entitlementID
		result = append(result, inv)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterating invitations: %w", err)
	}

	if result == nil {
		result = []Invitation{}
	}

	return result, total, nil
}

func (r *Repository) ListStudentInvitations(ctx context.Context, callerAccountID string) ([]Invitation, error) {
	if r == nil || r.pool == nil {
		return nil, errors.New("repository is not initialized")
	}

	var callerNormalizedEmail string
	err := r.pool.QueryRow(ctx, "SELECT normalized_email FROM accounts WHERE id = $1", callerAccountID).Scan(&callerNormalizedEmail)
	if errors.Is(err, pgx.ErrNoRows) {
		return []Invitation{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fetching caller account: %w", err)
	}

	rows, err := r.pool.Query(ctx, `
		SELECT id::text, normalized_email, email, course_id::text, created_by_account_id::text,
		       decided_by_account_id::text, accepted_by_account_id::text, state, decision_reason,
		       admin_note, external_reference, action_secret_id::text, created_at, accepted_at,
		       decided_at, cancelled_at
		  FROM course_access_invitations
		 WHERE normalized_email = $1
		 ORDER BY created_at DESC
	`, callerNormalizedEmail)
	if err != nil {
		return nil, fmt.Errorf("querying student invitations: %w", err)
	}
	defer rows.Close()

	var result []Invitation
	for rows.Next() {
		var inv Invitation
		var actionSecretID *string
		if err := rows.Scan(
			&inv.ID, &inv.NormalizedEmail, &inv.Email, &inv.CourseID, &inv.CreatedByAccountID,
			&inv.DecidedByAccountID, &inv.AcceptedByAccountID, &inv.State, &inv.DecisionReason,
			&inv.AdminNote, &inv.ExternalReference, &actionSecretID, &inv.CreatedAt, &inv.AcceptedAt,
			&inv.DecidedAt, &inv.CancelledAt,
		); err != nil {
			return nil, fmt.Errorf("scanning student invitation: %w", err)
		}
		inv.ActionSecretID = actionSecretID
		result = append(result, inv)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating student invitations: %w", err)
	}

	if result == nil {
		result = []Invitation{}
	}

	return result, nil
}

func (r *Repository) GetStudentInvitationByID(ctx context.Context, id string, callerAccountID string) (Invitation, error) {
	if r == nil || r.pool == nil {
		return Invitation{}, errors.New("repository is not initialized")
	}

	var callerNormalizedEmail string
	err := r.pool.QueryRow(ctx, "SELECT normalized_email FROM accounts WHERE id = $1", callerAccountID).Scan(&callerNormalizedEmail)
	if errors.Is(err, pgx.ErrNoRows) {
		return Invitation{}, ErrInvitationNotFound
	}
	if err != nil {
		return Invitation{}, fmt.Errorf("fetching caller account: %w", err)
	}

	var inv Invitation
	var actionSecretID *string
	err = r.pool.QueryRow(ctx, `
		SELECT id::text, normalized_email, email, course_id::text, created_by_account_id::text,
		       decided_by_account_id::text, accepted_by_account_id::text, state, decision_reason,
		       admin_note, external_reference, action_secret_id::text, created_at, accepted_at,
		       decided_at, cancelled_at
		  FROM course_access_invitations
		 WHERE id = $1 AND normalized_email = $2
	`, id, callerNormalizedEmail).Scan(
		&inv.ID, &inv.NormalizedEmail, &inv.Email, &inv.CourseID, &inv.CreatedByAccountID,
		&inv.DecidedByAccountID, &inv.AcceptedByAccountID, &inv.State, &inv.DecisionReason,
		&inv.AdminNote, &inv.ExternalReference, &actionSecretID, &inv.CreatedAt, &inv.AcceptedAt,
		&inv.DecidedAt, &inv.CancelledAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Invitation{}, ErrInvitationNotFound
	}
	if err != nil {
		return Invitation{}, fmt.Errorf("getting student invitation: %w", err)
	}
	inv.ActionSecretID = actionSecretID
	return inv, nil
}

func (r *Repository) ApproveInvitation(ctx context.Context, params ApproveInvitationParams) (ApproveInvitationResult, error) {
	if r == nil || r.pool == nil || r.outboxWriter == nil {
		return ApproveInvitationResult{}, errors.New("repository is not initialized")
	}
	invID := strings.TrimSpace(params.InvitationID)
	if invID == "" {
		return ApproveInvitationResult{}, ErrInvitationNotFound
	}
	if _, err := uuid.Parse(invID); err != nil {
		return ApproveInvitationResult{}, ErrInvitationNotFound
	}
	now := params.Now
	if now.IsZero() {
		now = time.Now()
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ApproveInvitationResult{}, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// 1. Lock course_access_invitations FOR UPDATE
	var inv Invitation
	var actionSecretID *string
	err = tx.QueryRow(ctx, `
		SELECT id::text, normalized_email, email, course_id::text, created_by_account_id::text,
		       decided_by_account_id::text, accepted_by_account_id::text, state, decision_reason,
		       admin_note, external_reference, action_secret_id::text, created_at, accepted_at,
		       decided_at, cancelled_at
		  FROM course_access_invitations
		 WHERE id = $1::uuid FOR UPDATE
	`, invID).Scan(
		&inv.ID, &inv.NormalizedEmail, &inv.Email, &inv.CourseID, &inv.CreatedByAccountID,
		&inv.DecidedByAccountID, &inv.AcceptedByAccountID, &inv.State, &inv.DecisionReason,
		&inv.AdminNote, &inv.ExternalReference, &actionSecretID, &inv.CreatedAt, &inv.AcceptedAt,
		&inv.DecidedAt, &inv.CancelledAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ApproveInvitationResult{}, ErrInvitationNotFound
	}
	if err != nil {
		return ApproveInvitationResult{}, fmt.Errorf("locking invitation: %w", err)
	}
	inv.ActionSecretID = actionSecretID

	// 2. If already APPROVED, return existing grant (Idempotent 200, FR-016, Race 1)
	if inv.State == StateApproved {
		var ent Entitlement
		var sourceInvID *string
		var revokedAt *time.Time
		err = tx.QueryRow(ctx, `
			SELECT id::text, student_account_id::text, scope_kind, scope_id::text, course_id::text,
			       grant_source, source_invitation_id::text, original_access_ends_at, access_ends_at,
			       revoked_at, retirement_eligibility_at, state, revision, created_at, updated_at
			  FROM entitlements
			 WHERE source_invitation_id = $1::uuid
			 LIMIT 1
		`, inv.ID).Scan(
			&ent.ID, &ent.StudentAccountID, &ent.ScopeKind, &ent.ScopeID, &ent.CourseID,
			&ent.GrantSource, &sourceInvID, &ent.OriginalAccessEndsAt, &ent.AccessEndsAt,
			&revokedAt, &ent.RetirementEligibilityAt, &ent.State, &ent.Revision, &ent.CreatedAt, &ent.UpdatedAt,
		)
		if err == nil {
			ent.SourceInvitationID = sourceInvID
			ent.RevokedAt = revokedAt
			return ApproveInvitationResult{
				Invitation:  inv,
				Entitlement: ent,
			}, nil
		}
	}

	// 3. Must be in PENDING_ADMIN_APPROVAL
	if inv.State != StatePendingAdminApproval {
		return ApproveInvitationResult{}, ErrInvitationStateConflict
	}

	// 4. Lock Course FOR SHARE and check lifecycle & default_access_ends_at
	var (
		courseLifecycle     string
		defaultAccessEndsAt *time.Time
	)
	err = tx.QueryRow(ctx, `
		SELECT lifecycle, default_access_ends_at
		  FROM courses
		 WHERE id = $1::uuid FOR SHARE
	`, inv.CourseID).Scan(&courseLifecycle, &defaultAccessEndsAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ApproveInvitationResult{}, ErrCourseNotFound
	}
	if err != nil {
		return ApproveInvitationResult{}, fmt.Errorf("locking course: %w", err)
	}

	switch strings.ToUpper(courseLifecycle) {
	case "ARCHIVED", "DELISTED", "RETIRED":
		return ApproveInvitationResult{}, ErrCourseNotGrantable
	}

	if defaultAccessEndsAt == nil || defaultAccessEndsAt.IsZero() {
		return ApproveInvitationResult{}, ErrExpiryRequired
	}
	if !defaultAccessEndsAt.After(now.UTC()) {
		return ApproveInvitationResult{}, ErrExpiryInPast
	}

	// 5. Identify recipient student account
	var studentAccountID string
	if inv.AcceptedByAccountID != nil && *inv.AcceptedByAccountID != "" {
		studentAccountID = *inv.AcceptedByAccountID
	} else {
		err = tx.QueryRow(ctx, "SELECT id::text FROM accounts WHERE normalized_email = $1", inv.NormalizedEmail).Scan(&studentAccountID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ApproveInvitationResult{}, ErrIneligibleRecipient
		}
		if err != nil {
			return ApproveInvitationResult{}, fmt.Errorf("finding recipient account: %w", err)
		}
	}

	var role, status string
	var recipientLocale identity.Locale
	err = tx.QueryRow(ctx, "SELECT role, status, locale FROM accounts WHERE id = $1::uuid", studentAccountID).Scan(&role, &status, &recipientLocale)
	if err != nil || role != "STUDENT" {
		return ApproveInvitationResult{}, ErrIneligibleRecipient
	}

	// 6. Check for active pre-existing entitlement for this student & course
	var existingActiveEntID string
	err = tx.QueryRow(ctx, `
		SELECT id::text FROM entitlements
		 WHERE student_account_id = $1::uuid AND course_id = $2::uuid AND state = 'ACTIVE' AND scope_kind = 'COURSE'
		 FOR UPDATE
	`, studentAccountID, inv.CourseID).Scan(&existingActiveEntID)
	if err == nil {
		return ApproveInvitationResult{}, ErrAlreadyHasActiveAccess
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return ApproveInvitationResult{}, fmt.Errorf("checking active entitlement: %w", err)
	}

	// 7. Create or reuse Enrollment
	var enrollmentID string
	err = tx.QueryRow(ctx, `
		INSERT INTO enrollments (id, student_account_id, course_id, created_at)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4)
		ON CONFLICT (student_account_id, course_id) DO UPDATE SET created_at = enrollments.created_at
		RETURNING id::text
	`, uuid.NewString(), studentAccountID, inv.CourseID, now).Scan(&enrollmentID)
	if err != nil {
		return ApproveInvitationResult{}, fmt.Errorf("creating or reusing enrollment: %w", err)
	}

	// 8. Create Entitlement
	entitlementID := uuid.NewString()
	var ent Entitlement
	var sourceInvID *string
	var revokedAt *time.Time
	err = tx.QueryRow(ctx, `
		INSERT INTO entitlements (
			id, student_account_id, scope_kind, scope_id, course_id,
			grant_source, source_invitation_id, original_access_ends_at,
			access_ends_at, retirement_eligibility_at, state, revision, created_at, updated_at
		) VALUES (
			$1::uuid, $2::uuid, 'COURSE', $3::uuid, $3::uuid,
			'MANUAL_INVITATION', $4::uuid, $5,
			$5, $6, 'ACTIVE', 1, $6, $6
		) RETURNING id::text, student_account_id::text, scope_kind, scope_id::text, course_id::text,
		            grant_source, source_invitation_id::text, original_access_ends_at, access_ends_at,
		            revoked_at, retirement_eligibility_at, state, revision, created_at, updated_at
	`, entitlementID, studentAccountID, inv.CourseID, inv.ID, *defaultAccessEndsAt, now).Scan(
		&ent.ID, &ent.StudentAccountID, &ent.ScopeKind, &ent.ScopeID, &ent.CourseID,
		&ent.GrantSource, &sourceInvID, &ent.OriginalAccessEndsAt, &ent.AccessEndsAt,
		&revokedAt, &ent.RetirementEligibilityAt, &ent.State, &ent.Revision, &ent.CreatedAt, &ent.UpdatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && (pgErr.ConstraintName == "entitlements_one_active_student_course" || pgErr.Code == "23505") {
			return ApproveInvitationResult{}, ErrAlreadyHasActiveAccess
		}
		return ApproveInvitationResult{}, fmt.Errorf("inserting entitlement: %w", err)
	}
	ent.SourceInvitationID = sourceInvID
	ent.RevokedAt = revokedAt

	// 9. Update Invitation to APPROVED
	inv.State = StateApproved
	inv.DecidedByAccountID = &params.AdminAccountID
	inv.DecidedAt = &now
	if inv.AcceptedByAccountID == nil {
		inv.AcceptedByAccountID = &studentAccountID
	}

	_, err = tx.Exec(ctx, `
		UPDATE course_access_invitations
		   SET state = 'APPROVED',
		       decided_by_account_id = $1::uuid,
		       decided_at = $2,
		       accepted_by_account_id = COALESCE(accepted_by_account_id, $3::uuid)
		 WHERE id = $4::uuid
	`, params.AdminAccountID, now, studentAccountID, inv.ID)
	if err != nil {
		return ApproveInvitationResult{}, fmt.Errorf("updating invitation decision: %w", err)
	}

	// 10. Audit events: invitation decided & entitlement granted
	auditDecidedMeta, _ := json.Marshal(map[string]any{
		"course_id":        inv.CourseID,
		"normalized_email": inv.NormalizedEmail,
		"entitlement_id":   ent.ID,
		"enrollment_id":    enrollmentID,
	})
	auditGrantMeta, _ := json.Marshal(map[string]any{
		"course_id":            inv.CourseID,
		"source_invitation_id": inv.ID,
		"access_ends_at":       ent.AccessEndsAt.UTC().Format(time.RFC3339),
	})

	_, err = tx.Exec(ctx, `
		INSERT INTO audit_events (
			actor_account_id, actor_role, actor_descriptor,
			action, module, target_type, target_id, reason, metadata
		) VALUES
		(
			$1::uuid, 'ADMIN', $2,
			'COURSE_ACCESS_INVITATION_DECIDED', 'IDENTITY_AND_ACCESS', 'COURSE_ACCESS_INVITATION', $3,
			'Course access invitation approved', $4
		),
		(
			$1::uuid, 'ADMIN', $2,
			'ENTITLEMENT_GRANTED', 'IDENTITY_AND_ACCESS', 'ENTITLEMENT', $5,
			'Course access entitlement granted by invitation approval', $6
		)
	`, params.AdminAccountID, params.AdminAccountID, inv.ID, auditDecidedMeta, ent.ID, auditGrantMeta)
	if err != nil {
		return ApproveInvitationResult{}, fmt.Errorf("writing audit events: %w", err)
	}

	// 11. Co-commit Outbox Event (access.granted)
	outboxEvt := outbox.Event{
		ID:                uuid.NewString(),
		Type:              "access.granted",
		SchemaVersion:     1,
		SourceModule:      "IDENTITY_AND_ACCESS",
		AggregateType:     "ENTITLEMENT",
		AggregateID:       ent.ID,
		AggregateRevision: 1,
		CorrelationID:     uuid.NewString(),
		SafePayload: map[string]any{
			"entitlement_id":       ent.ID,
			"student_account_id":   studentAccountID,
			"course_id":            inv.CourseID,
			"source_invitation_id": inv.ID,
			"access_ends_at":       ent.AccessEndsAt.UTC().Format(time.RFC3339),
			"locale":               string(recipientLocale),
			"template_contract":    "course-access-granted-v1",
		},
	}
	notice := outbox.NoticeDelivery{
		Destination:      inv.NormalizedEmail,
		Locale:           string(recipientLocale),
		TemplateContract: "course-access-granted-v1",
	}
	_, err = r.outboxWriter.Append(ctx, tx, outboxEvt, notice)
	if err != nil {
		return ApproveInvitationResult{}, fmt.Errorf("writing outbox event in tx: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return ApproveInvitationResult{}, fmt.Errorf("committing grant transaction: %w", err)
	}

	return ApproveInvitationResult{
		Invitation:  inv,
		Entitlement: ent,
	}, nil
}

func (r *Repository) RejectInvitation(ctx context.Context, params RejectInvitationParams) (Invitation, error) {
	if r == nil || r.pool == nil || r.outboxWriter == nil {
		return Invitation{}, errors.New("repository is not initialized")
	}
	invID := strings.TrimSpace(params.InvitationID)
	if invID == "" {
		return Invitation{}, ErrInvitationNotFound
	}
	if _, err := uuid.Parse(invID); err != nil {
		return Invitation{}, ErrInvitationNotFound
	}
	reason := strings.TrimSpace(params.Reason)
	if reason == "" {
		return Invitation{}, ErrReasonRequired
	}
	now := params.Now
	if now.IsZero() {
		now = time.Now()
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Invitation{}, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var inv Invitation
	var actionSecretID *string
	err = tx.QueryRow(ctx, `
		SELECT id::text, normalized_email, email, course_id::text, created_by_account_id::text,
		       decided_by_account_id::text, accepted_by_account_id::text, state, decision_reason,
		       admin_note, external_reference, action_secret_id::text, created_at, accepted_at,
		       decided_at, cancelled_at
		  FROM course_access_invitations
		 WHERE id = $1::uuid FOR UPDATE
	`, invID).Scan(
		&inv.ID, &inv.NormalizedEmail, &inv.Email, &inv.CourseID, &inv.CreatedByAccountID,
		&inv.DecidedByAccountID, &inv.AcceptedByAccountID, &inv.State, &inv.DecisionReason,
		&inv.AdminNote, &inv.ExternalReference, &actionSecretID, &inv.CreatedAt, &inv.AcceptedAt,
		&inv.DecidedAt, &inv.CancelledAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Invitation{}, ErrInvitationNotFound
	}
	if err != nil {
		return Invitation{}, fmt.Errorf("locking invitation: %w", err)
	}
	inv.ActionSecretID = actionSecretID

	if inv.State != StatePendingAdminApproval {
		return Invitation{}, ErrInvitationStateConflict
	}

	inv.State = StateRejected
	inv.DecisionReason = &reason
	inv.DecidedByAccountID = &params.AdminAccountID
	inv.DecidedAt = &now

	_, err = tx.Exec(ctx, `
		UPDATE course_access_invitations
		   SET state = 'REJECTED',
		       decision_reason = $1,
		       decided_by_account_id = $2::uuid,
		       decided_at = $3
		 WHERE id = $4::uuid
	`, reason, params.AdminAccountID, now, inv.ID)
	if err != nil {
		return Invitation{}, fmt.Errorf("updating invitation rejection: %w", err)
	}

	metadata, _ := json.Marshal(map[string]any{
		"course_id":        inv.CourseID,
		"normalized_email": inv.NormalizedEmail,
		"reason":           reason,
	})
	_, err = tx.Exec(ctx, `
		INSERT INTO audit_events (
			actor_account_id, actor_role, actor_descriptor,
			action, module, target_type, target_id, reason, metadata
		) VALUES (
			$1::uuid, 'ADMIN', $2,
			'COURSE_ACCESS_INVITATION_DECIDED', 'IDENTITY_AND_ACCESS', 'COURSE_ACCESS_INVITATION', $3,
			'Course access invitation rejected', $4
		)
	`, params.AdminAccountID, params.AdminAccountID, inv.ID, metadata)
	if err != nil {
		return Invitation{}, fmt.Errorf("writing audit event: %w", err)
	}

	var recipientLocale identity.Locale
	if err := tx.QueryRow(ctx, `SELECT locale FROM accounts WHERE normalized_email=$1`, inv.NormalizedEmail).Scan(&recipientLocale); err != nil {
		return Invitation{}, fmt.Errorf("resolving rejected invitation locale: %w", err)
	}
	rejectedEvent := outbox.Event{
		ID: uuid.NewString(), Type: "access.invitation_rejected", SchemaVersion: 1,
		SourceModule: "IDENTITY_AND_ACCESS", AggregateType: "COURSE_ACCESS_INVITATION",
		AggregateID: inv.ID, AggregateRevision: 2, CorrelationID: uuid.NewString(),
		SafePayload: map[string]any{
			"course_id": inv.CourseID, "locale": string(recipientLocale),
			"template_contract": "course-access-invitation-rejected-v1",
		},
	}
	if _, err := r.outboxWriter.Append(ctx, tx, rejectedEvent, outbox.NoticeDelivery{
		Destination: inv.NormalizedEmail, Locale: string(recipientLocale),
		TemplateContract: "course-access-invitation-rejected-v1",
	}); err != nil {
		return Invitation{}, fmt.Errorf("writing rejected invitation notification intent: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Invitation{}, fmt.Errorf("committing transaction: %w", err)
	}

	return inv, nil
}

func (r *Repository) CancelInvitation(ctx context.Context, params CancelInvitationParams) (Invitation, error) {
	if r == nil || r.pool == nil || r.outboxWriter == nil {
		return Invitation{}, errors.New("repository is not initialized")
	}
	invID := strings.TrimSpace(params.InvitationID)
	if invID == "" {
		return Invitation{}, ErrInvitationNotFound
	}
	if _, err := uuid.Parse(invID); err != nil {
		return Invitation{}, ErrInvitationNotFound
	}
	now := params.Now
	if now.IsZero() {
		now = time.Now()
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Invitation{}, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var inv Invitation
	var actionSecretID *string
	err = tx.QueryRow(ctx, `
		SELECT id::text, normalized_email, email, course_id::text, created_by_account_id::text,
		       decided_by_account_id::text, accepted_by_account_id::text, state, decision_reason,
		       admin_note, external_reference, action_secret_id::text, created_at, accepted_at,
		       decided_at, cancelled_at
		  FROM course_access_invitations
		 WHERE id = $1::uuid FOR UPDATE
	`, invID).Scan(
		&inv.ID, &inv.NormalizedEmail, &inv.Email, &inv.CourseID, &inv.CreatedByAccountID,
		&inv.DecidedByAccountID, &inv.AcceptedByAccountID, &inv.State, &inv.DecisionReason,
		&inv.AdminNote, &inv.ExternalReference, &actionSecretID, &inv.CreatedAt, &inv.AcceptedAt,
		&inv.DecidedAt, &inv.CancelledAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Invitation{}, ErrInvitationNotFound
	}
	if err != nil {
		return Invitation{}, fmt.Errorf("locking invitation: %w", err)
	}
	inv.ActionSecretID = actionSecretID

	if inv.State.IsTerminal() {
		return Invitation{}, ErrInvitationStateConflict
	}

	inv.State = StateCancelled
	inv.CancelledAt = &now

	_, err = tx.Exec(ctx, `
		UPDATE course_access_invitations
		   SET state = 'CANCELLED',
		       cancelled_at = $1
		 WHERE id = $2::uuid
	`, now, inv.ID)
	if err != nil {
		return Invitation{}, fmt.Errorf("updating invitation cancellation: %w", err)
	}

	if actionSecretID != nil && strings.TrimSpace(*actionSecretID) != "" {
		if _, err := uuid.Parse(*actionSecretID); err == nil {
			_, err = tx.Exec(ctx, `UPDATE identity_action_secrets SET consumed_at = $1 WHERE id = $2::uuid AND consumed_at IS NULL`, now, *actionSecretID)
			if err != nil {
				return Invitation{}, fmt.Errorf("consuming action secret: %w", err)
			}
		}
	}

	metadata, _ := json.Marshal(map[string]any{
		"course_id":        inv.CourseID,
		"normalized_email": inv.NormalizedEmail,
	})
	_, err = tx.Exec(ctx, `
		INSERT INTO audit_events (
			actor_account_id, actor_role, actor_descriptor,
			action, module, target_type, target_id, reason, metadata
		) VALUES (
			$1::uuid, 'ADMIN', $2,
			'COURSE_ACCESS_INVITATION_CANCELLED', 'IDENTITY_AND_ACCESS', 'COURSE_ACCESS_INVITATION', $3,
			'Course access invitation cancelled', $4
		)
	`, params.AdminAccountID, params.AdminAccountID, inv.ID, metadata)
	if err != nil {
		return Invitation{}, fmt.Errorf("writing audit event: %w", err)
	}

	recipientLocale := identity.LocaleArabic
	localeErr := tx.QueryRow(ctx, `SELECT locale FROM accounts WHERE normalized_email=$1`, inv.NormalizedEmail).Scan(&recipientLocale)
	if errors.Is(localeErr, pgx.ErrNoRows) {
		localeErr = tx.QueryRow(ctx, `SELECT safe_payload->>'locale' FROM outbox_events
			WHERE event_type='access.invitation_issued' AND aggregate_type='COURSE_ACCESS_INVITATION'
			AND aggregate_id=$1::uuid ORDER BY occurred_at ASC LIMIT 1`, inv.ID).Scan(&recipientLocale)
	}
	if localeErr != nil && !errors.Is(localeErr, pgx.ErrNoRows) {
		return Invitation{}, fmt.Errorf("resolving cancelled invitation locale: %w", localeErr)
	}
	cancelledEvent := outbox.Event{
		ID: uuid.NewString(), Type: "access.invitation_cancelled", SchemaVersion: 1,
		SourceModule: "IDENTITY_AND_ACCESS", AggregateType: "COURSE_ACCESS_INVITATION",
		AggregateID: inv.ID, AggregateRevision: 2, CorrelationID: uuid.NewString(),
		SafePayload: map[string]any{
			"course_id": inv.CourseID, "locale": string(recipientLocale),
			"template_contract": "course-access-invitation-cancelled-v1",
		},
	}
	if _, err := r.outboxWriter.Append(ctx, tx, cancelledEvent, outbox.NoticeDelivery{
		Destination: inv.NormalizedEmail, Locale: string(recipientLocale),
		TemplateContract: "course-access-invitation-cancelled-v1",
	}); err != nil {
		return Invitation{}, fmt.Errorf("writing cancelled invitation notification intent: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Invitation{}, fmt.Errorf("committing transaction: %w", err)
	}

	return inv, nil
}

func (r *Repository) ResendInvitation(ctx context.Context, params ResendInvitationParams) (Invitation, string, error) {
	if r == nil || r.pool == nil || r.outboxWriter == nil {
		return Invitation{}, "", errors.New("repository is not initialized")
	}
	invID := strings.TrimSpace(params.InvitationID)
	if invID == "" {
		return Invitation{}, "", ErrInvitationNotFound
	}
	if _, err := uuid.Parse(invID); err != nil {
		return Invitation{}, "", ErrInvitationNotFound
	}
	now := params.Now
	if now.IsZero() {
		now = time.Now()
	}
	ttl := params.TTL
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	expiresAt := now.Add(ttl)

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Invitation{}, "", fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var inv Invitation
	var actionSecretID *string
	err = tx.QueryRow(ctx, `
		SELECT id::text, normalized_email, email, course_id::text, created_by_account_id::text,
		       decided_by_account_id::text, accepted_by_account_id::text, state, decision_reason,
		       admin_note, external_reference, action_secret_id::text, created_at, accepted_at,
		       decided_at, cancelled_at
		  FROM course_access_invitations
		 WHERE id = $1::uuid FOR UPDATE
	`, invID).Scan(
		&inv.ID, &inv.NormalizedEmail, &inv.Email, &inv.CourseID, &inv.CreatedByAccountID,
		&inv.DecidedByAccountID, &inv.AcceptedByAccountID, &inv.State, &inv.DecisionReason,
		&inv.AdminNote, &inv.ExternalReference, &actionSecretID, &inv.CreatedAt, &inv.AcceptedAt,
		&inv.DecidedAt, &inv.CancelledAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Invitation{}, "", ErrInvitationNotFound
	}
	if err != nil {
		return Invitation{}, "", fmt.Errorf("locking invitation: %w", err)
	}

	if inv.State != StatePendingStudentAcceptance {
		return Invitation{}, "", ErrInvitationStateConflict
	}

	if actionSecretID != nil {
		_, _ = tx.Exec(ctx, `UPDATE identity_action_secrets SET consumed_at = $1 WHERE id = $2::uuid`, now, *actionSecretID)
	}

	rawBytes := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, rawBytes); err != nil {
		return Invitation{}, "", fmt.Errorf("generating action secret: %w", err)
	}
	bearerToken := base64.RawURLEncoding.EncodeToString(rawBytes)
	digest := sha256.Sum256([]byte(bearerToken))

	newActionSecretID := uuid.NewString()
	_, err = tx.Exec(ctx, `
		INSERT INTO identity_action_secrets (
			id, account_id, purpose, secret_digest, issued_at, expires_at
		) VALUES (
			$1::uuid, NULL, 'COURSE_ACCESS_INVITATION', $2, $3, $4
		)
	`, newActionSecretID, digest[:], now, expiresAt)
	if err != nil {
		return Invitation{}, "", fmt.Errorf("inserting action secret: %w", err)
	}

	inv.ActionSecretID = &newActionSecretID

	_, err = tx.Exec(ctx, `
		UPDATE course_access_invitations
		   SET action_secret_id = $1::uuid
		 WHERE id = $2::uuid
	`, newActionSecretID, inv.ID)
	if err != nil {
		return Invitation{}, "", fmt.Errorf("updating invitation action secret: %w", err)
	}

	metadata, _ := json.Marshal(map[string]any{
		"course_id":        inv.CourseID,
		"normalized_email": inv.NormalizedEmail,
		"action_secret_id": newActionSecretID,
	})
	_, err = tx.Exec(ctx, `
		INSERT INTO audit_events (
			actor_account_id, actor_role, actor_descriptor,
			action, module, target_type, target_id, reason, metadata
		) VALUES (
			$1::uuid, 'ADMIN', $2,
			'COURSE_ACCESS_INVITATION_LINK_REISSUED', 'IDENTITY_AND_ACCESS', 'COURSE_ACCESS_INVITATION', $3,
			'Course access invitation link reissued', $4
		)
	`, params.AdminAccountID, params.AdminAccountID, inv.ID, metadata)
	if err != nil {
		return Invitation{}, "", fmt.Errorf("writing audit event: %w", err)
	}
	recipientLocale := identity.LocaleArabic
	localeErr := tx.QueryRow(ctx, `SELECT locale FROM accounts WHERE normalized_email=$1`, inv.NormalizedEmail).Scan(&recipientLocale)
	if errors.Is(localeErr, pgx.ErrNoRows) {
		localeErr = tx.QueryRow(ctx, `SELECT safe_payload->>'locale' FROM outbox_events
			WHERE event_type='access.invitation_issued' AND aggregate_id=$1::uuid
			ORDER BY occurred_at ASC LIMIT 1`, inv.ID).Scan(&recipientLocale)
	}
	if localeErr != nil && !errors.Is(localeErr, pgx.ErrNoRows) {
		return Invitation{}, "", fmt.Errorf("resolving invitation locale: %w", localeErr)
	}

	outboxEvt := outbox.Event{
		ID:                uuid.NewString(),
		Type:              "access.invitation_issued",
		SchemaVersion:     1,
		SourceModule:      "IDENTITY_AND_ACCESS",
		AggregateType:     "COURSE_ACCESS_INVITATION",
		AggregateID:       inv.ID,
		AggregateRevision: 1,
		CorrelationID:     uuid.NewString(),
		SafePayload: map[string]any{
			"action_secret_id":  newActionSecretID,
			"purpose":           string(identity.ActionCourseAccessInvitation),
			"course_id":         inv.CourseID,
			"secret_expires_at": expiresAt,
			"locale":            string(recipientLocale),
			"template_contract": "course-access-invitation-v1",
		},
	}
	delivery := outbox.VerificationDelivery{
		Destination:       inv.NormalizedEmail,
		Locale:            string(recipientLocale),
		TemplateContract:  "course-access-invitation-v1",
		VerificationToken: bearerToken,
		ExpiresAt:         expiresAt,
	}
	_, err = r.outboxWriter.Append(ctx, tx, outboxEvt, delivery)
	if err != nil {
		return Invitation{}, "", fmt.Errorf("writing outbox event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Invitation{}, "", fmt.Errorf("committing transaction: %w", err)
	}

	return inv, bearerToken, nil
}

func (r *Repository) GetStudentAccessHistory(ctx context.Context, callerAccountID string) (StudentCourseAccessHistoryResponse, error) {
	if r == nil || r.pool == nil {
		return StudentCourseAccessHistoryResponse{}, errors.New("repository is not initialized")
	}

	var callerNormalizedEmail string
	err := r.pool.QueryRow(ctx, "SELECT normalized_email FROM accounts WHERE id = $1", callerAccountID).Scan(&callerNormalizedEmail)
	if errors.Is(err, pgx.ErrNoRows) {
		return StudentCourseAccessHistoryResponse{Items: []StudentCourseAccessHistoryItem{}}, nil
	}
	if err != nil {
		return StudentCourseAccessHistoryResponse{}, fmt.Errorf("fetching caller account: %w", err)
	}

	invitations, err := r.ListStudentInvitations(ctx, callerAccountID)
	if err != nil {
		return StudentCourseAccessHistoryResponse{}, err
	}

	rows, err := r.pool.Query(ctx, `
		SELECT course_id::text, access_ends_at
		  FROM entitlements
		 WHERE student_account_id = $1::uuid AND state = 'ACTIVE' AND scope_kind = 'COURSE'
	`, callerAccountID)
	if err != nil {
		return StudentCourseAccessHistoryResponse{}, fmt.Errorf("querying entitlements: %w", err)
	}
	defer rows.Close()

	activeEntitlements := make(map[string]time.Time)
	for rows.Next() {
		var cID string
		var endsAt time.Time
		if err := rows.Scan(&cID, &endsAt); err == nil {
			activeEntitlements[cID] = endsAt
		}
	}

	itemsByCourse := make(map[string]*StudentCourseAccessHistoryItem)
	for _, inv := range invitations {
		proj := inv.ToStudentProjection()
		item, exists := itemsByCourse[inv.CourseID]
		if !exists {
			item = &StudentCourseAccessHistoryItem{
				CourseID: inv.CourseID,
			}
			itemsByCourse[inv.CourseID] = item
		}
		if item.Invitation == nil {
			item.Invitation = &proj
		}
	}

	for cID, endsAt := range activeEntitlements {
		item, exists := itemsByCourse[cID]
		if !exists {
			item = &StudentCourseAccessHistoryItem{
				CourseID: cID,
			}
			itemsByCourse[cID] = item
		}
		item.HasActiveAccess = true
		endsAtCopy := endsAt
		item.AccessEndsAt = &endsAtCopy
	}

	items := make([]StudentCourseAccessHistoryItem, 0, len(itemsByCourse))
	for _, item := range itemsByCourse {
		items = append(items, *item)
	}
	if items == nil {
		items = []StudentCourseAccessHistoryItem{}
	}

	return StudentCourseAccessHistoryResponse{Items: items}, nil
}

// lockedEntitlement is the shared precondition of both elevated-Admin
// mutations: the row is locked for update, exists, is not already revoked,
// and matches the revision the Admin was looking at.
func lockEntitlementForAdjustment(
	ctx context.Context,
	tx pgx.Tx,
	entitlementID string,
	expectedRevision int64,
) (Entitlement, error) {
	var ent Entitlement
	var sourceInvID *string
	err := tx.QueryRow(ctx, `
		SELECT id::text, student_account_id::text, scope_kind, scope_id::text, course_id::text,
		       grant_source, source_invitation_id::text, original_access_ends_at, access_ends_at,
		       revoked_at, retirement_eligibility_at, state, revision, created_at, updated_at
		  FROM entitlements
		 WHERE id = $1::uuid
		 FOR UPDATE
	`, entitlementID).Scan(
		&ent.ID, &ent.StudentAccountID, &ent.ScopeKind, &ent.ScopeID, &ent.CourseID,
		&ent.GrantSource, &sourceInvID, &ent.OriginalAccessEndsAt, &ent.AccessEndsAt,
		&ent.RevokedAt, &ent.RetirementEligibilityAt, &ent.State, &ent.Revision,
		&ent.CreatedAt, &ent.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Entitlement{}, ErrEntitlementNotFound
	}
	if err != nil {
		return Entitlement{}, fmt.Errorf("locking entitlement: %w", err)
	}
	ent.SourceInvitationID = sourceInvID
	if ent.State == "REVOKED" || ent.RevokedAt != nil {
		return Entitlement{}, ErrEntitlementRevoked
	}
	if expectedRevision > 0 && ent.Revision != expectedRevision {
		return Entitlement{}, ErrEntitlementStale
	}
	return ent, nil
}

// entitlementActorDescriptor keeps the audit actor descriptor present, which
// the audit table requires, without letting a caller omission turn into a
// constraint failure mid-transaction.
func entitlementActorDescriptor(descriptor, adminAccountID string) string {
	if trimmed := strings.TrimSpace(descriptor); trimmed != "" {
		return trimmed
	}
	return adminAccountID
}

// entitlementRecipient reads the Student identity the notification event is
// addressed to. The Student is notified of a change to their own access.
func entitlementRecipient(ctx context.Context, tx pgx.Tx, studentAccountID string) (string, identity.Locale, error) {
	var email string
	var locale identity.Locale
	err := tx.QueryRow(ctx,
		`SELECT normalized_email, locale FROM accounts WHERE id = $1::uuid`, studentAccountID,
	).Scan(&email, &locale)
	if err != nil {
		return "", "", fmt.Errorf("loading entitlement recipient: %w", err)
	}
	if !locale.Valid() {
		locale = identity.LocaleArabic
	}
	return email, locale, nil
}

// AdjustEntitlementExpiry moves the effective `access_ends_at` of an existing
// entitlement later (extend) or earlier (shorten) in one transaction.
//
// BR-026: the effective instant changes only through an adjustment that
// atomically records old expiry, new expiry, reason, actor, timestamp and any
// support reference, with immutable Audit evidence and a transactional
// Student-notification event. `original_access_ends_at` is never touched, and
// a new instant in the past ends access immediately without deleting
// Enrollment, Progress, Invitation, or adjustment history.
func (r *Repository) AdjustEntitlementExpiry(
	ctx context.Context,
	params AdjustEntitlementExpiryParams,
) (AdminEntitlementDetail, error) {
	if r == nil || r.pool == nil || r.outboxWriter == nil {
		return AdminEntitlementDetail{}, errors.New("repository is not initialized")
	}
	if strings.TrimSpace(params.EntitlementID) == "" {
		return AdminEntitlementDetail{}, ErrEntitlementNotFound
	}
	if _, err := uuid.Parse(strings.TrimSpace(params.EntitlementID)); err != nil {
		return AdminEntitlementDetail{}, ErrEntitlementNotFound
	}
	if strings.TrimSpace(params.AdminAccountID) == "" {
		return AdminEntitlementDetail{}, errors.New("admin account ID is required")
	}
	if strings.TrimSpace(params.Reason) == "" {
		return AdminEntitlementDetail{}, ErrReasonRequired
	}
	if params.NewAccessEndsAt.IsZero() {
		return AdminEntitlementDetail{}, ErrExpiryRequired
	}
	now := params.Now
	if now.IsZero() {
		now = time.Now()
	}
	now = now.UTC()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return AdminEntitlementDetail{}, fmt.Errorf("beginning adjustment transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	ent, err := lockEntitlementForAdjustment(ctx, tx, params.EntitlementID, params.ExpectedRevision)
	if err != nil {
		return AdminEntitlementDetail{}, err
	}
	oldAccessEndsAt := ent.AccessEndsAt

	if err := tx.QueryRow(ctx, `
		UPDATE entitlements
		   SET access_ends_at = $1, revision = revision + 1, updated_at = $2
		 WHERE id = $3::uuid
		 RETURNING access_ends_at, revision, updated_at
	`, params.NewAccessEndsAt.UTC(), now, ent.ID).Scan(&ent.AccessEndsAt, &ent.Revision, &ent.UpdatedAt); err != nil {
		return AdminEntitlementDetail{}, fmt.Errorf("updating entitlement expiry: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO entitlement_adjustments (
			entitlement_id, old_access_ends_at, new_access_ends_at,
			reason, actor_account_id, support_reference, adjusted_at
		) VALUES ($1::uuid, $2, $3, $4, $5::uuid, $6, $7)
	`, ent.ID, oldAccessEndsAt, ent.AccessEndsAt, params.Reason,
		params.AdminAccountID, params.SupportReference, now); err != nil {
		return AdminEntitlementDetail{}, fmt.Errorf("recording entitlement adjustment: %w", err)
	}

	auditMeta, err := json.Marshal(map[string]any{
		"course_id":             ent.CourseID,
		"student_account_id":    ent.StudentAccountID,
		"old_access_ends_at":    oldAccessEndsAt.UTC().Format(time.RFC3339),
		"new_access_ends_at":    ent.AccessEndsAt.UTC().Format(time.RFC3339),
		"support_reference":     params.SupportReference,
		"entitlement_revision":  ent.Revision,
		"original_access_ends":  ent.OriginalAccessEndsAt.UTC().Format(time.RFC3339),
		"ends_access_immediate": !now.Before(ent.AccessEndsAt.UTC()),
	})
	if err != nil {
		return AdminEntitlementDetail{}, fmt.Errorf("marshaling adjustment audit metadata: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_events (
			actor_account_id, actor_role, actor_descriptor,
			action, module, target_type, target_id, reason, metadata
		) VALUES (
			$1::uuid, 'ADMIN', $2,
			'ENTITLEMENT_EXPIRY_ADJUSTED', 'IDENTITY_AND_ACCESS', 'ENTITLEMENT', $3,
			$4, $5
		)
	`, params.AdminAccountID, entitlementActorDescriptor(params.ActorDescriptor, params.AdminAccountID),
		ent.ID, params.Reason, auditMeta); err != nil {
		return AdminEntitlementDetail{}, fmt.Errorf("writing adjustment audit event: %w", err)
	}

	recipient, locale, err := entitlementRecipient(ctx, tx, ent.StudentAccountID)
	if err != nil {
		return AdminEntitlementDetail{}, err
	}
	if _, err := r.outboxWriter.Append(ctx, tx, outbox.Event{
		ID:                uuid.NewString(),
		Type:              "access.entitlement_adjusted",
		SchemaVersion:     1,
		SourceModule:      "IDENTITY_AND_ACCESS",
		AggregateType:     "ENTITLEMENT",
		AggregateID:       ent.ID,
		AggregateRevision: int(ent.Revision),
		CorrelationID:     uuid.NewString(),
		SafePayload: map[string]any{
			"entitlement_id":     ent.ID,
			"student_account_id": ent.StudentAccountID,
			"course_id":          ent.CourseID,
			"access_ends_at":     ent.AccessEndsAt.UTC().Format(time.RFC3339),
			"locale":             string(locale),
			"template_contract":  "course-access-adjusted-v1",
		},
	}, outbox.NoticeDelivery{
		Destination:      recipient,
		Locale:           string(locale),
		TemplateContract: "course-access-adjusted-v1",
	}); err != nil {
		return AdminEntitlementDetail{}, fmt.Errorf("writing adjustment outbox event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return AdminEntitlementDetail{}, fmt.Errorf("committing adjustment transaction: %w", err)
	}

	return r.GetAdminEntitlementByID(ctx, ent.ID)
}

// RevokeEntitlement ends an active grant explicitly.
//
// Revocation is a lifecycle transition, never a deletion: the entitlement row
// keeps its history and becomes REVOKED with a revocation instant, which the
// entitlement evaluator already treats as no applicable grant. Enrollment,
// Progress, the originating Invitation and adjustment history are untouched
// (BR-026, AD07).
func (r *Repository) RevokeEntitlement(
	ctx context.Context,
	params RevokeEntitlementParams,
) (AdminEntitlementDetail, error) {
	if r == nil || r.pool == nil || r.outboxWriter == nil {
		return AdminEntitlementDetail{}, errors.New("repository is not initialized")
	}
	if strings.TrimSpace(params.EntitlementID) == "" {
		return AdminEntitlementDetail{}, ErrEntitlementNotFound
	}
	if _, err := uuid.Parse(strings.TrimSpace(params.EntitlementID)); err != nil {
		return AdminEntitlementDetail{}, ErrEntitlementNotFound
	}
	if strings.TrimSpace(params.AdminAccountID) == "" {
		return AdminEntitlementDetail{}, errors.New("admin account ID is required")
	}
	if strings.TrimSpace(params.Reason) == "" {
		return AdminEntitlementDetail{}, ErrReasonRequired
	}
	now := params.Now
	if now.IsZero() {
		now = time.Now()
	}
	now = now.UTC()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return AdminEntitlementDetail{}, fmt.Errorf("beginning revocation transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	ent, err := lockEntitlementForAdjustment(ctx, tx, params.EntitlementID, params.ExpectedRevision)
	if err != nil {
		return AdminEntitlementDetail{}, err
	}

	if err := tx.QueryRow(ctx, `
		UPDATE entitlements
		   SET state = 'REVOKED', revoked_at = $1, revision = revision + 1, updated_at = $1
		 WHERE id = $2::uuid
		 RETURNING state, revoked_at, revision, updated_at
	`, now, ent.ID).Scan(&ent.State, &ent.RevokedAt, &ent.Revision, &ent.UpdatedAt); err != nil {
		return AdminEntitlementDetail{}, fmt.Errorf("revoking entitlement: %w", err)
	}

	auditMeta, err := json.Marshal(map[string]any{
		"course_id":            ent.CourseID,
		"student_account_id":   ent.StudentAccountID,
		"access_ends_at":       ent.AccessEndsAt.UTC().Format(time.RFC3339),
		"revoked_at":           now.Format(time.RFC3339),
		"support_reference":    params.SupportReference,
		"entitlement_revision": ent.Revision,
	})
	if err != nil {
		return AdminEntitlementDetail{}, fmt.Errorf("marshaling revocation audit metadata: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_events (
			actor_account_id, actor_role, actor_descriptor,
			action, module, target_type, target_id, reason, metadata
		) VALUES (
			$1::uuid, 'ADMIN', $2,
			'ENTITLEMENT_REVOKED', 'IDENTITY_AND_ACCESS', 'ENTITLEMENT', $3,
			$4, $5
		)
	`, params.AdminAccountID, entitlementActorDescriptor(params.ActorDescriptor, params.AdminAccountID),
		ent.ID, params.Reason, auditMeta); err != nil {
		return AdminEntitlementDetail{}, fmt.Errorf("writing revocation audit event: %w", err)
	}

	recipient, locale, err := entitlementRecipient(ctx, tx, ent.StudentAccountID)
	if err != nil {
		return AdminEntitlementDetail{}, err
	}
	if _, err := r.outboxWriter.Append(ctx, tx, outbox.Event{
		ID:                uuid.NewString(),
		Type:              "access.entitlement_revoked",
		SchemaVersion:     1,
		SourceModule:      "IDENTITY_AND_ACCESS",
		AggregateType:     "ENTITLEMENT",
		AggregateID:       ent.ID,
		AggregateRevision: int(ent.Revision),
		CorrelationID:     uuid.NewString(),
		SafePayload: map[string]any{
			"entitlement_id":     ent.ID,
			"student_account_id": ent.StudentAccountID,
			"course_id":          ent.CourseID,
			"revoked_at":         now.Format(time.RFC3339),
			"locale":             string(locale),
			"template_contract":  "course-access-revoked-v1",
		},
	}, outbox.NoticeDelivery{
		Destination:      recipient,
		Locale:           string(locale),
		TemplateContract: "course-access-revoked-v1",
	}); err != nil {
		return AdminEntitlementDetail{}, fmt.Errorf("writing revocation outbox event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return AdminEntitlementDetail{}, fmt.Errorf("committing revocation transaction: %w", err)
	}

	return r.GetAdminEntitlementByID(ctx, ent.ID)
}

func (r *Repository) GetAdminEntitlementByID(ctx context.Context, entitlementID string) (AdminEntitlementDetail, error) {
	if r == nil || r.pool == nil {
		return AdminEntitlementDetail{}, errors.New("repository is not initialized")
	}
	eID := strings.TrimSpace(entitlementID)
	if eID == "" {
		return AdminEntitlementDetail{}, errors.New("entitlement not found")
	}
	if _, err := uuid.Parse(eID); err != nil {
		return AdminEntitlementDetail{}, errors.New("entitlement not found")
	}

	var ent Entitlement
	var sourceInvID *string
	var revokedAt *time.Time
	err := r.pool.QueryRow(ctx, `
		SELECT id::text, student_account_id::text, scope_kind, scope_id::text, course_id::text,
		       grant_source, source_invitation_id::text, original_access_ends_at, access_ends_at,
		       revoked_at, retirement_eligibility_at, state, revision, created_at, updated_at
		  FROM entitlements
		 WHERE id = $1::uuid
	`, eID).Scan(
		&ent.ID, &ent.StudentAccountID, &ent.ScopeKind, &ent.ScopeID, &ent.CourseID,
		&ent.GrantSource, &sourceInvID, &ent.OriginalAccessEndsAt, &ent.AccessEndsAt,
		&revokedAt, &ent.RetirementEligibilityAt, &ent.State, &ent.Revision, &ent.CreatedAt, &ent.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return AdminEntitlementDetail{}, errors.New("entitlement not found")
	}
	if err != nil {
		return AdminEntitlementDetail{}, fmt.Errorf("getting entitlement: %w", err)
	}
	ent.SourceInvitationID = sourceInvID
	ent.RevokedAt = revokedAt

	rows, err := r.pool.Query(ctx, `
		SELECT id::text, entitlement_id::text, old_access_ends_at, new_access_ends_at,
		       reason, actor_account_id::text, support_reference, adjusted_at
		  FROM entitlement_adjustments
		 WHERE entitlement_id = $1::uuid
		 ORDER BY adjusted_at ASC
	`, eID)
	if err != nil {
		return AdminEntitlementDetail{}, fmt.Errorf("querying adjustments: %w", err)
	}
	defer rows.Close()

	var adjustments []EntitlementAdjustment
	for rows.Next() {
		var adj EntitlementAdjustment
		if err := rows.Scan(
			&adj.ID, &adj.EntitlementID, &adj.OldAccessEndsAt, &adj.NewAccessEndsAt,
			&adj.Reason, &adj.ActorAccountID, &adj.SupportReference, &adj.AdjustedAt,
		); err == nil {
			adjustments = append(adjustments, adj)
		}
	}
	if adjustments == nil {
		adjustments = []EntitlementAdjustment{}
	}

	return AdminEntitlementDetail{
		Entitlement: ent,
		Adjustments: adjustments,
	}, nil
}
