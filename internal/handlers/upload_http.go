package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/armonic-tech/armonic-backend/config"
	"github.com/armonic-tech/armonic-backend/internal/media"
	"github.com/armonic-tech/armonic-backend/internal/models/attachment"
	"github.com/armonic-tech/armonic-backend/pkg/logger"
	"github.com/armonic-tech/armonic-backend/pkg/ratelimit"
	"github.com/google/uuid"
)

const (
	uploadFormField = "file"
	multipartSlack  = 1 << 20
	blobCacheTTL    = "private, max-age=31536000, immutable"
)

type BlobStore interface {
	Put(hash, variant string, data []byte) error
	Open(hash, variant string) (io.ReadSeekCloser, time.Time, error)
}

type AttachmentRepository interface {
	Create(ctx context.Context, a attachment.Attachment) error
	GetByID(ctx context.Context, id string) (*attachment.Attachment, error)
}

type AvatarStore interface {
	SetAvatar(ctx context.Context, id, attachmentID string) error
}

type Uploads struct {
	store    BlobStore
	repo     AttachmentRepository
	limits   media.Limits
	maxBytes int64
	rate     *ratelimit.Limiter
	sem      chan struct{}
}

func NewUploads(store BlobStore, repo AttachmentRepository, cfg config.UploadConfig) *Uploads {
	return &Uploads{
		store: store,
		repo:  repo,
		limits: media.Limits{
			MaxBytes:  cfg.MaxBytes,
			MaxWidth:  cfg.MaxImageWidth,
			MaxHeight: cfg.MaxImageHeight,
			MaxPixels: cfg.MaxImagePixels,
			ThumbSize: cfg.ThumbnailSize,
		},
		maxBytes: cfg.MaxBytes,
		rate:     ratelimit.New(cfg.RatePerMin, cfg.Burst),
		sem:      make(chan struct{}, max(1, cfg.MaxConcurrent)),
	}
}

// @Summary      Upload an image
// @Description  Member-only. Multipart upload of one image (field "file"). The file is validated by magic number, decoded, stripped of all metadata by re-encoding its pixels, thumbnailed and stored. The uploaded bytes are discarded.
// @Tags         Attachment
// @Accept       multipart/form-data
// @Produce      json
// @Param        id path string true "Server ID"
// @Param        file formData file true "Image file"
// @Success      201 {object} attachment.Attachment
// @Failure      400 {string} string "image could not be decoded"
// @Failure      413 {string} string "file exceeds the maximum upload size"
// @Failure      415 {string} string "unsupported image format"
// @Failure      422 {string} string "image dimensions exceed the configured limit"
// @Failure      429 {string} string "rate limit exceeded"
// @Security     BearerAuth
// @Router       /server/{id}/upload [post]
func (u *Uploads) Upload() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := UserID(r.Context())
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		att, ok := u.ingest(w, r, r.PathValue("id"), userID)
		if !ok {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(att)
	}
}

