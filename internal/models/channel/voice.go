package channel

import (
	"log/slog"
	"sync"

	"github.com/armonic-tech/armonic-backend/internal/models/user"
	"github.com/armonic-tech/armonic-backend/pkg/logger"
	"github.com/pion/webrtc/v3"
)

type VoiceChannel struct {
	ID       string
	ServerID string
	Users    map[string]*user.User
	mu       sync.RWMutex
}

func NewVoiceChannel(id, serverID string) *VoiceChannel {
	return &VoiceChannel{
		ID:       id,
		ServerID: serverID,
		Users:    make(map[string]*user.User),
	}
}

func (vc *VoiceChannel) AddUser(u *user.User) {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	vc.Users[u.ID] = u
}

func (vc *VoiceChannel) Members() []Member {
	vc.mu.RLock()
	defer vc.mu.RUnlock()

	members := make([]Member, 0, len(vc.Users))
	for _, u := range vc.Users {
		members = append(members, Member{ID: u.ID, DisplayName: u.DisplayName})
	}
	return members
}

func (vc *VoiceChannel) RemoveUser(userID string) {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	delete(vc.Users, userID)
}

func (vc *VoiceChannel) KickUser(userID string) {
	vc.mu.Lock()
	u, exists := vc.Users[userID]
	if !exists {
		vc.mu.Unlock()
		return
	}
	delete(vc.Users, userID)
	vc.mu.Unlock()

	u.Signaling.SendJSON(map[string]any{"type": "kicked-voice"})
	u.Media.Close()
}

func (vc *VoiceChannel) ForwardTrack(senderID string, track *webrtc.TrackRemote) {
	vc.mu.RLock()
	defer vc.mu.RUnlock()

	for id, u := range vc.Users {
		if id == senderID {
			continue
		}

		localTrack, err := webrtc.NewTrackLocalStaticRTP(
			track.Codec().RTPCodecCapability,
			track.ID(),
			track.StreamID(),
		)
		if err != nil {
			slog.Error("error creating local track", logger.User(id), "error", err)
			continue
		}

		sender, err := u.Media.AddTrack(localTrack)
		if err != nil {
			slog.Error("error adding track", logger.User(id), "error", err)
			continue
		}

		// Renegociar — el browser necesita saber del nuevo track
		offer, err := u.Media.CreateOffer()
		if err != nil {
			slog.Error("error creating offer", logger.User(id), "error", err)
			continue
		}

		if err := u.Media.SetLocalDescription(offer); err != nil {
			slog.Error("error setting local description", logger.User(id), "error", err)
			continue
		}

		u.Signaling.SendJSON(map[string]interface{}{
			"type": "offer",
			"sdp":  offer,
		})

		go func() {
			buf := make([]byte, 1500)
			for {
				if _, _, err := sender.Read(buf); err != nil {
					return
				}
			}
		}()

		go func() {
			buf := make([]byte, 1500)
			for {
				n, _, err := track.Read(buf) // check
				if err != nil {
					return
				}
				if _, err := localTrack.Write(buf[:n]); err != nil {
					return
				}
			}
		}()
	}
}
