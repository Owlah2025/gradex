package media

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeOutputFixture(t *testing.T, root, relative, contents string) {
	t.Helper()
	path := filepath.Join(root, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestValidateLocalHLSOutputRejectsPartialObjectSets(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
	}{
		{name: "missing master", files: map[string]string{
			"720p/playlist.m3u8": "#EXTM3U\nsegment000.ts\n", "720p/segment000.ts": "segment",
		}},
		{name: "missing referenced segment", files: map[string]string{
			"master.m3u8": "#EXTM3U\n", "720p/playlist.m3u8": "#EXTM3U\nsegment000.ts\n",
		}},
		{name: "unsafe external segment", files: map[string]string{
			"master.m3u8": "#EXTM3U\n", "720p/playlist.m3u8": "#EXTM3U\nhttps://evil.test/segment.ts\n",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			files := make([]string, 0, len(tc.files))
			for relative, body := range tc.files {
				writeOutputFixture(t, root, relative, body)
				files = append(files, relative)
			}
			if err := validateLocalHLSOutput(root, files); !errors.Is(err, ErrTranscodeFailed) {
				t.Fatalf("validation error=%v, want ErrTranscodeFailed", err)
			}
		})
	}
}

type recordingProcessingStore struct {
	objects  map[string][]byte
	putOrder []string
	headFail string
}

func (*recordingProcessingStore) DownloadToFileVersion(context.Context, string, string) (string, func(), error) {
	return "", func() {}, errors.New("not used")
}

func (s *recordingProcessingStore) PutObject(_ context.Context, key string, body []byte, _ string) error {
	if s.objects == nil {
		s.objects = make(map[string][]byte)
	}
	s.objects[key] = append([]byte(nil), body...)
	s.putOrder = append(s.putOrder, key)
	return nil
}

func (s *recordingProcessingStore) HeadObject(_ context.Context, key string) (int64, bool, error) {
	if key == s.headFail {
		return 0, false, errors.New("injected HEAD failure")
	}
	body, ok := s.objects[key]
	return int64(len(body)), ok, nil
}

func (*recordingProcessingStore) DeletePrefix(context.Context, string) error { return nil }

func TestUploadHLSVerifiesEveryObjectAndPublishesMasterLast(t *testing.T) {
	root := t.TempDir()
	writeOutputFixture(t, root, "master.m3u8", "#EXTM3U\n720p/playlist.m3u8\n")
	writeOutputFixture(t, root, "720p/playlist.m3u8", "#EXTM3U\nsegment000.ts\n")
	writeOutputFixture(t, root, "720p/segment000.ts", "segment")
	store := &recordingProcessingStore{}
	processor := &FFmpegProcessor{store: store}
	if err := processor.uploadHLS(context.Background(), root, "media/version/hls/attempt"); err != nil {
		t.Fatalf("uploadHLS: %v", err)
	}
	if got := store.putOrder[len(store.putOrder)-1]; got != "media/version/hls/attempt/master.m3u8" {
		t.Fatalf("last uploaded object=%s, want master manifest", got)
	}

	store = &recordingProcessingStore{headFail: "media/version/hls/attempt/720p/segment000.ts"}
	processor.store = store
	if err := processor.uploadHLS(context.Background(), root, "media/version/hls/attempt"); !errors.Is(err, ErrStorageUnavailable) {
		t.Fatalf("HEAD failure error=%v, want ErrStorageUnavailable", err)
	}
}

func TestProcessorDiagnosticsAreBounded(t *testing.T) {
	var diagnostics cappedBuffer
	payload := make([]byte, maxProcessorDiagnosticBytes*4)
	for i := range payload {
		payload[i] = 'x'
	}
	if _, err := diagnostics.Write(payload); err != nil {
		t.Fatal(err)
	}
	if got := len(diagnostics.data); got != maxProcessorDiagnosticBytes {
		t.Fatalf("retained diagnostics=%d, want %d", got, maxProcessorDiagnosticBytes)
	}
	if diagnostics.omitted != len(payload)-maxProcessorDiagnosticBytes {
		t.Fatalf("omitted diagnostics=%d", diagnostics.omitted)
	}
}

func TestProcessingOutputPrefixIsAttemptScopedAndInjectionSafe(t *testing.T) {
	first := processingOutputPrefix("version-1", "operation-1")
	second := processingOutputPrefix("version-1", "operation-2")
	if first == second {
		t.Fatal("two processing attempts shared an output prefix")
	}
	malicious := processingOutputPrefix("version-1", "../../other-course?signature=secret")
	if malicious == "" || malicious == first || filepath.Base(malicious) == "other-course?signature=secret" {
		t.Fatalf("operation identity influenced object path: %q", malicious)
	}
	result := TranscodeResult{
		TrustedDurationMS: 1,
		OutputPrefix:      first,
		Renditions: []Rendition{{
			Name: "720p", StorageObjectKey: "media/other/hls/720p/playlist.m3u8",
		}},
	}
	if err := validateTranscodeCompletion("version-1", "operation-1", result); !errors.Is(err, ErrValidation) {
		t.Fatalf("cross-prefix rendition error=%v, want ErrValidation", err)
	}
}
