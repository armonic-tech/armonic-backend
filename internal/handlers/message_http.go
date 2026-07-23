package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/armonic-tech/armonic-backend/internal/models/message"
	"github.com/google/uuid"
)

type MessageGetter interface {
	GetByChannel(ctx context.Context, serverID, channelID string, limit int) ([]message.Message, error)
}

const (
	defaultMessageLimit = 50
	maxMessageLimit     = 100
)

// @Summary      Channel messages
// @Description  Get a page of messages for a channel (newest first)
// @Tags         Channel
// @Produce      json
// @Param        id path string true "Channel ID"
// @Param        limit query int false "Max messages to return (default 50, max 100)"
// @Success      200 {array} message.Message
// @Failure      400 {string} string "invalid channel id"
// @Failure      404 {string} string "channel not found"
// @Security     BearerAuth
// @Router       /channel/{id}/messages [get]
func GetChannelMessages(channels ChannelByIDGetter, messages MessageGetter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		channelID := r.PathValue("id")

		if _, err := uuid.Parse(channelID); err != nil {
			http.Error(w, "invalid channel id", http.StatusBadRequest)
			return
		}

		ch, err := channels.GetByID(ctx, channelID)
		if err != nil {
			http.Error(w, "error finding channel", http.StatusInternalServerError)
			return
		}
		if ch == nil {
			http.Error(w, "channel not found", http.StatusNotFound)
			return
		}

		limit := defaultMessageLimit
		if q := r.URL.Query().Get("limit"); q != "" {
			if n, err := strconv.Atoi(q); err == nil && n > 0 {
				limit = min(n, maxMessageLimit)
			}
		}

		msgs, err := messages.GetByChannel(ctx, ch.ServerID, channelID, limit)
		if err != nil {
			http.Error(w, "error loading messages", http.StatusInternalServerError)
			return
		}
		if msgs == nil {
			msgs = []message.Message{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(msgs)
	}
}
