package httpapi

import (
	"context"
	"errors"
	"fmt"

	"github.com/Owlah2025/gradex/backend/internal/catalogpublic"
)

type PublicCatalogFoundation struct {
	repository        *catalogpublic.Repository
	searchEventWriter func(context.Context, string, int, string) error
}

func (f *PublicCatalogFoundation) Repository() *catalogpublic.Repository {
	if f == nil {
		return nil
	}
	return f.repository
}

type PublicCatalogFoundationOptions struct {
	Repository        *catalogpublic.Repository
	SearchEventWriter func(context.Context, string, int, string) error
}

func NewPublicCatalogFoundation(options PublicCatalogFoundationOptions) (*PublicCatalogFoundation, error) {
	if options.Repository == nil {
		return nil, errors.New("public catalogue repository is required")
	}
	writer := options.SearchEventWriter
	if writer == nil {
		writer = options.Repository.RecordSearchEvent
	}
	return &PublicCatalogFoundation{repository: options.Repository, searchEventWriter: writer}, nil
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
