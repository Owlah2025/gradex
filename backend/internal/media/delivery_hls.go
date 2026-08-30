package media

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type renditionID string

type videoRendition struct {
	id         renditionID
	storageKey string
	width      int
	height     int
	bandwidth  int
}

type persistedRenditionMetadata struct {
	name             string
	storageKey       string
	width            int
	height           int
	videoBitrateKbps int
}

type mediaReference struct {
	lineIndex int
	objectKey string
}

type playbackSegmentSigner interface {
	PresignGetURLUntil(context.Context, string, time.Time) (string, error)
}

func (s *DeliveryService) loadVideoRenditions(ctx context.Context, assetVersionID string) ([]videoRendition, error) {
	rows, err := s.db.Query(ctx, `
		SELECT name, storage_object_key, COALESCE(width, 0), COALESCE(height, 0), COALESCE(bitrate_kbps, 0)
		FROM video_renditions
		WHERE asset_version_id = $1::uuid
		ORDER BY height DESC NULLS LAST, bitrate_kbps DESC NULLS LAST, name
	`, assetVersionID)
	if err != nil {
		return nil, fmt.Errorf("loading video renditions: %w", err)
	}
	defer rows.Close()
	return collectVideoRenditions(rows)
}

func collectVideoRenditions(rows pgx.Rows) ([]videoRendition, error) {
	renditions := make([]videoRendition, 0)
	for rows.Next() {
		rendition, err := scanVideoRendition(rows)
		if err != nil {
			return nil, err
		}
		renditions = append(renditions, rendition)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating video renditions: %w", err)
	}
	if err := validateVideoRenditions(renditions); err != nil {
		return nil, err
	}
	return renditions, nil
}

func scanVideoRendition(rows pgx.Rows) (videoRendition, error) {
	var metadata persistedRenditionMetadata
	if err := rows.Scan(&metadata.name, &metadata.storageKey, &metadata.width, &metadata.height, &metadata.videoBitrateKbps); err != nil {
		return videoRendition{}, fmt.Errorf("scanning video rendition: %w", err)
	}
	return persistedVideoRendition(metadata)
}

func persistedVideoRendition(metadata persistedRenditionMetadata) (videoRendition, error) {
	id, err := parseRenditionID(metadata.name)
	if err != nil || metadata.storageKey == "" || metadata.width <= 0 || metadata.height <= 0 || metadata.videoBitrateKbps <= 0 {
		return videoRendition{}, errors.New("persisted video rendition metadata is incomplete")
	}
	if _, err := renditionDirectory(metadata.storageKey); err != nil {
		return videoRendition{}, err
	}
	rung, ok := hlsRungByName(metadata.name)
	if !ok || rung.Width != metadata.width || rung.Height != metadata.height || rung.VideoKbps != metadata.videoBitrateKbps {
		return videoRendition{}, errors.New("persisted video rendition metadata does not match the processing ladder")
	}
	return videoRendition{
		id: id, storageKey: metadata.storageKey, width: metadata.width, height: metadata.height,
		bandwidth: hlsBandwidth(rung),
	}, nil
}

func parseRenditionID(value string) (renditionID, error) {
	if value == "" || len(value) > 64 {
		return "", errors.New("rendition identifier is malformed")
	}
	for index, character := range []byte(value) {
		if isASCIIAlphaNumeric(character) || (index > 0 && (character == '-' || character == '_')) {
			continue
		}
		return "", errors.New("rendition identifier is malformed")
	}
	return renditionID(value), nil
}

func isASCIIAlphaNumeric(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9'
}

func validateVideoRenditions(renditions []videoRendition) error {
	if len(renditions) == 0 {
		return errors.New("video has no persisted renditions")
	}
	seen := make(map[renditionID]struct{}, len(renditions))
	for _, rendition := range renditions {
		if rendition.id == "" || rendition.storageKey == "" || rendition.width <= 0 || rendition.height <= 0 || rendition.bandwidth <= 0 {
			return errors.New("video rendition metadata is incomplete")
		}
		if _, err := parseRenditionID(string(rendition.id)); err != nil {
			return err
		}
		if _, exists := seen[rendition.id]; exists {
			return errors.New("video rendition identifier is ambiguous")
		}
		seen[rendition.id] = struct{}{}
	}
	return nil
}

