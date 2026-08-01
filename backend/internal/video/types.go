package video

import (
	"context"
	"errors"
	"time"
)

// Error classes the HTTP boundary maps onto public problem types. They exist
// so httpapi can classify a failure without matching on message text, and so
// that an internal message — which carries object keys, queue names, and
// provider text — never has to be inspected outside this package.
//
// ErrConflict previously carried both genuine state conflicts and field
// validation failures. They map to different statuses, so they are separate
// classes now.
var (
	// ErrNotFound is an absent or concealed resource.
	ErrNotFound = errors.New("not found")
	// ErrConflict is a valid command that cannot run against the resource's
	// current state.
	ErrConflict = errors.New("conflict")
	// ErrConcurrentModification is a lost compare-and-swap: the command was
	// legal when it started, and another writer won. Retrying may succeed,
	// which is what separates it from ErrConflict.
	ErrConcurrentModification = errors.New("concurrent modification")
	// ErrValidation is a semantically unacceptable field value.
	ErrValidation = errors.New("validation failed")
	// ErrUnavailable is a temporary storage, queue, or provider failure —
	// retryable, and distinct from an unexpected fault.
	ErrUnavailable = errors.New("dependency unavailable")
)

// Service is retained only as a compile-time compatibility seam for tests
// that verify the legacy route was removed from production composition. There
// is deliberately no constructor or implementation in this package anymore;
// D7 production behavior lives in internal/media.
type VideoService interface {
	RequestUpload(context.Context, string, string, string) (UploadTicket, error)
	CompleteUpload(context.Context, string) error
	GetPlaybackURL(context.Context, string, string) (SignedURL, error)
	Retranscode(context.Context, string) error
}

type Service interface {
	VideoService
	Publish(context.Context, string) error
	UpdateProgress(context.Context, string, string, float64) (Progress, error)
	ServeManifest(context.Context, string, string, string) ([]byte, string, error)
}

type Progress struct {
	UserID              string
	LessonID            string
	MaxPositionSeconds  float64
	LastPositionSeconds float64
	Completed           bool
}

type UploadTicket struct {
	UploadURL string
	RawKey    string
	ExpiresAt time.Time
}

type SignedURL struct {
	URL                 string
	ExpiresAt           time.Time
	LastPositionSeconds float64
}
