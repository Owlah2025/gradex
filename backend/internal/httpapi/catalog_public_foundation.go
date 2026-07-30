package httpapi

import (
	"errors"
	"fmt"

	"github.com/Owlah2025/gradex/backend/internal/catalogpublic"
)

type PublicCatalogFoundation struct {
	repository *catalogpublic.Repository
}

type PublicCatalogFoundationOptions struct {
	Repository *catalogpublic.Repository
}

func NewPublicCatalogFoundation(options PublicCatalogFoundationOptions) (*PublicCatalogFoundation, error) {
	if options.Repository == nil {
		return nil, errors.New("public catalogue repository is required")
	}
	return &PublicCatalogFoundation{repository: options.Repository}, nil
}

func WithPublicCatalogFoundation(foundation *PublicCatalogFoundation) RouterOption {
	return func(options *routerOptions) error {
		if foundation == nil || foundation.repository == nil {
			return fmt.Errorf("complete public catalogue foundation is required")
		}
		if options.publicCatalog != nil {
			return fmt.Errorf("public catalogue foundation is already configured")
		}
		options.publicCatalog = foundation
		return nil
	}
}
