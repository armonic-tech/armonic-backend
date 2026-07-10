package claim

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// TicketTTL is how long a ticket issued by VerifyPassword stays valid.
const TicketTTL = 10 * time.Minute

type Manager struct {
	mu       sync.RWMutex
	password string
	ticket   string
	expires  time.Time
}

func New(password string) *Manager {
	return &Manager{password: password}
}

// VerifyPassword checks pw against the configured claim password.
func (m *Manager) VerifyPassword(pw string) (ticket string, expiresAt time.Time, ok bool) {
	if pw == "" || pw != m.password {
		return "", time.Time{}, false
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.ticket = uuid.New().String()
	m.expires = time.Now().Add(TicketTTL)
	return m.ticket, m.expires, true
}

func (m *Manager) ValidateTicket(ticket string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return ticket != "" && ticket == m.ticket && time.Now().Before(m.expires)
}

// InvalidateTicket clears the current ticket so it can't be replayed
func (m *Manager) InvalidateTicket() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ticket = ""
}
