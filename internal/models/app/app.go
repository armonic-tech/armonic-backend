package app

import (
	"sync"

	"github.com/armonic-tech/armonic-backend/internal/models/channel"
	"github.com/armonic-tech/armonic-backend/internal/models/server"
)

type App struct {
	Servers map[string]*server.Server
	mu      sync.RWMutex
}

func NewApp() *App {
	return &App{
		Servers: make(map[string]*server.Server),
	}
}

func (a *App) GetOrCreateServer(id string) *server.Server {
	a.mu.Lock()
	defer a.mu.Unlock()

	if s, exists := a.Servers[id]; exists {
		return s
	}

	s := server.NewServer(id)
	a.Servers[id] = s
	return s
}

func (a *App) getServer(id string) *server.Server {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.Servers[id]
}

func (a *App) VoiceMembers(serverID, channelID string) []channel.Member {
	srv := a.getServer(serverID)
	if srv == nil {
		return nil
	}
	vc := srv.GetVoiceChannel(channelID)
	if vc == nil {
		return nil
	}
	return vc.Members()
}

func (a *App) RemoveConnectedUser(userID string) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, s := range a.Servers {
		s.RemoveConnectedUser(userID)
	}
}
