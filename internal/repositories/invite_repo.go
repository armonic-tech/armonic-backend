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
	return r.create(ctx, serverID, createdBy, expiresIn, false, false)
}

// CreateBot mints an invite whose signup creates a bot account.
func (r *InviteRepo) CreateBot(ctx context.Context, serverID, createdBy string, expiresIn time.Duration) (string, error) {
	return r.create(ctx, serverID, createdBy, expiresIn, true, false)
}

// CreateMultiUse mints an invite any number of people can redeem until it
// expires: MarkUsed leaves it alone.
func (r *InviteRepo) CreateMultiUse(ctx context.Context, serverID, createdBy string, expiresIn time.Duration) (string, error) {
	return r.create(ctx, serverID, createdBy, expiresIn, false, true)
}

func (r *InviteRepo) create(ctx context.Context, serverID, createdBy string, expiresIn time.Duration, bot, multiUse bool) (string, error) {
	token := uuid.New().String()
	expiresAt := time.Now().Add(expiresIn).Unix()
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO invites (token, server_id, created_by, expires_at, bot, multi_use) VALUES ($1, $2, $3, $4, $5, $6)`,
		token, serverID, createdBy, expiresAt, bot, multiUse,
	)
	return token, err
}

func (r *InviteRepo) Get(ctx context.Context, token string) (*invite.Invite, error) {
	var inv invite.Invite
	var expiresAt int64
	err := r.db.QueryRowContext(ctx,
		`SELECT token, server_id, created_by, expires_at, bot, multi_use FROM invites WHERE token = $1 AND used_at IS NULL`,
		token,
	).Scan(&inv.Token, &inv.ServerID, &inv.CreatedBy, &expiresAt, &inv.Bot, &inv.MultiUse)
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
		`UPDATE invites SET used_at = $2 WHERE token = $1 AND NOT multi_use`,
		token, time.Now().Unix(),
	)
	return err
}
