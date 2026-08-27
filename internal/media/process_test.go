package media

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func testLimits() Limits {
	return Limits{MaxBytes: 5 << 20, MaxWidth: 1000, MaxHeight: 1000, MaxPixels: 1_000_000, ThumbSize: 64}
}

func solid(w, h int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.NRGBA{R: uint8(x % 256), G: uint8(y % 256), B: 0x40, A: 0xFF})
		}
	}
	return img
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func encodeJPEG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, img, nil))
	return buf.Bytes()
}

func encodeGIF(t *testing.T, frames int, w, h int) []byte {
	t.Helper()
	g := &gif.GIF{Config: image.Config{Width: w, Height: h}}
	for range frames {
		p := image.NewPaletted(image.Rect(0, 0, w, h), color.Palette{color.Black, color.White})
		g.Image = append(g.Image, p)
		g.Delay = append(g.Delay, 10)
	}
	var buf bytes.Buffer
	require.NoError(t, gif.EncodeAll(&buf, g))
	return buf.Bytes()
}

func insertPNGChunk(t *testing.T, data []byte, ctype string, payload []byte) []byte {
	t.Helper()
	idx := bytes.Index(data, []byte("IDAT"))
	require.Greater(t, idx, 4)
	at := idx - 4

	var chunk bytes.Buffer
	binary.Write(&chunk, binary.BigEndian, uint32(len(payload)))
	body := append([]byte(ctype), payload...)
	chunk.Write(body)
	binary.Write(&chunk, binary.BigEndian, crc32.ChecksumIEEE(body))

	out := make([]byte, 0, len(data)+chunk.Len())
	out = append(out, data[:at]...)
	out = append(out, chunk.Bytes()...)
	out = append(out, data[at:]...)
	return out
}

func insertJPEGExif(t *testing.T, data []byte, orientation uint16) []byte {
	t.Helper()
	tiff := new(bytes.Buffer)
	tiff.WriteString("II")
	binary.Write(tiff, binary.LittleEndian, uint16(42))
	binary.Write(tiff, binary.LittleEndian, uint32(8))
	binary.Write(tiff, binary.LittleEndian, uint16(1))
	binary.Write(tiff, binary.LittleEndian, uint16(0x0112))
	binary.Write(tiff, binary.LittleEndian, uint16(3))
	binary.Write(tiff, binary.LittleEndian, uint32(1))
	binary.Write(tiff, binary.LittleEndian, orientation)
	binary.Write(tiff, binary.LittleEndian, uint16(0))
	binary.Write(tiff, binary.LittleEndian, uint32(0))

	payload := append([]byte("Exif\x00\x00"), tiff.Bytes()...)
	seg := new(bytes.Buffer)
	seg.Write([]byte{0xFF, 0xE1})
	binary.Write(seg, binary.BigEndian, uint16(len(payload)+2))
	seg.Write(payload)

	out := make([]byte, 0, len(data)+seg.Len())
	out = append(out, data[:2]...)
	out = append(out, seg.Bytes()...)
	out = append(out, data[2:]...)
	return out
}

func TestProcessAcceptsSupportedFormats(t *testing.T) {
	cases := map[string]struct {
		data []byte
		want Format
	}{
		"png":  {encodePNG(t, solid(40, 20)), PNG},
		"jpeg": {encodeJPEG(t, solid(40, 20)), JPEG},
		"gif":  {encodeGIF(t, 1, 40, 20), GIF},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			img, err := Process(bytes.NewReader(tc.data), testLimits())
			require.NoError(t, err)
			require.Equal(t, tc.want, img.Format)
			require.Equal(t, 40, img.Width)
			require.Equal(t, 20, img.Height)
			require.NotEmpty(t, img.Data)
			require.NotEmpty(t, img.Thumb)
		})
	}
}

func TestProcessIgnoresClaimedTypeAndTrustsMagicBytes(t *testing.T) {
	png := encodePNG(t, solid(10, 10))
	img, err := Process(bytes.NewReader(png), testLimits())
	require.NoError(t, err)
	require.Equal(t, PNG, img.Format)

	_, err = Process(strings.NewReader("<svg xmlns='http://www.w3.org/2000/svg'></svg>"), testLimits())
	require.ErrorIs(t, err, ErrUnsupported)

	// A GIF header glued onto a script parses as a GIF logical screen with
	// junk dimensions, so it is rejected on either the dimension or the decode
	// gate depending on what the junk happens to say.
	_, err = Process(strings.NewReader("GIF89a<script>alert(1)</script>"), testLimits())
	require.Error(t, err)
}

func TestProcessRejectsTrailingPayloadOnPNG(t *testing.T) {
	data := append(encodePNG(t, solid(10, 10)), []byte("PK\x03\x04 appended zip")...)
	_, err := Process(bytes.NewReader(data), testLimits())
	require.ErrorIs(t, err, ErrPolyglot)
}

func TestProcessStripsPNGMetadataChunks(t *testing.T) {
	data := insertPNGChunk(t, encodePNG(t, solid(20, 20)), "tEXt", []byte("Comment\x00pwned"))
	require.Contains(t, string(data), "pwned")

	img, err := Process(bytes.NewReader(data), testLimits())
	require.NoError(t, err)
	require.NotContains(t, string(img.Data), "pwned")
	require.NotContains(t, string(img.Data), "tEXt")
}

