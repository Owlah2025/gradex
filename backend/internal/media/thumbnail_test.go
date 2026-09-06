package media

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"strings"
	"testing"
)

func thumbnailFixture(t *testing.T, format string, width, height int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x % 256), G: uint8(y % 256), B: 110, A: 255})
		}
	}
	var b bytes.Buffer
	var err error
	if format == "png" {
		err = png.Encode(&b, img)
	} else {
		err = jpeg.Encode(&b, img, &jpeg.Options{Quality: 90})
	}
	if err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestThumbnailRasterVariants(t *testing.T) {
	for _, format := range []string{"jpeg", "png", "webp"} {
		t.Run(format, func(t *testing.T) {
			var body []byte
			if format == "webp" {
				var err error
				body, err = os.ReadFile("testdata/thumbnail.webp")
				if err != nil {
					t.Fatal(err)
				}
			} else {
				body = thumbnailFixture(t, format, 1200, 675)
			}
			variants, err := processThumbnailBytes(body, "image/"+format)
			if err != nil {
				t.Fatal(err)
			}
			for name, data := range map[string][]byte{"card": variants.card, "large": variants.large} {
				cfg, decodedFormat, err := image.DecodeConfig(bytes.NewReader(data))
				if err != nil || decodedFormat != "jpeg" {
					t.Fatalf("%s is not a decoded JPEG: %s %v", name, decodedFormat, err)
				}
				if cfg.Width > 1200 || cfg.Height > 675 {
					t.Fatalf("upscaled %s: %+v", name, cfg)
				}
				if name == "card" && (cfg.Width != 800 || cfg.Height != 450) {
					t.Fatalf("card dimensions: %+v", cfg)
				}
				if bytes.Contains(data, []byte("Exif")) {
					t.Fatal("metadata survived re-encode")
				}
			}
		})
	}
}

func TestThumbnailRejectsHostileInput(t *testing.T) {
	valid := thumbnailFixture(t, "jpeg", 800, 450)
	for _, tc := range []struct {
		name, mime string
		body       []byte
	}{
		{"svg", "image/svg+xml", []byte(`<svg onload="alert(1)"></svg>`)},
		{"spoof", "image/png", valid},
		{"malformed", "image/jpeg", valid[:len(valid)/2]},
		{"oversize", "image/jpeg", make([]byte, ThumbnailMaxBytes+1)},
		{"tiny", "image/png", thumbnailFixture(t, "png", 799, 450)},
		{"extreme side", "image/png", thumbnailFixture(t, "png", 8193, 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := processThumbnailBytes(tc.body, tc.mime); !errors.Is(err, ErrValidation) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestThumbnailNormalizesJPEGOrientation(t *testing.T) {
	body := thumbnailFixture(t, "jpeg", 450, 800)
	// EXIF TIFF orientation=6 (rotate clockwise), plus a GPS-looking metadata marker.
	exif := []byte{'E', 'x', 'i', 'f', 0, 0, 'I', 'I', 42, 0, 8, 0, 0, 0, 1, 0, 0x12, 1, 3, 0, 1, 0, 0, 0, 6, 0, 0, 0, 0, 0, 0, 0}
	segment := append([]byte{0xff, 0xe1, 0, byte(len(exif) + 2)}, exif...)
	oriented := append(append(append([]byte{}, body[:2]...), segment...), body[2:]...)
	result, err := processThumbnailBytes(oriented, "image/jpeg")
	if err != nil {
		t.Fatal(err)
	}
	if result.width != 800 || result.height != 450 {
		t.Fatalf("orientation not normalized: %dx%d", result.width, result.height)
	}
	if bytes.Contains(result.card, []byte("Exif")) {
		t.Fatal("EXIF retained")
	}
}

func TestThumbnailPNGMetadataAndPixelBounds(t *testing.T) {
	portrait := thumbnailFixture(t, "png", 450, 800)
	tiff := []byte{'I', 'I', 42, 0, 8, 0, 0, 0, 1, 0, 0x12, 1, 3, 0, 1, 0, 0, 0, 6, 0, 0, 0, 0, 0, 0, 0}
	chunk := func(kind string, payload []byte) []byte {
		encoded := make([]byte, len(payload)+12)
		binary.BigEndian.PutUint32(encoded, uint32(len(payload)))
		copy(encoded[4:], kind)
		copy(encoded[8:], payload)
		binary.BigEndian.PutUint32(encoded[len(encoded)-4:], crc32.ChecksumIEEE(encoded[4:len(encoded)-4]))
		return encoded
	}
	withChunk := func(data, extra []byte) []byte {
		return append(append(append([]byte{}, data[:33]...), extra...), data[33:]...)
	}
	result, err := processThumbnailBytes(withChunk(portrait, chunk("eXIf", tiff)), "image/png")
	if err != nil || result.width != 800 || result.height != 450 {
		t.Fatalf("PNG orientation: %+v %v", result, err)
	}
	if _, err = processThumbnailBytes(withChunk(portrait, chunk("acTL", make([]byte, 8))), "image/png"); !errors.Is(err, ErrValidation) {
		t.Fatalf("animated PNG: %v", err)
	}
	bomb := append([]byte{}, portrait...)
	binary.BigEndian.PutUint32(bomb[16:20], 5000)
	binary.BigEndian.PutUint32(bomb[20:24], 4000)
	binary.BigEndian.PutUint32(bomb[29:33], crc32.ChecksumIEEE(bomb[12:29]))
	if _, err = processThumbnailBytes(bomb, "image/png"); err == nil || !strings.Contains(err.Error(), "dimensions") {
		t.Fatalf("pixel limit: %v", err)
	}
}

func TestThumbnailWebPOrientationIsAppliedBeforeMinimumDimensions(t *testing.T) {
	body, err := os.ReadFile("testdata/thumbnail.webp")
	if err != nil {
		t.Fatal(err)
	}
	tiff := []byte{'I', 'I', 42, 0, 8, 0, 0, 0, 1, 0, 0x12, 1, 3, 0, 1, 0, 0, 0, 6, 0, 0, 0, 0, 0, 0, 0}
	extra := make([]byte, len(tiff)+8)
	copy(extra, "EXIF")
	binary.LittleEndian.PutUint32(extra[4:8], uint32(len(tiff)))
	copy(extra[8:], tiff)
	body = append(body, extra...)
	binary.LittleEndian.PutUint32(body[4:8], uint32(len(body)-8))
	if _, err = processThumbnailBytes(body, "image/webp"); err == nil || !strings.Contains(err.Error(), "800 by 450") {
		t.Fatalf("WebP orientation: %v", err)
	}
}
