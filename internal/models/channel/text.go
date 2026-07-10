package channel

import (
	"sync"

	"github.com/armonic-tech/armonic-backend/internal/models/message"
	"github.com/armonic-tech/armonic-backend/internal/models/user"
)

type TextChannel struct {
	ID       string
	ServerID string
	Users    map[string]*user.User
	Messages []*message.Message
	mu       sync.RWMutex
}

func NewTextChannel(id, serverID string) *TextChannel {
	return &TextChannel{
		ID:       id,
		ServerID: serverID,
		Users:    make(map[string]*user.User),
		Messages: make([]*message.Message, 0),
	}
}

func (tc *TextChannel) AddUser(u *user.User) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.Users[u.ID] = u
}

func (tc *TextChannel) RemoveUser(userID string) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	delete(tc.Users, userID)
}

func (tc *TextChannel) Broadcast(senderID string, msg []byte) {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	for id, u := range tc.Users {
		if id == senderID {
			continue
		}
		u.Signaling.Send(msg)
	}
}

func (tc *TextChannel) AddMessage(msg *message.Message) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.Messages = append(tc.Messages, msg)
}
