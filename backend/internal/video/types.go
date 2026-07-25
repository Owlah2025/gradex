package video

import (
	"errors"
	"time"
)

type Status string

const (
	StatusDraft      Status = "DRAFT"
	StatusUploading  Status = "UPLOADING"
	StatusUploaded   Status = "UPLOADED"
	StatusQueued     Status = "QUEUED"
	StatusProcessing Status = "PROCESSING"
	StatusReady      Status = "READY"
	StatusPublished  Status = "PUBLISHED"
	StatusFailed     Status = "FAILED"
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

type Lesson struct {
	ID              string
	CourseID        string
	Status          Status
	DurationSeconds *float64
}

type Video struct {
	ID              string
	LessonID        string
	Status          Status
	FailedReason    *string
	RetryCount      int
	RawKey          *string
	HLSMasterKey    *string
	Resolution      *string
	Bitrate         *int
	Codec           *string
	FPS             *float64
	FileSizeBytes   *int64
	Provider        string
	ProviderAssetID *string
	ProviderStatus  *string
	SyncVersion     int
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

// Job payloads, shared between the API (enqueue) and worker (decode) sides.
type MetadataExtractPayload struct {
	VideoID  string `json:"video_id"`
	LessonID string `json:"lesson_id"`
	RawKey   string `json:"raw_key"`
}

type TranscodeJobPayload struct {
	VideoID    string `json:"video_id"`
	LessonID   string `json:"lesson_id"`
	RawKey     string `json:"raw_key"`
	Resolution string `json:"resolution"`
}
