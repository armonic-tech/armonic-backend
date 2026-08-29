package media

import "bytes"

type Format string

const (
	PNG  Format = "png"
	JPEG Format = "jpeg"
	GIF  Format = "gif"
	WEBP Format = "webp"
)

const sniffLen = 12

func (f Format) MIME() string {
	switch f {
	case PNG:
		return "image/png"
	case JPEG:
		return "image/jpeg"
	case GIF:
		return "image/gif"
	case WEBP:
		return "image/webp"
	}
	return "application/octet-stream"
}

func (f Format) Ext() string {
	switch f {
	case JPEG:
		return ".jpg"
	default:
		return "." + string(f)
	}
}

var (
	magicPNG   = []byte("\x89PNG\r\n\x1a\n")
	magicJPEG  = []byte{0xFF, 0xD8, 0xFF}
	magicGIF87 = []byte("GIF87a")
	magicGIF89 = []byte("GIF89a")
	magicRIFF  = []byte("RIFF")
	magicWEBP  = []byte("WEBP")
)

func sniff(b []byte) (Format, bool) {
	switch {
	case bytes.HasPrefix(b, magicPNG):
		return PNG, true
	case bytes.HasPrefix(b, magicJPEG):
		return JPEG, true
	case bytes.HasPrefix(b, magicGIF87), bytes.HasPrefix(b, magicGIF89):
		return GIF, true
	case len(b) >= sniffLen && bytes.HasPrefix(b, magicRIFF) && bytes.Equal(b[8:12], magicWEBP):
		return WEBP, true
	}
	return "", false
}
