package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/armonic-tech/armonic-backend/internal/media"
	"github.com/armonic-tech/armonic-backend/internal/models/attachment"
	"github.com/stretchr/testify/require"
)

func encryptedUpload(t *testing.T, full, thumb []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("file", "blob")
	require.NoError(t, err)
	_, _ = part.Write(full)
	if thumb != nil {
		part, err = mw.CreateFormFile("thumb", "thumb")
		require.NoError(t, err)
		_, _ = part.Write(thumb)
	}
	require.NoError(t, mw.Close())
	r := httptest.NewRequest(http.MethodPost, "/server/server-1/upload/encrypted", &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.SetPathValue("id", "server-1")
	return r.WithContext(WithUserID(r.Context(), "user-1"))
}

func TestEncryptedUploadStoresOpaqueBytes(t *testing.T) {
	blobs, repo := newFakeBlobs(), &fakeAttachments{}
	u := NewUploads(blobs, repo, testUploadConfig())

	// Deliberately not an image: the server must not care.
	full := []byte("ciphertext-of-a-photo")
	thumb := []byte("ciphertext-of-a-thumb")
	w := httptest.NewRecorder()
	u.UploadEncrypted()(w, encryptedUpload(t, full, thumb))
	require.Equal(t, http.StatusCreated, w.Code)

	var got attachment.Attachment
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.True(t, got.Encrypted)
	require.Equal(t, "application/octet-stream", got.MIME)
	require.Equal(t, int64(len(full)), got.Size)
	require.Equal(t, int64(len(thumb)), got.ThumbSize)
	require.Zero(t, got.Width)

	require.Len(t, repo.saved, 1)
	require.Equal(t, full, blobs.blob[repo.saved[0].Hash+"/"+media.VariantFull])
	require.Equal(t, thumb, blobs.blob[repo.saved[0].Hash+"/"+media.VariantThumb])
}

func TestEncryptedUploadWithoutThumbReusesTheBlob(t *testing.T) {
	blobs, repo := newFakeBlobs(), &fakeAttachments{}
	u := NewUploads(blobs, repo, testUploadConfig())

	w := httptest.NewRecorder()
	u.UploadEncrypted()(w, encryptedUpload(t, []byte("only-full"), nil))
	require.Equal(t, http.StatusCreated, w.Code)
	require.Equal(t, []byte("only-full"), blobs.blob[repo.saved[0].Hash+"/"+media.VariantThumb])
}

func TestEncryptedUploadRejectsOversizedPart(t *testing.T) {
	cfg := testUploadConfig()
	cfg.MaxBytes = 16
	u := NewUploads(newFakeBlobs(), &fakeAttachments{}, cfg)

	w := httptest.NewRecorder()
	u.UploadEncrypted()(w, encryptedUpload(t, bytes.Repeat([]byte("x"), 17), nil))
	require.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
}

func TestEncryptedAttachmentIsServedAsOctetStream(t *testing.T) {
	blobs := newFakeBlobs()
	require.NoError(t, blobs.Put("h", media.VariantFull, []byte("cipher")))
	repo := &fakeAttachments{get: &attachment.Attachment{ID: "a", Hash: "h", Format: "bin", Encrypted: true}}
	u := NewUploads(blobs, repo, testUploadConfig())

	r := httptest.NewRequest(http.MethodGet, "/attachment/a", nil)
	r.SetPathValue("id", "a")
	w := httptest.NewRecorder()
	u.Serve(media.VariantFull)(w, r)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "application/octet-stream", w.Header().Get("Content-Type"))
	require.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
	body, _ := io.ReadAll(w.Body)
	require.Equal(t, "cipher", string(body))
}

func TestQuotaRejectsUploadsPastTheLimit(t *testing.T) {
	cfg := testUploadConfig()
	cfg.MaxUserBytes = 100
	repo := &fakeAttachments{used: 90}
	u := NewUploads(newFakeBlobs(), repo, cfg)

	w := httptest.NewRecorder()
	u.UploadEncrypted()(w, encryptedUpload(t, bytes.Repeat([]byte("x"), 11), nil))
	require.Equal(t, http.StatusInsufficientStorage, w.Code)
	require.Empty(t, repo.saved)

	// The sanitizing path is measured against the same quota.
	w = httptest.NewRecorder()
	u.Upload()(w, multipartUpload(t, "file", pngBytes(t, 64, 32), "user-1", "server-1"))
	require.Equal(t, http.StatusInsufficientStorage, w.Code)
}

func TestQuotaCountsThumbnailsAndAllowsExactFit(t *testing.T) {
	cfg := testUploadConfig()
	cfg.MaxUserBytes = 20
	repo := &fakeAttachments{}
	u := NewUploads(newFakeBlobs(), repo, cfg)

	w := httptest.NewRecorder()
	u.UploadEncrypted()(w, encryptedUpload(t, bytes.Repeat([]byte("a"), 10), bytes.Repeat([]byte("b"), 10)))
	require.Equal(t, http.StatusCreated, w.Code)

	w = httptest.NewRecorder()
	u.UploadEncrypted()(w, encryptedUpload(t, []byte("1"), nil))
	require.Equal(t, http.StatusInsufficientStorage, w.Code)
}

func TestStorageEndpointReportsUsageAndLimit(t *testing.T) {
	cfg := testUploadConfig()
	cfg.MaxUserBytes = 1000
	u := NewUploads(newFakeBlobs(), &fakeAttachments{used: 42}, cfg)

	r := httptest.NewRequest(http.MethodGet, "/me/storage", nil).WithContext(WithUserID(t.Context(), "user-1"))
	w := httptest.NewRecorder()
	u.Storage()(w, r)
	require.Equal(t, http.StatusOK, w.Code)
	var got StorageResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Equal(t, StorageResponse{UsedBytes: 42, LimitBytes: 1000}, got)
}
