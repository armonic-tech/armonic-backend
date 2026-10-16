package server

import (
	"log/slog"
	"sync"

	"github.com/armonic-tech/armonic-backend/internal/models/channel"
	user "github.com/armonic-tech/armonic-backend/internal/models/user"
	"github.com/armonic-tech/armonic-backend/pkg/logger"
)

type Server struct {
	ID            string
	OwnerID       string
	TextChannels  map[string]*channel.TextChannel
	VoiceChannels map[string]*channel.VoiceChannel
	// userID → every live WebSocket of that user. Keyed by socket, not by
	// *user.User: join-voice swaps the session's User for a new one, while the
	// socket stays the same for the whole connection.
	ConnectedUsers map[string]map[user.Socket]struct{}
	voice          map[string]VoiceSession
	// botID → the voice channel the owner summoned it to. A bot account may
	// join voice only there (docs/ai/bots.md).
	summons map[string]string
	mu      sync.RWMutex
	joinMu  sync.Mutex
}

// VoiceSession is the one call a user is allowed to be in. Channel and Media
// are copied at claim time so evicting it never reads fields the owning
// connection may be writing.
type VoiceSession struct {
	User      *user.User
	ChannelID string
	Media     user.RTCConn
}

func NewServer(id string) *Server {
	return &Server{
		ID:             id,
		OwnerID:        "",
		TextChannels:   make(map[string]*channel.TextChannel),
		VoiceChannels:  make(map[string]*channel.VoiceChannel),
		ConnectedUsers: make(map[string]map[user.Socket]struct{}),
		voice:          make(map[string]VoiceSession),
		summons:        make(map[string]string),
	}
}

func (s *Server) AddConnectedUser(u *user.User) {
	s.mu.Lock()
	defer s.mu.Unlock()
	conns := s.ConnectedUsers[u.ID]
	if conns == nil {
		conns = make(map[user.Socket]struct{})
		s.ConnectedUsers[u.ID] = conns
	}
	conns[u.Signaling] = struct{}{}
}

// RemoveConnection drops one socket; the user stays online while any other
// remains. Reports how many of the user's connections are left.
func (s *Server) RemoveConnection(userID string, sock user.Socket) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	conns := s.ConnectedUsers[userID]
	delete(conns, sock)
	if len(conns) == 0 {
		delete(s.ConnectedUsers, userID)
	}
	return len(conns)
}

// RemoveUser drops every connection of the user, e.g. on kick-server, so
// they stop receiving the server's events at once.
func (s *Server) RemoveUser(userID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.ConnectedUsers[userID])
	delete(s.ConnectedUsers, userID)
	return n
}

func (s *Server) ConnectedUserIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids := make([]string, 0, len(s.ConnectedUsers))
	for id := range s.ConnectedUsers {
		ids = append(ids, id)
	}
	return ids
}

func (s *Server) Counts() (connected, voice int) {
	s.mu.RLock()
	connected = len(s.ConnectedUsers)
	vcs := make([]*channel.VoiceChannel, 0, len(s.VoiceChannels))
	for _, vc := range s.VoiceChannels {
		vcs = append(vcs, vc)
	}
	s.mu.RUnlock()

	for _, vc := range vcs {
		voice += len(vc.Members())
	}
	return connected, voice
}

// BroadcastMessage sends msg to every connection except `except`, the one
// the event came from (nil = everyone). Excluding a connection rather than a
// user is what lets a user's other devices see what this one sent.
func (s *Server) BroadcastMessage(except user.Socket, msg any) {
	s.mu.RLock()
	targets := make([]user.Socket, 0, len(s.ConnectedUsers))
	ids := make([]string, 0, len(s.ConnectedUsers))
	for id, conns := range s.ConnectedUsers {
		for sock := range conns {
			if except != nil && sock == except {
				continue
			}
			targets = append(targets, sock)
			ids = append(ids, id)
		}
	}
	s.mu.RUnlock()

	for i, sock := range targets {
		if err := sock.SendJSON(msg); err != nil {
			slog.Debug("broadcast: send failed", logger.Server(s.ID), logger.User(ids[i]), "error", err)
		}
	}
}

// JoinVoice makes vs the user's only voice session and puts it in ch,
// returning the session it replaced. Replace, evict and insert run as one
// step under joinMu: the channel maps are keyed by user id, so two devices
// joining at once could otherwise each overwrite the other's entry. joinMu is
// taken only here, and channels never call back into the server.
func (s *Server) JoinVoice(vs VoiceSession, ch *channel.VoiceChannel) (prev VoiceSession, replaced, evicted bool) {
	s.joinMu.Lock()
	defer s.joinMu.Unlock()

	s.mu.Lock()
	prev, replaced = s.voice[vs.User.ID]
	s.voice[vs.User.ID] = vs
	s.mu.Unlock()
	replaced = replaced && prev.User != vs.User

	if replaced {
		if pc := s.GetVoiceChannel(prev.ChannelID); pc != nil {
			evicted = pc.RemoveUserIf(prev.User.ID, prev.User)
		}
	}
	ch.AddUser(vs.User)
	return prev, replaced, evicted
}

// ReleaseVoice clears the claim only if u still holds it, so a late cleanup
// of a replaced session cannot release its successor.
func (s *Server) ReleaseVoice(u *user.User) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.voice[u.ID].User == u {
		delete(s.voice, u.ID)
	}
}

func (s *Server) GetVoiceChannel(id string) *channel.VoiceChannel {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.VoiceChannels[id]
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

func (s *Server) RemoveVoiceChannel(id string) *channel.VoiceChannel {
	s.mu.Lock()
	defer s.mu.Unlock()
	vc := s.VoiceChannels[id]
	delete(s.VoiceChannels, id)
	return vc
}

func (s *Server) RemoveTextChannel(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.TextChannels, id)
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

// SendToUser pushes msg to every live socket of userID and reports how many
// it reached; 0 means the user is offline.
func (s *Server) SendToUser(userID string, msg any) int {
	s.mu.RLock()
	targets := make([]user.Socket, 0, len(s.ConnectedUsers[userID]))
	for sock := range s.ConnectedUsers[userID] {
		targets = append(targets, sock)
	}
	s.mu.RUnlock()

	sent := 0
	for _, sock := range targets {
		if err := sock.SendJSON(msg); err != nil {
			slog.Debug("send to user failed", logger.Server(s.ID), logger.User(userID), "error", err)
			continue
		}
		sent++
	}
	return sent
}

// VoiceChannelOf is the channel userID's voice session is in, "" if none.
func (s *Server) VoiceChannelOf(userID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.voice[userID].ChannelID
}

func (s *Server) SummonBot(botID, channelID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.summons[botID] = channelID
}

// DismissBot clears the summon and returns the channel it pointed at.
func (s *Server) DismissBot(botID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := s.summons[botID]
	delete(s.summons, botID)
	return ch
}

func (s *Server) SummonedChannel(botID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.summons[botID]
}