func selectVideoRendition(renditions []videoRendition, selector string) (videoRendition, error) {
	requested, err := parseRenditionID(selector)
	if err != nil || validateVideoRenditions(renditions) != nil {
		return videoRendition{}, ErrProtectedUnavailable
	}
	var selected videoRendition
	matches := 0
	for _, rendition := range renditions {
		if rendition.id == requested {
			selected = rendition
			matches++
		}
	}
	if matches != 1 {
		return videoRendition{}, ErrProtectedUnavailable
	}
	return selected, nil
}

func renderProtectedMaster(manifestRoot string, renditions []videoRendition) ([]byte, error) {
	if manifestRoot == "" || validateVideoRenditions(renditions) != nil {
		return nil, ErrProtectedUnavailable
	}
	sorted := append([]videoRendition(nil), renditions...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].height > sorted[j].height })
	var builder strings.Builder
	builder.WriteString("#EXTM3U\n#EXT-X-VERSION:3\n")
	for _, rendition := range sorted {
		fmt.Fprintf(&builder, "#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%dx%d\n%s/renditions/%s/index.m3u8\n",
			rendition.bandwidth, rendition.width, rendition.height, manifestRoot, rendition.id)
	}
	return []byte(builder.String()), nil
}

func (s *DeliveryService) issueMasterManifest(ctx context.Context, assetVersionID, manifestRoot string) (PlaybackManifest, error) {
	renditions, err := s.loadVideoRenditions(ctx, assetVersionID)
	if err != nil {
		return PlaybackManifest{}, ErrProtectedUnavailable
	}
	contents, err := renderProtectedMaster(manifestRoot, renditions)
	if err != nil {
		return PlaybackManifest{}, ErrProtectedUnavailable
	}
	return PlaybackManifest{Contents: contents}, nil
}

func (s *DeliveryService) issueRenditionManifest(ctx context.Context, assetVersionID, selector string, expiresAt time.Time) (PlaybackManifest, error) {
	renditions, err := s.loadVideoRenditions(ctx, assetVersionID)
	if err != nil {
		return PlaybackManifest{}, ErrProtectedUnavailable
	}
	rendition, err := selectVideoRendition(renditions, selector)
	if err != nil {
		return PlaybackManifest{}, ErrProtectedUnavailable
	}
	contents, err := s.store.DownloadObject(ctx, rendition.storageKey)
	if err != nil || len(contents) == 0 || len(contents) > maxPlaybackManifestBytes {
		return PlaybackManifest{}, ErrProtectedUnavailable
	}
	rewritten, err := s.rewriteMediaPlaylist(ctx, rendition.storageKey, contents, expiresAt)
	if err != nil {
		return PlaybackManifest{}, ErrProtectedUnavailable
	}
	return PlaybackManifest{Contents: rewritten}, nil
}

func (s *DeliveryService) rewriteMediaPlaylist(ctx context.Context, storageKey string, contents []byte, expiresAt time.Time) ([]byte, error) {
	lines, references, err := parseMediaPlaylist(storageKey, contents)
	if err != nil {
		return nil, err
	}
	signer, ok := s.store.(playbackSegmentSigner)
	if !ok {
		return nil, errors.New("delivery store cannot enforce absolute segment expiry")
	}
	for _, reference := range references {
		signed, err := signer.PresignGetURLUntil(ctx, reference.objectKey, expiresAt)
		if err != nil {
			return nil, err
		}
		lines[reference.lineIndex] = signed
	}
	return []byte(strings.Join(lines, "\n")), nil
}

func parseMediaPlaylist(storageKey string, contents []byte) ([]string, []mediaReference, error) {
	directory, err := renditionDirectory(storageKey)
	if err != nil {
		return nil, nil, err
	}
	lines := strings.Split(strings.ReplaceAll(string(contents), "\r\n", "\n"), "\n")
	if len(lines) == 0 || lines[0] != "#EXTM3U" {
		return nil, nil, errors.New("HLS media playlist header is missing")
	}
	return parseMediaPlaylistLines(directory, lines)
}

func parseMediaPlaylistLines(directory string, lines []string) ([]string, []mediaReference, error) {
	state := mediaPlaylistState{directory: directory, references: make([]mediaReference, 0)}
	for index, line := range lines[1:] {
		if err := state.consume(index+1, line); err != nil {
			return nil, nil, err
		}
	}
	if !state.complete() {
		return nil, nil, errors.New("HLS media playlist is incomplete")
	}
	return lines, state.references, nil
}

type mediaPlaylistState struct {
	directory         string
	references        []mediaReference
	pendingSegment    bool
	sawTargetDuration bool
	sawVOD            bool
	sawEndList        bool
}