// @Summary      Set own avatar
// @Description  Uploads an image through the same pipeline as attachments and points the caller's avatar at it.
// @Tags         Users
// @Accept       multipart/form-data
// @Produce      json
// @Param        file formData file true "Image file"
// @Success      200 {object} attachment.Attachment
// @Failure      413 {string} string "file exceeds the maximum upload size"
// @Failure      415 {string} string "unsupported image format"
// @Failure      429 {string} string "rate limit exceeded"
// @Security     BearerAuth
// @Router       /me/avatar [post]
func (u *Uploads) Avatar(users AvatarStore, serverID string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := UserID(r.Context())
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		att, ok := u.ingest(w, r, serverID, userID)
		if !ok {
			return
		}
		if err := users.SetAvatar(r.Context(), userID, att.ID); err != nil {
			slog.ErrorContext(r.Context(), "avatar: error saving", logger.User(userID), "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(att)
	}
}

// @Summary      Fetch an attachment
// @Description  Member-only. Serves the sanitized image bytes.
// @Tags         Attachment
// @Param        id path string true "Attachment ID"
// @Success      200 {file} binary
// @Failure      404 {string} string "attachment not found"
// @Security     BearerAuth
// @Router       /attachment/{id} [get]
func (u *Uploads) Serve(variant string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		att, err := u.repo.GetByID(ctx, r.PathValue("id"))
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if att == nil {
			http.Error(w, "attachment not found", http.StatusNotFound)
			return
		}

		format := att.Format
		if variant == media.VariantThumb {
			format = att.ThumbFormat
		}
		body, modTime, err := u.store.Open(att.Hash, variant)
		if errors.Is(err, media.ErrNotFound) {
			slog.ErrorContext(ctx, "attachment row without a blob", "attachmentId", att.ID, "hash", att.Hash)
			http.Error(w, "attachment not found", http.StatusNotFound)
			return
		}
		if err != nil {
			slog.ErrorContext(ctx, "attachment: error opening blob", "attachmentId", att.ID, "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		defer body.Close()

		w.Header().Set("Content-Type", media.Format(format).MIME())
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Disposition", "inline")
		w.Header().Set("Cache-Control", blobCacheTTL)
		http.ServeContent(w, r, "", modTime, body)
	}
}

func (u *Uploads) ingest(w http.ResponseWriter, r *http.Request, serverID, userID string) (*attachment.Attachment, bool) {
	ctx := r.Context()

	if !u.rate.Allow(userID) {
		ratelimit.Reject(w, u.rate)
		return nil, false
	}
	if serverID == "" {
		http.Error(w, "invalid server id", http.StatusBadRequest)
		return nil, false
	}

	r.Body = http.MaxBytesReader(w, r.Body, u.maxBytes+multipartSlack)
	part, err := imagePart(r)
	if err != nil {
		writeUploadError(w, err)
		return nil, false
	}
	defer part.Close()

	release, err := u.acquire(ctx)
	if err != nil {
		http.Error(w, "server busy", http.StatusServiceUnavailable)
		return nil, false
	}
	img, err := media.Process(part, u.limits)
	release()
	if err != nil {
		writeUploadError(w, err)
		return nil, false
	}

	hash := media.Hash(img.Data)
	if err := u.store.Put(hash, media.VariantFull, img.Data); err != nil {
		slog.ErrorContext(ctx, "upload: error storing blob", logger.User(userID), "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return nil, false
	}
	if err := u.store.Put(hash, media.VariantThumb, img.Thumb); err != nil {
		slog.ErrorContext(ctx, "upload: error storing thumbnail", logger.User(userID), "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return nil, false
	}

	att := attachment.Attachment{
		ID:          uuid.New().String(),
		ServerID:    serverID,
		UserID:      userID,
		Hash:        hash,
		Format:      string(img.Format),
		ThumbFormat: string(img.ThumbFormat),
		MIME:        img.Format.MIME(),
		Size:        int64(len(img.Data)),
		Width:       img.Width,
		Height:      img.Height,
		CreatedAt:   time.Now().UTC(),
	}
	if err := u.repo.Create(ctx, att); err != nil {
		slog.ErrorContext(ctx, "upload: error saving attachment", logger.User(userID), logger.Server(serverID), "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return nil, false
	}

	slog.InfoContext(ctx, "attachment uploaded", logger.User(userID), logger.Server(serverID),
		"attachmentId", att.ID, "format", att.Format, "size", att.Size, "width", att.Width, "height", att.Height)
	return att.WithURLs(), true
}

func (u *Uploads) acquire(ctx context.Context) (func(), error) {
	select {
	case u.sem <- struct{}{}:
		return func() { <-u.sem }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func imagePart(r *http.Request) (*multipart.Part, error) {
	mr, err := r.MultipartReader()
	if err != nil {
		return nil, err
	}
	for {
		part, err := mr.NextPart()
		if err != nil {
			return nil, err
		}
		if part.FormName() == uploadFormField {
			return part, nil
		}
		part.Close()
	}
}

func writeUploadError(w http.ResponseWriter, err error) {
	var maxBytes *http.MaxBytesError
	switch {
	case errors.As(err, &maxBytes), errors.Is(err, media.ErrTooLarge):
		http.Error(w, media.ErrTooLarge.Error(), http.StatusRequestEntityTooLarge)
	case errors.Is(err, media.ErrUnsupported):
		http.Error(w, err.Error(), http.StatusUnsupportedMediaType)
	case errors.Is(err, media.ErrDimensions):
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
	case errors.Is(err, media.ErrCorrupt), errors.Is(err, media.ErrPolyglot):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		http.Error(w, "invalid upload", http.StatusBadRequest)
	}
}
