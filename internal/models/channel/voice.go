package channel

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/armonic-tech/armonic-backend/internal/models/user"
	"github.com/armonic-tech/armonic-backend/pkg/logger"
	"github.com/pion/webrtc/v3"
)

type VoiceChannel struct {
	ID       string
	ServerID string
	Users    map[string]*user.User
	pubs     map[string]*publication
	mu       sync.RWMutex
}

type publication struct {
	sender    *user.User
	track     *webrtc.TrackRemote
	channelID string
	streamID  string

	mu      sync.Mutex
	outs    atomic.Pointer[[]*outbound]
	closed  bool
	read    atomic.Uint64
	written atomic.Uint64
	logged  atomic.Bool
}

type outbound struct {
	receiver *user.User
	track    *webrtc.TrackLocalStaticRTP

	rtcp atomic.Uint64
}

func NewVoiceChannel(id, serverID string) *VoiceChannel {
	return &VoiceChannel{
		ID:       id,
		ServerID: serverID,
		Users:    make(map[string]*user.User),
		pubs:     make(map[string]*publication),
	}
}

func (vc *VoiceChannel) AddUser(u *user.User) {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	vc.Users[u.ID] = u
}

func (vc *VoiceChannel) AttachPending(u *user.User) {
	vc.mu.RLock()
	pubs := make([]*publication, 0, len(vc.pubs))
	for id, p := range vc.pubs {
		if id != u.ID {
			pubs = append(pubs, p)
		}
	}
	vc.mu.RUnlock()

	for _, p := range pubs {
		if p.has(u.ID) {
			continue
		}
		attach(p, u)
		return
	}
}

func (p *publication) listeners() []*outbound {
	if s := p.outs.Load(); s != nil {
		return *s
	}
	return nil
}

func (p *publication) has(receiverID string) bool {
	for _, o := range p.listeners() {
		if o.receiver.ID == receiverID {
			return true
		}
	}
	return false
}

func (p *publication) add(o *outbound) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return false
	}
	cur := p.listeners()
	for _, e := range cur {
		if e.receiver.ID == o.receiver.ID {
			return false
		}
	}
	next := make([]*outbound, len(cur), len(cur)+1)
	copy(next, cur)
	next = append(next, o)
	p.outs.Store(&next)
	return true
}

func (p *publication) remove(receiverID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	cur := p.listeners()
	next := make([]*outbound, 0, len(cur))
	for _, e := range cur {
		if e.receiver.ID != receiverID {
			next = append(next, e)
		}
	}
	p.outs.Store(&next)
}

func (p *publication) close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	empty := []*outbound{}
	p.outs.Store(&empty)
}

func (vc *VoiceChannel) Members() []Member {
	vc.mu.RLock()
	defer vc.mu.RUnlock()

	members := make([]Member, 0, len(vc.Users))
	for _, u := range vc.Users {
		members = append(members, Member{
			ID:          u.ID,
			DisplayName: u.DisplayName,
			AvatarID:    u.AvatarID,
			Muted:       u.IsMuted(),
			Deafened:    u.IsDeafened(),
			Linked:      u.IsLinked(),
			IsBot:       u.IsBot,
		})
	}
	return members
}

func (vc *VoiceChannel) RemoveUser(userID string) {
	vc.mu.Lock()
	delete(vc.Users, userID)
	pub := vc.pubs[userID]
	delete(vc.pubs, userID)
	others := make([]*publication, 0, len(vc.pubs))
	for _, p := range vc.pubs {
		others = append(others, p)
	}
	vc.mu.Unlock()

	if pub != nil {
		pub.close()
	}
	for _, p := range others {
		p.remove(userID)
	}
}

func (vc *VoiceChannel) RemoveUserIf(userID string, u *user.User) bool {
	vc.mu.Lock()
	if vc.Users[userID] != u {
		vc.mu.Unlock()
		return false
	}
	vc.mu.Unlock()

	vc.RemoveUser(userID)
	return true
}

func (vc *VoiceChannel) KickUser(userID string) bool {
	vc.mu.RLock()
	u, exists := vc.Users[userID]
	vc.mu.RUnlock()
	if !exists {
		return false
	}

	vc.RemoveUser(userID)
	if err := u.Signaling.SendJSON(map[string]any{"type": "kicked-voice"}); err != nil {
		slog.Debug("voice: kicked-voice send failed", logger.User(userID), logger.Channel(vc.ID), "error", err)
	}
	u.Media.Close()
	return true
}

func (vc *VoiceChannel) CloseAll() int {
	vc.mu.Lock()
	members := make([]*user.User, 0, len(vc.Users))
	for _, u := range vc.Users {
		members = append(members, u)
	}
	vc.Users = make(map[string]*user.User)
	vc.pubs = make(map[string]*publication)
	vc.mu.Unlock()

	for _, u := range members {
		if u.Media != nil {
			u.Media.Close()
		}
	}
	return len(members)
}

func (vc *VoiceChannel) Broadcast(senderID string, msg any) {
	vc.mu.RLock()
	targets := make([]user.Socket, 0, len(vc.Users))
	for id, u := range vc.Users {
		if id != senderID {
			targets = append(targets, u.Signaling)
		}
	}
	vc.mu.RUnlock()

	for _, s := range targets {
		if err := s.SendJSON(msg); err != nil {
			slog.Debug("voice: broadcast send failed", logger.Channel(vc.ID), "error", err)
		}
	}
}

