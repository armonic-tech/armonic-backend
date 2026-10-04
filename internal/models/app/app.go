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

func (a *App) ConnectedUserIDs(serverID string) []string {
	srv := a.getServer(serverID)
	if srv == nil {
		return nil
	}
	return srv.ConnectedUserIDs()
}

// Stats counts live state for the runtime heartbeat. Each layer is locked on
// its own, so the numbers are a near-snapshot, not an atomic one.
func (a *App) Stats() (servers, connected, voice int) {
	a.mu.RLock()
	list := make([]*server.Server, 0, len(a.Servers))
	for _, s := range a.Servers {
		list = append(list, s)
	}
	a.mu.RUnlock()

	for _, s := range list {
		c, v := s.Counts()
		connected += c
		voice += v
	}
	return len(list), connected, voice
}

func (a *App) RemoveConnectedUser(userID string) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, s := range a.Servers {
		s.RemoveConnectedUser(userID)
	}
}

// BroadcastToServer pushes a frame to every live socket attached to a
// server; a no-op for a server nobody is connected to.
func (a *App) BroadcastToServer(serverID string, msg any) {
	srv := a.getServer(serverID)
	if srv == nil {
		return
	}
	srv.BroadcastMessage("", msg)
}
