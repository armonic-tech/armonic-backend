package repositories

import (
	"context"

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

func (r *ChannelRepo) GetByServer(ctx context.Context, serverID string) ([]channel.ChannelInfo, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, server_id, name, type FROM channels WHERE server_id = $1`,
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
