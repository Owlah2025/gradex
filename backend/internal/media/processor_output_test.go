package media

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
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

func TestValidateLocalRungRejectsPartialOrUnsafeRungs(t *testing.T) {
	cases := []struct {
		name    string
		rung    hlsRung
		files   map[string]string
		wantErr bool
	}{
		{
			name: "valid rung",
			rung: hlsRung{Name: "720p"},
			files: map[string]string{
				"720p/playlist.m3u8": "#EXTM3U\nsegment000.ts\nsegment001.ts\n",
				"720p/segment000.ts": "seg0",
				"720p/segment001.ts": "seg1",
			},
			wantErr: false,
		},
		{
			name: "missing segment",
			rung: hlsRung{Name: "720p"},
			files: map[string]string{
				"720p/playlist.m3u8": "#EXTM3U\nsegment000.ts\nsegment001.ts\n",
				"720p/segment000.ts": "seg0",
			},
			wantErr: true,
		},
		{
			name: "unsafe URL segment",
			rung: hlsRung{Name: "720p"},
			files: map[string]string{
				"720p/playlist.m3u8": "#EXTM3U\nhttps://evil.test/segment000.ts\n",
			},
			wantErr: true,
		},
		{
			name: "path traversal segment",
			rung: hlsRung{Name: "720p"},
			files: map[string]string{
				"720p/playlist.m3u8": "#EXTM3U\n../secret.ts\n",
			},
			wantErr: true,
		},
		{
			name: "empty playlist without segments",
			rung: hlsRung{Name: "720p"},
			files: map[string]string{
				"720p/playlist.m3u8": "#EXTM3U\n#EXT-X-VERSION:3\n",
			},
			wantErr: true,
		},
		{
			name: "escaping rung name",
			rung: hlsRung{Name: "../escaping"},
			files: map[string]string{
				"escaping/playlist.m3u8": "#EXTM3U\nsegment000.ts\n",
			},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for rel, body := range tc.files {
				writeOutputFixture(t, root, rel, body)
			}
			segments, err := validateLocalRung(root, tc.rung)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("validateLocalRung unexpectedly succeeded for %s", tc.name)
				}
			} else {
				if err != nil {
					t.Fatalf("validateLocalRung failed: %v", err)
				}
				if len(segments) != 2 || segments[0] != "segment000.ts" || segments[1] != "segment001.ts" {
					t.Fatalf("validateLocalRung returned unexpected segments: %v", segments)
				}
			}
		})
	}
}

type pipelineEventStore struct {
	mu           sync.Mutex
	logPath      string
	objects      map[string][]byte
	failPutKey   string
	failHeadKey  string
	sourceFile   string
	afterPutHook func(key string)
}

