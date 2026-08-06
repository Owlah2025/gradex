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
	rawEmail := strings.TrimSpace(params.Email)
	if rawEmail == "" {
		return Invitation{}, "", errors.New("email is required")
	}
	normalizedEmail := strings.ToLower(rawEmail)

	now := params.Now
	if now.IsZero() {
		now = time.Now()
	}
	ttl := params.TTL
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	expiresAt := now.Add(ttl)

	// Check eligibility: if an account exists for this normalized email, it MUST be a STUDENT account (FR-004, BR-082).
	var role string
	err := r.pool.QueryRow(ctx, "SELECT role FROM accounts WHERE normalized_email = $1", normalizedEmail).Scan(&role)
	if err == nil {
		if role != "STUDENT" {
			return Invitation{}, "", ErrIneligibleRecipient
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return Invitation{}, "", fmt.Errorf("checking account eligibility: %w", err)
	}

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
	`, invitationID, normalizedEmail, rawEmail, params.CourseID, params.AdminAccountID,
		params.AdminNote, params.ExternalReference, actionSecretID, now,
	).Scan(
		&inv.ID, &inv.NormalizedEmail, &inv.Email, &inv.CourseID, &inv.CreatedByAccountID,
		&inv.State, &inv.AdminNote, &inv.ExternalReference, &inv.ActionSecretID, &inv.CreatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && (pgErr.ConstraintName == "cai_one_non_terminal_per_pair" || pgErr.Code == "23505") {
			return Invitation{}, "", ErrDuplicateInvitation
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
		},
	}
	delivery := outbox.VerificationDelivery{
		Destination:       normalizedEmail,
		Locale:            "en",
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
		whereClauses = append(whereClauses, fmt.Sprintf("state = $%d", argIdx))
		args = append(args, string(*filter.State))
		argIdx++
	}
	if filter.CourseID != nil && strings.TrimSpace(*filter.CourseID) != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("course_id = $%d::uuid", argIdx))
		args = append(args, *filter.CourseID)
		argIdx++
	}

	whereStmt := strings.Join(whereClauses, " AND ")

	countQuery := fmt.Sprintf("SELECT count(*) FROM course_access_invitations WHERE %s", whereStmt)
	var total int
	if err := r.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("counting invitations: %w", err)
	}

	query := fmt.Sprintf(`
		SELECT id::text, normalized_email, email, course_id::text, created_by_account_id::text,
		       decided_by_account_id::text, accepted_by_account_id::text, state, decision_reason,
		       admin_note, external_reference, action_secret_id::text, created_at, accepted_at,
		       decided_at, cancelled_at
		  FROM course_access_invitations
		 WHERE %s
		 ORDER BY created_at DESC
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
		if err := rows.Scan(
			&inv.ID, &inv.NormalizedEmail, &inv.Email, &inv.CourseID, &inv.CreatedByAccountID,
			&inv.DecidedByAccountID, &inv.AcceptedByAccountID, &inv.State, &inv.DecisionReason,
			&inv.AdminNote, &inv.ExternalReference, &actionSecretID, &inv.CreatedAt, &inv.AcceptedAt,
			&inv.DecidedAt, &inv.CancelledAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scanning invitation: %w", err)
		}
		inv.ActionSecretID = actionSecretID
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
