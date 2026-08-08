package httpapi

import (
	"context"
	"errors"
	"fmt"

	"github.com/Owlah2025/gradex/backend/internal/media"
)

// mediaDeliveryIssuer is the narrow production boundary used by the live
// router. Keeping it at the HTTP edge lets route-composition tests exercise
// the shipped middleware and refusal constructor without recreating handlers
// or routes in test code.
type mediaDeliveryIssuer interface {
	IssuePlayback(context.Context, media.PlaybackRequest) (media.PlaybackAuthorization, error)
	IssuePlaybackManifest(context.Context, string, string) (media.PlaybackManifest, error)
	IssueDownload(context.Context, media.DownloadRequest) (media.DownloadAuthorization, error)
	IssueDownloadEntry(context.Context, media.DownloadEntryRequest) (media.DownloadAuthorization, error)
	IssuePreview(context.Context, string) (media.PreviewAuthorization, error)
}

// LearningMedia returns the same already-composed S4 delivery boundary used by
// media routes. Learning therefore cannot construct a second signer.
func (f *MediaFoundation) LearningMedia() LearningMedia {
	if f == nil {
		return nil
	}
	service, ok := f.delivery.(LearningMedia)
	if !ok {
		return nil
	}
	return service
}

// MediaFoundation is the D7 byte-pipeline boundary exposed to HTTP. It does
// not contain Course publication or entitlement decisions; those remain in
// their owning slices.
type MediaFoundation struct {
	service  *media.Service
	delivery mediaDeliveryIssuer
}

type MediaFoundationOptions struct {
	Service  *media.Service
	Delivery mediaDeliveryIssuer
}

func NewMediaFoundation(options MediaFoundationOptions) (*MediaFoundation, error) {
	if options.Service == nil {
		return nil, errors.New("media service is required")
	}
	return &MediaFoundation{service: options.Service, delivery: options.Delivery}, nil
}

// WithMediaFoundation mounts D7 pipeline routes and, when the complete D8
// delivery dependency set is supplied, protected issuance routes.
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
