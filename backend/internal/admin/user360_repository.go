package admin

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/entitlement"
	"github.com/Owlah2025/gradex/backend/internal/learning"
)

type RepositoryOptions struct {
	Devices            deviceReader
	EmailPayloadReader ProtectedPayloadReader
}

func NewRepositoryWithOptions(pool *pgxpool.Pool, options RepositoryOptions) (*Repository, error) {
	if pool == nil {
		return nil, ErrRepositoryNil
	}
	learningRepository, err := learning.NewRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("building admin learning reader: %w", err)
	}
	entitlementRepository, err := entitlement.NewRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("building admin entitlement reader: %w", err)
	}
	evaluator, err := entitlement.NewEvaluator(entitlementRepository)
	if err != nil {
		return nil, fmt.Errorf("building admin entitlement evaluator: %w", err)
	}
	return &Repository{
		pool: pool, learning: learningRepository, evaluator: evaluator, devices: options.Devices,
		emailPayloadReader: options.EmailPayloadReader,
	}, nil
}

var _ interface {
	GetUser360(context.Context, User360Request) (User360, error)
	ListNotes(context.Context, NoteListRequest) (NoteListResult, error)
	AddNote(context.Context, AddNoteRequest) (AdminNote, error)
	RevokeAccountSessions(context.Context, SessionRevocationRequest) (SessionRevocationResult, error)
	ListCourseOptions(context.Context, CourseOptionsRequest) ([]CourseOption, error)
	DiagnoseAccess(context.Context, AccessDiagnosticRequest) (AccessDiagnostic, error)
	ListSecurityEvents(context.Context, SecurityEventsRequest) (SecurityEventsResult, error)
} = (*Repository)(nil)
