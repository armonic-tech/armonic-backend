package repositories

import (
	"context"
	"time"

	"github.com/armonic-tech/armonic-backend/internal/models/message"
)

type MessageRepo struct {
	db DBTX
}

func NewMessageRepo(db DBTX) *MessageRepo {
	return &MessageRepo{db: db}
}

func (r *MessageRepo) Save(ctx context.Context, msg message.Message) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO messages (id, server_id, channel_id, user_id, content, created_at) VALUES ($1, $2, $3, $4, $5, $6)`,
		msg.ID, msg.ServerID, msg.ChannelID, msg.UserID, msg.Content, msg.CreatedAt.Unix(),
	)
	return err
}

func (r *MessageRepo) GetByChannel(ctx context.Context, serverID, channelID string, limit int) ([]message.Message, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, user_id, content, created_at FROM messages WHERE server_id = $1 AND channel_id = $2 ORDER BY created_at DESC LIMIT $3`,
		serverID, channelID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []message.Message
	for rows.Next() {
		var m message.Message
		var createdAt int64
		if err := rows.Scan(&m.ID, &m.UserID, &m.Content, &createdAt); err != nil {
			return nil, err
		}
		m.ServerID = serverID
		m.ChannelID = channelID
		m.CreatedAt = time.Unix(createdAt, 0).UTC()
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}
