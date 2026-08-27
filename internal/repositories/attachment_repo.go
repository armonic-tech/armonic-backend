package repositories

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/armonic-tech/armonic-backend/internal/models/attachment"
)

type AttachmentRepo struct {
	db DBTX
}

func NewAttachmentRepo(db DBTX) *AttachmentRepo {
	return &AttachmentRepo{db: db}
}

func (r *AttachmentRepo) Create(ctx context.Context, a attachment.Attachment) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO attachments (id, hash, server_id, user_id, format, thumb_format, mime, size, width, height, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		a.ID, a.Hash, a.ServerID, a.UserID, a.Format, a.ThumbFormat, a.MIME, a.Size, a.Width, a.Height, a.CreatedAt.Unix(),
	)
	return err
}

func (r *AttachmentRepo) GetByID(ctx context.Context, id string) (*attachment.Attachment, error) {
	var a attachment.Attachment
	var createdAt int64
	err := r.db.QueryRowContext(ctx,
		`SELECT id, hash, server_id, user_id, format, thumb_format, mime, size, width, height, created_at
		 FROM attachments WHERE id = $1`, id,
	).Scan(&a.ID, &a.Hash, &a.ServerID, &a.UserID, &a.Format, &a.ThumbFormat, &a.MIME, &a.Size, &a.Width, &a.Height, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.CreatedAt = time.Unix(createdAt, 0).UTC()
	return a.WithURLs(), nil
}
