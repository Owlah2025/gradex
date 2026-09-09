package media

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// FFmpegProcessor is the production processor boundary. It reads only the
// exact object version selected by the worker, derives duration from ffprobe,
// and writes HLS output below a media-owned prefix. It never writes legacy
// videos rows or decides Course publication.
type FFmpegProcessor struct {
	store             ProcessingStore
	ffmpegPath        string
	ffprobePath       string
	processingTimeout time.Duration
}

// ProcessingStore is the exact-version storage capability needed by the
// trusted processor. Keeping this interface in media prevents the processor
// from depending on storage implementation details while making replacement
// bytes impossible to substitute by logical key alone.
type ProcessingStore interface {
	DownloadToFileVersion(context.Context, string, string) (string, func(), error)
	PutObject(context.Context, string, []byte, string) error
	HeadObject(context.Context, string) (sizeBytes int64, exists bool, err error)
	DeletePrefix(context.Context, string) error
}

// CleanupAttempt removes only one immutable attempt prefix. It is used after
// durable stale-claim recovery; cleanup failure never rolls back recovery or
// affects a newer attempt's objects.
func (p *FFmpegProcessor) CleanupAttempt(ctx context.Context, assetVersionID, operationID string) error {
	return p.store.DeletePrefix(ctx, processingOutputPrefix(assetVersionID, operationID))
}

func NewFFmpegProcessor(store ProcessingStore, ffmpegPath, ffprobePath string, processingTimeout time.Duration) (*FFmpegProcessor, error) {
	if store == nil {
		return nil, fmt.Errorf("media ffmpeg storage is required")
	}
	if strings.TrimSpace(ffmpegPath) == "" || strings.TrimSpace(ffprobePath) == "" {
		return nil, fmt.Errorf("media ffmpeg and ffprobe paths are required")
	}
	if processingTimeout == 0 {
		processingTimeout = DefaultProcessingTimeout
	}
	if processingTimeout <= 0 {
		return nil, fmt.Errorf("media processing timeout must be positive")
	}
	return &FFmpegProcessor{store: store, ffmpegPath: ffmpegPath, ffprobePath: ffprobePath, processingTimeout: processingTimeout}, nil
}

