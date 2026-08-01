package httpapi

import (
	"errors"
	"fmt"

	"github.com/Owlah2025/gradex/backend/internal/media"
)

// MediaFoundation is the D7 byte-pipeline boundary exposed to HTTP. It does
// not contain Course publication or entitlement decisions; those remain in
// their owning slices.
type MediaFoundation struct {
	service *media.Service
}

type MediaFoundationOptions struct {
	Service *media.Service
}

func NewMediaFoundation(options MediaFoundationOptions) (*MediaFoundation, error) {
	if options.Service == nil {
		return nil, errors.New("media service is required")
	}
	return &MediaFoundation{service: options.Service}, nil
}

// WithMediaFoundation mounts only the D7 upload/status/retry routes. It does
// not mount protected playback or download issuance, which are D8 concerns.
func WithMediaFoundation(foundation *MediaFoundation) RouterOption {
	return func(options *routerOptions) error {
		if foundation == nil || foundation.service == nil {
			return fmt.Errorf("complete media foundation is required")
		}
		if options.media != nil {
			return fmt.Errorf("media foundation is already configured")
		}
		options.media = foundation
		return nil
	}
}
