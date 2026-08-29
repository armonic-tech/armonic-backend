package repositories

import (
	"context"
	"database/sql"
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
		`INSERT INTO messages (id, server_id, channel_id, user_id, content, created_at, attachment_id) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		msg.ID, msg.ServerID, msg.ChannelID, msg.UserID, msg.Content, msg.CreatedAt.Unix(), nullable(msg.AttachmentID),
	)
	return err
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (r *MessageRepo) GetByChannel(ctx context.Context, serverID, channelID string, limit int) ([]message.Message, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, user_id, content, created_at, attachment_id FROM messages WHERE server_id = $1 AND channel_id = $2 AND deleted_at IS NULL ORDER BY created_at DESC LIMIT $3`,
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
		var attachmentID sql.NullString
		if err := rows.Scan(&m.ID, &m.UserID, &m.Content, &createdAt, &attachmentID); err != nil {
			return nil, err
		}
		m.ServerID = serverID
		m.ChannelID = channelID
		m.AttachmentID = attachmentID.String
		m.CreatedAt = time.Unix(createdAt, 0).UTC()
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

func (r *MessageRepo) SoftDelete(ctx context.Context, serverID, channelID, messageID, deletedBy string) (bool, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE messages SET deleted_at = $1, deleted_by = $2 WHERE id = $3 AND server_id = $4 AND channel_id = $5 AND deleted_at IS NULL`,
		time.Now().Unix(), deletedBy, messageID, serverID, channelID,
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
