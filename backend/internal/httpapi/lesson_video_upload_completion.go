package httpapi

import (
	"context"
	"fmt"

	"github.com/Owlah2025/gradex/backend/internal/catalog"
	"github.com/Owlah2025/gradex/backend/internal/media"
)

type mediaUploadCompleter interface {
	CompleteUpload(context.Context, media.CompleteUploadRequest) (media.CompletionResult, error)
}

type lessonVideoUploadSelector interface {
	ClaimLessonVideoUpload(context.Context, catalog.ClaimLessonVideoUploadRequest, string) (*catalog.LessonVideoUploadClaim, error)
}

type lessonVideoUploadCompletionRequest struct {
	completion      media.CompleteUploadRequest
	selection       catalog.ClaimLessonVideoUploadRequest
	actorDescriptor string
}

type lessonVideoUploadCompletionResult struct {
	assetVersionID string
	state          media.AssetVersionState
	duplicate      bool
	selected       bool
}

type lessonVideoUploadCompletionStage string

const (
	lessonVideoMediaCompletionStage lessonVideoUploadCompletionStage = "MEDIA_COMPLETION"
	lessonVideoSelectionStage       lessonVideoUploadCompletionStage = "LESSON_SELECTION"
)

type lessonVideoUploadCompletionError struct {
	stage lessonVideoUploadCompletionStage
	cause error
}

func (e *lessonVideoUploadCompletionError) Error() string {
	return fmt.Sprintf("%s: %v", e.stage, e.cause)
}

func (e *lessonVideoUploadCompletionError) Unwrap() error { return e.cause }

func runLessonVideoUploadCompletion(
	ctx context.Context,
	completer mediaUploadCompleter,
	selector lessonVideoUploadSelector,
	request lessonVideoUploadCompletionRequest,
) (lessonVideoUploadCompletionResult, error) {
	completed, err := completer.CompleteUpload(ctx, request.completion)
	if err != nil {
		return lessonVideoUploadCompletionResult{}, &lessonVideoUploadCompletionError{
			stage: lessonVideoMediaCompletionStage, cause: err,
		}
	}
	claim, err := selector.ClaimLessonVideoUpload(ctx, request.selection, request.actorDescriptor)
	if err != nil {
		return lessonVideoUploadCompletionResult{}, &lessonVideoUploadCompletionError{
			stage: lessonVideoSelectionStage, cause: err,
		}
	}
	return lessonVideoUploadCompletionResult{
		assetVersionID: completed.AssetVersionID,
		state:          completed.State,
		duplicate:      completed.Duplicate,
		selected:       claim.Selected,
	}, nil
}
