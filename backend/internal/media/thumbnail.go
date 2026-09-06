package media

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png"
	"strings"

	"github.com/disintegration/imaging"
	"github.com/jackc/pgx/v5"
	_ "golang.org/x/image/webp"
)

const ThumbnailMaxBytes int64 = 5 * 1024 * 1024

// Limit concurrent decodes as well as the work of each individual decode.
var thumbnailProcessingSlots = make(chan struct{}, 2)

type thumbnailStore interface {
	PutObject(context.Context, string, []byte, string) error
	DownloadObject(context.Context, string) ([]byte, error)
}

type thumbnailImages struct {
	width, height int
	card, large   []byte
}

func processThumbnailBytes(body []byte, declared string) (thumbnailImages, error) {
	if len(body) == 0 || int64(len(body)) > ThumbnailMaxBytes {
		return thumbnailImages{}, fmt.Errorf("%w: thumbnail size", ErrValidation)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil || map[string]string{"jpeg": "image/jpeg", "png": "image/png", "webp": "image/webp"}[format] != declared {
		return thumbnailImages{}, fmt.Errorf("%w: thumbnail format", ErrValidation)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > 8192 || cfg.Height > 8192 || int64(cfg.Width)*int64(cfg.Height) > 16000000 {
		return thumbnailImages{}, fmt.Errorf("%w: thumbnail dimensions", ErrValidation)
	}
	orientation, err := thumbnailOrientation(body, format)
	if err != nil {
		return thumbnailImages{}, err
	}
	img, err := imaging.Decode(bytes.NewReader(body), imaging.AutoOrientation(true))
	if err != nil {
		return thumbnailImages{}, fmt.Errorf("%w: malformed thumbnail", ErrValidation)
	}
	img = orientThumbnail(img, orientation)
	bounds := img.Bounds()
	if bounds.Dx() < 800 || bounds.Dy() < 450 {
		return thumbnailImages{}, fmt.Errorf("%w: thumbnail must be at least 800 by 450", ErrValidation)
	}
	result := thumbnailImages{width: bounds.Dx(), height: bounds.Dy()}
	result.card, err = encodeThumbnail(img, 800)
	if err != nil {
		return thumbnailImages{}, err
	}
	result.large, err = encodeThumbnail(img, 1440)
	return result, err
}

func encodeThumbnail(img image.Image, side int) ([]byte, error) {
	resized := imaging.Fit(img, side, side, imaging.Lanczos)
	// JPEG has no alpha. Composite on white instead of turning transparent pixels black.
	opaque := image.NewRGBA(resized.Bounds())
	draw.Draw(opaque, opaque.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(opaque, opaque.Bounds(), resized, image.Point{}, draw.Over)
	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, opaque, &jpeg.Options{Quality: 82}); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func (s *Service) processThumbnail(ctx context.Context, tx pgx.Tx, completion uploadCompletion) (AssetVersionState, error) {
	store, ok := s.store.(thumbnailStore)
	if !ok {
		return "", ErrUnavailable
	}
	select {
	case thumbnailProcessingSlots <- struct{}{}:
		defer func() { <-thumbnailProcessingSlots }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	req := completion.request
	body, err := s.store.DownloadPrefixVersion(ctx, req.StorageObjectKey, req.StorageObjectVersion, ThumbnailMaxBytes+1)
	if err != nil {
		return "", fmt.Errorf("%w: reading thumbnail", ErrUnavailable)
	}
	images, err := processThumbnailBytes(body, req.ContentType)
	if err != nil {
		return "", err
	}
	prefix := "media/" + req.AssetVersionID + "/thumbnail/"
	cardKey, largeKey := prefix+"card.jpg", prefix+"large.jpg"
	if err = store.PutObject(ctx, cardKey, images.card, "image/jpeg"); err != nil {
		return "", fmt.Errorf("%w: writing thumbnail card", ErrUnavailable)
	}
	if err = store.PutObject(ctx, largeKey, images.large, "image/jpeg"); err != nil {
		return "", fmt.Errorf("%w: writing thumbnail large", ErrUnavailable)
	}
	_, err = tx.Exec(ctx, `INSERT INTO media_thumbnail_variants(asset_version_id,source_object_version,source_sha256,width,height,card_key,large_key)
 VALUES($1::uuid,$2,$3,$4,$5,$6,$7)`, req.AssetVersionID, req.StorageObjectVersion, strings.ToLower(req.SHA256Hex), images.width, images.height, cardKey, largeKey)
	if err != nil {
		return "", fmt.Errorf("recording thumbnail processing: %w", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE media_asset_versions SET state='READY' WHERE id=$1::uuid AND state='QUARANTINED'`, req.AssetVersionID); err != nil {
		return "", err
	}
	err = appendMediaAudit(ctx, tx, req.OwnerAccountID, "INSTRUCTOR", "COURSE_THUMBNAIL_PROCESSED", req.AssetVersionID, "Raster decoded and re-encoded; original remains private", map[string]any{"width": images.width, "height": images.height})
	return StateReady, err
}
