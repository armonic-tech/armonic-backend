package repositories

import (
	"context"
	"database/sql"
	"time"

	channel "github.com/armonic-tech/armonic-backend/internal/models/channel"
)

type ChannelRepo struct {
	db DBTX
}

func NewChannelRepo(db DBTX) *ChannelRepo {
	return &ChannelRepo{db: db}
}

func (r *ChannelRepo) Create(ctx context.Context, id, serverID, name, chType string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO channels (id, server_id, name, type) VALUES ($1, $2, $3, $4)`,
		id, serverID, name, chType,
	)
	return err
}

func (r *ChannelRepo) NameTaken(ctx context.Context, serverID, chType, name string) (bool, error) {
	var taken bool
	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM channels
			WHERE server_id = $1 AND type = $2 AND lower(name) = lower($3)
			  AND deleted_at IS NULL
		)`,
		serverID, chType, name,
	).Scan(&taken)
	return taken, err
}

func (r *ChannelRepo) GetByID(ctx context.Context, id string) (*channel.ChannelInfo, error) {
	var ch channel.ChannelInfo
	err := r.db.QueryRowContext(ctx,
		`SELECT id, server_id, name, type FROM channels WHERE id = $1 AND deleted_at IS NULL`,
		id,
	).Scan(&ch.ID, &ch.ServerID, &ch.Name, &ch.Type)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ch, nil
}

func (r *ChannelRepo) GetChannelByServer(ctx context.Context, serverID string) ([]channel.ChannelInfo, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, server_id, name, type FROM channels WHERE server_id = $1 AND deleted_at IS NULL`,
		serverID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var channels []channel.ChannelInfo
	for rows.Next() {
		var ch channel.ChannelInfo
		if err := rows.Scan(&ch.ID, &ch.ServerID, &ch.Name, &ch.Type); err != nil {
			return nil, err
		}
		channels = append(channels, ch)
	}
	return channels, rows.Err()
}

func (r *ChannelRepo) SoftDelete(ctx context.Context, serverID, channelID string) (bool, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE channels SET deleted_at = $1 WHERE id = $2 AND server_id = $3 AND deleted_at IS NULL`,
		time.Now().Unix(), channelID, serverID,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
