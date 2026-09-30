package httpapi

import (
	"context"
	"errors"
	"fmt"
	"time"

	adminread "github.com/Owlah2025/gradex/backend/internal/admin"
	"github.com/Owlah2025/gradex/backend/internal/ratelimit"
)

type AdminService interface {
	SearchAccounts(context.Context, adminread.AccountDirectoryRequest) (adminread.AccountDirectoryResult, error)
	GetAccount(context.Context, adminread.AccountIdentityRequest) (adminread.AccountDirectoryEntry, error)
	ListAuditEvents(context.Context, adminread.AuditEventRequest) (adminread.AuditEventResult, error)
	ListEmailDeliveries(context.Context, adminread.EmailDeliveriesRequest) (adminread.EmailDeliveriesResult, error)
	ListMediaFailures(context.Context, adminread.MediaFailuresRequest) (adminread.MediaFailuresResult, error)
	ExportAccounts(context.Context, adminread.AccountDirectoryRequest) (adminread.AccountExportResult, error)
}

type AdminUserService interface {
	GetUser360(context.Context, adminread.User360Request) (adminread.User360, error)
	ListNotes(context.Context, adminread.NoteListRequest) (adminread.NoteListResult, error)
	AddNote(context.Context, adminread.AddNoteRequest) (adminread.AdminNote, error)
	RevokeAccountSessions(context.Context, adminread.SessionRevocationRequest) (adminread.SessionRevocationResult, error)
	ListCourseOptions(context.Context, adminread.CourseOptionsRequest) ([]adminread.CourseOption, error)
	DiagnoseAccess(context.Context, adminread.AccessDiagnosticRequest) (adminread.AccessDiagnostic, error)
	ListSecurityEvents(context.Context, adminread.SecurityEventsRequest) (adminread.SecurityEventsResult, error)
}

type AdminFoundation struct {
	service          AdminService
	metricsService   adminread.AdminMetricsService
	userService      AdminUserService
	limiter          *ratelimit.Limiter
	endpointPolicies map[string]ratelimit.Policy
	recentAuthWindow time.Duration
}

type AdminFoundationOptions struct {
	Service          AdminService
	Limiter          *ratelimit.Limiter
	EndpointPolicies map[string]ratelimit.Policy
	RecentAuthWindow time.Duration
}

func NewAdminFoundation(options AdminFoundationOptions) (*AdminFoundation, error) {
	if options.Service == nil {
		return nil, errors.New("admin service is required")
	}
	policies := make(map[string]ratelimit.Policy, len(options.EndpointPolicies))
	for endpoint, policy := range options.EndpointPolicies {
		if endpoint == "" || endpoint != policy.Endpoint {
			return nil, errors.New("admin endpoint policy key does not match its endpoint")
		}
		if err := policy.Validate(); err != nil {
			return nil, err
		}
		policies[endpoint] = policy
	}
	recentAuthWindow := options.RecentAuthWindow
	if recentAuthWindow <= 0 {
		recentAuthWindow = 15 * time.Minute
	}
	return &AdminFoundation{
		service: options.Service, metricsService: metricsService(options.Service), userService: userService(options.Service), limiter: options.Limiter,
		endpointPolicies: policies, recentAuthWindow: recentAuthWindow,
	}, nil
}

func metricsService(service AdminService) adminread.AdminMetricsService {
	value, _ := service.(adminread.AdminMetricsService)
	return value
}

func userService(service AdminService) AdminUserService {
	value, _ := service.(AdminUserService)
	return value
}

func WithAdminFoundation(foundation *AdminFoundation) RouterOption {
	return func(options *routerOptions) error {
		if foundation == nil {
			return fmt.Errorf("admin foundation is required")
		}
		if options.admin != nil {
			return fmt.Errorf("admin foundation is already configured")
		}
		options.admin = foundation
		return nil
	}
}
