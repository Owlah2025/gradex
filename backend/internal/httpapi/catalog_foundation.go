package httpapi

import (
	"errors"
	"fmt"

	"github.com/Owlah2025/gradex/backend/internal/catalog"
)

type CatalogFoundation struct {
	repository     *catalog.Repository
	ownership      CourseOwnershipChecker
	assetValidator catalog.AssetVersionValidator
}

type CatalogFoundationOptions struct {
	Repository     *catalog.Repository
	Ownership      CourseOwnershipChecker
	AssetValidator catalog.AssetVersionValidator
}

// NewCatalogFoundation constructs CatalogFoundation.
// Standing clause: required dependencies validated at construction.
func NewCatalogFoundation(options CatalogFoundationOptions) (*CatalogFoundation, error) {
	if options.Repository == nil {
		return nil, errors.New("catalog repository is required")
	}
	ownership := options.Ownership
	if ownership == nil {
		ownership = options.Repository
	}
	if options.AssetValidator == nil {
		return nil, errors.New("asset version validator is required")
	}
	return &CatalogFoundation{
		repository:     options.Repository,
		ownership:      ownership,
		assetValidator: options.AssetValidator,
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
