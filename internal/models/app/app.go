package app

import (
	"sync"

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

func (a *App) RemoveConnectedUser(userID string) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, s := range a.Servers {
		s.RemoveConnectedUser(userID)
	}
}
