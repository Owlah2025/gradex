package httpapi

import (
	"errors"
	"fmt"

	"github.com/Owlah2025/gradex/backend/internal/catalog"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

type CatalogFoundation struct {
	repository     *catalog.Repository
	ownership      CourseOwnershipChecker
	assetValidator catalog.AssetVersionValidator
	outboxWriter   *outbox.Writer
}

type CatalogFoundationOptions struct {
	Repository     *catalog.Repository
	Ownership      CourseOwnershipChecker
	AssetValidator catalog.AssetVersionValidator
	OutboxWriter   *outbox.Writer
}

// NewCatalogFoundation constructs CatalogFoundation.
// Standing clause: required dependencies validated at construction.
func NewCatalogFoundation(options CatalogFoundationOptions) (*CatalogFoundation, error) {
	ownership := options.Ownership
	if ownership == nil && options.Repository != nil {
		ownership = options.Repository
	}
	if ownership == nil {
		return nil, errors.New("catalog repository or ownership checker is required")
	}
	if options.AssetValidator == nil {
		return nil, errors.New("asset version validator is required")
	}
	if options.OutboxWriter == nil {
		return nil, errors.New("outbox writer is required")
	}
	return &CatalogFoundation{
		repository:     options.Repository,
		ownership:      ownership,
		assetValidator: options.AssetValidator,
		outboxWriter:   options.OutboxWriter,
	}, nil
}

// WithCatalogFoundation mounts catalog authoring and review routes.
func WithCatalogFoundation(foundation *CatalogFoundation) RouterOption {
	return func(options *routerOptions) error {
		if foundation == nil {
			return fmt.Errorf("catalog foundation is required")
		}
		if options.catalog != nil {
			return fmt.Errorf("catalog foundation is already configured")
		}
		options.catalog = foundation
		return nil
	}
}
