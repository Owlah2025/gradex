package httpapi

import (
	"errors"
	"fmt"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/access"
)

type AccessFoundation struct {
	repository          *access.Repository
	clock               func() time.Time
	salesWhatsAppNumber string
}

type AccessFoundationOptions struct {
	Repository          *access.Repository
	Clock               func() time.Time
	SalesWhatsAppNumber string
}

func NewAccessFoundation(options AccessFoundationOptions) (*AccessFoundation, error) {
	if options.Repository == nil {
		return nil, errors.New("access repository is required")
	}
	clock := options.Clock
	if clock == nil {
		clock = time.Now
	}
	salesWhatsAppNumber := options.SalesWhatsAppNumber
	if salesWhatsAppNumber == "" {
		// The test-only default keeps existing isolated route fixtures
		// deterministic. Real application composition receives the validated
		// configuration value.
		salesWhatsAppNumber = "15550000000"
	}
	return &AccessFoundation{
		repository: options.Repository, clock: clock, salesWhatsAppNumber: salesWhatsAppNumber,
	}, nil
}

func WithAccessFoundation(foundation *AccessFoundation) RouterOption {
	return func(options *routerOptions) error {
		if foundation == nil {
			return fmt.Errorf("access foundation is required")
		}
		if options.access != nil {
			return fmt.Errorf("access foundation is already configured")
		}
		options.access = foundation
		return nil
	}
}
