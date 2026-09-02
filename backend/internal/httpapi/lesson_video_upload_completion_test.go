package httpapi

import (
	"context"
	"errors"
	"testing"

	"github.com/Owlah2025/gradex/backend/internal/catalog"
	"github.com/Owlah2025/gradex/backend/internal/media"
)

type recordingUploadCompleter struct {
	requests []media.CompleteUploadRequest
	failure  error
}

func (c *recordingUploadCompleter) CompleteUpload(
	_ context.Context,
	request media.CompleteUploadRequest,
) (media.CompletionResult, error) {
	c.requests = append(c.requests, request)
	if c.failure != nil {
		return media.CompletionResult{}, c.failure
	}
	return media.CompletionResult{
		AssetVersionID: request.AssetVersionID,
		State:          media.StateProcessing,
		Duplicate:      len(c.requests) > 1,
	}, nil
}

type recordingLessonVideoSelector struct {
	requests  []catalog.ClaimLessonVideoUploadRequest
	failFirst error
}

func (s *recordingLessonVideoSelector) ClaimLessonVideoUpload(
	_ context.Context,
	request catalog.ClaimLessonVideoUploadRequest,
	_ string,
) (*catalog.LessonVideoUploadClaim, error) {
	s.requests = append(s.requests, request)
	if len(s.requests) == 1 && s.failFirst != nil {
		return nil, s.failFirst
	}
	return &catalog.LessonVideoUploadClaim{Selected: true}, nil
}

func combinedLessonVideoRequest() lessonVideoUploadCompletionRequest {
	return lessonVideoUploadCompletionRequest{
		completion: media.CompleteUploadRequest{
			OwnerAccountID: "owner", AssetVersionID: "video-b", ProviderEventID: "event-b",
			StorageObjectKey: "quarantine/course/video-b/source", StorageObjectVersion: "object-b",
			ContentType: "video/mp4", SizeBytes: 1024, SHA256Hex: "checksum-b",
		},
		selection: catalog.ClaimLessonVideoUploadRequest{
			CourseID: "course", RevisionID: "revision", LessonID: "lesson",
			VideoAssetVersionID: "video-b", OwnerAccountID: "owner",
		},
		actorDescriptor: "owner",
	}
}

func TestCombinedLessonVideoCompletionIsIdempotent(t *testing.T) {
	completer := &recordingUploadCompleter{}
	selector := &recordingLessonVideoSelector{}
	request := combinedLessonVideoRequest()

	first, err := runLessonVideoUploadCompletion(context.Background(), completer, selector, request)
	if err != nil || first.duplicate || !first.selected || first.state != media.StateProcessing {
		t.Fatalf("first completion = %+v, err=%v", first, err)
	}
	second, err := runLessonVideoUploadCompletion(context.Background(), completer, selector, request)
	if err != nil || !second.duplicate || !second.selected {
		t.Fatalf("duplicate completion = %+v, err=%v", second, err)
	}
	if len(completer.requests) != 2 || completer.requests[0] != completer.requests[1] {
		t.Fatalf("completion retry changed evidence: %+v", completer.requests)
	}
	if len(selector.requests) != 2 || selector.requests[0] != selector.requests[1] {
		t.Fatalf("selection retry changed target: %+v", selector.requests)
	}
}

func TestCombinedLessonVideoCompletionRetryConvergesAfterSelectionFailure(t *testing.T) {
	selectionFailure := errors.New("simulated crash before Lesson selection")
	completer := &recordingUploadCompleter{}
	selector := &recordingLessonVideoSelector{failFirst: selectionFailure}
	request := combinedLessonVideoRequest()

	_, err := runLessonVideoUploadCompletion(context.Background(), completer, selector, request)
	var completionError *lessonVideoUploadCompletionError
	if !errors.As(err, &completionError) || completionError.stage != lessonVideoSelectionStage || !errors.Is(err, selectionFailure) {
		t.Fatalf("first completion error = %v, want selection-stage failure", err)
	}
	retried, err := runLessonVideoUploadCompletion(context.Background(), completer, selector, request)
	if err != nil || !retried.duplicate || !retried.selected {
		t.Fatalf("retried completion = %+v, err=%v; want duplicate media completion and selected Lesson", retried, err)
	}
}

func TestFailedMediaCompletionNeverAttemptsLessonSelection(t *testing.T) {
	completionFailure := errors.New("browser object was not accepted")
	completer := &recordingUploadCompleter{failure: completionFailure}
	selector := &recordingLessonVideoSelector{}

	_, err := runLessonVideoUploadCompletion(
		context.Background(), completer, selector, combinedLessonVideoRequest(),
	)
	var completionError *lessonVideoUploadCompletionError
	if !errors.As(err, &completionError) || completionError.stage != lessonVideoMediaCompletionStage || !errors.Is(err, completionFailure) {
		t.Fatalf("completion error = %v, want media-stage failure", err)
	}
	if len(selector.requests) != 0 {
		t.Fatalf("failed media completion attempted Lesson selection: %+v", selector.requests)
	}
}
