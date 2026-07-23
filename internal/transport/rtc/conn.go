package rtc

import "github.com/pion/webrtc/v3"

type WebRTCConn struct {
	pc *webrtc.PeerConnection
}

var defaultConfig = webrtc.Configuration{
	ICEServers: []webrtc.ICEServer{
		{URLs: []string{"stun:stun.l.google.com:19302"}},
	},
}

func NewWebRTCConn() (*WebRTCConn, error) {
	pc, err := webrtc.NewPeerConnection(defaultConfig)
	if err != nil {
		return nil, err
	}
	return &WebRTCConn{pc: pc}, nil
}

func (w *WebRTCConn) Close() error {
	return w.pc.Close()
}

func (w *WebRTCConn) IsConnected() bool {
	return w.pc.ConnectionState() == webrtc.PeerConnectionStateConnected
}

func (w *WebRTCConn) AddAudioTransceiver() error {
	_, err := w.pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionRecvonly,
	})
	return err
}

func (w *WebRTCConn) OnICECandidate(f func(*webrtc.ICECandidate)) {
	w.pc.OnICECandidate(f)
}

func (w *WebRTCConn) CreateOffer() (webrtc.SessionDescription, error) {
	return w.pc.CreateOffer(nil)
}

func (w *WebRTCConn) SetLocalDescription(sdp webrtc.SessionDescription) error {
	return w.pc.SetLocalDescription(sdp)
}

func (w *WebRTCConn) SetRemoteDescription(sdp webrtc.SessionDescription) error {
	return w.pc.SetRemoteDescription(sdp)
}

func (w *WebRTCConn) AddICECandidate(candidate webrtc.ICECandidateInit) error {
	return w.pc.AddICECandidate(candidate)
}

func (w *WebRTCConn) AddTrack(track *webrtc.TrackLocalStaticRTP) (*webrtc.RTPSender, error) {
	return w.pc.AddTrack(track)
}

func (w *WebRTCConn) OnTrack(f func(*webrtc.TrackRemote, *webrtc.RTPReceiver)) {
	w.pc.OnTrack(f)
}

func (w *WebRTCConn) OnConnectionStateChange(f func(webrtc.PeerConnectionState)) {
	w.pc.OnConnectionStateChange(f)
}
