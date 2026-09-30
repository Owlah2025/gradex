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

const maxAccountExportRows = 5000

func (r *Repository) ExportAccounts(
	ctx context.Context,
	request AccountDirectoryRequest,
) (AccountExportResult, error) {
	if err := validateAccountExportRequest(request); err != nil {
		return AccountExportResult{}, err
	}
	if r == nil || r.pool == nil {
		return AccountExportResult{}, ErrRepositoryNil
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return AccountExportResult{}, fmt.Errorf("beginning account export: %w", err)
	}
	defer tx.Rollback(ctx)
	where, args, filterKeys := accountWhere(request)
	accounts, err := queryExportAccounts(ctx, tx, request, where, args)
	if err != nil {
		return AccountExportResult{}, err
	}
	if len(accounts) > maxAccountExportRows {
		return AccountExportResult{}, ErrInvalidInput
	}
	if err := WritePrivilegedReadAudit(ctx, tx, PrivilegedReadAudit{
		Principal:     request.Principal,
		CorrelationID: request.CorrelationID,
		Action:        ActionExportCreated,
		Module:        catalog.AuditModuleIdentityAndAccess,
		TargetType:    DirectoryTargetType,
		TargetID:      DirectoryTargetID,
		Reason:        "privileged export: account directory",
		Metadata: map[string]any{
			"filter_keys_present": filterKeys,
			"row_count":           len(accounts),
		},
	}); err != nil {
		return AccountExportResult{}, fmt.Errorf("auditing account export: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return AccountExportResult{}, fmt.Errorf("committing account export: %w", err)
	}
	return AccountExportResult{Accounts: accounts}, nil
}

func validateAccountExportRequest(request AccountDirectoryRequest) error {
	decision := identity.Authorize(request.Principal, identity.CapUserAdministration)
	if !decision.Allowed {
		return fmt.Errorf("%w: %s", ErrUnauthorized, decision.Reason)
	}
	if !request.Locale.Valid() || request.Limit < 1 || request.Limit > maxAccountExportRows {
		return ErrInvalidInput
	}
	if utf8.RuneCountInString(strings.TrimSpace(request.Query)) > 200 {
		return ErrInvalidInput
	}
	if request.Role != "" && !request.Role.Valid() {
		return ErrInvalidInput
	}
	if request.Status != "" && !request.Status.Valid() {
		return ErrInvalidInput
	}
	if request.InstitutionID != "" {
		if _, err := uuid.Parse(request.InstitutionID); err != nil {
			return ErrInvalidInput
		}
	}
	if request.JoinedFrom != nil && request.JoinedTo != nil && !request.JoinedFrom.Before(*request.JoinedTo) {
		return ErrInvalidInput
	}
	return nil
}

func queryExportAccounts(
	ctx context.Context,
	tx pgx.Tx,
	request AccountDirectoryRequest,
	where string,
	args []any,
) ([]AccountDirectoryEntry, error) {
	queryArgs := append(append([]any(nil), args...), request.Limit+1)
	rows, err := tx.Query(ctx, `
		SELECT a.id::text, a.display_name, a.email, a.role::text, a.status::text,
		       a.locale, a.email_verified_at IS NOT NULL, a.created_at,
		       i.name_ar, i.name_en, activity.last_activity_at
		  FROM accounts a
		  LEFT JOIN student_academic_profiles sap ON sap.account_id = a.id
		  LEFT JOIN institutions i ON i.id = sap.institution_id
		  LEFT JOIN LATERAL (
			SELECT MAX(s.last_activity_at) AS last_activity_at
			  FROM sessions s
			 WHERE s.account_id = a.id
		  ) activity ON TRUE
		 WHERE `+where+`
		 ORDER BY a.created_at DESC, a.id DESC
		 LIMIT $`+fmt.Sprint(len(args)+1), queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("querying account export rows: %w", err)
	}
	defer rows.Close()

	accounts := make([]AccountDirectoryEntry, 0, request.Limit)
	for rows.Next() {
		var account AccountDirectoryEntry
		var role, status, locale string
		var institutionAr, institutionEn *string
		if err := rows.Scan(&account.ID, &account.DisplayName, &account.Email, &role, &status,
			&locale, &account.EmailVerified, &account.CreatedAt, &institutionAr, &institutionEn,
			&account.LastSignInActivityAt); err != nil {
			return nil, fmt.Errorf("scanning account export row: %w", err)
		}
		account.Role = identity.Role(role)
		account.Status = identity.AccountStatus(status)
		account.Locale = identity.Locale(locale)
		account.InstitutionLabel = localizedInstitution(request.Locale, institutionAr, institutionEn)
		accounts = append(accounts, account)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating account export rows: %w", err)
	}
	return accounts, nil
}

var _ interface {
	ExportAccounts(context.Context, AccountDirectoryRequest) (AccountExportResult, error)
} = (*Repository)(nil)
