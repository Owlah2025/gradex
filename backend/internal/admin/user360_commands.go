package admin

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Owlah2025/gradex/backend/internal/catalog"
	"github.com/Owlah2025/gradex/backend/internal/identity"
)

func (r *Repository) ListNotes(ctx context.Context, req NoteListRequest) (NoteListResult, error) {
	if err := validateIdentityRead(req.Principal); err != nil {
		return NoteListResult{}, err
	}
	if _, err := uuid.Parse(req.AccountID); err != nil {
		return NoteListResult{}, ErrAccountNotFound
	}
	limit := req.Limit
	if limit < 1 || limit > 50 {
		limit = user360NotesLimit
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return NoteListResult{}, fmt.Errorf("beginning account notes read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := ensureAccountExists(ctx, tx, req.AccountID); err != nil {
		return NoteListResult{}, err
	}
	if err := WritePrivilegedReadAudit(ctx, tx, PrivilegedReadAudit{
		Principal: req.Principal, CorrelationID: req.CorrelationID,
		Action: ActionUserViewed, Module: catalog.AuditModuleIdentityAndAccess,
		TargetType: User360TargetType, TargetID: req.AccountID,
		Reason: AuditReasonUserViewed, Metadata: map[string]any{"sections": []string{"notes"}},
	}); err != nil {
		return NoteListResult{}, fmt.Errorf("auditing account notes read: %w", err)
	}
	notes, err := queryNotes(ctx, tx, req.AccountID, limit)
	if err != nil {
		return NoteListResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return NoteListResult{}, fmt.Errorf("committing account notes read: %w", err)
	}
	return NoteListResult{Notes: notes}, nil
}

func (r *Repository) AddNote(ctx context.Context, req AddNoteRequest) (AdminNote, error) {
	decision := identity.Authorize(req.Principal, identity.CapUserAdministration)
	if !decision.Allowed {
		return AdminNote{}, fmt.Errorf("%w: %s", ErrUnauthorized, decision.Reason)
	}
	if _, err := uuid.Parse(req.AccountID); err != nil {
		return AdminNote{}, ErrAccountNotFound
	}
	body := strings.TrimSpace(req.Body)
	if utf8.RuneCountInString(body) < 1 || utf8.RuneCountInString(body) > 4000 {
		return AdminNote{}, ErrInvalidInput
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return AdminNote{}, fmt.Errorf("beginning admin note write: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := ensureAccountExists(ctx, tx, req.AccountID); err != nil {
		return AdminNote{}, err
	}
	var note AdminNote
	if err := tx.QueryRow(ctx, `
		INSERT INTO admin_notes (subject_account_id, author_account_id, body)
		VALUES ($1::uuid, $2::uuid, $3)
		RETURNING id::text, created_at`, req.AccountID, req.Principal.AccountID, body,
	).Scan(&note.ID, &note.CreatedAt); err != nil {
		return AdminNote{}, fmt.Errorf("inserting admin note: %w", err)
	}
	if err := tx.QueryRow(ctx, `SELECT COALESCE(display_name, email) FROM accounts WHERE id = $1::uuid`, req.Principal.AccountID).Scan(&note.AuthorName); err != nil {
		return AdminNote{}, fmt.Errorf("reading admin note author: %w", err)
	}
	note.Body = body
	if err := catalog.WriteAuditEvent(ctx, tx, catalog.AuditEvent{
		ActorAccountID:  stringPointer(req.Principal.AccountID),
		ActorRole:       string(req.Principal.Role),
		ActorDescriptor: req.Principal.AccountID,
		Action:          ActionNoteAdded,
		Module:          catalog.AuditModuleIdentityAndAccess,
		TargetType:      User360TargetType,
		TargetID:        req.AccountID,
		Reason:          ActionNoteAdded,
		Metadata: map[string]any{
			"body_length": utf8.RuneCountInString(body),
		},
		CorrelationID: optionalString(req.CorrelationID),
	}); err != nil {
		return AdminNote{}, fmt.Errorf("auditing admin note: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return AdminNote{}, fmt.Errorf("committing admin note: %w", err)
	}
	return note, nil
}

func (r *Repository) RevokeAccountSessions(ctx context.Context, req SessionRevocationRequest) (SessionRevocationResult, error) {
	decision := identity.Authorize(req.Principal, identity.CapSecurityOperations)
	if !decision.Allowed {
		return SessionRevocationResult{}, fmt.Errorf("%w: %s", ErrUnauthorized, decision.Reason)
	}
	if err := identity.CheckRecentAuthentication(req.ActorSession, req.RecentAuthWindow, req.Now); err != nil {
		return SessionRevocationResult{}, fmt.Errorf("%w: recent authentication check failed", identity.ErrRecentAuthRequired)
	}
	if strings.TrimSpace(req.Reason) == "" || utf8.RuneCountInString(req.Reason) > 1000 {
		return SessionRevocationResult{}, ErrInvalidInput
	}
	principalID, err := uuid.Parse(req.Principal.AccountID)
	if err != nil {
		return SessionRevocationResult{}, fmt.Errorf("%w: invalid principal account", ErrUnauthorized)
	}
	targetID, err := uuid.Parse(req.AccountID)
	if err != nil {
		return SessionRevocationResult{}, ErrAccountNotFound
	}
	if principalID == targetID {
		return SessionRevocationResult{}, fmt.Errorf("%w: self-target is not permitted", ErrUnauthorized)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return SessionRevocationResult{}, fmt.Errorf("beginning account session revocation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := ensureAccountExists(ctx, tx, req.AccountID); err != nil {
		return SessionRevocationResult{}, err
	}
	epoch, revoked, err := identity.RevokeAllSessionsInTransaction(ctx, tx, req.AccountID, identity.RevokedByAdmin, req.Now)
	if err != nil {
		return SessionRevocationResult{}, err
	}
	var revision int
	if err := tx.QueryRow(ctx, `SELECT revision FROM accounts WHERE id = $1::uuid`, req.AccountID).Scan(&revision); err != nil {
		return SessionRevocationResult{}, fmt.Errorf("reading revoked account revision: %w", err)
	}
	if err := identity.WriteSecurityEvent(ctx, tx, identity.SecurityEventRequest{
		EventType: "ADMIN_SESSIONS_REVOKED", AccountID: req.AccountID, Revision: revision,
		RequestID: req.CorrelationID, Evidence: map[string]any{
			"actor_account_id":      req.Principal.AccountID,
			"reason":                req.Reason,
			"revoked_session_count": revoked,
		},
	}); err != nil {
		return SessionRevocationResult{}, fmt.Errorf("writing session revocation security evidence: %w", err)
	}
	if err := catalog.WriteAuditEvent(ctx, tx, catalog.AuditEvent{
		ActorAccountID:  stringPointer(req.Principal.AccountID),
		ActorRole:       string(req.Principal.Role),
		ActorDescriptor: req.Principal.AccountID,
		Action:          ActionSessionsRevoked,
		Module:          catalog.AuditModuleIdentityAndAccess,
		TargetType:      User360TargetType,
		TargetID:        req.AccountID,
		Reason:          req.Reason,
		Metadata: map[string]any{
			"epoch": epoch, "revoked_session_count": revoked,
		},
		CorrelationID: optionalString(req.CorrelationID),
	}); err != nil {
		return SessionRevocationResult{}, fmt.Errorf("auditing account session revocation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return SessionRevocationResult{}, fmt.Errorf("committing account session revocation: %w", err)
	}
	return SessionRevocationResult{Epoch: epoch, RevokedSessionCount: revoked}, nil
}

func (r *Repository) ListSecurityEvents(ctx context.Context, req SecurityEventsRequest) (SecurityEventsResult, error) {
	if err := validateIdentityRead(req.Principal); err != nil {
		return SecurityEventsResult{}, err
	}
	if _, err := uuid.Parse(req.AccountID); err != nil {
		return SecurityEventsResult{}, ErrAccountNotFound
	}
	if req.Page < 1 || req.Page > maxUser360Page || req.Limit < 1 || req.Limit > 50 {
		return SecurityEventsResult{}, ErrInvalidInput
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return SecurityEventsResult{}, fmt.Errorf("beginning security events read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var total int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM identity_security_events WHERE account_id = $1::uuid`, req.AccountID).Scan(&total); err != nil {
		return SecurityEventsResult{}, fmt.Errorf("counting account security events: %w", err)
	}
	if err := WritePrivilegedReadAudit(ctx, tx, PrivilegedReadAudit{
		Principal: req.Principal, CorrelationID: req.CorrelationID,
		Action: ActionUserViewed, Module: catalog.AuditModuleIdentityAndAccess,
		TargetType: User360TargetType, TargetID: req.AccountID,
		Reason: AuditReasonUserViewed, Metadata: map[string]any{"sections": []string{"security_events"}},
	}); err != nil {
		return SecurityEventsResult{}, fmt.Errorf("auditing account security events read: %w", err)
	}
	events, err := querySecurityEvents(ctx, tx, req.AccountID, req.Page, req.Limit)
	if err != nil {
		return SecurityEventsResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SecurityEventsResult{}, fmt.Errorf("committing security events read: %w", err)
	}
	return SecurityEventsResult{Events: events, Total: total, Page: req.Page, Limit: req.Limit}, nil
}

func ensureAccountExists(ctx context.Context, tx pgx.Tx, accountID string) error {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM accounts WHERE id = $1::uuid)`, accountID).Scan(&exists); err != nil {
		return fmt.Errorf("checking account existence: %w", err)
	}
	if !exists {
		return ErrAccountNotFound
	}
	return nil
}

func stringPointer(value string) *string { return &value }

func optionalString(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	trimmed := strings.TrimSpace(value)
	return &trimmed
}
