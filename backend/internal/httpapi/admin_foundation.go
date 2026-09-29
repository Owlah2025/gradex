package httpapi

import (
	"context"
	"errors"
	"fmt"

	adminread "github.com/Owlah2025/gradex/backend/internal/admin"
)

type AdminService interface {
	SearchAccounts(context.Context, adminread.AccountDirectoryRequest) (adminread.AccountDirectoryResult, error)
	GetAccount(context.Context, adminread.AccountIdentityRequest) (adminread.AccountDirectoryEntry, error)
	ListAuditEvents(context.Context, adminread.AuditEventRequest) (adminread.AuditEventResult, error)
}

type AdminFoundation struct {
	service AdminService
}

type AdminFoundationOptions struct {
	Service AdminService
}

func NewAdminFoundation(options AdminFoundationOptions) (*AdminFoundation, error) {
	if options.Service == nil {
		return nil, errors.New("admin service is required")
	}
	return &AdminFoundation{service: options.Service}, nil
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
