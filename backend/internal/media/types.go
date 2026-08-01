package media

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

type AssetKind string

const (
	KindVideo       AssetKind = "VIDEO"
	KindResource    AssetKind = "RESOURCE"
	KindLabMaterial AssetKind = "LAB_MATERIAL"
	KindPreview     AssetKind = "PREVIEW"
)

func (k AssetKind) Valid() bool {
	switch k {
	case KindVideo, KindResource, KindLabMaterial, KindPreview:
		return true
	default:
		return false
	}
}

var (
	ErrNotFound               = errors.New("media asset not found")
	ErrNotAuthorized          = errors.New("media asset not authorized")
	ErrValidation             = errors.New("media validation failed")
	ErrConflict               = errors.New("media state conflict")
	ErrUnavailable            = errors.New("media dependency unavailable")
	ErrConcurrentModification = errors.New("media concurrent modification")
)

// ObjectStore is the narrow storage capability the media pipeline needs. The
// concrete S3/MinIO client implements it; tests can provide a contract fake.
type ObjectStore interface {
	PresignPutURL(context.Context, string, string, time.Duration) (string, error)
	HeadObjectVersion(context.Context, string, string) (sizeBytes int64, exists bool, err error)
	DownloadPrefixVersion(context.Context, string, string, int64) ([]byte, error)
	HashObjectVersion(context.Context, string, string) (string, error)
}

type UploadRequest struct {
	OwnerAccountID string
	CourseID       string
	LessonID       string
	LogicalAssetID string
	Kind           AssetKind
	ContentType    string
	SizeBytes      int64
}

type UploadTicket struct {
	AssetVersionID string    `json:"asset_version_id"`
	UploadURL      string    `json:"upload_url"`
	ExpiresAt      time.Time `json:"expires_at"`
}

type CompleteUploadRequest struct {
	OwnerAccountID       string
	AssetVersionID       string
	ProviderEventID      string
	StorageObjectKey     string
	StorageObjectVersion string
	ContentType          string
	SizeBytes            int64
	SHA256Hex            string
}

type CompletionResult struct {
	AssetVersionID string
	State          AssetVersionState
	Duplicate      bool
}

type AssetStatus struct {
	AssetVersionID    string
	LogicalAssetID    string
	Kind              AssetKind
	State             AssetVersionState
	SizeBytes         int64
	TrustedDurationMS *int64
	CreatedAt         time.Time
}

func (s AssetStatus) Deliverable() bool { return s.State.Deliverable() }

type Viewer struct {
	AccountID string
	Role      string
}

type RetryRequest struct {
	AssetVersionID  string
	AdminAccountID  string
	ActorDescriptor string
}

type ScanWork struct {
	AssetVersionID string `json:"asset_version_id"`
	ScanWorkID     string `json:"scan_work_id"`
}

type TranscodeWork struct {
	AssetVersionID string `json:"asset_version_id"`
	OperationID    string `json:"operation_id"`
}

type Rendition struct {
	Name             string
	StorageObjectKey string
	Width            int
	Height           int
	BitrateKbps      int
	DurationMS       int64
}

type TranscodeResult struct {
	OperationID       string
	OutputPrefix      string
	TrustedDurationMS int64
	Renditions        []Rendition
}

// Processor is the durable worker boundary for HLS processing. It returns
// trusted output metadata; clients never supply duration or rendition counts.
type Processor interface {
	Transcode(context.Context, ObjectVersion) (TranscodeResult, error)
}

type ServiceOptions struct {
	DB              *pgxpool.Pool
	Store           ObjectStore
	Outbox          *outbox.Writer
	Scanner         *ScannerAdapter
	UploadURLExpiry time.Duration
	MaxUploadBytes  int64
	Now             func() time.Time
}

const DefaultProcessingTimeout = 15 * time.Minute
