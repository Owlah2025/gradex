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

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
)

type Lesson struct {
	ID              string
	CourseID        string
	Status          Status
	DurationSeconds *float64
}

type Video struct {
	ID                string
	LessonID          string
	Status            Status
	FailedReason      *string
	RetryCount        int
	RawKey            *string
	HLSMasterKey      *string
	Resolution        *string
	Bitrate           *int
	Codec             *string
	FPS               *float64
	FileSizeBytes     *int64
	Provider          string
	ProviderAssetID   *string
	ProviderStatus    *string
	SyncVersion       int
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
