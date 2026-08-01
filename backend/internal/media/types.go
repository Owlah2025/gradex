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

// OperatingMode makes the unresolved LG-014 response explicit. Scanner mode
// keeps normal Instructor upload available but fail-closed; Admin catalogue
// mode disables it and accepts only an audited Admin procedure with exact
// out-of-band scan evidence.
type OperatingMode string

const (
	OperatingModeScanner        OperatingMode = "SCANNER"
	OperatingModeAdminCatalogue OperatingMode = "ADMIN_CATALOGUE"
)

func (m OperatingMode) Valid() bool {
	return m == OperatingModeScanner || m == OperatingModeAdminCatalogue
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

// DeliveryStore is deliberately narrower than ObjectStore. Protected delivery
// can mint an expiry-bounded read URL only after the S4 evaluator allows the
// exact Asset Version; it cannot list, fetch, or make an object public.
type DeliveryStore interface {
	PresignGetURL(context.Context, string, time.Duration) (string, error)
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
	OperatingMode   OperatingMode
	Now             func() time.Time
}

type CatalogueLoadRequest struct {
	AdminAccountID string
	CourseID       string
	LessonID       string
	LogicalAssetID string
	Kind           AssetKind
	ContentType    string
	SizeBytes      int64
}

type CatalogueCompletionRequest struct {
	AdminAccountID       string
	AssetVersionID       string
	ProviderEventID      string
	StorageObjectKey     string
	StorageObjectVersion string
	ContentType          string
	SizeBytes            int64
	SHA256Hex            string
}

type OutOfBandScanEvidence struct {
	AdminAccountID       string
	AssetVersionID       string
	StorageObjectVersion string
	Method               string
	Provider             string
	Reference            string
}

const DefaultProcessingTimeout = 15 * time.Minute
