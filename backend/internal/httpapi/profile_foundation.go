package httpapi

import (
	"errors"
	"fmt"

	"github.com/Owlah2025/gradex/backend/internal/catalogpublic"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/instructorprofile"
)

type ProfileFoundation struct {
	instructor *instructorprofile.Repository
	account    *identity.AccountProfileService
	catalog    *catalogpublic.Repository
}

type ProfileFoundationOptions struct {
	Instructor *instructorprofile.Repository
	Account    *identity.AccountProfileService
	Catalog    *catalogpublic.Repository
}

func NewProfileFoundation(options ProfileFoundationOptions) (*ProfileFoundation, error) {
	if options.Instructor == nil {
		return nil, errors.New("instructor profile repository is required")
	}
	if options.Account == nil {
		return nil, errors.New("account profile service is required")
	}
	if options.Catalog == nil {
		return nil, errors.New("public catalogue repository is required")
	}
	return &ProfileFoundation{
		instructor: options.Instructor,
		account:    options.Account,
		catalog:    options.Catalog,
	}, nil
}

func WithProfileFoundation(foundation *ProfileFoundation) RouterOption {
	return func(options *routerOptions) error {
		if foundation == nil {
			return fmt.Errorf("profile foundation is required")
		}
		if options.profiles != nil {
			return fmt.Errorf("profile foundation is already configured")
		}
		options.profiles = foundation
		return nil
	}
}
