package media

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"github.com/disintegration/imaging"
	"image"
)

// PNG and WebP can carry EXIF too. imaging handles JPEG EXIF; these bounded
// container walks handle the other two formats and reject animated covers.
func thumbnailOrientation(body []byte, format string) (int, error) {
	var metadata []byte
	switch format {
	case "png":
		for offset := 8; offset+12 <= len(body); {
			size := int64(binary.BigEndian.Uint32(body[offset : offset+4]))
			if size > int64(len(body)-offset-12) {
				return 0, fmt.Errorf("%w: malformed PNG chunk", ErrValidation)
			}
			kind := string(body[offset+4 : offset+8])
			if kind == "acTL" {
				return 0, fmt.Errorf("%w: animated thumbnails are unsupported", ErrValidation)
			}
			if kind == "eXIf" {
				metadata = body[offset+8 : offset+8+int(size)]
			}
			offset += 12 + int(size)
		}
	case "webp":
		for offset := 12; offset+8 <= len(body); {
			size := int64(binary.LittleEndian.Uint32(body[offset+4 : offset+8]))
			if size > int64(len(body)-offset-8) {
				return 0, fmt.Errorf("%w: malformed WebP chunk", ErrValidation)
			}
			kind := string(body[offset : offset+4])
			if kind == "ANIM" || kind == "ANMF" {
				return 0, fmt.Errorf("%w: animated thumbnails are unsupported", ErrValidation)
			}
			if kind == "EXIF" {
				metadata = body[offset+8 : offset+8+int(size)]
			}
			offset += 8 + int(size) + int(size%2)
		}
	}
	if len(metadata) == 0 {
		return 1, nil
	}
	return tiffOrientation(bytes.TrimPrefix(metadata, []byte("Exif\x00\x00")))
}

// Read only IFD0's orientation SHORT. Never follow arbitrary EXIF/GPS offsets.
func tiffOrientation(data []byte) (int, error) {
	invalid := fmt.Errorf("%w: invalid image orientation metadata", ErrValidation)
	if len(data) < 8 {
		return 0, invalid
	}
	var order binary.ByteOrder
	switch string(data[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 0, invalid
	}
	if order.Uint16(data[2:4]) != 42 {
		return 0, invalid
	}
	offset := int64(order.Uint32(data[4:8]))
	if offset < 8 || offset > int64(len(data)-2) {
		return 0, invalid
	}
	count := int64(order.Uint16(data[offset : offset+2]))
	if count > (int64(len(data))-offset-2)/12 {
		return 0, invalid
	}
	for i := int64(0); i < count; i++ {
		entry := data[offset+2+i*12 : offset+14+i*12]
		if order.Uint16(entry[:2]) != 0x0112 {
			continue
		}
		if order.Uint16(entry[2:4]) != 3 || order.Uint32(entry[4:8]) != 1 {
			return 0, invalid
		}
		orientation := int(order.Uint16(entry[8:10]))
		if orientation < 1 || orientation > 8 {
			return 0, invalid
		}
		return orientation, nil
	}
	return 1, nil
}

func orientThumbnail(img image.Image, orientation int) image.Image {
	switch orientation {
	case 2:
		return imaging.FlipH(img)
	case 3:
		return imaging.Rotate180(img)
	case 4:
		return imaging.FlipV(img)
	case 5:
		return imaging.Transpose(img)
	case 6:
		return imaging.Rotate270(img)
	case 7:
		return imaging.Transverse(img)
	case 8:
		return imaging.Rotate90(img)
	default:
		return img
	}
}
