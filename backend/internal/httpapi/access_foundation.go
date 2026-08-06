package httpapi

import (
	"errors"
	"fmt"

	"github.com/Owlah2025/gradex/backend/internal/access"
)

type AccessFoundation struct {
	repository *access.Repository
}

type AccessFoundationOptions struct {
	Repository *access.Repository
}

func NewAccessFoundation(options AccessFoundationOptions) (*AccessFoundation, error) {
	if options.Repository == nil {
		return nil, errors.New("access repository is required")
	}
	return &AccessFoundation{repository: options.Repository}, nil
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
