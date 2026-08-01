package media

import "testing"

func TestUploadValidationRequiresConfiguredTypeAndSizeBeforeAcceptance(t *testing.T) {
	valid := UploadRequest{Kind: KindVideo, Filename: "lesson.mp4", ContentType: "video/mp4", SizeBytes: 100}
	cases := []struct {
		name   string
		mutate func(*UploadRequest)
	}{
		{name: "unsupported content type", mutate: func(request *UploadRequest) { request.ContentType = "application/octet-stream" }},
		{name: "zero size", mutate: func(request *UploadRequest) { request.SizeBytes = 0 }},
		{name: "over configured limit", mutate: func(request *UploadRequest) { request.SizeBytes = 101 }},
		{name: "missing filename", mutate: func(request *UploadRequest) { request.Filename = "" }},
		{name: "unknown kind", mutate: func(request *UploadRequest) { request.Kind = AssetKind("OTHER") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := valid
			tc.mutate(&request)
			if err := validateUploadRequest(request, 100); err == nil {
				t.Fatal("invalid upload was accepted before a storage target could be issued")
			}
		})
	}
}

func TestContentValidationUsesBytesAndKeepsAcceptedTargetInQuarantine(t *testing.T) {
	if !contentMatchesDeclaredType([]byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}, "video/mp4") {
		t.Fatal("valid MP4 signature was rejected")
	}
	if contentMatchesDeclaredType([]byte("not an mp4"), "video/mp4") {
		t.Fatal("bytes without an MP4 signature were accepted")
	}
	request := CompleteUploadRequest{AssetVersionID: "00000000-0000-0000-0000-000000000001", ProviderEventID: "provider-event", StorageObjectKey: "public/source.mp4", StorageObjectVersion: "object-v1", SizeBytes: 1, SHA256Hex: "0000000000000000000000000000000000000000000000000000000000000000"}
	if err := validateCompletionRequest(request); err == nil {
		t.Fatal("completion outside quarantine was accepted")
	}
}