func (state *mediaPlaylistState) consume(lineIndex int, line string) error {
	if line == "" {
		return nil
	}
	if state.sawEndList {
		return errors.New("HLS media playlist contains content after end list")
	}
	if strings.HasPrefix(line, "#") {
		return state.consumeTag(line)
	}
	return state.consumeSegment(lineIndex, line)
}

func (state *mediaPlaylistState) consumeTag(line string) error {
	switch {
	case strings.HasPrefix(line, "#EXTINF:"):
		if state.pendingSegment || !validEXTINF(line) {
			return errors.New("HLS media playlist contains malformed segment metadata")
		}
		state.pendingSegment = true
	case strings.HasPrefix(line, "#EXT-X-TARGETDURATION:"):
		state.sawTargetDuration = positiveIntegerTag(line, "#EXT-X-TARGETDURATION:")
		if !state.sawTargetDuration {
			return errors.New("HLS media playlist target duration is malformed")
		}
	case line == "#EXT-X-PLAYLIST-TYPE:VOD":
		state.sawVOD = true
	case line == "#EXT-X-ENDLIST":
		if state.pendingSegment {
			return errors.New("HLS media playlist segment URI is missing")
		}
		state.sawEndList = true
	case supportedMediaMetadataTag(line):
	default:
		return errors.New("HLS media playlist contains an unsupported tag")
	}
	return nil
}

func (state *mediaPlaylistState) consumeSegment(lineIndex int, line string) error {
	if !state.pendingSegment {
		return errors.New("HLS media playlist contains a non-segment reference")
	}
	objectKey, err := mediaSegmentObjectKey(state.directory, line)
	if err != nil {
		return err
	}
	state.references = append(state.references, mediaReference{lineIndex: lineIndex, objectKey: objectKey})
	state.pendingSegment = false
	return nil
}

func (state mediaPlaylistState) complete() bool {
	return !state.pendingSegment && len(state.references) > 0 && state.sawTargetDuration && state.sawVOD && state.sawEndList
}

func supportedMediaMetadataTag(line string) bool {
	if strings.HasPrefix(line, "#EXT-X-VERSION:") {
		return positiveIntegerTag(line, "#EXT-X-VERSION:")
	}
	if strings.HasPrefix(line, "#EXT-X-MEDIA-SEQUENCE:") {
		_, err := strconv.ParseUint(strings.TrimPrefix(line, "#EXT-X-MEDIA-SEQUENCE:"), 10, 64)
		return err == nil
	}
	return false
}

func positiveIntegerTag(line, prefix string) bool {
	value, err := strconv.ParseUint(strings.TrimPrefix(line, prefix), 10, 64)
	return err == nil && value > 0
}

func validEXTINF(line string) bool {
	value := strings.TrimPrefix(line, "#EXTINF:")
	durationText, _, found := strings.Cut(value, ",")
	duration, err := strconv.ParseFloat(durationText, 64)
	return found && err == nil && duration > 0 && !math.IsInf(duration, 0)
}

func renditionDirectory(storageKey string) (string, error) {
	if storageKey == "" || strings.TrimSpace(storageKey) != storageKey || strings.HasPrefix(storageKey, "/") || strings.Contains(storageKey, "\\") {
		return "", errors.New("persisted rendition storage key is malformed")
	}
	cleaned := path.Clean(storageKey)
	directory := path.Dir(cleaned)
	if cleaned != storageKey || path.Ext(cleaned) != ".m3u8" || directory == "." || directory == ".." || strings.HasPrefix(directory, "../") {
		return "", errors.New("persisted rendition storage key is malformed")
	}
	return directory, nil
}

func mediaSegmentObjectKey(directory, reference string) (string, error) {
	if strings.TrimSpace(reference) != reference {
		return "", errors.New("HLS media segment must be a plain relative path")
	}
	parsed, err := url.Parse(reference)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.User != nil || parsed.Opaque != "" ||
		strings.HasPrefix(parsed.Path, "/") || parsed.RawPath != "" || parsed.RawQuery != "" || parsed.Fragment != "" ||
		strings.Contains(reference, "%") || strings.Contains(reference, "\\") {
		return "", errors.New("HLS media segment must be a plain relative path")
	}
	cleaned := path.Clean(parsed.Path)
	if cleaned != parsed.Path || path.Ext(cleaned) != ".ts" || cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", errors.New("HLS media segment escapes its rendition directory")
	}
	objectKey := path.Join(directory, cleaned)
	if !strings.HasPrefix(objectKey, directory+"/") {
		return "", errors.New("HLS media segment escapes its rendition directory")
	}
	return objectKey, nil
}
