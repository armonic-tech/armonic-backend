package media

import (
	"bytes"
	"errors"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"

	"golang.org/x/image/draw"

	"golang.org/x/image/webp"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
)

var (
	ErrTooLarge    = errors.New("file exceeds the maximum upload size")
	ErrUnsupported = errors.New("unsupported image format")
	ErrDimensions  = errors.New("image dimensions exceed the configured limit")
	ErrCorrupt     = errors.New("image could not be decoded")
	ErrPolyglot    = errors.New("image container does not match its contents")
)

const jpegQuality = 85

type Limits struct {
	MaxBytes  int64
	MaxWidth  int
	MaxHeight int
	MaxPixels int64
	ThumbSize int
}

type Image struct {
	Format      Format
	ThumbFormat Format
	Width       int
	Height      int
	Data        []byte
	Thumb       []byte
}

func Process(r io.Reader, lim Limits) (*Image, error) {
	raw, err := readLimited(r, lim.MaxBytes)
	if err != nil {
		return nil, err
	}

	format, ok := sniff(raw)
	if !ok {
		return nil, ErrUnsupported
	}

	cfg, cfgName, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, ErrCorrupt
	}
	if cfgName != string(format) {
		return nil, ErrPolyglot
	}
	if err := lim.check(cfg.Width, cfg.Height, 1); err != nil {
		return nil, err
	}

	if format == GIF {
		return processGIF(raw, lim)
	}
	return processStill(raw, format, cfg, lim)
}

func processStill(raw []byte, format Format, cfg image.Config, lim Limits) (*Image, error) {
	rd := bytes.NewReader(raw)
	img, err := decodeAs(rd, format)
	if err != nil {
		return nil, ErrCorrupt
	}
	if format == PNG && rd.Len() > 0 {
		return nil, ErrPolyglot
	}
	if b := img.Bounds(); b.Dx() != cfg.Width || b.Dy() != cfg.Height {
		return nil, ErrCorrupt
	}

	if format == JPEG {
		img = applyOrientation(img, jpegOrientation(raw))
	}

	out := PNG
	if format == JPEG {
		out = JPEG
	}

	data, err := encode(img, out)
	if err != nil {
		return nil, err
	}
	thumb, err := encode(thumbnail(img, lim.ThumbSize), out)
	if err != nil {
		return nil, err
	}

	b := img.Bounds()
	return &Image{
		Format: out, ThumbFormat: out,
		Width: b.Dx(), Height: b.Dy(),
		Data: data, Thumb: thumb,
	}, nil
}

func decodeAs(rd *bytes.Reader, format Format) (image.Image, error) {
	switch format {
	case PNG:
		return png.Decode(rd)
	case JPEG:
		return jpeg.Decode(rd)
	case WEBP:
		return webp.Decode(rd)
	}
	return nil, ErrUnsupported
}

func processGIF(raw []byte, lim Limits) (*Image, error) {
	g, err := gif.DecodeAll(bytes.NewReader(raw))
	if err != nil {
		return nil, ErrCorrupt
	}
	if len(g.Image) == 0 {
		return nil, ErrCorrupt
	}
	w, h := g.Config.Width, g.Config.Height
	if w <= 0 || h <= 0 {
		b := g.Image[0].Bounds()
		w, h = b.Dx(), b.Dy()
	}
	if err := lim.check(w, h, len(g.Image)); err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		return nil, ErrCorrupt
	}

	thumb, err := encode(thumbnail(flatten(g.Image[0], w, h), lim.ThumbSize), PNG)
	if err != nil {
		return nil, err
	}
	return &Image{
		Format: GIF, ThumbFormat: PNG,
		Width: w, Height: h,
		Data: buf.Bytes(), Thumb: thumb,
	}, nil
}

func (l Limits) check(width, height, frames int) error {
	if width <= 0 || height <= 0 {
		return ErrCorrupt
	}
	if width > l.MaxWidth || height > l.MaxHeight {
		return ErrDimensions
	}
	if int64(width)*int64(height)*int64(frames) > l.MaxPixels {
		return ErrDimensions
	}
	return nil
}

func encode(img image.Image, f Format) ([]byte, error) {
	var buf bytes.Buffer
	var err error
	switch f {
	case JPEG:
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: jpegQuality})
	default:
		enc := png.Encoder{CompressionLevel: png.DefaultCompression}
		err = enc.Encode(&buf, img)
	}
	if err != nil {
		return nil, ErrCorrupt
	}
	return buf.Bytes(), nil
}

func thumbnail(img image.Image, maxSide int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if maxSide <= 0 || (w <= maxSide && h <= maxSide) {
		return img
	}
	scale := float64(maxSide) / float64(max(w, h))
	dst := image.NewNRGBA(image.Rect(0, 0, max(1, int(float64(w)*scale)), max(1, int(float64(h)*scale))))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
	return dst
}

func readLimited(r io.Reader, maxBytes int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, ErrTooLarge
	}
	if len(data) < sniffLen {
		return nil, ErrUnsupported
	}
	return data, nil
}
