package tools

import "image"

// exifOrientation reads the TIFF Orientation tag from a JPEG or WebP payload.
// Missing or unreadable tags return 1 (no transform). JPEG scans skip APP1
// segments that are not EXIF, so an XMP APP1 before the EXIF APP1 is ignored.
func exifOrientation(data []byte) int {
	off := -1
	switch {
	case len(data) >= 2 && data[0] == 0xff && data[1] == 0xd8:
		off = findJpegTiffOffset(data)
	case len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		off = findWebpTiffOffset(data)
	}
	if off < 0 {
		return 1
	}
	return readOrientationFromTiff(data, off)
}

func orientationSwapsAxes(orientation int) bool {
	return orientation >= 5 && orientation <= 8
}

func hasExifHeader(data []byte, offset int) bool {
	return offset >= 0 && offset+6 <= len(data) &&
		data[offset] == 'E' && data[offset+1] == 'x' && data[offset+2] == 'i' && data[offset+3] == 'f' &&
		data[offset+4] == 0 && data[offset+5] == 0
}

func findJpegTiffOffset(data []byte) int {
	offset := 2
	for offset < len(data)-1 {
		if data[offset] != 0xff {
			return -1
		}
		marker := data[offset+1]
		if marker == 0xff {
			offset++
			continue
		}
		if marker == 0xe1 {
			if offset+4 >= len(data) {
				return -1
			}
			segmentStart := offset + 4
			if segmentStart+6 > len(data) {
				return -1
			}
			if hasExifHeader(data, segmentStart) {
				return segmentStart + 6
			}
		}
		if offset+4 > len(data) {
			return -1
		}
		length := int(data[offset+2])<<8 | int(data[offset+3])
		if length < 2 {
			return -1
		}
		offset += 2 + length
	}
	return -1
}

func findWebpTiffOffset(data []byte) int {
	offset := 12
	for offset+8 <= len(data) {
		chunkID := string(data[offset : offset+4])
		chunkSize := int(data[offset+4]) | int(data[offset+5])<<8 | int(data[offset+6])<<16 | int(data[offset+7])<<24
		dataStart := offset + 8
		if chunkID == "EXIF" {
			if chunkSize < 0 || dataStart+chunkSize > len(data) {
				return -1
			}
			if chunkSize >= 6 && hasExifHeader(data, dataStart) {
				return dataStart + 6
			}
			return dataStart
		}
		if chunkSize < 0 {
			return -1
		}
		next := dataStart + chunkSize + chunkSize%2
		if next <= offset {
			return -1
		}
		offset = next
	}
	return -1
}

func readOrientationFromTiff(data []byte, tiffStart int) int {
	if tiffStart < 0 || tiffStart+8 > len(data) {
		return 1
	}
	byteOrder := int(data[tiffStart])<<8 | int(data[tiffStart+1])
	le := byteOrder == 0x4949
	read16 := func(pos int) int {
		if pos < 0 || pos+2 > len(data) {
			return 0
		}
		if le {
			return int(data[pos]) | int(data[pos+1])<<8
		}
		return int(data[pos])<<8 | int(data[pos+1])
	}
	read32 := func(pos int) int {
		if pos < 0 || pos+4 > len(data) {
			return 0
		}
		if le {
			return int(data[pos]) | int(data[pos+1])<<8 | int(data[pos+2])<<16 | int(data[pos+3])<<24
		}
		return int(data[pos])<<24 | int(data[pos+1])<<16 | int(data[pos+2])<<8 | int(data[pos+3])
	}
	ifdStart := tiffStart + read32(tiffStart+4)
	if ifdStart < 0 || ifdStart+2 > len(data) {
		return 1
	}
	entryCount := read16(ifdStart)
	for i := 0; i < entryCount; i++ {
		entryPos := ifdStart + 2 + i*12
		if entryPos < 0 || entryPos+12 > len(data) {
			return 1
		}
		if read16(entryPos) != 0x0112 {
			continue
		}
		value := read16(entryPos + 8)
		if value >= 1 && value <= 8 {
			return value
		}
		return 1
	}
	return 1
}

// applyOrientation bakes a TIFF orientation into pixels. Orientation 1 and
// unknown values return src unchanged.
func applyOrientation(src image.Image, orientation int) image.Image {
	switch orientation {
	case 2:
		return flipImage(src, true, false)
	case 3:
		return flipImage(src, true, true)
	case 4:
		return flipImage(src, false, true)
	case 5:
		return flipImage(rotate90(src, true), true, false)
	case 6:
		return rotate90(src, true)
	case 7:
		return flipImage(rotate90(src, false), true, false)
	case 8:
		return rotate90(src, false)
	default:
		return src
	}
}

func rotate90(src image.Image, clockwise bool) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, h, w))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := src.At(b.Min.X+x, b.Min.Y+y)
			if clockwise {
				dst.Set(h-1-y, x, c)
				continue
			}
			dst.Set(y, w-1-x, c)
		}
	}
	return dst
}

func flipImage(src image.Image, horizontal, vertical bool) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dx, dy := x, y
			if horizontal {
				dx = w - 1 - x
			}
			if vertical {
				dy = h - 1 - y
			}
			dst.Set(dx, dy, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}
