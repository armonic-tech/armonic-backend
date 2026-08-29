package repositories

import (
	"context"
	"database/sql"
)

type UserRepo struct {
	db DBTX
}

func NewUserRepo(db DBTX) *UserRepo {
	return &UserRepo{db: db}
}

func (r *UserRepo) Upsert(ctx context.Context, id, displayName string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO users (id, display_name) VALUES ($1, $2)
		 ON CONFLICT(id) DO UPDATE SET display_name = excluded.display_name`,
		id, displayName,
	)
	return err
}

func (r *UserRepo) GetName(ctx context.Context, id string) (string, error) {
	var name string
	err := r.db.QueryRowContext(ctx, `SELECT display_name FROM users WHERE id = $1`, id).Scan(&name)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return name, err
}

func (r *UserRepo) GetProfile(ctx context.Context, id string) (displayName, avatarID string, err error) {
	var avatar sql.NullString
	err = r.db.QueryRowContext(ctx,
		`SELECT display_name, avatar_id FROM users WHERE id = $1`, id,
	).Scan(&displayName, &avatar)
	if err == sql.ErrNoRows {
		return "", "", nil
	}
	return displayName, avatar.String, err
}

func (r *UserRepo) SetAvatar(ctx context.Context, id, attachmentID string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE users SET avatar_id = $1 WHERE id = $2`, attachmentID, id)
	return err
}

func (r *UserRepo) Create(ctx context.Context, id, username, passwordHash string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO users (id, display_name, username, password_hash) VALUES ($1, $2, $3, $4)`,
		id, username, username, passwordHash,
	)
	return err
}

func (r *UserRepo) GetByUsername(ctx context.Context, username string) (id, passwordHash string, err error) {
	err = r.db.QueryRowContext(ctx,
		`SELECT id, password_hash FROM users WHERE username = $1`, username,
	).Scan(&id, &passwordHash)
	return id, passwordHash, err
}
