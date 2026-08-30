package media

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

type hlsDeliveryStore struct {
	keys       []string
	expiresAt  []time.Time
	presignErr error
}

func (s *hlsDeliveryStore) PresignGetURL(_ context.Context, key string, lifetime time.Duration) (string, error) {
	s.keys = append(s.keys, key)
	return fmt.Sprintf("https://storage.test/private/%s?ttl=%d", key, int64(lifetime/time.Second)), nil
}

func (s *hlsDeliveryStore) PresignGetURLUntil(_ context.Context, key string, expiresAt time.Time) (string, error) {
	if s.presignErr != nil {
		return "", s.presignErr
	}
	s.keys = append(s.keys, key)
	s.expiresAt = append(s.expiresAt, expiresAt)
	return "https://storage.test/private/" + key, nil
}

func (*hlsDeliveryStore) DownloadObject(context.Context, string) ([]byte, error) {
	return nil, nil
}

func TestProtectedMasterUsesPersistedResolutionAndAggregateBandwidth(t *testing.T) {
	renditions := []videoRendition{
		mustVideoRendition(t, persistedRenditionMetadata{name: "720p", storageKey: "media/version/hls/720p/playlist.m3u8", width: 1280, height: 720, videoBitrateKbps: 2800}),
		mustVideoRendition(t, persistedRenditionMetadata{name: "240p", storageKey: "media/version/hls/240p/playlist.m3u8", width: 426, height: 240, videoBitrateKbps: 400}),
	}
	root := "/api/v1/media/playback-manifests/session"
	contents, err := renderProtectedMaster(root, renditions)
	if err != nil {
		t.Fatalf("rendering master: %v", err)
	}
	master := string(contents)
	for _, expected := range []string{
		"#EXTM3U\n",
		"#EXT-X-STREAM-INF:BANDWIDTH=2928000,RESOLUTION=1280x720\n" + root + "/renditions/720p/index.m3u8",
		"#EXT-X-STREAM-INF:BANDWIDTH=496000,RESOLUTION=426x240\n" + root + "/renditions/240p/index.m3u8",
	} {
		if !strings.Contains(master, expected) {
			t.Fatalf("master %q omitted %q", master, expected)
		}
	}
	if strings.Contains(master, "storage.test") || strings.Contains(master, "media/version") {
		t.Fatalf("master leaked private storage details: %q", master)
	}
}

func TestPersistedRenditionMustMatchAuthoritativeProcessingLadder(t *testing.T) {
	valid := persistedRenditionMetadata{
		name: "720p", storageKey: "media/version/hls/720p/playlist.m3u8", width: 1280, height: 720, videoBitrateKbps: 2800,
	}
	for name, mutate := range map[string]func(*persistedRenditionMetadata){
		"unknown rung":       func(metadata *persistedRenditionMetadata) { metadata.name = "custom" },
		"mismatched width":   func(metadata *persistedRenditionMetadata) { metadata.width = 1278 },
		"mismatched bitrate": func(metadata *persistedRenditionMetadata) { metadata.videoBitrateKbps = 2928 },
		"unsafe storage key": func(metadata *persistedRenditionMetadata) { metadata.storageKey = "../720p/playlist.m3u8" },
	} {
		t.Run(name, func(t *testing.T) {
			metadata := valid
			mutate(&metadata)
			if _, err := persistedVideoRendition(metadata); err == nil {
				t.Fatal("invalid persisted rendition metadata was accepted")
			}
		})
	}
}

func TestRenditionSelectorRejectsUnsafeUnknownAndAmbiguousValues(t *testing.T) {
	available := []videoRendition{
		mustVideoRendition(t, persistedRenditionMetadata{name: "720p", storageKey: "media/version/hls/720p/playlist.m3u8", width: 1280, height: 720, videoBitrateKbps: 2800}),
	}
	for _, selector := range []string{"", "1080p", "../720p", "720p/private", "%2e%2e", "720p%2Fprivate", "720p.m3u8"} {
		t.Run(selector, func(t *testing.T) {
			if _, err := selectVideoRendition(available, selector); err == nil {
				t.Fatalf("unsafe or unknown selector %q was accepted", selector)
			}
		})
	}
	ambiguous := append(append([]videoRendition(nil), available...), available[0])
	if _, err := selectVideoRendition(ambiguous, "720p"); err == nil {
		t.Fatal("ambiguous persisted rendition identifier was accepted")
	}
}

