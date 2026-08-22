package httpapi

import (
	"errors"
	"fmt"

	"github.com/Owlah2025/gradex/backend/internal/academic"
)

// AcademicFoundation mounts the D-091 Academic Catalog administration boundary.
//
// T1 scope: Admin-only management. No public, Student-onboarding, or Instructor
// selection surface is mounted here, and no Course path consumes it. Composing
// the router without this foundation leaves every existing route unchanged,
// which is what makes T1 rollback cheap.
type AcademicFoundation struct {
	repository *academic.Repository
}

type AcademicFoundationOptions struct {
	Repository *academic.Repository
}

func NewAcademicFoundation(options AcademicFoundationOptions) (*AcademicFoundation, error) {
	if options.Repository == nil {
		return nil, errors.New("academic repository is required")
	}
	return &AcademicFoundation{repository: options.Repository}, nil
}

func WithAcademicFoundation(foundation *AcademicFoundation) RouterOption {
	return func(options *routerOptions) error {
		if foundation == nil {
			return fmt.Errorf("academic foundation is required")
		}
		if options.academic != nil {
			return fmt.Errorf("academic catalog is already configured")
		}
		options.academic = foundation
		return nil
	}
}
