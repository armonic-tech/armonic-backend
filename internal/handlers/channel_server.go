package handlers

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/armonic-tech/armonic-backend/internal/models/channel"
	"github.com/google/uuid"
)

type ChannelGetter interface {
	GetChannelByServer(ctx context.Context, serverID string) ([]channel.ChannelInfo, error)
}

type ChannelByIDGetter interface {
	GetByID(ctx context.Context, id string) (*channel.ChannelInfo, error)
}

type VoiceMemberLister interface {
	VoiceMembers(serverID, channelID string) []channel.Member
}

type ChannelDetailResponse struct {
	channel.ChannelInfo
	Connected      []channel.Member `json:"connected"`
	ConnectedCount int              `json:"connectedCount"`
}

// @Summary      Server id info
// @Description  Get channels from server with server-id
// @Tags         Server
// @Accept       json
// @Produce      json
// @Param id path string true "Server ID"
// @Success 200 {array} channel.ChannelInfo
// @Security BearerAuth
// @Router       /server/{id} [get]
func GetByServer(channels ChannelGetter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		channelID := r.PathValue("id")

		if _, err := uuid.Parse(channelID); err != nil {
			http.Error(w, "invalid channel id", http.StatusBadRequest)
			return
		}

		// list, err := channels.GetByServer(ctx, channelID)
		list, err := channels.GetChannelByServer(ctx, channelID)
		if err != nil {
			http.Error(w, "Error finding channel", http.StatusInternalServerError)
			return
		}
		if len(list) == 0 {
			http.Error(w, "channel not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(list)
	}
}

// @Summary      Channel info by id
// @Description  Get a single channel by its id, enriched with live voice state (who is connected right now) when it's a voice channel
// @Tags         Channel
// @Accept       json
// @Produce      json
// @Param id path string true "Channel ID"
// @Success 200 {object} ChannelDetailResponse
// @Failure 400 {string} string "invalid channel id"
// @Failure 404 {string} string "channel not found"
// @Security BearerAuth
// @Router       /channel/{id} [get]
func GetChannelByID(channels ChannelByIDGetter, voice VoiceMemberLister) http.HandlerFunc {
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

		resp := ChannelDetailResponse{ChannelInfo: *ch, Connected: []channel.Member{}}
		// Live presence only makes sense for voice channels.
		if ch.Type == "voice" {
			if members := voice.VoiceMembers(ch.ServerID, ch.ID); members != nil {
				resp.Connected = members
			}
			resp.ConnectedCount = len(resp.Connected)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}
