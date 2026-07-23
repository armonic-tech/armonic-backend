package user

import (
	"sync/atomic"

	"github.com/pion/webrtc/v3"
)

type User struct {
	ID             string
	DisplayName    string
	Signaling      Socket
	Media          RTCConn
	VoiceChannelID string
	ServerID       string

	muted    atomic.Bool
	deafened atomic.Bool
}

func (u *User) SetVoiceState(muted, deafened bool) (bool, bool) {
	if deafened {
		muted = true
	}
	u.muted.Store(muted)
	u.deafened.Store(deafened)
	return muted, deafened
}

func (u *User) IsMuted() bool    { return u.muted.Load() }
func (u *User) IsDeafened() bool { return u.deafened.Load() }

type Socket interface {
	Send([]byte) error
	SendJSON(v any) error
	Close() error
}

type RTCConn interface {
	Close() error
	IsConnected() bool
	AddAudioTransceiver() error
	OnICECandidate(func(*webrtc.ICECandidate))
	CreateOffer() (webrtc.SessionDescription, error)
	SetLocalDescription(webrtc.SessionDescription) error
	SetRemoteDescription(webrtc.SessionDescription) error
	AddICECandidate(webrtc.ICECandidateInit) error
	AddTrack(track *webrtc.TrackLocalStaticRTP) (*webrtc.RTPSender, error)
	OnTrack(func(*webrtc.TrackRemote, *webrtc.RTPReceiver))
	OnConnectionStateChange(func(webrtc.PeerConnectionState))
}
