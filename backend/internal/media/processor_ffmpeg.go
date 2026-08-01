package media

import (
	"context"
	"encoding/json"
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
	DeletePrefix(context.Context, string) error
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

func (p *FFmpegProcessor) Transcode(ctx context.Context, object ObjectVersion) (result TranscodeResult, err error) {
	if !object.valid() {
		return TranscodeResult{}, ErrStaleScanEvidence
	}
	processingCtx, cancel := context.WithTimeout(ctx, p.processingTimeout)
	defer cancel()
	prefix := "media/" + object.AssetVersionID + "/hls"
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
		return TranscodeResult{}, fmt.Errorf("downloading exact media object: %w", err)
	}
	defer cleanup()

	probe, err := p.probe(processingCtx, localPath)
	if err != nil {
		return TranscodeResult{}, err
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
	if err := p.renderHLS(processingCtx, localPath, outDir, metadata.rungs); err != nil {
		return TranscodeResult{}, err
	}
	if err := p.uploadHLS(processingCtx, outDir, prefix); err != nil {
		return TranscodeResult{}, err
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
		return processingMetadata{}, fmt.Errorf("ffprobe did not return a positive trusted duration")
	}
	for _, stream := range probe.Streams {
		if stream.CodecType == "video" && stream.Height > 0 {
			return processingMetadata{durationMS: int64(durationSeconds*1000 + 0.5), rungs: hlsRungsForHeight(stream.Height)}, nil
		}
	}
	return processingMetadata{}, fmt.Errorf("ffprobe did not return a video height")
}

func (p *FFmpegProcessor) renderHLS(ctx context.Context, input, outDir string, rungs []hlsRung) error {
	for _, rung := range rungs {
		if err := p.transcodeRung(ctx, input, outDir, rung); err != nil {
			return err
		}
	}
	return writeMediaMaster(filepath.Join(outDir, "master.m3u8"), rungs)
}

func (p *FFmpegProcessor) uploadHLS(ctx context.Context, outDir, prefix string) error {
	files, err := walkMediaFiles(outDir)
	if err != nil {
		return fmt.Errorf("walking HLS output: %w", err)
	}
	if len(files) == 0 {
		return fmt.Errorf("HLS processing produced no output files")
	}
	for _, relative := range files {
		contents, err := os.ReadFile(filepath.Join(outDir, relative))
		if err != nil {
			return fmt.Errorf("reading HLS output %s: %w", relative, err)
		}
		if err := p.store.PutObject(ctx, prefix+"/"+filepath.ToSlash(relative), contents, mediaContentType(relative)); err != nil {
			return fmt.Errorf("storing HLS output %s: %w", relative, err)
		}
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
	output, err := cmd.Output()
	if err != nil {
		return processorProbe{}, fmt.Errorf("ffprobe failed: %w", err)
	}
	var probe processorProbe
	if err := json.Unmarshal(output, &probe); err != nil {
		return processorProbe{}, fmt.Errorf("parsing ffprobe output: %w", err)
	}
	return probe, nil
}

func (p *FFmpegProcessor) transcodeRung(ctx context.Context, input, outDir string, rung hlsRung) error {
	directory := filepath.Join(outDir, rung.Name)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("creating HLS rendition directory: %w", err)
	}
	videoKbps := fmt.Sprintf("%dk", rung.VideoKbps)
	args := []string{
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
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg rendition %s failed: %w (%s)", rung.Name, err, truncateMediaOutput(string(output), 2000))
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
		fmt.Fprintf(&builder, "#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%dx%d\n%s/playlist.m3u8\n", (rung.VideoKbps+rung.AudioKbps)*1000, rung.Width, rung.Height, rung.Name)
	}
	return os.WriteFile(path, []byte(builder.String()), 0o644)
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
