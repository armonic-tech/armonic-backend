package media

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/draw"
)

func jpegOrientation(data []byte) int {
	const (
		markerAPP1 = 0xE1
		markerSOS  = 0xDA
		markerEOI  = 0xD9
		tagOrient  = 0x0112
	)
	exifPrefix := []byte("Exif\x00\x00")

	i := 2
	for i+4 <= len(data) {
		if data[i] != 0xFF {
			return 1
		}
		marker := data[i+1]
		if marker == markerSOS || marker == markerEOI {
			return 1
		}
		segLen := int(binary.BigEndian.Uint16(data[i+2:]))
		if segLen < 2 || i+2+segLen > len(data) {
			return 1
		}
		payload := data[i+4 : i+2+segLen]
		if marker == markerAPP1 && bytes.HasPrefix(payload, exifPrefix) {
			if o, ok := orientationFromTIFF(payload[len(exifPrefix):], tagOrient); ok {
				return o
			}
			return 1
		}
		i += 2 + segLen
	}
	return 1
}

func orientationFromTIFF(tiff []byte, tag uint16) (int, bool) {
	if len(tiff) < 8 {
		return 0, false
	}
	var bo binary.ByteOrder
	switch {
	case tiff[0] == 'I' && tiff[1] == 'I':
		bo = binary.LittleEndian
	case tiff[0] == 'M' && tiff[1] == 'M':
		bo = binary.BigEndian
	default:
		return 0, false
	}
	if bo.Uint16(tiff[2:]) != 42 {
		return 0, false
	}

	off := int(bo.Uint32(tiff[4:]))
	if off < 8 || off+2 > len(tiff) {
		return 0, false
	}
	count := int(bo.Uint16(tiff[off:]))
	for e := range count {
		entry := off + 2 + e*12
		if entry+12 > len(tiff) {
			return 0, false
		}
		if bo.Uint16(tiff[entry:]) != tag {
			continue
		}
		if bo.Uint16(tiff[entry+2:]) != 3 {
			return 0, false
		}
		o := int(bo.Uint16(tiff[entry+8:]))
		if o < 1 || o > 8 {
			return 0, false
		}
		return o, true
	}
	return 0, false
}

func applyOrientation(img image.Image, o int) image.Image {
	if o <= 1 || o > 8 {
		return img
	}
	src := img.Bounds()
	w, h := src.Dx(), src.Dy()

	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	dst := image.NewNRGBA(image.Rect(0, 0, dw, dh))

	for y := range h {
		for x := range w {
			var dx, dy int
			switch o {
			case 2:
				dx, dy = w-1-x, y
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dx, dy = x, h-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			}
			dst.Set(dx, dy, img.At(src.Min.X+x, src.Min.Y+y))
		}
	}
	return dst
}

func flatten(img image.Image, w, h int) image.Image {
	canvas := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(canvas, img.Bounds(), img, img.Bounds().Min, draw.Src)
	return canvas
}
