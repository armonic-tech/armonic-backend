package rtc

import (
	"errors"
	"sync"

	"github.com/pion/webrtc/v3"
)

const maxPendingCandidates = 64

type WebRTCConn struct {
	pc *webrtc.PeerConnection

	mu            sync.Mutex
	remoteDescSet bool
	pending       []webrtc.ICECandidateInit
}

func NewWebRTCConn() (*WebRTCConn, error) {
	pc, err := newPeerConnection()
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
	if err := w.pc.SetRemoteDescription(sdp); err != nil {
		return err
	}
	w.mu.Lock()
	pending := w.pending
	w.pending = nil
	w.remoteDescSet = true
	w.mu.Unlock()

	var errs []error
	for _, c := range pending {
		if err := w.pc.AddICECandidate(c); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (w *WebRTCConn) AddICECandidate(candidate webrtc.ICECandidateInit) error {
	w.mu.Lock()
	if !w.remoteDescSet {
		if len(w.pending) < maxPendingCandidates {
			w.pending = append(w.pending, candidate)
		}
		w.mu.Unlock()
		return nil
	}
	w.mu.Unlock()
	return w.pc.AddICECandidate(candidate)
}

func (w *WebRTCConn) AddTrack(track *webrtc.TrackLocalStaticRTP) (*webrtc.RTPSender, error) {
	tr, err := w.pc.AddTransceiverFromTrack(track, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionSendonly,
	})
	if err != nil {
		return nil, err
	}
	return tr.Sender(), nil
}

func (w *WebRTCConn) OnTrack(f func(*webrtc.TrackRemote, *webrtc.RTPReceiver)) {
	w.pc.OnTrack(f)
}

func (w *WebRTCConn) OnConnectionStateChange(f func(webrtc.PeerConnectionState)) {
	w.pc.OnConnectionStateChange(f)
}
