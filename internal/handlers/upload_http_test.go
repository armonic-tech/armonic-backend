package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/armonic-tech/armonic-backend/config"
	"github.com/armonic-tech/armonic-backend/internal/media"
	"github.com/armonic-tech/armonic-backend/internal/models/attachment"
	"github.com/stretchr/testify/require"
)

type fakeBlobs struct {
	mu   sync.Mutex
	blob map[string][]byte
}

func newFakeBlobs() *fakeBlobs { return &fakeBlobs{blob: map[string][]byte{}} }

func (f *fakeBlobs) Put(hash, variant string, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.blob[hash+"/"+variant] = data
	return nil
}

func (f *fakeBlobs) Open(hash, variant string) (io.ReadSeekCloser, time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, ok := f.blob[hash+"/"+variant]
	if !ok {
		return nil, time.Time{}, media.ErrNotFound
	}
	return nopCloser{bytes.NewReader(data)}, time.Unix(0, 0), nil
}

type nopCloser struct{ *bytes.Reader }

func (nopCloser) Close() error { return nil }

type fakeAttachments struct {
	mu    sync.Mutex
	saved []attachment.Attachment
	get   *attachment.Attachment
	err   error
}

func (f *fakeAttachments) Create(_ context.Context, a attachment.Attachment) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saved = append(f.saved, a)
	return f.err
}

func (f *fakeAttachments) GetByID(context.Context, string) (*attachment.Attachment, error) {
	return f.get, f.err
}

type fakeAvatars struct{ id string }

func (f *fakeAvatars) SetAvatar(_ context.Context, _, attachmentID string) error {
	f.id = attachmentID
	return nil
}

