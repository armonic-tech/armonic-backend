package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/armonic-tech/armonic-backend/internal/media"
	"github.com/stretchr/testify/require"
)

type fakeLogos struct {
	id  string
	err error
}

func (f *fakeLogos) SetLogo(_ context.Context, _, attachmentID string) error {
	f.id = attachmentID
	return f.err
}

func (f *fakeLogos) ClearLogo(context.Context, string) error {
	f.id = ""
	return f.err
}

func (f *fakeLogos) LogoID(context.Context, string) (string, error) { return f.id, f.err }

func TestServerLogoPointsTheServerAtTheUploadAndNudgesMembers(t *testing.T) {
	blobs, repo, logos := newFakeBlobs(), &fakeAttachments{}, &fakeLogos{}
	presence := &fakeMLSPresence{}
	u := NewUploads(blobs, repo, testUploadConfig())

	w := httptest.NewRecorder()
	u.ServerLogo(logos, presence)(w, multipartUpload(t, "file", pngBytes(t, 40, 40), "owner-1", "server-1"))
	require.Equal(t, http.StatusOK, w.Code)

	var got ServerLogoResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Equal(t, got.AttachmentID, logos.id)
	// The ?v= is what stops a cache from serving the previous logo off the
	// unchanged path, and the thumb URL is here so no client has to build one.
	require.Equal(t, "/server/server-1/logo?v="+got.LogoID, got.LogoURL)
	require.Equal(t, "/server/server-1/logo/thumb?v="+got.LogoID, got.LogoThumbURL)

	require.Len(t, presence.broadcast, 1)
	require.Equal(t, "server-updated", presence.broadcast[0]["type"])
	require.Equal(t, got.LogoID, presence.broadcast[0]["logoId"])
	require.Equal(t, got.LogoThumbURL, presence.broadcast[0]["logoThumbUrl"])
}

func TestDeleteServerLogoLeavesTheServerWithoutOneAndSaysSo(t *testing.T) {
	logos, presence := &fakeLogos{id: "att-old"}, &fakeMLSPresence{}
	u := NewUploads(newFakeBlobs(), &fakeAttachments{}, testUploadConfig())

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/server/server-1/logo", nil)
	r.SetPathValue("id", "server-1")
	u.DeleteServerLogo(logos, presence)(w, r)

	require.Equal(t, http.StatusNoContent, w.Code)
	require.Empty(t, logos.id)

	// No logoId at all, not an empty one: that is what tells a client already
	// painting a logo to drop it.
	require.Len(t, presence.broadcast, 1)
	require.Equal(t, "server-updated", presence.broadcast[0]["type"])
	require.NotContains(t, presence.broadcast[0], "logoId")

	// And the bytes stop being served.
	w = httptest.NewRecorder()
	u.ServeLogo(logos, media.VariantFull)(w, r)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeLogoIsNotFoundUntilOneIsSet(t *testing.T) {
	u := NewUploads(newFakeBlobs(), &fakeAttachments{}, testUploadConfig())

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/server/server-1/logo", nil)
	r.SetPathValue("id", "server-1")
	u.ServeLogo(&fakeLogos{}, media.VariantFull)(w, r)

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeLogoReturnsTheBytesWithoutClaimingTheyNeverChange(t *testing.T) {
	blobs, repo, logos := newFakeBlobs(), &fakeAttachments{}, &fakeLogos{}
	presence := &fakeMLSPresence{}
	u := NewUploads(blobs, repo, testUploadConfig())
	u.ServerLogo(logos, presence)(httptest.NewRecorder(),
		multipartUpload(t, "file", pngBytes(t, 40, 40), "owner-1", "server-1"))
	repo.get = &repo.saved[0]

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/server/server-1/logo", nil)
	r.SetPathValue("id", "server-1")
	u.ServeLogo(logos, media.VariantFull)(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "image/png", w.Header().Get("Content-Type"))
	require.NotEmpty(t, w.Body.Bytes())
	require.NotContains(t, w.Header().Get("Cache-Control"), "immutable")
}
