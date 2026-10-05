package media

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/outbox"
	"github.com/Owlah2025/gradex/backend/internal/storage"
)

type AssetKind string

const (
	KindVideo       AssetKind = "VIDEO"
	KindResource    AssetKind = "RESOURCE"
	KindLabMaterial AssetKind = "LAB_MATERIAL"
	KindPreview     AssetKind = "PREVIEW"
	KindThumbnail   AssetKind = "THUMBNAIL"
)

func (k AssetKind) Valid() bool {
	switch k {
	case KindVideo, KindResource, KindLabMaterial, KindPreview, KindThumbnail:
		return true
	default:
		return false
	}
}

// OperatingMode makes the deployment's media-safety posture explicit.
//
// Scanner mode keeps normal Instructor upload available but fail-closed behind
// malware scanning. Admin catalogue mode disables Instructor upload and accepts
// only an audited Admin procedure with exact out-of-band scan evidence.
// Trusted-Instructor mode is the bounded D-088 launch profile as amended by
// D-096: an ACTIVE vetted Instructor may upload only the approved MP4 Lesson
// video, PDF/DOCX Lesson Resource, and MP4 public Course preview types, which
// progress on exact-version validation evidence instead of malware scanning. A
// trusted preview still owes successful FFmpeg processing before READY.
// Everything outside that profile stays scanner-gated in every mode.
type OperatingMode string

const (
	OperatingModeScanner           OperatingMode = "SCANNER"
	OperatingModeAdminCatalogue    OperatingMode = "ADMIN_CATALOGUE"
	OperatingModeTrustedInstructor OperatingMode = "TRUSTED_INSTRUCTOR"
)

func (m OperatingMode) Valid() bool {
	return m == OperatingModeScanner ||
		m == OperatingModeAdminCatalogue ||
		m == OperatingModeTrustedInstructor
}

var (
	ErrNotFound               = errors.New("media asset not found")
	ErrNotAuthorized          = errors.New("media asset not authorized")
	ErrValidation             = errors.New("media validation failed")
	ErrContentTypeMismatch    = errors.New("media content type mismatch")
	ErrConflict               = errors.New("media state conflict")
	ErrUnavailable            = errors.New("media dependency unavailable")
	ErrConcurrentModification = errors.New("media concurrent modification")
	ErrStorageUnavailable     = errors.New("media storage unavailable")
	ErrInvalidMedia           = errors.New("invalid media")
	ErrProcessTimeout         = errors.New("media processing timeout")
	ErrTranscodeFailed        = errors.New("media transcode failed")
	ErrRetryScheduled         = errors.New("media retry scheduled")
	ErrEnhancementNotEligible = errors.New("media enhancement retry is not eligible")
	ErrEnhancementActive      = errors.New("media enhancement retry is already active")

	// ErrLeaseExpired is the refusal a worker receives when its work lease is no
	// longer valid according to *database* time. It wraps ErrConcurrentModification
	// because that is exactly what it is — the row now belongs to the recovery
	// pass or to a replacement attempt — so every existing caller that classifies
	// concurrent modification keeps behaving identically while the specific cause
	// stays legible in logs and tests.
	ErrLeaseExpired = fmt.Errorf("%w: media work lease expired", ErrConcurrentModification)
)

// ContentTypeMismatchError is returned only when the stored bytes are a
// recognized video container that contradicts the declared content type. The
// declared type is allowlisted by the upload intent and is safe for the HTTP
// layer to use when choosing a public violation message.
type ContentTypeMismatchError struct {
	DeclaredContentType string
	ActualContentType   string
}

func (e *ContentTypeMismatchError) Error() string {
	return fmt.Sprintf("%s: stored %s does not match declared %s", ErrContentTypeMismatch, e.ActualContentType, e.DeclaredContentType)
}

func (e *ContentTypeMismatchError) Unwrap() error { return ErrValidation }

func (e *ContentTypeMismatchError) Is(target error) bool {
	return target == ErrContentTypeMismatch
}

// ObjectStore is the narrow storage capability the media pipeline needs. The
// concrete S3/MinIO client implements it; tests can provide a contract fake.
type ObjectStore interface {
	PresignPutURL(context.Context, string, string, time.Duration) (string, error)
	HeadObjectVersion(context.Context, string, string) (sizeBytes int64, exists bool, err error)
	DownloadPrefixVersion(context.Context, string, string, int64) ([]byte, error)
	HashObjectVersion(context.Context, string, string) (string, error)
}

type multipartStore interface {
	CreateMultipartUpload(context.Context, string, string) (string, error)
	PresignUploadPartURL(context.Context, storage.MultipartPartUpload) (string, error)
	CompleteMultipartUpload(context.Context, string, string, []int32, []string) (string, error)
	AbortMultipartUpload(context.Context, string, string) error
	ListMultipartParts(context.Context, string, string) ([]storage.MultipartPart, error)
	MultipartObjectIdentity(context.Context, string) (string, error)
	DeleteMultipartObject(context.Context, string, string) error
	AbortMultipartKey(context.Context, string) error
	FindMultipartUpload(context.Context, string) (string, error)
}

