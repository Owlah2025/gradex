package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/catalog"
	"github.com/Owlah2025/gradex/backend/internal/entitlement"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/learning"
)

type Repository struct {
	pool      *pgxpool.Pool
	learning  *learning.Repository
	evaluator *entitlement.Evaluator
	devices   deviceReader
}

func NewRepository(pool *pgxpool.Pool) (*Repository, error) {
	return NewRepositoryWithOptions(pool, RepositoryOptions{})
}

func (r *Repository) SearchAccounts(
	ctx context.Context,
	req AccountDirectoryRequest,
) (AccountDirectoryResult, error) {
	if err := validateAccountDirectoryRequest(req); err != nil {
		return AccountDirectoryResult{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return AccountDirectoryResult{}, fmt.Errorf("beginning account directory read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	where, args, filterKeys := accountWhere(req)
	total, err := countAccounts(ctx, tx, where, args)
	if err != nil {
		return AccountDirectoryResult{}, err
	}
	accounts, err := queryAccounts(ctx, tx, req, where, args)
	if err != nil {
		return AccountDirectoryResult{}, err
	}
	if err := WritePrivilegedReadAudit(ctx, tx, PrivilegedReadAudit{
		Principal:     req.Principal,
		CorrelationID: req.CorrelationID,
		Action:        ActionUserSearched,
		Module:        catalog.AuditModuleIdentityAndAccess,
		TargetType:    DirectoryTargetType,
		TargetID:      DirectoryTargetID,
		Reason:        "privileged read: user directory search",
		Metadata: map[string]any{
			"filter_keys_present": filterKeys,
			"query_kind":          queryKind(req.Query),
			"query_length":        utf8.RuneCountInString(strings.TrimSpace(req.Query)),
			"result_count":        len(accounts),
			"page":                req.Page,
		},
	}); err != nil {
		return AccountDirectoryResult{}, fmt.Errorf("auditing account directory read: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return AccountDirectoryResult{}, fmt.Errorf("committing account directory read: %w", err)
	}
	return AccountDirectoryResult{
		Accounts: accounts, Total: total, Page: req.Page, Limit: req.Limit,
	}, nil
}

func (r *Repository) GetAccount(
	ctx context.Context,
	req AccountIdentityRequest,
) (AccountDirectoryEntry, error) {
	if err := validateIdentityRead(req.Principal); err != nil {
		return AccountDirectoryEntry{}, err
	}
	if _, err := uuid.Parse(req.AccountID); err != nil {
		return AccountDirectoryEntry{}, ErrAccountNotFound
	}
	if !req.Locale.Valid() {
		return AccountDirectoryEntry{}, ErrInvalidInput
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return AccountDirectoryEntry{}, fmt.Errorf("beginning account identity read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	entry, err := queryAccount(ctx, tx, req.Locale, req.AccountID)
	if err != nil {
		return AccountDirectoryEntry{}, err
	}
	if err := WritePrivilegedReadAudit(ctx, tx, PrivilegedReadAudit{
		Principal:     req.Principal,
		CorrelationID: req.CorrelationID,
		Action:        ActionUserViewed,
		Module:        catalog.AuditModuleIdentityAndAccess,
		TargetType:    "ACCOUNT",
		TargetID:      req.AccountID,
		Reason:        "privileged read: account identity view",
		Metadata:      map[string]any{"sections": []string{"identity"}},
	}); err != nil {
		return AccountDirectoryEntry{}, fmt.Errorf("auditing account identity read: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return AccountDirectoryEntry{}, fmt.Errorf("committing account identity read: %w", err)
	}
	return entry, nil
}

func (r *Repository) ListAuditEvents(
	ctx context.Context,
	req AuditEventRequest,
) (AuditEventResult, error) {
	if err := validateAuditEventRequest(req); err != nil {
		return AuditEventResult{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return AuditEventResult{}, fmt.Errorf("beginning audit viewer read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	asOf, err := auditReadAsOf(ctx, tx, req.AsOf)
	if err != nil {
		return AuditEventResult{}, err
	}
	where, args, filterKeys := auditWhere(req, asOf)
	events, hasMore, err := queryAuditEvents(ctx, tx, req, where, args)
	if err != nil {
		return AuditEventResult{}, err
	}
	if err := WritePrivilegedReadAudit(ctx, tx, PrivilegedReadAudit{
		Principal:     req.Principal,
		CorrelationID: req.CorrelationID,
		Action:        ActionAuditViewed,
		Module:        catalog.AuditModuleAudit,
		TargetType:    AuditTargetType,
		TargetID:      AuditTargetID,
		Reason:        "privileged read: audit event viewer",
		Metadata: map[string]any{
			"filter_keys_present": filterKeys,
			"result_count":        len(events),
			"has_more":            hasMore,
			"page":                req.Page,
		},
	}); err != nil {
		return AuditEventResult{}, fmt.Errorf("auditing audit viewer read: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return AuditEventResult{}, fmt.Errorf("committing audit viewer read: %w", err)
	}
	return AuditEventResult{Events: events, Page: req.Page, Limit: req.Limit, HasMore: hasMore, AsOf: asOf}, nil
}

func validateIdentityRead(principal identity.Principal) error {
	decision := identity.Authorize(principal, identity.CapUserDirectoryRead)
	if !decision.Allowed {
		return fmt.Errorf("%w: %s", ErrUnauthorized, decision.Reason)
	}
	return nil
}

func validateAccountDirectoryRequest(req AccountDirectoryRequest) error {
	if err := validateIdentityRead(req.Principal); err != nil {
		return err
	}
	if !req.Locale.Valid() || !validPage(req.Page, req.Limit) {
		return ErrInvalidInput
	}
	if utf8.RuneCountInString(strings.TrimSpace(req.Query)) > 200 {
		return ErrInvalidInput
	}
	if req.Role != "" && !req.Role.Valid() {
		return ErrInvalidInput
	}
	if req.Status != "" && !req.Status.Valid() {
		return ErrInvalidInput
	}
	if req.InstitutionID != "" {
		if _, err := uuid.Parse(req.InstitutionID); err != nil {
			return ErrInvalidInput
		}
	}
	if req.JoinedFrom != nil && req.JoinedTo != nil && !req.JoinedFrom.Before(*req.JoinedTo) {
		return ErrInvalidInput
	}
	return nil
}

func validateAuditEventRequest(req AuditEventRequest) error {
	if err := validateIdentityRead(req.Principal); err != nil {
		return err
	}
	if !validPage(req.Page, req.Limit) || !validAuditFilter(req) {
		return ErrInvalidInput
	}
	if req.ActorAccountID != "" {
		if _, err := uuid.Parse(req.ActorAccountID); err != nil {
			return ErrInvalidInput
		}
	}
	if req.OccurredFrom != nil && req.OccurredTo != nil && !req.OccurredFrom.Before(*req.OccurredTo) {
		return ErrInvalidInput
	}
	if req.AsOf != nil && req.AsOf.IsZero() {
		return ErrInvalidInput
	}
	return nil
}

func validPage(page, limit int) bool {
	return page >= 1 && page <= 10_000 && limit >= 1 && limit <= 50
}

func validAuditFilter(req AuditEventRequest) bool {
	return len(req.ActorQuery) <= 200 && len(req.TargetType) <= 100 &&
		len(req.TargetID) <= 200 && len(req.Action) <= 100 && len(req.Module) <= 100 &&
		(req.Module == "" || knownAuditModule(req.Module))
}

func knownAuditModule(module string) bool {
	switch module {
	case catalog.AuditModuleIdentityAndAccess, catalog.AuditModuleCatalog,
		catalog.AuditModuleModeration, catalog.AuditModuleAudit,
		"MEDIA_AND_ASSETS", "ENTITLEMENTS", "COMMERCE", "LEARNING",
		"OFFICE_HOURS", "NOTIFICATIONS", "REPORTING_AND_PAYOUTS":
		return true
	default:
		return false
	}
}

func accountWhere(req AccountDirectoryRequest) (string, []any, []string) {
	conditions := []string{"1=1"}
	args := make([]any, 0, 6)
	filterKeys := make([]string, 0, 6)
	add := func(key, condition string, value any) {
		args = append(args, value)
		conditions = append(conditions, fmt.Sprintf(condition, len(args)))
		filterKeys = append(filterKeys, key)
	}
	query := strings.TrimSpace(req.Query)
	if query != "" {
		add("q", `(LOWER(a.display_name) LIKE '%%' || LOWER($%[1]d) || '%%' ESCAPE chr(92) OR a.normalized_email LIKE LOWER($%[1]d) || '%%' ESCAPE chr(92))`, escapeLikePattern(query))
	}
	if req.Role != "" {
		add("role", "a.role = $%d::account_role", string(req.Role))
	}
	if req.Status != "" {
		add("status", "a.status = $%d::account_status", string(req.Status))
	}
	if req.InstitutionID != "" {
		add("institutionId", "sap.institution_id = $%d::uuid", req.InstitutionID)
	}
	if req.JoinedFrom != nil {
		add("joinedFrom", "a.created_at >= $%d", *req.JoinedFrom)
	}
	if req.JoinedTo != nil {
		add("joinedTo", "a.created_at < $%d", *req.JoinedTo)
	}
	return strings.Join(conditions, " AND "), args, filterKeys
}

func auditWhere(req AuditEventRequest, asOf time.Time) (string, []any, []string) {
	conditions := []string{"1=1"}
	args := make([]any, 0, 7)
	filterKeys := make([]string, 0, 7)
	add := func(key, condition string, value any) {
		args = append(args, value)
		conditions = append(conditions, fmt.Sprintf(condition, len(args)))
		filterKeys = append(filterKeys, key)
	}
	if req.ActorAccountID != "" {
		add("actorAccountId", "ae.actor_account_id = $%d::uuid", req.ActorAccountID)
	}
	if strings.TrimSpace(req.ActorQuery) != "" {
		add("actor", `(LOWER(COALESCE(actor.display_name, '')) LIKE '%%' || LOWER($%[1]d) || '%%' ESCAPE chr(92) OR LOWER(COALESCE(actor.email, '')) LIKE '%%' || LOWER($%[1]d) || '%%' ESCAPE chr(92))`, escapeLikePattern(strings.TrimSpace(req.ActorQuery)))
	}
	if req.TargetType != "" {
		add("targetType", "ae.target_type = $%d", req.TargetType)
	}
	if req.TargetID != "" {
		add("targetId", "ae.target_id = $%d", req.TargetID)
	}
	if req.Action != "" {
		add("action", "ae.action = $%d", req.Action)
	}
	if req.Module != "" {
		add("module", "ae.module = $%d::audit_module", req.Module)
	}
	if req.OccurredFrom != nil {
		add("from", "ae.occurred_at >= $%d", *req.OccurredFrom)
	}
	if req.OccurredTo != nil {
		add("to", "ae.occurred_at < $%d", *req.OccurredTo)
	}
	args = append(args, asOf)
	conditions = append(conditions, fmt.Sprintf("ae.occurred_at < $%d", len(args)))
	return strings.Join(conditions, " AND "), args, filterKeys
}

func escapeLikePattern(value string) string {
	return strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(value)
}

func queryKind(query string) string {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return "none"
	}
	if strings.Contains(trimmed, "@") {
		return "email"
	}
	return "name"
}

func countAccounts(ctx context.Context, tx pgx.Tx, where string, args []any) (int, error) {
	var total int
	err := tx.QueryRow(ctx, "SELECT count(*) FROM accounts a LEFT JOIN student_academic_profiles sap ON sap.account_id = a.id WHERE "+where, args...).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("counting account directory rows: %w", err)
	}
	return total, nil
}

func queryAccounts(
	ctx context.Context,
	tx pgx.Tx,
	req AccountDirectoryRequest,
	where string,
	args []any,
) ([]AccountDirectoryEntry, error) {
	queryArgs := append(append([]any(nil), args...), req.Limit, (req.Page-1)*req.Limit)
	rows, err := tx.Query(ctx, `WITH account_page AS (
		SELECT a.id
		FROM accounts a
		LEFT JOIN student_academic_profiles sap ON sap.account_id = a.id
		WHERE `+where+`
		ORDER BY a.created_at DESC, a.id DESC
		LIMIT $`+fmt.Sprint(len(args)+1)+` OFFSET $`+fmt.Sprint(len(args)+2)+`
	)
	SELECT a.id::text, a.display_name, a.email, a.role::text, a.status::text,
		a.locale, a.email_verified_at IS NOT NULL, a.created_at, i.name_ar, i.name_en,
		activity.last_activity_at
	FROM account_page
	JOIN accounts a ON a.id = account_page.id
	LEFT JOIN student_academic_profiles sap ON sap.account_id = a.id
	LEFT JOIN institutions i ON i.id = sap.institution_id
	LEFT JOIN LATERAL (
		SELECT MAX(s.last_activity_at) AS last_activity_at
		FROM sessions s
		WHERE s.account_id = a.id
	) activity ON TRUE
	ORDER BY a.created_at DESC, a.id DESC`, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("querying account directory: %w", err)
	}
	defer rows.Close()

	accounts := make([]AccountDirectoryEntry, 0, req.Limit)
	for rows.Next() {
		var account AccountDirectoryEntry
		var role, status, locale string
		var institutionAr, institutionEn *string
		if err := rows.Scan(&account.ID, &account.DisplayName, &account.Email, &role, &status,
			&locale, &account.EmailVerified, &account.CreatedAt, &institutionAr, &institutionEn,
			&account.LastSignInActivityAt); err != nil {
			return nil, fmt.Errorf("scanning account directory row: %w", err)
		}
		account.Role = identity.Role(role)
		account.Status = identity.AccountStatus(status)
		account.Locale = identity.Locale(locale)
		account.InstitutionLabel = localizedInstitution(req.Locale, institutionAr, institutionEn)
		accounts = append(accounts, account)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating account directory rows: %w", err)
	}
	return accounts, nil
}

func queryAccount(
	ctx context.Context,
	tx pgx.Tx,
	locale identity.Locale,
	accountID string,
) (AccountDirectoryEntry, error) {
	var account AccountDirectoryEntry
	var role, status, accountLocale string
	var institutionAr, institutionEn *string
	err := tx.QueryRow(ctx, `SELECT a.id::text, a.display_name, a.email, a.role::text, a.status::text,
		a.locale, a.email_verified_at IS NOT NULL, a.created_at, i.name_ar, i.name_en,
		activity.last_activity_at
		FROM accounts a
		LEFT JOIN student_academic_profiles sap ON sap.account_id = a.id
		LEFT JOIN institutions i ON i.id = sap.institution_id
		LEFT JOIN LATERAL (
			SELECT MAX(s.last_activity_at) AS last_activity_at
			FROM sessions s
			WHERE s.account_id = a.id
		) activity ON TRUE
		WHERE a.id = $1::uuid
		`, accountID).Scan(
		&account.ID, &account.DisplayName, &account.Email, &role, &status, &accountLocale,
		&account.EmailVerified, &account.CreatedAt, &institutionAr, &institutionEn,
		&account.LastSignInActivityAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return AccountDirectoryEntry{}, ErrAccountNotFound
	}
	if err != nil {
		return AccountDirectoryEntry{}, fmt.Errorf("querying account identity: %w", err)
	}
	account.Role = identity.Role(role)
	account.Status = identity.AccountStatus(status)
	account.Locale = identity.Locale(accountLocale)
	account.InstitutionLabel = localizedInstitution(locale, institutionAr, institutionEn)
	return account, nil
}

func auditReadAsOf(ctx context.Context, tx pgx.Tx, requested *time.Time) (time.Time, error) {
	if requested != nil {
		return requested.UTC(), nil
	}
	var asOf time.Time
	if err := tx.QueryRow(ctx, "SELECT now()").Scan(&asOf); err != nil {
		return time.Time{}, fmt.Errorf("reading audit viewer snapshot time: %w", err)
	}
	return asOf, nil
}

func queryAuditEvents(
	ctx context.Context,
	tx pgx.Tx,
	req AuditEventRequest,
	where string,
	args []any,
) ([]AuditEvent, bool, error) {
	queryArgs := append(append([]any(nil), args...), req.Limit+1, (req.Page-1)*req.Limit)
	rows, err := tx.Query(ctx, `SELECT ae.id::text, ae.occurred_at, ae.actor_account_id::text,
		COALESCE(actor.display_name, ae.actor_descriptor), ae.actor_role, ae.action, ae.module::text,
		ae.target_type, ae.target_id, COALESCE(target.display_name, ''), ae.reason, ae.metadata
		FROM audit_events ae
		LEFT JOIN accounts actor ON actor.id = ae.actor_account_id
		LEFT JOIN accounts target ON CASE WHEN ae.target_type = 'ACCOUNT' THEN ae.target_id::uuid END = target.id
		WHERE `+where+`
		ORDER BY ae.occurred_at DESC, ae.id DESC
		LIMIT $`+fmt.Sprint(len(args)+1)+` OFFSET $`+fmt.Sprint(len(args)+2), queryArgs...)
	if err != nil {
		return nil, false, fmt.Errorf("querying audit viewer rows: %w", err)
	}
	defer rows.Close()

	events := make([]AuditEvent, 0, req.Limit)
	for rows.Next() {
		var event AuditEvent
		var actorAccountID *string
		var targetLabel string
		var metadata []byte
		if err := rows.Scan(&event.ID, &event.OccurredAt, &actorAccountID, &event.ActorDisplayName,
			&event.ActorRole, &event.Action, &event.Module, &event.TargetType, &event.TargetID,
			&targetLabel, &event.Reason, &metadata); err != nil {
			return nil, false, fmt.Errorf("scanning audit viewer row: %w", err)
		}
		event.ActorAccountID = actorAccountID
		event.TargetLabel = targetLabel
		event.Metadata = map[string]any{}
		if len(metadata) > 0 {
			if err := json.Unmarshal(metadata, &event.Metadata); err != nil {
				return nil, false, fmt.Errorf("decoding audit viewer metadata: %w", err)
			}
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("iterating audit viewer rows: %w", err)
	}
	hasMore := len(events) > req.Limit
	if hasMore {
		events = events[:req.Limit]
	}
	return events, hasMore, nil
}

func localizedInstitution(locale identity.Locale, arabic, english *string) string {
	if locale == identity.LocaleArabic {
		if arabic != nil && strings.TrimSpace(*arabic) != "" {
			return *arabic
		}
		if english != nil {
			return *english
		}
		return ""
	}
	if english != nil && strings.TrimSpace(*english) != "" {
		return *english
	}
	if arabic != nil {
		return *arabic
	}
	return ""
}