func TestMediaPlaylistPassesAbsoluteSessionExpiryToSigner(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	store := &hlsDeliveryStore{}
	service := &DeliveryService{store: store}
	contents := []byte("#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:6\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXTINF:6,\nsegment000.ts\n#EXTINF:6,\nsegment001.ts\n#EXT-X-ENDLIST\n")
	expiresAt := now.Add(2500 * time.Millisecond)

	rewritten, err := service.rewriteMediaPlaylist(context.Background(), "media/version/hls/720p/playlist.m3u8", contents, expiresAt)
	if err != nil {
		t.Fatalf("rewriting media playlist: %v", err)
	}
	if got, want := store.keys, []string{"media/version/hls/720p/segment000.ts", "media/version/hls/720p/segment001.ts"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("signed keys=%v, want %v", got, want)
	}
	if len(store.expiresAt) != 2 {
		t.Fatalf("absolute signer expiry calls=%d, want 2", len(store.expiresAt))
	}
	for _, requestedExpiry := range store.expiresAt {
		if !requestedExpiry.Equal(expiresAt) {
			t.Fatalf("signer received expiry=%s, want %s", requestedExpiry, expiresAt)
		}
	}
	if !strings.Contains(string(rewritten), "https://storage.test/private/media/version/hls/720p/segment000.ts") {
		t.Fatalf("rewritten playlist omitted signed segment: %q", rewritten)
	}
}

func TestMediaPlaylistFailsClosedWhenAbsoluteExpirySignerRefuses(t *testing.T) {
	store := &hlsDeliveryStore{presignErr: errors.New("insufficient absolute lifetime")}
	service := &DeliveryService{store: store}
	contents := []byte("#EXTM3U\n#EXT-X-TARGETDURATION:6\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXTINF:6,\nsegment000.ts\n#EXT-X-ENDLIST\n")

	if _, err := service.rewriteMediaPlaylist(context.Background(), "media/version/hls/720p/playlist.m3u8", contents, time.Now().UTC()); err == nil {
		t.Fatal("absolute-expiry signer refusal was ignored")
	}
	if len(store.keys) != 0 {
		t.Fatalf("refused absolute-expiry signer recorded signed keys: %v", store.keys)
	}
}

func TestMediaPlaylistRejectsUnsupportedOrUnsafeReferences(t *testing.T) {
	for name, contents := range map[string]string{
		"master child":   "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=2928000\n720p/playlist.m3u8\n",
		"tag URI":        "#EXTM3U\n#EXT-X-TARGETDURATION:6\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXTINF:6,\nsegment.m4s\n#EXT-X-ENDLIST\n",
		"traversal":      "#EXTM3U\n#EXT-X-TARGETDURATION:6\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXTINF:6,\n../segment.ts\n#EXT-X-ENDLIST\n",
		"encoded escape": "#EXTM3U\n#EXT-X-TARGETDURATION:6\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXTINF:6,\n%2e%2e/segment.ts\n#EXT-X-ENDLIST\n",
		"absolute URL":   "#EXTM3U\n#EXT-X-TARGETDURATION:6\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXTINF:6,\nhttps://storage.test/segment.ts\n#EXT-X-ENDLIST\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := parseMediaPlaylist("media/version/hls/720p/playlist.m3u8", []byte(contents)); err == nil {
				t.Fatal("unsafe or unsupported playlist was accepted")
			}
		})
	}
}

func mustVideoRendition(t *testing.T, metadata persistedRenditionMetadata) videoRendition {
	t.Helper()
	rendition, err := persistedVideoRendition(metadata)
	if err != nil {
		t.Fatalf("building rendition: %v", err)
	}
	return rendition
}