func (s *pipelineEventStore) appendLog(event string) {
	if s.logPath == "" {
		return
	}
	f, err := os.OpenFile(s.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = fmt.Fprintln(f, event)
}

func (s *pipelineEventStore) DownloadToFileVersion(_ context.Context, _, _ string) (string, func(), error) {
	s.appendLog("download:source")
	return s.sourceFile, func() {}, nil
}

func (s *pipelineEventStore) PutObject(_ context.Context, key string, body []byte, _ string) error {
	s.appendLog("put:" + key)
	if s.failPutKey != "" && strings.Contains(key, s.failPutKey) {
		return fmt.Errorf("%w: injected PUT failure for %s", ErrStorageUnavailable, key)
	}
	s.mu.Lock()
	if s.objects == nil {
		s.objects = make(map[string][]byte)
	}
	s.objects[key] = append([]byte(nil), body...)
	s.mu.Unlock()
	if s.afterPutHook != nil {
		s.afterPutHook(key)
	}
	return nil
}

func (s *pipelineEventStore) HeadObject(_ context.Context, key string) (int64, bool, error) {
	s.appendLog("head:" + key)
	if s.failHeadKey != "" && strings.Contains(key, s.failHeadKey) {
		return 0, false, fmt.Errorf("%w: injected HEAD failure for %s", ErrStorageUnavailable, key)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	body, ok := s.objects[key]
	return int64(len(body)), ok, nil
}

func (s *pipelineEventStore) DeletePrefix(_ context.Context, prefix string) error {
	s.appendLog("delete:" + prefix)
	return nil
}

type recordingPipelineSink struct {
	logPath    string
	mu         sync.Mutex
	onProgress func(stage ProcessingStage, percent int)
	onPersist  func(rendition Rendition) error
}

func (s *recordingPipelineSink) Progress(ctx context.Context, stage ProcessingStage, percent int) {
	if s.logPath != "" {
		f, err := os.OpenFile(s.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err == nil {
			_, _ = fmt.Fprintf(f, "sink:%s:%d\n", stage, percent)
			_ = f.Close()
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.onProgress != nil {
		s.onProgress(stage, percent)
	}
}

func (s *recordingPipelineSink) PersistVerifiedRendition(ctx context.Context, rendition Rendition) error {
	if s.logPath != "" {
		f, err := os.OpenFile(s.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err == nil {
			_, _ = fmt.Fprintf(f, "persist:%s\n", rendition.Name)
			_ = f.Close()
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.onPersist != nil {
		return s.onPersist(rendition)
	}
	return nil
}

func createFakePipelineHarness(t *testing.T, failRung string, height int) (probePath, ffmpegPath, sourcePath, logPath string) {
	t.Helper()
	dir := t.TempDir()

	sourcePath = filepath.Join(dir, "source.mp4")
	if err := os.WriteFile(sourcePath, []byte("fake-mp4-data"), 0o600); err != nil {
		t.Fatal(err)
	}

	logPath = filepath.Join(dir, "events.log")

	probePath = filepath.Join(dir, "ffprobe.sh")
	probeContent := fmt.Sprintf(`#!/bin/sh
cat << 'EOF'
{
  "streams": [{"codec_type": "video", "width": %d, "height": %d}],
  "format": {"duration": "10.0"}
}
EOF
`, height*16/9, height)
	if err := os.WriteFile(probePath, []byte(probeContent), 0o755); err != nil {
		t.Fatal(err)
	}

	ffmpegPath = filepath.Join(dir, "ffmpeg.sh")
	ffmpegContent := fmt.Sprintf(`#!/bin/sh
for arg do last="$arg"; done
dir=$(dirname "$last")
rung=$(basename "$dir")
echo "encode:$rung" >> "%s"
if [ "$rung" = "%s" ]; then
  echo "injected failure for $rung" >&2
  exit 1
fi
mkdir -p "$dir"
printf "fake-segment" > "$dir/segment000.ts"
printf "#EXTM3U\nsegment000.ts\n" > "$last"
printf "out_time_ms=5000000\nprogress=continue\n"
printf "out_time_ms=10000000\nprogress=end\n"
`, logPath, failRung)
	if err := os.WriteFile(ffmpegPath, []byte(ffmpegContent), 0o755); err != nil {
		t.Fatal(err)
	}

	return probePath, ffmpegPath, sourcePath, logPath
}

func readLogEvents(t *testing.T, logPath string) []string {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var out []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

func TestPerRungPipelineOrderAndVerification(t *testing.T) {
	probePath, ffmpegPath, sourcePath, logPath := createFakePipelineHarness(t, "", 720)
	store := &pipelineEventStore{logPath: logPath, sourceFile: sourcePath}
	proc, err := NewFFmpegProcessor(store, ffmpegPath, probePath, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	var progressReports []int
	sink := &recordingPipelineSink{
		logPath: logPath,
		onProgress: func(stage ProcessingStage, percent int) {
			progressReports = append(progressReports, percent)
		},
	}

	obj := ObjectVersion{
		AssetVersionID:        "version-1",
		StorageObjectKey:      "quarantine/version-1",
		StorageObjectVersion:  "v1",
		ProcessingOperationID: "op-1",
	}

	res, err := proc.TranscodeWithProgress(context.Background(), obj, sink)
	if err != nil {
		t.Fatalf("TranscodeWithProgress failed: %v", err)
	}

	expectedPrefix := processingOutputPrefix("version-1", "op-1")
	if res.OutputPrefix != expectedPrefix {
		t.Fatalf("OutputPrefix=%s, want %s", res.OutputPrefix, expectedPrefix)
	}
	if len(res.Renditions) != 3 {
		t.Fatalf("len(res.Renditions)=%d, want 3", len(res.Renditions))
	}

	if len(res.ExpectedRenditions) != 3 || res.ExpectedRenditions[0] != "720p" || res.ExpectedRenditions[1] != "480p" || res.ExpectedRenditions[2] != "240p" {
		t.Fatalf("res.ExpectedRenditions=%v, want [720p 480p 240p]", res.ExpectedRenditions)
	}

	events := readLogEvents(t, logPath)
	indexOf := func(target string) int {
		for i, ev := range events {
			if strings.Contains(ev, target) {
				return i
			}
		}
		return -1
	}

	// 1. Operation order: 720p encode -> 720p upload/verify -> 720p persist callback -> 720p progress -> 480p encode -> ...
	enc720 := indexOf("encode:720p")
	putSeg720 := indexOf("put:" + expectedPrefix + "/720p/segment000.ts")
	headSeg720 := indexOf("head:" + expectedPrefix + "/720p/segment000.ts")
	putPl720 := indexOf("put:" + expectedPrefix + "/720p/playlist.m3u8")
	headPl720 := indexOf("head:" + expectedPrefix + "/720p/playlist.m3u8")
	persist720 := indexOf("persist:720p")
	prog720 := indexOf("sink:TRANSCODING:33")

	enc480 := indexOf("encode:480p")
	putSeg480 := indexOf("put:" + expectedPrefix + "/480p/segment000.ts")
	headSeg480 := indexOf("head:" + expectedPrefix + "/480p/segment000.ts")
	putPl480 := indexOf("put:" + expectedPrefix + "/480p/playlist.m3u8")
	headPl480 := indexOf("head:" + expectedPrefix + "/480p/playlist.m3u8")
	persist480 := indexOf("persist:480p")
	prog480 := indexOf("sink:TRANSCODING:66")

	enc240 := indexOf("encode:240p")
	putSeg240 := indexOf("put:" + expectedPrefix + "/240p/segment000.ts")
	headSeg240 := indexOf("head:" + expectedPrefix + "/240p/segment000.ts")
	putPl240 := indexOf("put:" + expectedPrefix + "/240p/playlist.m3u8")
	headPl240 := indexOf("head:" + expectedPrefix + "/240p/playlist.m3u8")
	persist240 := indexOf("persist:240p")
	prog240 := indexOf("sink:TRANSCODING:99")

	putMaster := indexOf("put:" + expectedPrefix + "/master.m3u8")
	headMaster := indexOf("head:" + expectedPrefix + "/master.m3u8")

	order := []struct {
		name  string
		index int
	}{
		{"enc720", enc720},
		{"putSeg720", putSeg720},
		{"headSeg720", headSeg720},
		{"putPl720", putPl720},
		{"headPl720", headPl720},
		{"persist720", persist720},
		{"prog720", prog720},
		{"enc480", enc480},
		{"putSeg480", putSeg480},
		{"headSeg480", headSeg480},
		{"putPl480", putPl480},
		{"headPl480", headPl480},
		{"persist480", persist480},
		{"prog480", prog480},
		{"enc240", enc240},
		{"putSeg240", putSeg240},
		{"headSeg240", headSeg240},
		{"putPl240", putPl240},
		{"headPl240", headPl240},
		{"persist240", persist240},
		{"prog240", prog240},
		{"putMaster", putMaster},
		{"headMaster", headMaster},
	}

	for i := 0; i < len(order)-1; i++ {
		if order[i].index == -1 {
			t.Fatalf("missing event %s in events: %v", order[i].name, events)
		}
		if order[i+1].index == -1 {
			t.Fatalf("missing event %s in events: %v", order[i+1].name, events)
		}
		if order[i].index >= order[i+1].index {
			t.Fatalf("ordering violation: %s (idx %d) must precede %s (idx %d). Full events: %v",
				order[i].name, order[i].index, order[i+1].name, order[i+1].index, events)
		}
	}

	// Requirement 5: Master PUT is the last PUT in the entire sequence.
	var lastPut string
	for _, ev := range events {
		if strings.HasPrefix(ev, "put:") {
			lastPut = ev
		}
	}
	if lastPut != "put:"+expectedPrefix+"/master.m3u8" {
		t.Fatalf("last put object=%s, want master.m3u8", lastPut)
	}

	// Requirement 18: Progress is monotonic and remains < 100 before READY.
	lastProgress := -1
	for _, p := range progressReports {
		if p < lastProgress {
			t.Fatalf("progress regression: %d -> %d", lastProgress, p)
		}
		if p >= 100 {
			t.Fatalf("progress reached %d before CompleteTranscode / READY", p)
		}
		lastProgress = p
	}
}

func TestPerRungPipelineFailures(t *testing.T) {
	cases := []struct {
		name        string
		failRung    string
		failPutKey  string
		failHeadKey string
		assertLog   func(t *testing.T, events []string)
	}{
		{
			name:       "first_rung_segment_put_failure",
			failPutKey: "720p/segment000.ts",
			assertLog: func(t *testing.T, events []string) {
				for _, ev := range events {
					if ev == "encode:480p" {
						t.Fatal("480p was encoded after 720p segment PUT failure")
					}
					if strings.Contains(ev, "master.m3u8") {
						t.Fatal("master was referenced after 720p segment PUT failure")
					}
				}
			},
		},
		{
			name:        "first_rung_segment_head_failure",
			failHeadKey: "720p/segment000.ts",
			assertLog: func(t *testing.T, events []string) {
				for _, ev := range events {
					if ev == "encode:480p" {
						t.Fatal("480p was encoded after 720p segment HEAD failure")
					}
					if strings.Contains(ev, "720p/playlist.m3u8") {
						t.Fatal("playlist was published after segment HEAD failure")
					}
				}
			},
		},
		{
			name:       "first_rung_playlist_put_failure",
			failPutKey: "720p/playlist.m3u8",
			assertLog: func(t *testing.T, events []string) {
				for _, ev := range events {
					if ev == "encode:480p" {
						t.Fatal("480p was encoded after 720p playlist PUT failure")
					}
				}
			},
		},
		{
			name:        "first_rung_playlist_head_failure",
			failHeadKey: "720p/playlist.m3u8",
			assertLog: func(t *testing.T, events []string) {
				for _, ev := range events {
					if ev == "encode:480p" {
						t.Fatal("480p was encoded after 720p playlist HEAD failure")
					}
				}
			},
		},
		{
			name:     "later_rung_transcode_failure",
			failRung: "480p",
			assertLog: func(t *testing.T, events []string) {
				for _, ev := range events {
					if ev == "encode:240p" {
						t.Fatal("240p was encoded after 480p transcode failure")
					}
					if strings.Contains(ev, "master.m3u8") {
						t.Fatal("master was referenced after 480p failure")
					}
				}
			},
		},
		{
			name:       "master_put_failure",
			failPutKey: "master.m3u8",
			assertLog: func(t *testing.T, events []string) {
				for _, ev := range events {
					if strings.HasPrefix(ev, "head:") && strings.Contains(ev, "master.m3u8") {
						t.Fatal("master HEAD executed after master PUT failure")
					}
				}
			},
		},
		{
			name:        "master_head_failure",
			failHeadKey: "master.m3u8",
			assertLog: func(t *testing.T, events []string) {
				putSeen := false
				for _, ev := range events {
					if strings.HasPrefix(ev, "put:") && strings.Contains(ev, "master.m3u8") {
						putSeen = true
					}
				}
				if !putSeen {
					t.Fatal("master PUT was expected before master HEAD failure")
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			probePath, ffmpegPath, sourcePath, logPath := createFakePipelineHarness(t, tc.failRung, 720)
			store := &pipelineEventStore{
				logPath:     logPath,
				sourceFile:  sourcePath,
				failPutKey:  tc.failPutKey,
				failHeadKey: tc.failHeadKey,
			}
			proc, err := NewFFmpegProcessor(store, ffmpegPath, probePath, 5*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			obj := ObjectVersion{
				AssetVersionID:        "version-1",
				StorageObjectKey:      "quarantine/version-1",
				StorageObjectVersion:  "v1",
				ProcessingOperationID: "op-fail",
			}
			_, err = proc.TranscodeWithProgress(context.Background(), obj, nil)
			if err == nil {
				t.Fatal("expected error from failed pipeline, got nil")
			}
			events := readLogEvents(t, logPath)
			tc.assertLog(t, events)
		})
	}
}

func TestProgressiveProcessorCallbackFailures(t *testing.T) {
	cases := []struct {
		name         string
		failRung     string
		assertEvents func(t *testing.T, events []string)
	}{
		{
			name:     "first_rung_persistence_failure_aborts_pipeline",
			failRung: "720p",
			assertEvents: func(t *testing.T, events []string) {
				hasEvent := func(prefix string) bool {
					for _, ev := range events {
						if strings.HasPrefix(ev, prefix) {
							return true
						}
					}
					return false
				}
				if !hasEvent("persist:720p") {
					t.Fatal("expected persist:720p to be attempted")
				}
				if hasEvent("sink:TRANSCODING:33") {
					t.Fatal("boundary progress 33% was emitted despite persistence failure")
				}
				if hasEvent("encode:480p") {
					t.Fatal("subsequent rung 480p was encoded despite 720p persistence failure")
				}
				if hasEvent("put:") && strings.Contains(events[len(events)-1], "master.m3u8") {
					t.Fatal("master manifest was uploaded despite persistence failure")
				}
			},
		},
		{
			name:     "second_rung_persistence_failure_aborts_pipeline",
			failRung: "480p",
			assertEvents: func(t *testing.T, events []string) {
				hasEvent := func(prefix string) bool {
					for _, ev := range events {
						if strings.HasPrefix(ev, prefix) {
							return true
						}
					}
					return false
				}
				if !hasEvent("persist:720p") || !hasEvent("sink:TRANSCODING:33") {
					t.Fatal("first rung should have succeeded before second rung failure")
				}
				if !hasEvent("persist:480p") {
					t.Fatal("expected persist:480p to be attempted")
				}
				if hasEvent("sink:TRANSCODING:66") {
					t.Fatal("boundary progress 66% was emitted despite 480p persistence failure")
				}
				if hasEvent("encode:240p") {
					t.Fatal("subsequent rung 240p was encoded despite 480p persistence failure")
				}
				if hasEvent("put:") && strings.Contains(events[len(events)-1], "master.m3u8") {
					t.Fatal("master manifest was uploaded despite persistence failure")
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			probePath, ffmpegPath, sourcePath, logPath := createFakePipelineHarness(t, "", 720)
			store := &pipelineEventStore{logPath: logPath, sourceFile: sourcePath}
			proc, err := NewFFmpegProcessor(store, ffmpegPath, probePath, 5*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			injectedErr := errors.New("injected DB persistence error")
			sink := &recordingPipelineSink{
				logPath: logPath,
				onPersist: func(rendition Rendition) error {
					if rendition.Name == tc.failRung {
						return injectedErr
					}
					return nil
				},
			}
			obj := ObjectVersion{
				AssetVersionID:        "version-1",
				StorageObjectKey:      "quarantine/version-1",
				StorageObjectVersion:  "v1",
				ProcessingOperationID: "op-cb-fail",
			}
			_, err = proc.TranscodeWithProgress(context.Background(), obj, sink)
			if !errors.Is(err, injectedErr) {
				t.Fatalf("expected injectedErr, got: %v", err)
			}
			events := readLogEvents(t, logPath)
			tc.assertEvents(t, events)
		})
	}
}

func TestPerRungCancellationBoundary(t *testing.T) {
	probePath, ffmpegPath, sourcePath, logPath := createFakePipelineHarness(t, "", 720)
	store := &pipelineEventStore{logPath: logPath, sourceFile: sourcePath}
	proc, err := NewFFmpegProcessor(store, ffmpegPath, probePath, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var cancelOnce sync.Once
	sink := &recordingPipelineSink{
		logPath: logPath,
		onProgress: func(stage ProcessingStage, percent int) {
			// Cancel when 720p completes (percent = 33)
			if stage == StageTranscoding && percent == 33 {
				cancelOnce.Do(func() {
					cancel()
				})
			}
		},
	}

	obj := ObjectVersion{
		AssetVersionID:        "version-1",
		StorageObjectKey:      "quarantine/version-1",
		StorageObjectVersion:  "v1",
		ProcessingOperationID: "op-cancel",
	}

	_, err = proc.TranscodeWithProgress(ctx, obj, sink)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}

	events := readLogEvents(t, logPath)
	for _, ev := range events {
		if ev == "encode:480p" {
			t.Fatal("encode:480p started after cancellation")
		}
		if ev == "encode:240p" {
			t.Fatal("encode:240p started after cancellation")
		}
		if strings.Contains(ev, "master.m3u8") {
			t.Fatal("master.m3u8 was uploaded after cancellation")
		}
	}
}

func TestFinalFullTreeValidationExecutesBeforeMasterPublication(t *testing.T) {
	probePath, ffmpegPath, sourcePath, logPath := createFakePipelineHarness(t, "", 720)
	store := &pipelineEventStore{
		logPath:    logPath,
		sourceFile: sourcePath,
		afterPutHook: func(key string) {
			// When the last rung's playlist is put, delete its segment from disk
			// to trigger a full-tree validation failure before master publication.
			if strings.HasSuffix(key, "240p/playlist.m3u8") {
				matches, _ := filepath.Glob(filepath.Join(os.TempDir(), "gradex-media-hls-*", "240p", "segment000.ts"))
				for _, m := range matches {
					_ = os.Remove(m)
				}
			}
		},
	}
	proc, err := NewFFmpegProcessor(store, ffmpegPath, probePath, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	obj := ObjectVersion{
		AssetVersionID:        "version-1",
		StorageObjectKey:      "quarantine/version-1",
		StorageObjectVersion:  "v1",
		ProcessingOperationID: "op-tree-val",
	}

	_, err = proc.TranscodeWithProgress(context.Background(), obj, nil)
	if !errors.Is(err, ErrTranscodeFailed) {
		t.Fatalf("expected ErrTranscodeFailed from full-tree validation failure, got: %v", err)
	}

	events := readLogEvents(t, logPath)
	for _, ev := range events {
		if strings.Contains(ev, "master.m3u8") {
			t.Fatal("master.m3u8 was uploaded even though full-tree validation failed")
		}
	}
}

func TestPerRungTerminalLiveProgressDoesNotPrematurelyEmitBoundary(t *testing.T) {
	probePath, ffmpegPath, sourcePath, logPath := createFakePipelineHarness(t, "", 720)
	store := &pipelineEventStore{logPath: logPath, sourceFile: sourcePath}
	proc, err := NewFFmpegProcessor(store, ffmpegPath, probePath, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	var rawReports []struct {
		stage   ProcessingStage
		percent int
	}
	var mu sync.Mutex
	sink := &recordingPipelineSink{
		logPath: logPath,
		onProgress: func(stage ProcessingStage, percent int) {
			mu.Lock()
			defer mu.Unlock()
			rawReports = append(rawReports, struct {
				stage   ProcessingStage
				percent int
			}{stage, percent})
		},
	}

	obj := ObjectVersion{
		AssetVersionID:        "version-1",
		StorageObjectKey:      "quarantine/version-1",
		StorageObjectVersion:  "v1",
		ProcessingOperationID: "op-term-progress",
	}

	res, err := proc.TranscodeWithProgress(context.Background(), obj, sink)
	if err != nil {
		t.Fatalf("TranscodeWithProgress failed: %v", err)
	}

	count := len(res.Renditions)
	if count != 3 {
		t.Fatalf("len(res.Renditions)=%d, want 3", count)
	}
	duration := 10 * time.Second
	expectedPrefix := processingOutputPrefix("version-1", "op-term-progress")

	// Calculate authoritative boundaries for the 3 rungs.
	b0 := rungProgressPercent(1, count, 0, duration) // Rung 0 completion (e.g. 33)
	b1 := rungProgressPercent(2, count, 0, duration) // Rung 1 completion (e.g. 66)
	b2 := rungProgressPercent(3, count, 0, duration) // Rung 2 completion (e.g. 99)

	events := readLogEvents(t, logPath)

	indexOf := func(target string) int {
		for i, ev := range events {
			if strings.Contains(ev, target) {
				return i
			}
		}
		return -1
	}

	// Boundary 0 (720p) assertions
	enc720 := indexOf("encode:720p")
	headPl720 := indexOf("head:" + expectedPrefix + "/720p/playlist.m3u8")
	if enc720 == -1 || headPl720 == -1 {
		t.Fatalf("missing 720p encode or head events: %v", events)
	}
	// Verify all progress events during 720p FFmpeg live encode are strictly below b0
	for i := enc720; i < headPl720; i++ {
		ev := events[i]
		if strings.HasPrefix(ev, "sink:TRANSCODING:") {
			var p int
			fmt.Sscanf(ev, "sink:TRANSCODING:%d", &p)
			if p >= b0 {
				t.Fatalf("terminal FFmpeg progress for rung 0 emitted %d >= boundary %d before storage verification! Events: %v", p, b0, events)
			}
		}
	}
	// Boundary b0 must appear after headPl720
	boundaryEvent0 := fmt.Sprintf("sink:TRANSCODING:%d", b0)
	countB0 := 0
	firstB0Index := -1
	for i, ev := range events {
		if ev == boundaryEvent0 {
			countB0++
			if firstB0Index == -1 {
				firstB0Index = i
			}
		}
	}
	if countB0 != 1 {
		t.Fatalf("expected boundary %s to be emitted exactly once, got %d times in events: %v", boundaryEvent0, countB0, events)
	}
	if firstB0Index <= headPl720 {
		t.Fatalf("boundary %s index %d must be after headPl720 %d", boundaryEvent0, firstB0Index, headPl720)
	}

	// Boundary 1 (480p) assertions
	enc480 := indexOf("encode:480p")
	headPl480 := indexOf("head:" + expectedPrefix + "/480p/playlist.m3u8")
	if enc480 == -1 || headPl480 == -1 {
		t.Fatalf("missing 480p encode or head events: %v", events)
	}
	for i := enc480; i < headPl480; i++ {
		ev := events[i]
		if strings.HasPrefix(ev, "sink:TRANSCODING:") {
			var p int
			fmt.Sscanf(ev, "sink:TRANSCODING:%d", &p)
			if p >= b1 {
				t.Fatalf("terminal FFmpeg progress for rung 1 emitted %d >= boundary %d before storage verification! Events: %v", p, b1, events)
			}
		}
	}
	boundaryEvent1 := fmt.Sprintf("sink:TRANSCODING:%d", b1)
	countB1 := 0
	firstB1Index := -1
	for i, ev := range events {
		if ev == boundaryEvent1 {
			countB1++
			if firstB1Index == -1 {
				firstB1Index = i
			}
		}
	}
	if countB1 != 1 {
		t.Fatalf("expected boundary %s to be emitted exactly once, got %d times in events: %v", boundaryEvent1, countB1, events)
	}
	if firstB1Index <= headPl480 {
		t.Fatalf("boundary %s index %d must be after headPl480 %d", boundaryEvent1, firstB1Index, headPl480)
	}

	// Boundary 2 (240p) assertions
	enc240 := indexOf("encode:240p")
	headPl240 := indexOf("head:" + expectedPrefix + "/240p/playlist.m3u8")
	if enc240 == -1 || headPl240 == -1 {
		t.Fatalf("missing 240p encode or head events: %v", events)
	}
	for i := enc240; i < headPl240; i++ {
		ev := events[i]
		if strings.HasPrefix(ev, "sink:TRANSCODING:") {
			var p int
			fmt.Sscanf(ev, "sink:TRANSCODING:%d", &p)
			if p >= b2 {
				t.Fatalf("terminal FFmpeg progress for rung 2 emitted %d >= boundary %d before storage verification! Events: %v", p, b2, events)
			}
		}
	}
	boundaryEvent2 := fmt.Sprintf("sink:TRANSCODING:%d", b2)
	countB2 := 0
	firstB2Index := -1
	for i, ev := range events {
		if ev == boundaryEvent2 {
			countB2++
			if firstB2Index == -1 {
				firstB2Index = i
			}
		}
	}
	if countB2 != 1 {
		t.Fatalf("expected boundary %s to be emitted exactly once, got %d times in events: %v", boundaryEvent2, countB2, events)
	}
	if firstB2Index <= headPl240 {
		t.Fatalf("boundary %s index %d must be after headPl240 %d", boundaryEvent2, firstB2Index, headPl240)
	}

	// Monotonicity and bounded below 100
	lastP := -1
	for _, r := range rawReports {
		if r.percent < lastP {
			t.Fatalf("progress regression: %d -> %d", lastP, r.percent)
		}
		if r.percent >= 100 {
			t.Fatalf("progress reached %d >= 100 before READY", r.percent)
		}
		lastP = r.percent
	}
}
