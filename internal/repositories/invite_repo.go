package repositories

import (
	"context"
	"database/sql"
	"time"

	"github.com/armonic-tech/armonic-backend/internal/models/invite"
	"github.com/google/uuid"
)

type InviteRepo struct {
	db DBTX
}

func NewInviteRepo(db DBTX) *InviteRepo {
	return &InviteRepo{db: db}
}

func (r *InviteRepo) Create(ctx context.Context, serverID, createdBy string, expiresIn time.Duration) (string, error) {
	token := uuid.New().String()
	expiresAt := time.Now().Add(expiresIn).Unix()
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO invites (token, server_id, created_by, expires_at) VALUES ($1, $2, $3, $4)`,
		token, serverID, createdBy, expiresAt,
	)
	return token, err
}

func (r *InviteRepo) Get(ctx context.Context, token string) (*invite.Invite, error) {
	var inv invite.Invite
	var expiresAt int64
	err := r.db.QueryRowContext(ctx,
		`SELECT token, server_id, created_by, expires_at FROM invites WHERE token = $1 AND used_at IS NULL`,
		token,
	).Scan(&inv.Token, &inv.ServerID, &inv.CreatedBy, &expiresAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	inv.ExpiresAt = time.Unix(expiresAt, 0).UTC()
	if inv.ExpiresAt.Before(time.Now()) {
		return nil, nil
	}
	return &inv, nil
}

func (r *InviteRepo) MarkUsed(ctx context.Context, token string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE invites SET used_at = $2 WHERE token = $1`,
		token, time.Now().Unix(),
	)
	return err
}
