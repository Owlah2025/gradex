package media

import "testing"

func TestUploadValidationRequiresConfiguredTypeAndSizeBeforeAcceptance(t *testing.T) {
	valid := UploadRequest{Kind: KindVideo, ContentType: "video/mp4", SizeBytes: 100}
	cases := []struct {
		name   string
		mutate func(*UploadRequest)
	}{
		{name: "unsupported content type", mutate: func(request *UploadRequest) { request.ContentType = "application/octet-stream" }},
		{name: "zero size", mutate: func(request *UploadRequest) { request.SizeBytes = 0 }},
		{name: "over configured limit", mutate: func(request *UploadRequest) { request.SizeBytes = 101 }},
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
	validMP4 := []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm', 0, 0, 0, 1}
	if !contentMatchesDeclaredType(validMP4, "video/mp4") {
		t.Fatal("valid MP4 signature was rejected")
	}
	if contentMatchesDeclaredType([]byte{0x00, 0xff, 0x01, 0x7f, 0x80, 0xfe, 0x17, 0xaa, 0x55, 0xc3}, "video/mp4") {
		t.Fatal("arbitrary binary declared as MP4 was accepted")
	}
	if contentMatchesDeclaredType([]byte("%PDF-1.7\n"), "video/mp4") {
		t.Fatal("mismatched declared MP4 type was accepted")
	}
	if contentMatchesDeclaredType([]byte{0, 0, 0, 24, 'f', 't', 'y', 'p'}, "video/mp4") {
		t.Fatal("inconclusive MP4 evidence was accepted")
	}
	if contentMatchesDeclaredType([]byte{0, 0, 0, 8, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm', 0, 0, 0, 1}, "video/mp4") {
		t.Fatal("invalid MP4 file-type box length was accepted")
	}
	request := CompleteUploadRequest{AssetVersionID: "00000000-0000-0000-0000-000000000001", ProviderEventID: "provider-event", StorageObjectKey: "public/source.mp4", StorageObjectVersion: "object-v1", SizeBytes: 1, SHA256Hex: "0000000000000000000000000000000000000000000000000000000000000000"}
	if err := validateCompletionRequest(request); err == nil {
		t.Fatal("completion outside quarantine was accepted")
	}
}
