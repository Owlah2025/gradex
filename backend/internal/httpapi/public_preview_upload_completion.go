package httpapi

import (
	"context"
	"fmt"

	"github.com/Owlah2025/gradex/backend/internal/catalog"
	"github.com/Owlah2025/gradex/backend/internal/media"
)

type publicPreviewUploadSelector interface {
	ClaimPublicPreviewUpload(context.Context, catalog.ClaimPublicPreviewUploadRequest, string) (*catalog.PublicPreviewUploadClaim, error)
}

type publicPreviewUploadCompletionRequest struct {
	completion      media.CompleteUploadRequest
	selection       catalog.ClaimPublicPreviewUploadRequest
	actorDescriptor string
}

type publicPreviewUploadCompletionResult struct {
	assetVersionID string
	state          media.AssetVersionState
	duplicate      bool
	selected       bool
	revision       *catalog.CourseRevision
}

type publicPreviewUploadCompletionStage string

const (
	publicPreviewMediaCompletionStage publicPreviewUploadCompletionStage = "MEDIA_COMPLETION"
	publicPreviewSelectionStage       publicPreviewUploadCompletionStage = "PREVIEW_SELECTION"
)

type publicPreviewUploadCompletionError struct {
	stage publicPreviewUploadCompletionStage
	cause error
}

func (e *publicPreviewUploadCompletionError) Error() string {
	return fmt.Sprintf("%s: %v", e.stage, e.cause)
}

func (e *publicPreviewUploadCompletionError) Unwrap() error { return e.cause }

// runPublicPreviewUploadCompletion is the public-preview twin of
// runLessonVideoUploadCompletion, and deliberately the same shape.
//
// Two operations, one browser request, no distributed transaction. Convergence
// comes from each half being individually idempotent: CompleteUpload is keyed
// on the provider event identifier and answers a replay with the same result
// marked Duplicate, and ClaimPublicPreviewUpload is a compare-and-set against
// durable upload-intent order that returns the existing selection unchanged
// when the incoming version is already selected.
//
// So a retry after the connection dropped between the two halves re-runs
// completion (duplicate, success) and then reaches the selection it never got
// to perform. That is why the duplicate result is not an early return.
func runPublicPreviewUploadCompletion(
	ctx context.Context,
	completer mediaUploadCompleter,
	selector publicPreviewUploadSelector,
	request publicPreviewUploadCompletionRequest,
) (publicPreviewUploadCompletionResult, error) {
	completed, err := completer.CompleteUpload(ctx, request.completion)
	if err != nil {
		return publicPreviewUploadCompletionResult{}, &publicPreviewUploadCompletionError{
			stage: publicPreviewMediaCompletionStage, cause: err,
		}
	}
	claim, err := selector.ClaimPublicPreviewUpload(ctx, request.selection, request.actorDescriptor)
	if err != nil {
		return publicPreviewUploadCompletionResult{}, &publicPreviewUploadCompletionError{
			stage: publicPreviewSelectionStage, cause: err,
		}
	}
	return publicPreviewUploadCompletionResult{
		assetVersionID: completed.AssetVersionID,
		state:          completed.State,
		duplicate:      completed.Duplicate,
		selected:       claim.Selected,
		revision:       claim.Revision,
	}, nil
}
