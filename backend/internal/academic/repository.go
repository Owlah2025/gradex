package academic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository refuses to build without a pool. Standing clause: a component
// never starts without a security-relevant dependency, and no
// mutation-after-construction path may restore one.
func NewRepository(pool *pgxpool.Pool) (*Repository, error) {
	if pool == nil {
		return nil, ErrRepositoryNil
	}
	return &Repository{pool: pool}, nil
}

func (r *Repository) Pool() *pgxpool.Pool { return r.pool }

func (r *Repository) ExecTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}
	return nil
}

// actor is the audited principal behind every catalog mutation. Every mutating
// entry point takes one; none defaults it.
//
// Two shapes exist. A human Admin carries an AccountID and is audited with role
// ADMIN. The catalog importer runs without a human operator and is audited with
// the same SYSTEM convention identity/bootstrap.go already uses: a NULL
// actor_account_id, actor_role SYSTEM, and a descriptor naming the deployment
// principal. Inventing an Admin account for CLI tooling would put a false actor
// into the audit trail, and skipping the audit entirely is not an option.
type actor struct {
	AccountID  string
	Descriptor string
	System     bool
}

func (a actor) validate() error {
	if a.System {
		if strings.TrimSpace(a.Descriptor) == "" {
			return errors.New("a system actor requires a descriptor")
		}
		return nil
	}
	if strings.TrimSpace(a.AccountID) == "" {
		return ErrAdminRequired
	}
	return nil
}

func (a actor) role() string {
	if a.System {
		return "SYSTEM"
	}
	return "ADMIN"
}

// accountID returns nil for a system actor so audit_events.actor_account_id is
// written NULL rather than pointing at a fabricated Account.
func (a actor) accountID() *string {
	if a.System {
		return nil
	}
	id := a.AccountID
	return &id
}

func (a actor) descriptor() string {
	if strings.TrimSpace(a.Descriptor) == "" {
		return a.AccountID
	}
	return a.Descriptor
}

// Actor identifies the principal performing a catalog mutation: either an
// authenticated Admin, or a named system principal when System is set.
type Actor struct {
	AdminAccountID  string
	ActorDescriptor string
	System          bool
}

func (a Actor) internal() actor {
	return actor{AccountID: a.AdminAccountID, Descriptor: a.ActorDescriptor, System: a.System}
}

// SystemActor names a non-human deployment principal, such as the catalog
// importer running from the command line.
func SystemActor(descriptor string) Actor {
	return Actor{ActorDescriptor: descriptor, System: true}
}

type auditRequest struct {
	Actor      actor
	Action     string
	TargetType string
	TargetID   string
	Reason     string
	Metadata   map[string]any
}

// writeAudit records one privileged catalog action in the same transaction as
// the change. Metadata carries only catalog-shaped values: no secrets, no
// bearer tokens, no raw request payloads, no personal data.
func writeAudit(ctx context.Context, tx pgx.Tx, req auditRequest) error {
	if tx == nil {
		return errors.New("transaction is required for audit writing")
	}
	if req.Action == "" || req.TargetType == "" || req.TargetID == "" || req.Reason == "" {
		return errors.New("academic audit requires action, target, and reason")
	}
	if err := req.Actor.validate(); err != nil {
		return err
	}
	metadataJSON := []byte("{}")
	if len(req.Metadata) > 0 {
		var err error
		metadataJSON, err = json.Marshal(req.Metadata)
		if err != nil {
			return fmt.Errorf("marshaling academic audit metadata: %w", err)
		}
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO audit_events (
			actor_account_id, actor_role, actor_descriptor,
			action, module, target_type, target_id, reason, metadata
		) VALUES ($1::uuid, $2, $3, $4, 'CATALOG_AND_AUTHORING', $5, $6, $7, $8::jsonb)
	`, req.Actor.accountID(), req.Actor.role(), req.Actor.descriptor(), req.Action,
		req.TargetType, req.TargetID, req.Reason, metadataJSON)
	if err != nil {
		return fmt.Errorf("writing academic audit event: %w", err)
	}
	return nil
}

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func validateSlug(slug string) error {
	trimmed := strings.TrimSpace(slug)
	if len(trimmed) < 2 || len(trimmed) > 80 || !slugPattern.MatchString(trimmed) {
		return ErrInvalidInput
	}
	return nil
}

func validateBilingualName(ar, en string) error {
	if strings.TrimSpace(ar) == "" || strings.TrimSpace(en) == "" {
		return ErrInvalidInput
	}
	return nil
}

func trimmedOrNil(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// NormalizeCode mirrors the SQL academic_normalize_code generated column so the
// application can predict a conflict without a round trip. The database index
// remains the authority; this is never the only check.
func NormalizeCode(code string) string {
	var b strings.Builder
	for _, r := range code {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - 32)
		case (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
		}
	}
	return b.String()
}

func pgErrorOf(err error) *pgconn.PgError {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr
	}
	return nil
}

// classifyConstraint maps a database refusal onto a domain error so callers
// never have to read a driver message. Every branch corresponds to a named
// invariant in D-091.
func classifyConstraint(err error) error {
	pgErr := pgErrorOf(err)
	if pgErr == nil {
		return err
	}
	switch pgErr.Code {
	case "23505": // unique_violation
		switch pgErr.ConstraintName {
		case "subjects_institution_code_unique",
			"subjects_institution_title_ar_unique",
			"subjects_institution_title_en_unique":
			return ErrDuplicateSubject
		case "curricula_one_active_per_program":
			return ErrCurriculumActive
		case "curricula_program_version_unique":
			return ErrVersionLabelTaken
		case "curriculum_subjects_unique":
			return ErrMappingDuplicate
		case "institutions_slug_unique",
			"academic_units_institution_slug_unique",
			"programs_institution_slug_unique":
			return ErrSlugTaken
		}
		return ErrInvalidInput
	case "23503": // foreign_key_violation
		// Every composite foreign key in 0023 exists to pin a relationship to a
		// single Institution, so a violation is a cross-Institution attempt.
		if strings.Contains(pgErr.ConstraintName, "same_institution") {
			return ErrCrossInstitution
		}
		return ErrNotFound
	case "23514": // check_violation, including the cycle and level-bound guards
		if strings.Contains(pgErr.Message, "cycle") || strings.Contains(pgErr.Message, "depth") {
			return ErrHierarchyCycle
		}
		if strings.Contains(pgErr.Message, "recommended level") {
			return ErrLevelOutOfRange
		}
		if pgErr.ConstraintName == "academic_units_no_self_parent" {
			return ErrHierarchyCycle
		}
		return ErrInvalidInput
	case "22P02": // invalid_text_representation — a malformed UUID from the wire
		return ErrNotFound
	}
	return err
}

// AuditEvent is the exported shape the catalog importer writes through. It
// exists so the importer can record its mutations under the same audit contract
// as the Admin API rather than inventing its own.
type AuditEvent struct {
	Action     string
	TargetType string
	TargetID   string
	Reason     string
	Metadata   map[string]any
}

// WriteAuditEvent records one catalog mutation inside an existing transaction.
func WriteAuditEvent(ctx context.Context, tx pgx.Tx, principal Actor, event AuditEvent) error {
	return writeAudit(ctx, tx, auditRequest{
		Actor: principal.internal(), Action: event.Action, TargetType: event.TargetType,
		TargetID: event.TargetID, Reason: event.Reason, Metadata: event.Metadata,
	})
}

// Validate reports whether this principal may perform a catalog mutation.
func (a Actor) Validate() error { return a.internal().validate() }
