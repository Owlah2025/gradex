package httpapi

import (
	"context"
	"errors"
	"fmt"

	"github.com/Owlah2025/gradex/backend/internal/catalog"
	"github.com/Owlah2025/gradex/backend/internal/learning"
)

type moderationReportRepository interface {
	ListAdminReports(context.Context, learning.AdminReportPageRequest) (learning.AdminReportPage, error)
	GetAdminReport(context.Context, string) (learning.AdminReport, error)
	ResolveAdminReport(context.Context, learning.AdminReportResolution, learning.AdminReportActionExecutor) (learning.AdminReport, error)
}

type ModerationFoundation struct {
	reports moderationReportRepository
	catalog *catalog.Repository
}

type ModerationFoundationOptions struct {
	Reports moderationReportRepository
	Catalog *catalog.Repository
}

func NewModerationFoundation(options ModerationFoundationOptions) (*ModerationFoundation, error) {
	if options.Reports == nil {
		return nil, errors.New("moderation report repository is required")
	}
	if options.Catalog == nil {
		return nil, errors.New("moderation catalog repository is required")
	}
	return &ModerationFoundation{reports: options.Reports, catalog: options.Catalog}, nil
}

func WithModerationFoundation(foundation *ModerationFoundation) RouterOption {
	return func(options *routerOptions) error {
		if foundation == nil {
			return fmt.Errorf("moderation foundation is required")
		}
		if options.moderation != nil {
			return fmt.Errorf("moderation foundation is already configured")
		}
		options.moderation = foundation
		return nil
	}
}
