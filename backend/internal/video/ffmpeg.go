package video

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
)

// FFmpeg is the only place in the codebase that shells out to ffmpeg/ffprobe.
// Requires both binaries on PATH (or overridden via FFMPEG_BINARY_PATH/
// FFPROBE_BINARY_PATH — see internal/config) on whatever host runs cmd/worker.
type FFmpeg struct {
	ffmpegPath  string
	ffprobePath string
}

func NewFFmpeg(ffmpegPath, ffprobePath string) *FFmpeg {
	return &FFmpeg{ffmpegPath: ffmpegPath, ffprobePath: ffprobePath}
}

type Metadata struct {
	Resolution      string
	Bitrate         int
	Codec           string
	FPS             float64
	DurationSeconds float64
}

type ffprobeStream struct {
	CodecType  string `json:"codec_type"`
	CodecName  string `json:"codec_name"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	RFrameRate string `json:"r_frame_rate"`
	BitRate    string `json:"bit_rate"`
}

type ffprobeOutput struct {
	Streams []ffprobeStream `json:"streams"`
	Format  struct {
		Duration string `json:"duration"`
		BitRate  string `json:"bit_rate"`
	} `json:"format"`
}

// ExtractMetadata runs ffprobe against a local file and returns the fields
// the videos table tracks (resolution/bitrate/codec/fps/duration).
func (f *FFmpeg) ExtractMetadata(ctx context.Context, localPath string) (Metadata, error) {
	cmd := exec.CommandContext(ctx, f.ffprobePath,
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		localPath,
	)
	out, err := cmd.Output()
	if err != nil {
		return Metadata{}, fmt.Errorf("ffprobe failed: %w", err)
	}

	var parsed ffprobeOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return Metadata{}, fmt.Errorf("parsing ffprobe output: %w", err)
	}

	var videoStream *ffprobeStream
	for i := range parsed.Streams {
		if parsed.Streams[i].CodecType == "video" {
			videoStream = &parsed.Streams[i]
			break
		}
	}
	if videoStream == nil {
		return Metadata{}, fmt.Errorf("no video stream found in %s", localPath)
	}

	meta := Metadata{
		Resolution: fmt.Sprintf("%dx%d", videoStream.Width, videoStream.Height),
		Codec:      videoStream.CodecName,
		FPS:        parseFrameRate(videoStream.RFrameRate),
	}

	if bitrate := videoStream.BitRate; bitrate != "" {
		meta.Bitrate, _ = strconv.Atoi(bitrate)
	} else if parsed.Format.BitRate != "" {
		meta.Bitrate, _ = strconv.Atoi(parsed.Format.BitRate)
	}

	if d, err := strconv.ParseFloat(parsed.Format.Duration, 64); err == nil {
		meta.DurationSeconds = d
	}

	return meta, nil
}

// Rendition is one HLS ladder rung.
type Rendition struct {
	Name             string // "1080p", "720p", "480p", "240p" — also the output subdirectory name
	Width, Height    int
	VideoBitrateKbps int
	AudioBitrateKbps int
}

// fullLadder is the full HLS ladder per
// docs/superpowers/specs/2026-07-17-video-streaming-design.md §2/§4.
var fullLadder = []Rendition{
	{"1080p", 1920, 1080, 5000, 192},
	{"720p", 1280, 720, 2800, 128},
	{"480p", 854, 480, 1400, 128},
	{"240p", 426, 240, 400, 96},
}

// RenditionsForSourceHeight picks the ladder rungs whose height doesn't
// exceed the source's — matches the spec's "transcode ladder decisions
// depend on source resolution" (§4), avoiding pointless upscaling. A source
// below the smallest rung (240p) still gets that one rung so playback always
// has at least one rendition, accepting a minor upscale in that rare case.
func RenditionsForSourceHeight(sourceHeight int) []Rendition {
	var out []Rendition
	for _, r := range fullLadder {
		if r.Height <= sourceHeight {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		out = append(out, fullLadder[len(fullLadder)-1]) // smallest rung, i.e. 240p
	}
	return out
}

// Transcode runs one ffmpeg invocation per rendition, producing
// <outDir>/<rendition>/playlist.m3u8 + segments, then writes a master
// playlist referencing all of them. Returns the list of files written,
// relative to outDir, for the caller to upload to storage.
func (f *FFmpeg) Transcode(ctx context.Context, localPath, outDir string, renditions []Rendition) (relFiles []string, err error) {
	for _, r := range renditions {
		if err := f.transcodeRendition(ctx, localPath, outDir, r); err != nil {
			return nil, fmt.Errorf("transcoding %s: %w", r.Name, err)
		}
	}

	masterPath := filepath.Join(outDir, "master.m3u8")
	if err := writeMasterPlaylist(masterPath, renditions); err != nil {
		return nil, fmt.Errorf("writing master playlist: %w", err)
	}

	return walkRelative(outDir)
}

func (f *FFmpeg) transcodeRendition(ctx context.Context, localPath, outDir string, r Rendition) error {
	renditionDir := filepath.Join(outDir, r.Name)
	if err := os.MkdirAll(renditionDir, 0o755); err != nil {
		return fmt.Errorf("creating rendition dir: %w", err)
	}

	videoBitrate := fmt.Sprintf("%dk", r.VideoBitrateKbps)
	maxrate := fmt.Sprintf("%dk", r.VideoBitrateKbps*107/100)
	bufsize := fmt.Sprintf("%dk", r.VideoBitrateKbps*150/100)
	audioBitrate := fmt.Sprintf("%dk", r.AudioBitrateKbps)

	args := []string{
		"-y",
		"-i", localPath,
		// force_divisible_by=2 avoids odd pixel dimensions (e.g. 853x480)
		// that libx264/yuv420p reject outright — see worker.go transcode
		// failure history for the exact error this guards against.
		"-vf", fmt.Sprintf("scale=w=%d:h=%d:force_original_aspect_ratio=decrease:force_divisible_by=2", r.Width, r.Height),
		"-c:v", "libx264", "-profile:v", "main", "-crf", "20", "-sc_threshold", "0",
		"-g", "48", "-keyint_min", "48",
		"-b:v", videoBitrate, "-maxrate", maxrate, "-bufsize", bufsize,
		"-c:a", "aac", "-ar", "48000", "-b:a", audioBitrate,
		"-hls_time", "6",
		"-hls_playlist_type", "vod",
		"-hls_segment_filename", filepath.Join(renditionDir, "segment%03d.ts"),
		filepath.Join(renditionDir, "playlist.m3u8"),
	}

	cmd := exec.CommandContext(ctx, f.ffmpegPath, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg failed: %w (output: %s)", err, truncateTail(string(out), 2000))
	}
	return nil
}

// writeMasterPlaylist writes an HLS master manifest referencing each
// rendition's own playlist, highest bitrate first (a common convention;
// players pick by bandwidth regardless of order).
func writeMasterPlaylist(masterPath string, renditions []Rendition) error {
	sorted := make([]Rendition, len(renditions))
	copy(sorted, renditions)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].VideoBitrateKbps > sorted[j].VideoBitrateKbps })

	var b strings.Builder
	b.WriteString("#EXTM3U\n#EXT-X-VERSION:3\n")
	for _, r := range sorted {
		bandwidthBps := (r.VideoBitrateKbps + r.AudioBitrateKbps) * 1000
		fmt.Fprintf(&b, "#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%dx%d\n%s/playlist.m3u8\n",
			bandwidthBps, r.Width, r.Height, r.Name)
	}
	return os.WriteFile(masterPath, []byte(b.String()), 0o644)
}

func walkRelative(root string) ([]string, error) {
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, rel)
		return nil
	})
	return files, err
}

// truncateTail keeps the last n characters — ffmpeg's actual error is near
// the end of its output, after a long version/config banner at the start.
func truncateTail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "(truncated)..." + s[len(s)-n:]
}

// parseFrameRate converts ffprobe's "25/1" style rational fps into a float.
func parseFrameRate(rate string) float64 {
	parts := strings.SplitN(rate, "/", 2)
	if len(parts) != 2 {
		f, _ := strconv.ParseFloat(rate, 64)
		return f
	}
	num, errNum := strconv.ParseFloat(parts[0], 64)
	den, errDen := strconv.ParseFloat(parts[1], 64)
	if errNum != nil || errDen != nil || den == 0 {
		return 0
	}
	return num / den
}