type processorProbe struct {
	Streams []struct {
		CodecType string `json:"codec_type"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

type hlsRung struct {
	Name      string
	Width     int
	Height    int
	VideoKbps int
	AudioKbps int
}

var hlsLadder = []hlsRung{
	{Name: "1080p", Width: 1920, Height: 1080, VideoKbps: 5000, AudioKbps: 192},
	{Name: "720p", Width: 1280, Height: 720, VideoKbps: 2800, AudioKbps: 128},
	{Name: "480p", Width: 854, Height: 480, VideoKbps: 1400, AudioKbps: 128},
	{Name: "240p", Width: 426, Height: 240, VideoKbps: 400, AudioKbps: 96},
}

// Transcode runs the pipeline without reporting progress. It exists for
// callers that have nowhere to put an observation; the worker uses
// TranscodeWithProgress.
func (p *FFmpegProcessor) Transcode(ctx context.Context, object ObjectVersion) (TranscodeResult, error) {
	return p.TranscodeWithProgress(ctx, object, nil)
}

// TranscodeWithProgress is the same pipeline, reporting real measured progress
// into sink as it goes. Progress is derived from FFmpeg's own structured
// `-progress` stream against the ffprobe duration of the exact source object —
// never from elapsed time — so a stalled encode stops advancing rather than
// creeping toward a number nobody measured.
func (p *FFmpegProcessor) TranscodeWithProgress(ctx context.Context, object ObjectVersion, sink ProgressSink) (result TranscodeResult, err error) {
	if !object.valid() || strings.TrimSpace(object.ProcessingOperationID) == "" {
		return TranscodeResult{}, ErrStaleScanEvidence
	}
	processingCtx, cancel := context.WithTimeout(ctx, p.processingTimeout)
	defer cancel()
	prefix := processingOutputPrefix(object.AssetVersionID, object.ProcessingOperationID)
	completed := false
	defer func() {
		if completed {
			return
		}
		// Cleanup cannot rely on the processing context: a timeout or caller
		// cancellation is precisely when a partial rendition prefix must still
		// be removed from private storage.
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if cleanupErr := p.store.DeletePrefix(cleanupCtx, prefix); cleanupErr != nil && err == nil {
			err = fmt.Errorf("removing partial HLS output: %w", cleanupErr)
		}
	}()

	localPath, cleanup, err := p.store.DownloadToFileVersion(processingCtx, object.StorageObjectKey, object.StorageObjectVersion)
	if err != nil {
		return TranscodeResult{}, fmt.Errorf("%w: downloading exact media object: %v", ErrStorageUnavailable, err)
	}
	defer cleanup()

	probe, err := p.probe(processingCtx, localPath)
	if err != nil {
		return TranscodeResult{}, classifyProcessorContext(processingCtx, err)
	}
	metadata, err := trustedMediaMetadata(probe)
	if err != nil {
		return TranscodeResult{}, err
	}
	outDir, err := os.MkdirTemp("", "gradex-media-hls-*")
	if err != nil {
		return TranscodeResult{}, fmt.Errorf("creating HLS scratch directory: %w", err)
	}
	defer os.RemoveAll(outDir)
	if err := p.renderHLS(processingCtx, localPath, outDir, metadata, sink); err != nil {
		return TranscodeResult{}, classifyProcessorContext(processingCtx, err)
	}
	// Uploading the finished ladder is its own phase. It has no continuous
	// measure worth trusting, so it reports the stage at the point transcoding
	// reached rather than inventing a second fraction.
	reportProgress(processingCtx, sink, StagePackaging, 99)
	if err := p.uploadHLS(processingCtx, outDir, prefix); err != nil {
		return TranscodeResult{}, classifyProcessorContext(processingCtx, err)
	}
	completed = true
	return transcodeResult(prefix, metadata), nil
}

type processingMetadata struct {
	durationMS int64
	rungs      []hlsRung
}

func trustedMediaMetadata(probe processorProbe) (processingMetadata, error) {
	durationSeconds, err := strconv.ParseFloat(probe.Format.Duration, 64)
	if err != nil || durationSeconds <= 0 {
		return processingMetadata{}, fmt.Errorf("%w: ffprobe did not return a positive trusted duration", ErrInvalidMedia)
	}
	for _, stream := range probe.Streams {
		if stream.CodecType == "video" && stream.Height > 0 {
			return processingMetadata{durationMS: int64(durationSeconds*1000 + 0.5), rungs: hlsRungsForHeight(stream.Height)}, nil
		}
	}
	return processingMetadata{}, fmt.Errorf("%w: ffprobe did not return a video height", ErrInvalidMedia)
}

func (p *FFmpegProcessor) renderHLS(ctx context.Context, input, outDir string, metadata processingMetadata, sink ProgressSink) error {
	duration := time.Duration(metadata.durationMS) * time.Millisecond
	count := len(metadata.rungs)
	reportProgress(ctx, sink, StageTranscoding, 0)
	for index, rung := range metadata.rungs {
		if err := p.transcodeRung(ctx, input, outDir, rung, func(processed time.Duration) {
			reportProgress(ctx, sink, StageTranscoding, rungProgressPercent(index, count, processed, duration))
		}); err != nil {
			return err
		}
		reportProgress(ctx, sink, StageTranscoding, rungProgressPercent(index+1, count, 0, duration))
	}
	return writeMediaMaster(filepath.Join(outDir, "master.m3u8"), metadata.rungs)
}

func reportProgress(ctx context.Context, sink ProgressSink, stage ProcessingStage, percent int) {
	if sink == nil {
		return
	}
	sink.Progress(ctx, stage, percent)
}

func (p *FFmpegProcessor) uploadHLS(ctx context.Context, outDir, prefix string) error {
	files, err := walkMediaFiles(outDir)
	if err != nil {
		return fmt.Errorf("walking HLS output: %w", err)
	}
	if len(files) == 0 {
		return fmt.Errorf("HLS processing produced no output files")
	}
	if err := validateLocalHLSOutput(outDir, files); err != nil {
		return err
	}
	// The master is the publication marker inside the private attempt prefix.
	// Upload it last, after every playlist and segment it can lead to.
	sort.SliceStable(files, func(i, j int) bool {
		if files[i] == "master.m3u8" {
			return false
		}
		if files[j] == "master.m3u8" {
			return true
		}
		return files[i] < files[j]
	})
	for _, relative := range files {
		if err := p.uploadVerifiedHLSObject(ctx, outDir, prefix, relative); err != nil {
			return err
		}
	}
	return nil
}

func (p *FFmpegProcessor) uploadVerifiedHLSObject(ctx context.Context, outDir, prefix, relative string) error {
	contents, err := os.ReadFile(filepath.Join(outDir, relative))
	if err != nil {
		return fmt.Errorf("reading HLS output %s: %w", relative, err)
	}
	key := prefix + "/" + filepath.ToSlash(relative)
	if err := p.store.PutObject(ctx, key, contents, mediaContentType(relative)); err != nil {
		return fmt.Errorf("%w: storing HLS output %s: %v", ErrStorageUnavailable, relative, err)
	}
	size, exists, err := p.store.HeadObject(ctx, key)
	if err != nil {
		return fmt.Errorf("%w: verifying HLS output %s: %v", ErrStorageUnavailable, relative, err)
	}
	if !exists || size != int64(len(contents)) {
		return fmt.Errorf("%w: HLS output %s was not stored completely", ErrStorageUnavailable, relative)
	}
	return nil
}

func transcodeResult(prefix string, metadata processingMetadata) TranscodeResult {
	result := TranscodeResult{OutputPrefix: prefix, TrustedDurationMS: metadata.durationMS, Renditions: make([]Rendition, 0, len(metadata.rungs))}
	for _, rung := range metadata.rungs {
		result.Renditions = append(result.Renditions, Rendition{
			Name: rung.Name, StorageObjectKey: prefix + "/" + rung.Name + "/playlist.m3u8",
			Width: rung.Width, Height: rung.Height, BitrateKbps: rung.VideoKbps,
			DurationMS: metadata.durationMS,
		})
	}
	return result
}

func (p *FFmpegProcessor) probe(ctx context.Context, localPath string) (processorProbe, error) {
	cmd := exec.CommandContext(ctx, p.ffprobePath, "-v", "quiet", "-print_format", "json", "-show_format", "-show_streams", localPath)
	diagnostics := cappedBuffer{limit: maxProcessorDiagnosticBytes}
	output := cappedBuffer{limit: maxProbeOutputBytes}
	cmd.Stderr = &diagnostics
	cmd.Stdout = &output
	if err := cmd.Run(); err != nil {
		return processorProbe{}, fmt.Errorf("%w: ffprobe failed: %v (%s)", ErrInvalidMedia, err, diagnostics.String())
	}
	if output.omitted > 0 {
		return processorProbe{}, fmt.Errorf("%w: ffprobe output exceeded %d bytes", ErrInvalidMedia, maxProbeOutputBytes)
	}
	var probe processorProbe
	if err := json.Unmarshal(output.data, &probe); err != nil {
		return processorProbe{}, fmt.Errorf("%w: parsing ffprobe output: %v", ErrInvalidMedia, err)
	}
	return probe, nil
}

func (p *FFmpegProcessor) transcodeRung(ctx context.Context, input, outDir string, rung hlsRung, onProcessed func(time.Duration)) error {
	directory := filepath.Join(outDir, rung.Name)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("creating HLS rendition directory: %w", err)
	}
	videoKbps := fmt.Sprintf("%dk", rung.VideoKbps)
	args := []string{
		// The machine-readable progress stream on stdout, and the human status
		// line off. Only the structured form is ever parsed.
		"-progress", "pipe:1", "-nostats",
		"-y", "-i", input,
		"-vf", fmt.Sprintf("scale=w=%d:h=%d:force_original_aspect_ratio=decrease:force_divisible_by=2", rung.Width, rung.Height),
		"-c:v", "libx264", "-profile:v", "main", "-crf", "20", "-sc_threshold", "0",
		"-g", "48", "-keyint_min", "48", "-b:v", videoKbps,
		"-maxrate", fmt.Sprintf("%dk", rung.VideoKbps*107/100), "-bufsize", fmt.Sprintf("%dk", rung.VideoKbps*150/100),
		"-c:a", "aac", "-ar", "48000", "-b:a", fmt.Sprintf("%dk", rung.AudioKbps),
		"-hls_time", "6", "-hls_playlist_type", "vod",
		"-hls_segment_filename", filepath.Join(directory, "segment%03d.ts"),
		filepath.Join(directory, "playlist.m3u8"),
	}
	cmd := exec.CommandContext(ctx, p.ffmpegPath, args...)
	progress, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("opening ffmpeg progress stream: %w", err)
	}
	diagnostics := cappedBuffer{limit: maxProcessorDiagnosticBytes}
	cmd.Stderr = &diagnostics
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting ffmpeg rendition %s: %w", rung.Name, err)
	}
	// Drained on this goroutine so FFmpeg is never blocked writing to a full
	// pipe, and so the reader has finished before Wait reaps the process.
	_ = scanFFmpegProgress(progress, onProcessed)
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("%w: ffmpeg rendition %s failed: %v (%s)", ErrTranscodeFailed, rung.Name, err, diagnostics.String())
	}
	return nil
}

const maxProcessorDiagnosticBytes = 2000
const maxProbeOutputBytes = 1024 * 1024

type cappedBuffer struct {
	data    []byte
	omitted int
	limit   int
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	limit := b.limit
	if limit <= 0 {
		limit = maxProcessorDiagnosticBytes
	}
	remaining := limit - len(b.data)
	if remaining > 0 {
		kept := len(p)
		if kept > remaining {
			kept = remaining
		}
		b.data = append(b.data, p[:kept]...)
	}
	if len(p) > remaining {
		b.omitted += len(p) - max(remaining, 0)
	}
	return len(p), nil
}

func (b *cappedBuffer) String() string {
	text := strings.TrimSpace(string(b.data))
	if b.omitted > 0 {
		return fmt.Sprintf("%s [truncated %d bytes]", text, b.omitted)
	}
	return text
}

func classifyProcessorContext(ctx context.Context, err error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("%w: %v", ErrProcessTimeout, err)
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return context.Canceled
	}
	return err
}

// ProcessingOutputPrefix is the canonical attempt-scoped HLS prefix for one
// (Asset Version, processing operation). It is exported because the worker
// validates completion against exactly this value, so anything constructing a
// TranscodeResult outside this package must derive the prefix here rather than
// rebuild the convention and drift from it.
func ProcessingOutputPrefix(assetVersionID, operationID string) string {
	return processingOutputPrefix(assetVersionID, operationID)
}

func processingOutputPrefix(assetVersionID, operationID string) string {
	sum := sha256.Sum256([]byte(operationID))
	return fmt.Sprintf("media/%s/hls/%x", assetVersionID, sum[:12])
}

func validateLocalHLSOutput(root string, files []string) error {
	present := make(map[string]struct{}, len(files))
	for _, relative := range files {
		clean, err := safeHLSRelativePath(relative)
		if err != nil {
			return fmt.Errorf("%w: HLS output path escapes the attempt", ErrTranscodeFailed)
		}
		present[clean] = struct{}{}
	}
	if _, ok := present["master.m3u8"]; !ok {
		return fmt.Errorf("%w: HLS master manifest is missing", ErrTranscodeFailed)
	}
	for _, relative := range files {
		if !strings.HasSuffix(relative, "/playlist.m3u8") {
			continue
		}
		if err := validateLocalRendition(root, relative, present); err != nil {
			return err
		}
	}
	return nil
}

func safeHLSRelativePath(relative string) (string, error) {
	clean := filepath.ToSlash(filepath.Clean(relative))
	if clean != filepath.ToSlash(relative) || clean == "." || strings.HasPrefix(clean, "../") || filepath.IsAbs(relative) {
		return "", ErrTranscodeFailed
	}
	return clean, nil
}

func validateLocalRendition(root, relative string, present map[string]struct{}) error {
	body, err := os.ReadFile(filepath.Join(root, relative))
	if err != nil {
		return fmt.Errorf("reading HLS rendition manifest %s: %w", relative, err)
	}
	directory := filepath.ToSlash(filepath.Dir(relative))
	segments := 0
	for _, line := range strings.Split(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if err := validateLocalSegmentReference(directory, line, present); err != nil {
			return err
		}
		segments++
	}
	if segments == 0 {
		return fmt.Errorf("%w: HLS rendition has no segments", ErrTranscodeFailed)
	}
	return nil
}

func validateLocalSegmentReference(directory, reference string, present map[string]struct{}) error {
	if strings.Contains(reference, "://") || strings.ContainsAny(reference, "?\\/") || reference == "." || reference == ".." {
		return fmt.Errorf("%w: HLS rendition contains an unsafe segment reference", ErrTranscodeFailed)
	}
	if _, ok := present[directory+"/"+reference]; !ok {
		return fmt.Errorf("%w: HLS rendition references missing segment %s", ErrTranscodeFailed, reference)
	}
	return nil
}

func hlsRungsForHeight(height int) []hlsRung {
	out := make([]hlsRung, 0, len(hlsLadder))
	for _, rung := range hlsLadder {
		if rung.Height <= height {
			out = append(out, rung)
		}
	}
	if len(out) == 0 {
		out = append(out, hlsLadder[len(hlsLadder)-1])
	}
	return out
}

func writeMediaMaster(path string, rungs []hlsRung) error {
	sorted := append([]hlsRung(nil), rungs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].VideoKbps > sorted[j].VideoKbps })
	var builder strings.Builder
	builder.WriteString("#EXTM3U\n#EXT-X-VERSION:3\n")
	for _, rung := range sorted {
		fmt.Fprintf(&builder, "#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%dx%d\n%s/playlist.m3u8\n", hlsBandwidth(rung), rung.Width, rung.Height, rung.Name)
	}
	return os.WriteFile(path, []byte(builder.String()), 0o644)
}

func hlsRungByName(name string) (hlsRung, bool) {
	for _, rung := range hlsLadder {
		if rung.Name == name {
			return rung, true
		}
	}
	return hlsRung{}, false
}

// hlsBandwidth is the aggregate video-plus-audio rate already used by the
// FFmpeg master. Persisted Rendition.BitrateKbps is video-only, so protected
// master generation must add the matching ladder's audio rate as well.
func hlsBandwidth(rung hlsRung) int {
	return (rung.VideoKbps + rung.AudioKbps) * 1000
}

func walkMediaFiles(root string) ([]string, error) {
	files := make([]string, 0)
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, relative)
		return nil
	})
	return files, err
}

func mediaContentType(path string) string {
	switch filepath.Ext(path) {
	case ".m3u8":
		return "application/vnd.apple.mpegurl"
	case ".ts":
		return "video/mp2t"
	default:
		return "application/octet-stream"
	}
}

func truncateMediaOutput(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return "(truncated)..." + value[len(value)-max:]
}