func testUploadConfig() config.UploadConfig {
	return config.UploadConfig{
		MaxBytes: 1 << 20, MaxImageWidth: 1000, MaxImageHeight: 1000,
		MaxImagePixels: 1_000_000, ThumbnailSize: 32,
		RatePerMin: 60, Burst: 5, MaxConcurrent: 2,
	}
}

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 0x20, A: 0xFF})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func multipartUpload(t *testing.T, field string, body []byte, userID, serverID string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	// A deliberately dishonest filename and content type: neither is consulted.
	part, err := mw.CreateFormFile(field, "totally-a-script.php")
	require.NoError(t, err)
	_, err = part.Write(body)
	require.NoError(t, err)
	require.NoError(t, mw.Close())

	r := httptest.NewRequest(http.MethodPost, "/server/"+serverID+"/upload", &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.SetPathValue("id", serverID)
	return r.WithContext(WithUserID(context.Background(), userID))
}

func TestUploadStoresSanitizedImageAndThumbnail(t *testing.T) {
	blobs, repo := newFakeBlobs(), &fakeAttachments{}
	u := NewUploads(blobs, repo, testUploadConfig())

	w := httptest.NewRecorder()
	u.Upload()(w, multipartUpload(t, "file", pngBytes(t, 64, 32), "user-1", "server-1"))
	require.Equal(t, http.StatusCreated, w.Code)

	var got attachment.Attachment
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Equal(t, "server-1", got.ServerID)
	require.Equal(t, "user-1", got.UserID)
	require.Equal(t, "image/png", got.MIME)
	require.Equal(t, 64, got.Width)
	require.Equal(t, 32, got.Height)
	require.Equal(t, "/attachment/"+got.ID, got.URL)
	require.Equal(t, "/attachment/"+got.ID+"/thumb", got.ThumbURL)

	require.Len(t, repo.saved, 1)
	require.Len(t, blobs.blob, 2)
}

func TestUploadIgnoresFilenameAndContentType(t *testing.T) {
	// The part is announced as a .php with an octet-stream content type; only
	// the magic bytes decide, so the PNG goes through.
	u := NewUploads(newFakeBlobs(), &fakeAttachments{}, testUploadConfig())

	w := httptest.NewRecorder()
	u.Upload()(w, multipartUpload(t, "file", pngBytes(t, 8, 8), "user-1", "server-1"))
	require.Equal(t, http.StatusCreated, w.Code)
}

func TestUploadRejectsNonImage(t *testing.T) {
	u := NewUploads(newFakeBlobs(), &fakeAttachments{}, testUploadConfig())

	w := httptest.NewRecorder()
	u.Upload()(w, multipartUpload(t, "file", []byte("#!/bin/sh\nrm -rf /\n"), "user-1", "server-1"))
	require.Equal(t, http.StatusUnsupportedMediaType, w.Code)
}

func TestUploadRejectsOversizedFile(t *testing.T) {
	cfg := testUploadConfig()
	cfg.MaxBytes = 128
	u := NewUploads(newFakeBlobs(), &fakeAttachments{}, cfg)

	w := httptest.NewRecorder()
	u.Upload()(w, multipartUpload(t, "file", pngBytes(t, 200, 200), "user-1", "server-1"))
	require.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
}

func TestUploadRejectsOversizedDimensions(t *testing.T) {
	cfg := testUploadConfig()
	cfg.MaxImageWidth, cfg.MaxImageHeight = 16, 16
	u := NewUploads(newFakeBlobs(), &fakeAttachments{}, cfg)

	w := httptest.NewRecorder()
	u.Upload()(w, multipartUpload(t, "file", pngBytes(t, 64, 64), "user-1", "server-1"))
	require.Equal(t, http.StatusUnprocessableEntity, w.Code)
}

func TestUploadRejectsMissingFilePart(t *testing.T) {
	u := NewUploads(newFakeBlobs(), &fakeAttachments{}, testUploadConfig())

	w := httptest.NewRecorder()
	u.Upload()(w, multipartUpload(t, "avatar", pngBytes(t, 8, 8), "user-1", "server-1"))
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUploadRateLimitIsPerUser(t *testing.T) {
	cfg := testUploadConfig()
	cfg.RatePerMin, cfg.Burst = 1, 1
	u := NewUploads(newFakeBlobs(), &fakeAttachments{}, cfg)

	first := httptest.NewRecorder()
	u.Upload()(first, multipartUpload(t, "file", pngBytes(t, 8, 8), "user-1", "server-1"))
	require.Equal(t, http.StatusCreated, first.Code)

	second := httptest.NewRecorder()
	u.Upload()(second, multipartUpload(t, "file", pngBytes(t, 8, 8), "user-1", "server-1"))
	require.Equal(t, http.StatusTooManyRequests, second.Code)
	require.NotEmpty(t, second.Header().Get("Retry-After"))

	// A different user has their own bucket, so one uploader cannot starve
	// everybody else.
	other := httptest.NewRecorder()
	u.Upload()(other, multipartUpload(t, "file", pngBytes(t, 8, 8), "user-2", "server-1"))
	require.Equal(t, http.StatusCreated, other.Code)
}

func TestUploadRequiresAuthenticatedCaller(t *testing.T) {
	u := NewUploads(newFakeBlobs(), &fakeAttachments{}, testUploadConfig())

	r := httptest.NewRequest(http.MethodPost, "/server/s1/upload", nil)
	r.SetPathValue("id", "s1")
	w := httptest.NewRecorder()
	u.Upload()(w, r)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAvatarPointsProfileAtTheUpload(t *testing.T) {
	repo, avatars := &fakeAttachments{}, &fakeAvatars{}
	u := NewUploads(newFakeBlobs(), repo, testUploadConfig())

	w := httptest.NewRecorder()
	u.Avatar(avatars, "server-1")(w, multipartUpload(t, "file", pngBytes(t, 8, 8), "user-1", ""))
	require.Equal(t, http.StatusOK, w.Code)

	require.Len(t, repo.saved, 1)
	require.Equal(t, repo.saved[0].ID, avatars.id)
	require.Equal(t, "server-1", repo.saved[0].ServerID)
}

func TestServeSendsBytesWithHardenedHeaders(t *testing.T) {
	blobs := newFakeBlobs()
	payload := pngBytes(t, 8, 8)
	hash := media.Hash(payload)
	require.NoError(t, blobs.Put(hash, media.VariantFull, payload))

	repo := &fakeAttachments{get: &attachment.Attachment{
		ID: "a1", Hash: hash, Format: "png", ThumbFormat: "png", MIME: "image/png",
	}}
	u := NewUploads(blobs, repo, testUploadConfig())

	r := httptest.NewRequest(http.MethodGet, "/attachment/a1", nil)
	r.SetPathValue("id", "a1")
	w := httptest.NewRecorder()
	u.Serve(media.VariantFull)(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, payload, w.Body.Bytes())
	require.Equal(t, "image/png", w.Header().Get("Content-Type"))
	require.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
	require.Equal(t, "inline", w.Header().Get("Content-Disposition"))
}

func TestServeUsesThumbFormatForThumbVariant(t *testing.T) {
	blobs := newFakeBlobs()
	hash := media.Hash([]byte("thumb"))
	require.NoError(t, blobs.Put(hash, media.VariantThumb, []byte("thumb")))

	repo := &fakeAttachments{get: &attachment.Attachment{
		ID: "a1", Hash: hash, Format: "gif", ThumbFormat: "png", MIME: "image/gif",
	}}
	u := NewUploads(blobs, repo, testUploadConfig())

	r := httptest.NewRequest(http.MethodGet, "/attachment/a1/thumb", nil)
	r.SetPathValue("id", "a1")
	w := httptest.NewRecorder()
	u.Serve(media.VariantThumb)(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "image/png", w.Header().Get("Content-Type"))
}

func TestServeUnknownAttachment(t *testing.T) {
	u := NewUploads(newFakeBlobs(), &fakeAttachments{}, testUploadConfig())

	r := httptest.NewRequest(http.MethodGet, "/attachment/nope", nil)
	r.SetPathValue("id", "nope")
	w := httptest.NewRecorder()
	u.Serve(media.VariantFull)(w, r)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeAttachmentRowWithoutBlob(t *testing.T) {
	repo := &fakeAttachments{get: &attachment.Attachment{
		ID: "a1", Hash: media.Hash([]byte("missing")), Format: "png", ThumbFormat: "png",
	}}
	u := NewUploads(newFakeBlobs(), repo, testUploadConfig())

	r := httptest.NewRequest(http.MethodGet, "/attachment/a1", nil)
	r.SetPathValue("id", "a1")
	w := httptest.NewRecorder()
	u.Serve(media.VariantFull)(w, r)
	require.Equal(t, http.StatusNotFound, w.Code)
}
