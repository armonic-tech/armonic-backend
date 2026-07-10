package server

import (
	"sync"

	"github.com/armonic-tech/armonic-backend/internal/models/channel"
	user "github.com/armonic-tech/armonic-backend/internal/models/user"
)

type Server struct {
	ID             string
	OwnerID        string
	TextChannels   map[string]*channel.TextChannel
	VoiceChannels  map[string]*channel.VoiceChannel
	ConnectedUsers map[string]*user.User
	mu             sync.RWMutex
}

func NewServer(id string) *Server {
	return &Server{
		ID:             id,
		OwnerID:        "",
		TextChannels:   make(map[string]*channel.TextChannel),
		VoiceChannels:  make(map[string]*channel.VoiceChannel),
		ConnectedUsers: make(map[string]*user.User),
	}
}

func (s *Server) AddConnectedUser(u *user.User) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ConnectedUsers[u.ID] = u
}

func (s *Server) RemoveConnectedUser(userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.ConnectedUsers, userID)
}

func (s *Server) BroadcastMessage(senderID string, msg any) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for id, u := range s.ConnectedUsers {
		if id == senderID {
			continue
		}
		u.Signaling.SendJSON(msg)
	}
}

func (s *Server) GetOrCreateVoiceChannel(id string) *channel.VoiceChannel {
	s.mu.Lock()
	defer s.mu.Unlock()

	if vc, exists := s.VoiceChannels[id]; exists {
		return vc
	}

	vc := channel.NewVoiceChannel(id, s.ID)
	s.VoiceChannels[id] = vc
	return vc
}

func (s *Server) GetOrCreateTextChannel(id string) *channel.TextChannel {
	s.mu.Lock()
	defer s.mu.Unlock()

	if tc, exists := s.TextChannels[id]; exists {
		return tc
	}

	tc := channel.NewTextChannel(id, s.ID)
	s.TextChannels[id] = tc
	return tc
}

func (s *Server) AddTextChannel(id string) *channel.TextChannel {
	s.mu.Lock()
	defer s.mu.Unlock()

	ch := channel.NewTextChannel(id, s.ID)
	s.TextChannels[id] = ch
	return ch
}

func (s *Server) KickUserFromAllVoice(userID string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, vc := range s.VoiceChannels {
		vc.KickUser(userID)
	}
}

func (s *Server) AddVoiceChannel(id string) *channel.VoiceChannel {
	s.mu.Lock()
	defer s.mu.Unlock()

	ch := channel.NewVoiceChannel(id, s.ID)
	s.VoiceChannels[id] = ch
	return ch
}