// DeliveryStore is deliberately narrower than ObjectStore. Protected delivery
// can mint an expiry-bounded read URL only after the S4 evaluator allows the
// exact Asset Version; it cannot list, fetch, or make an object public.
type DeliveryStore interface {
	PresignGetURL(context.Context, string, time.Duration) (string, error)
	DownloadObject(context.Context, string) ([]byte, error)
}

type UploadRequest struct {
	ClientRequestID string
	OwnerAccountID  string
	CourseID        string
	// RevisionID is required only for a separately stored public-preview asset.
	// Lesson media is bound through LessonID instead.
	RevisionID     string
	LessonID       string
	LogicalAssetID string
	Kind           AssetKind
	ContentType    string
	SizeBytes      int64
}

type UploadTicket struct {
	AssetVersionID string `json:"asset_version_id"`
	UploadURL      string `json:"upload_url"`
	// StorageObjectKey is the quarantine key this ticket authorizes, and the
	// exact key the completion callback must echo back. It is not a secret and
	// carries no signing material: the presigned upload URL already contains
	// it, and the server re-derives the intent from the Asset Version rather
	// than trusting the caller's copy.
	StorageObjectKey string    `json:"storage_object_key"`
	ExpiresAt        time.Time `json:"expires_at"`
}

type MultipartCompletedPart struct {
	PartNumber int32
	ETag       string
}

type CompleteMultipartRequest struct {
	OwnerAccountID   string
	AssetVersionID   string
	ProviderEventID  string
	StorageObjectKey string
	ContentType      string
	SizeBytes        int64
	SHA256Hex        string
	UploadID         string
	Parts            []MultipartCompletedPart
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

	// The current processing attempt's own account of how far it has got.
	// All three are nil together: either an attempt has reported a measured
	// observation or none has, and a missing observation is reported as
	// missing rather than as zero.
	ProcessingStage           *ProcessingStage
	ProcessingProgressPercent *int
	ProcessingUpdatedAt       *time.Time
	FailureCategory           *string
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

// EnhancementWork is intentionally only the immutable Asset Version identity.
// Claim, source validation, ladder derivation, and operation identity belong to
// worker execution rather than queue creation.
type EnhancementWork struct {
	AssetVersionID string `json:"asset_version_id"`
	// AutoRecoveryIntentID is the automatic recovery intent that asked for this
	// work, and is also the id of the outbox event that carries it.
	//
	// It is optional, and omitted entirely for manual work. That is what keeps
	// the already-deployed 3C-B payload shape valid: a task carrying only
	// asset_version_id decodes to an empty intent here and is manual by
	// definition, which is exactly what it is. Automatic work always carries it,
	// and the worker refuses an intent the authoritative scheduler row no longer
	// names rather than treating an unrecognised one as manual.
	AutoRecoveryIntentID string `json:"auto_recovery_intent_id,omitempty"`
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
	OperationID        string
	OutputPrefix       string
	TrustedDurationMS  int64
	Renditions         []Rendition
	ExpectedRenditions []string
}

// EnhancementProbe is the execution-time source truth used to plan recovery.
type EnhancementProbe struct {
	ExpectedRenditions []string
	TrustedDurationMS  int64
}

// EnhancementProcessor is an optional extension of the normal processor. The
// full-processing interface remains unchanged; only a processor that can
// re-probe an exact source and encode a selected rung set may execute manual
// enhancement recovery.
type EnhancementProcessor interface {
	ProbeExpected(context.Context, ObjectVersion) (EnhancementProbe, error)
	TranscodeMissing(context.Context, ObjectVersion, []string, ProgressSink, VerifiedRenditionSink) (TranscodeResult, error)
}

// VerifiedRenditionSink receives fully verified progressive renditions as they
// complete storage upload and HEAD verification during transcoding. Calls are
// synchronous and block the next rendition encode until durable persistence is
// committed. Any error aborts the transcode pipeline.
type VerifiedRenditionSink interface {
	PersistVerifiedRendition(ctx context.Context, rendition Rendition) error
}

// ProgressiveProcessor is the processor capability that supports synchronous
// progressive rendition persistence as each rung finishes verification.
type ProgressiveProcessor interface {
	Processor
	TranscodeProgressive(ctx context.Context, object ObjectVersion, progress ProgressSink, renditions VerifiedRenditionSink) (TranscodeResult, error)
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

	// AutoRecoveryStateAvailable reports that the database is at schema 45 or
	// later, so an accepted manual RetryEnhancements request can durably suppress
	// automatic scheduling in the same transaction as its outbox event.
	//
	// It is a capability gate, not a feature switch: the suppression is written
	// whenever the state exists, regardless of whether the automatic reconciler is
	// enabled, because a queued automatic task can outlive the flag being turned
	// off. Below schema 45 there is no scheduler state to suppress.
	AutoRecoveryStateAvailable bool

	// The D-011 per-bucket caps enforced under BR-068. Each is optional and
	// falls back to the Default* value in limits.go; they are tunable
	// implementation parameters, not deployment switches, so composition
	// normally leaves them unset.
	ResourceMaxBytes          int64
	ResourceLessonMaxBytes    int64
	LabMaterialMaxBytes       int64
	LabMaterialLessonMaxBytes int64

	Now func() time.Time
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

const (
	DefaultWorkLeaseGrace = time.Minute
	MaxWorkAttempts       = 3
)
