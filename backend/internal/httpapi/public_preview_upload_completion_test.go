package httpapi

import (
	"context"
	"errors"
	"testing"

	"github.com/Owlah2025/gradex/backend/internal/catalog"
	"github.com/Owlah2025/gradex/backend/internal/media"
)

type recordingPublicPreviewSelector struct {
	requests  []catalog.ClaimPublicPreviewUploadRequest
	failFirst error
	selected  bool
}

func (s *recordingPublicPreviewSelector) ClaimPublicPreviewUpload(
	_ context.Context,
	request catalog.ClaimPublicPreviewUploadRequest,
	_ string,
) (*catalog.PublicPreviewUploadClaim, error) {
	s.requests = append(s.requests, request)
	if len(s.requests) == 1 && s.failFirst != nil {
		return nil, s.failFirst
	}
	return &catalog.PublicPreviewUploadClaim{Selected: !s.loses()}, nil
}

func (s *recordingPublicPreviewSelector) loses() bool { return s.selected }

func combinedPublicPreviewRequest() publicPreviewUploadCompletionRequest {
	return publicPreviewUploadCompletionRequest{
		completion: media.CompleteUploadRequest{
			OwnerAccountID: "owner", AssetVersionID: "preview-b", ProviderEventID: "event-b",
			StorageObjectKey: "quarantine/course/preview-b/source", StorageObjectVersion: "object-b",
			ContentType: "video/mp4", SizeBytes: 1024, SHA256Hex: "checksum-b",
		},
		selection: catalog.ClaimPublicPreviewUploadRequest{
			CourseID: "course", RevisionID: "revision",
			PreviewAssetVersionID: "preview-b", OwnerAccountID: "owner",
		},
		actorDescriptor: "owner",
	}
}

func TestCombinedPublicPreviewCompletionIsIdempotent(t *testing.T) {
	completer := &recordingUploadCompleter{}
	selector := &recordingPublicPreviewSelector{}
	request := combinedPublicPreviewRequest()

	first, err := runPublicPreviewUploadCompletion(context.Background(), completer, selector, request)
	if err != nil || first.duplicate || !first.selected || first.state != media.StateProcessing {
		t.Fatalf("first completion = %+v, err=%v", first, err)
	}
	second, err := runPublicPreviewUploadCompletion(context.Background(), completer, selector, request)
	if err != nil || !second.duplicate || !second.selected {
		t.Fatalf("duplicate completion = %+v, err=%v; a replay must still carry the selection", second, err)
	}
	if len(completer.requests) != 2 || completer.requests[0] != completer.requests[1] {
		t.Fatalf("completion retry changed evidence: %+v", completer.requests)
	}
	if len(selector.requests) != 2 || selector.requests[0] != selector.requests[1] {
		t.Fatalf("selection retry changed target: %+v", selector.requests)
	}
}

// The exact production crash this operation exists for: the completion
// persisted, the connection died before the preview was selected, and the
// browser retried the identical evidence.
func TestCombinedPublicPreviewCompletionRetryConvergesAfterSelectionFailure(t *testing.T) {
	selectionFailure := errors.New("simulated crash before preview selection")
	completer := &recordingUploadCompleter{}
	selector := &recordingPublicPreviewSelector{failFirst: selectionFailure}
	request := combinedPublicPreviewRequest()

	_, err := runPublicPreviewUploadCompletion(context.Background(), completer, selector, request)
	var completionError *publicPreviewUploadCompletionError
	if !errors.As(err, &completionError) || completionError.stage != publicPreviewSelectionStage || !errors.Is(err, selectionFailure) {
		t.Fatalf("first completion error = %v, want selection-stage failure", err)
	}
	retried, err := runPublicPreviewUploadCompletion(context.Background(), completer, selector, request)
	if err != nil || !retried.duplicate || !retried.selected {
		t.Fatalf("retried completion = %+v, err=%v; want duplicate media completion and a selected preview", retried, err)
	}
}

func TestFailedMediaCompletionNeverAttemptsPreviewSelection(t *testing.T) {
	completionFailure := errors.New("browser object was not accepted")
	completer := &recordingUploadCompleter{failure: completionFailure}
	selector := &recordingPublicPreviewSelector{}

	_, err := runPublicPreviewUploadCompletion(
		context.Background(), completer, selector, combinedPublicPreviewRequest(),
	)
	var completionError *publicPreviewUploadCompletionError
	if !errors.As(err, &completionError) || completionError.stage != publicPreviewMediaCompletionStage || !errors.Is(err, completionFailure) {
		t.Fatalf("completion error = %v, want media-stage failure", err)
	}
	if len(selector.requests) != 0 {
		t.Fatalf("failed media completion attempted preview selection: %+v", selector.requests)
	}
}

// A losing upload still completes successfully; only the selection is refused.
// The browser must be able to tell those apart, which is what `selected` is for.
func TestSupersededPublicPreviewCompletionSucceedsWithoutSelecting(t *testing.T) {
	completer := &recordingUploadCompleter{}
	selector := &recordingPublicPreviewSelector{selected: true}

	result, err := runPublicPreviewUploadCompletion(
		context.Background(), completer, selector, combinedPublicPreviewRequest(),
	)
	if err != nil {
		t.Fatalf("superseded completion errored: %v", err)
	}
	if result.selected {
		t.Fatal("a superseded upload reported itself as the selected preview")
	}
	if result.state != media.StateProcessing {
		t.Fatalf("superseded completion state = %q, want the media completion to have succeeded", result.state)
	}
}
