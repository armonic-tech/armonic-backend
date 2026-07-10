package user

import (
	"github.com/pion/webrtc/v3"
)

type User struct {
	ID             string
	DisplayName    string
	Signaling      Socket
	Media          RTCConn
	VoiceChannelID string
	ServerID       string
}

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
}
