package message

import "time"

type Message struct {
	ID           string    `json:"id"`
	ChannelID    string    `json:"channelId"`
	ServerID     string    `json:"serverId"`
	UserID       string    `json:"userId"`
	Content      string    `json:"content"`
	AttachmentID string    `json:"attachmentId,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
}