func TestProcessStripsExifAndBakesOrientation(t *testing.T) {
	data := insertJPEGExif(t, encodeJPEG(t, solid(40, 20)), 6)
	require.Contains(t, string(data), "Exif")

	img, err := Process(bytes.NewReader(data), testLimits())
	require.NoError(t, err)
	require.NotContains(t, string(img.Data), "Exif")
	// Orientation 6 is "rotate 90 CW", so the axes swap.
	require.Equal(t, 20, img.Width)
	require.Equal(t, 40, img.Height)
}

func TestProcessRejectsOversizedDimensions(t *testing.T) {
	lim := testLimits()
	lim.MaxWidth, lim.MaxHeight = 30, 30

	_, err := Process(bytes.NewReader(encodePNG(t, solid(40, 20))), lim)
	require.ErrorIs(t, err, ErrDimensions)
}

func TestProcessRejectsPixelBudgetOverrun(t *testing.T) {
	lim := testLimits()
	lim.MaxPixels = 100

	_, err := Process(bytes.NewReader(encodePNG(t, solid(40, 20))), lim)
	require.ErrorIs(t, err, ErrDimensions)
}

func TestProcessCountsGIFFramesTowardsPixelBudget(t *testing.T) {
	lim := testLimits()
	lim.MaxPixels = 5000

	// One 40x20 frame is 800 pixels and passes; ten frames is 8000 and does not.
	_, err := Process(bytes.NewReader(encodeGIF(t, 1, 40, 20)), lim)
	require.NoError(t, err)

	_, err = Process(bytes.NewReader(encodeGIF(t, 10, 40, 20)), lim)
	require.ErrorIs(t, err, ErrDimensions)
}

func TestProcessPreservesGIFAnimation(t *testing.T) {
	img, err := Process(bytes.NewReader(encodeGIF(t, 4, 40, 20)), testLimits())
	require.NoError(t, err)
	require.Equal(t, GIF, img.Format)
	require.Equal(t, PNG, img.ThumbFormat)

	g, err := gif.DecodeAll(bytes.NewReader(img.Data))
	require.NoError(t, err)
	require.Len(t, g.Image, 4)
}

func TestProcessReencodesWebPAsPNG(t *testing.T) {
	data := losslessWebP(t)
	img, err := Process(bytes.NewReader(data), testLimits())
	require.NoError(t, err)
	require.Equal(t, PNG, img.Format)
	require.True(t, bytes.HasPrefix(img.Data, magicPNG))
}

func TestProcessRejectsOversizedFile(t *testing.T) {
	lim := testLimits()
	lim.MaxBytes = 64

	_, err := Process(bytes.NewReader(encodePNG(t, solid(200, 200))), lim)
	require.ErrorIs(t, err, ErrTooLarge)
}

func TestProcessRejectsTruncatedAndEmpty(t *testing.T) {
	_, err := Process(bytes.NewReader(nil), testLimits())
	require.ErrorIs(t, err, ErrUnsupported)

	data := encodePNG(t, solid(20, 20))
	_, err = Process(bytes.NewReader(data[:len(data)/2]), testLimits())
	require.ErrorIs(t, err, ErrCorrupt)
}

func TestThumbnailFitsInsideBox(t *testing.T) {
	lim := testLimits()
	lim.ThumbSize = 16

	img, err := Process(bytes.NewReader(encodePNG(t, solid(100, 50))), lim)
	require.NoError(t, err)

	cfg, _, err := image.DecodeConfig(bytes.NewReader(img.Thumb))
	require.NoError(t, err)
	require.Equal(t, 16, cfg.Width)
	require.Equal(t, 8, cfg.Height)
}

func TestSniff(t *testing.T) {
	cases := map[string]struct {
		data []byte
		want Format
		ok   bool
	}{
		"png":       {encodePNG(t, solid(4, 4)), PNG, true},
		"jpeg":      {encodeJPEG(t, solid(4, 4)), JPEG, true},
		"gif":       {encodeGIF(t, 1, 4, 4), GIF, true},
		"webp":      {losslessWebP(t), WEBP, true},
		"text":      {[]byte("just some text here"), "", false},
		"riff-wave": {[]byte("RIFF\x00\x00\x00\x00WAVEfmt "), "", false},
		"short":     {[]byte("\x89PNG"), "", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, ok := sniff(tc.data)
			require.Equal(t, tc.ok, ok)
			require.Equal(t, tc.want, got)
		})
	}
}

// losslessWebP is a hand-built 1x1 VP8L file; x/image ships no WebP encoder.
func losslessWebP(t *testing.T) []byte {
	t.Helper()
	return []byte{
		0x52, 0x49, 0x46, 0x46, 0x1A, 0x00, 0x00, 0x00,
		0x57, 0x45, 0x42, 0x50, 0x56, 0x50, 0x38, 0x4C,
		0x0D, 0x00, 0x00, 0x00, 0x2F, 0x00, 0x00, 0x00,
		0x10, 0x07, 0x10, 0x11, 0x11, 0x88, 0x88, 0xFE,
		0x07, 0x00,
	}
}