func (vc *VoiceChannel) ForwardTrack(sender *user.User, track *webrtc.TrackRemote) {
	pub := &publication{sender: sender, track: track, channelID: vc.ID, streamID: StreamIDFor(sender.ID)}

	vc.mu.Lock()
	vc.pubs[sender.ID] = pub
	listeners := make([]*user.User, 0, len(vc.Users))
	for id, u := range vc.Users {
		if id != sender.ID {
			listeners = append(listeners, u)
		}
	}
	vc.mu.Unlock()

	slog.Info("voice: publication started", logger.User(sender.ID), logger.Channel(vc.ID), "listeners", len(listeners))
	for _, u := range listeners {
		attach(pub, u)
	}

	go func() {
		defer logger.Recover(context.Background(), "voice pump")
		pub.pump()
	}()
}

func attach(pub *publication, receiver *user.User) {
	if receiver.Media == nil {
		return
	}

	if pub.has(receiver.ID) {
		return
	}

	localTrack, err := webrtc.NewTrackLocalStaticRTP(
		pub.track.Codec().RTPCodecCapability,
		pub.track.ID(),
		pub.streamID,
	)
	if err != nil {
		slog.Error("voice: creating local track", logger.User(receiver.ID), logger.Channel(pub.channelID), "error", err)
		return
	}

	rtpSender, err := receiver.Media.AddTrack(localTrack)
	if err != nil {
		slog.Error("voice: adding track", logger.User(receiver.ID), logger.Channel(pub.channelID), "error", err)
		return
	}

	offer, err := receiver.Media.CreateOffer()
	if err != nil {
		slog.Error("voice: renegotiation offer", logger.User(receiver.ID), logger.Channel(pub.channelID), "error", err)
		return
	}
	if err := receiver.Media.SetLocalDescription(offer); err != nil {
		slog.Error("voice: renegotiation local description", logger.User(receiver.ID), logger.Channel(pub.channelID), "error", err)
		return
	}

	out := &outbound{receiver: receiver, track: localTrack}
	if !pub.add(out) {
		slog.Debug("voice: attach skipped, publication closed or listener present",
			"from", pub.sender.ID, "to", receiver.ID, logger.Channel(pub.channelID))
		return
	}

	if err := receiver.Signaling.SendJSON(map[string]any{
		"type": "offer",
		"sdp":  offer,
	}); err != nil {
		slog.Error("voice: sending renegotiation offer", logger.User(receiver.ID), logger.Channel(pub.channelID), "error", err)
	}

	slog.Info("voice: track attached",
		"from", pub.sender.ID, "to", receiver.ID, logger.Channel(pub.channelID), "stream", pub.streamID,
		"offer", SDPSummary(offer.SDP))

	if rtpSender != nil {
		go func() {
			defer logger.Recover(context.Background(), "voice rtcp reader")
			buf := make([]byte, 1500)
			for {
				if _, _, err := rtpSender.Read(buf); err != nil {
					return
				}
				out.rtcp.Add(1)
			}
		}()
	}
}

// StreamIDFor names a relayed stream "<senderUserID>.<random>": the listener
// learns whose audio a track carries from the msid alone, and the suffix keeps
// a sender's next publication (after a rejoin) from colliding with the dead
// one still sitting in the listener's peer connection.
func StreamIDFor(senderID string) string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return senderID + "." + hex.EncodeToString(b[:])
}

func SDPSummary(sdp string) string {
	var out []string
	for _, line := range strings.Split(sdp, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "m="):
			fields := strings.Fields(strings.TrimPrefix(line, "m="))
			if len(fields) > 0 {
				out = append(out, fields[0]+":?")
			}
		case line == "a=sendrecv", line == "a=sendonly",
			line == "a=recvonly", line == "a=inactive":
			if len(out) > 0 {
				out[len(out)-1] = strings.TrimSuffix(out[len(out)-1], ":?") +
					":" + strings.TrimPrefix(line, "a=")
			}
		}
	}
	if len(out) == 0 {
		return "none"
	}
	return strings.Join(out, " ")
}

func (p *publication) listenerReport() string {
	current := p.listeners()
	parts := make([]string, 0, len(current))
	for _, out := range current {
		parts = append(parts, fmt.Sprintf("%s:%d", out.receiver.ID, out.rtcp.Load()))
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, " ")
}

const minWriteGap = 4 * time.Millisecond

func (p *publication) pump() {
	buf := make([]byte, 1500)
	var lastWrite time.Time
	for {
		n, _, err := p.track.Read(buf)
		if err != nil {
			slog.Info("voice: publication ended", logger.User(p.sender.ID), logger.Channel(p.channelID),
				"read", p.read.Load(), "written", p.written.Load(),
				"listeners", p.listenerReport(), "error", err)
			return
		}
		p.read.Add(1)

		if p.sender.IsMuted() {
			continue
		}

		if gap := time.Since(lastWrite); gap < minWriteGap {
			time.Sleep(minWriteGap - gap)
		}
		lastWrite = time.Now()

		for _, out := range p.listeners() {
			if out.receiver.IsDeafened() {
				continue
			}
			if _, err := out.track.Write(buf[:n]); err != nil {
				if !p.logged.Swap(true) {
					slog.Error("voice: writing to listener",
						logger.User(out.receiver.ID), "from", p.sender.ID, logger.Channel(p.channelID), "error", err)
				}
				continue
			}
			p.written.Add(1)
		}

		if r := p.read.Load(); r%500 == 0 {
			slog.Debug("voice: relay progress", logger.User(p.sender.ID), logger.Channel(p.channelID),
				"read", r, "written", p.written.Load(),
				"listeners", p.listenerReport(), "muted", p.sender.IsMuted())
		}
	}
}
