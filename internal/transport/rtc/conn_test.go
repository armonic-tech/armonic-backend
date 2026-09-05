package rtc

import (
	"strings"
	"testing"

	"github.com/pion/webrtc/v3"
)

func TestCandidatesBeforeAnswerAreBufferedAndApplied(t *testing.T) {
	conn, err := NewWebRTCConn()
	if err != nil {
		t.Fatalf("NewWebRTCConn: %v", err)
	}
	defer conn.Close()

	if err := conn.AddAudioTransceiver(); err != nil {
		t.Fatalf("AddAudioTransceiver: %v", err)
	}
	offer, err := conn.CreateOffer()
	if err != nil {
		t.Fatalf("CreateOffer: %v", err)
	}
	if err := conn.SetLocalDescription(offer); err != nil {
		t.Fatalf("SetLocalDescription: %v", err)
	}

	answer := answerTo(t, offer)

	sdpMid, sdpMLineIndex := "0", uint16(0)
	early := webrtc.ICECandidateInit{
		Candidate:     "candidate:1 1 udp 2130706431 192.168.0.136 41234 typ host",
		SDPMid:        &sdpMid,
		SDPMLineIndex: &sdpMLineIndex,
	}
	if err := conn.AddICECandidate(early); err != nil {
		t.Fatalf("candidate before the answer must be buffered, got: %v", err)
	}

	if err := conn.SetRemoteDescription(answer); err != nil {
		t.Fatalf("SetRemoteDescription drained the buffer with: %v", err)
	}

	// Post-answer candidates still go straight through.
	if err := conn.AddICECandidate(early); err != nil {
		t.Fatalf("candidate after the answer: %v", err)
	}
}

// A custom webrtc.API starts with an empty MediaEngine, so an offer built from
// one that never registered codecs has nothing to negotiate and join-voice
// fails with "excessive retries in CreateOffer".
func TestConfiguredAPIOffersCodecs(t *testing.T) {
	if err := Configure(Config{}); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	conn, err := NewWebRTCConn()
	if err != nil {
		t.Fatalf("NewWebRTCConn: %v", err)
	}
	defer conn.Close()

	if err := conn.AddAudioTransceiver(); err != nil {
		t.Fatalf("AddAudioTransceiver: %v", err)
	}
	offer, err := conn.CreateOffer()
	if err != nil {
		t.Fatalf("CreateOffer: %v", err)
	}
	if !strings.Contains(offer.SDP, "m=audio") {
		t.Fatalf("offer carries no audio section:\n%s", offer.SDP)
	}
	if !strings.Contains(strings.ToLower(offer.SDP), "opus") {
		t.Fatalf("offer carries no opus codec:\n%s", offer.SDP)
	}
}

// The muxed socket is the path a real deployment takes, so it has to produce a
// usable offer too — not just the unmuxed fallback.
func TestConfiguredAPIOffersCodecsWithUDPMux(t *testing.T) {
	if err := Configure(Config{UDPPort: 51999}); err != nil {
		t.Skipf("cannot bind the mux port here: %v", err)
	}
	t.Cleanup(func() { _ = Configure(Config{}) })

	conn, err := NewWebRTCConn()
	if err != nil {
		t.Fatalf("NewWebRTCConn: %v", err)
	}
	defer conn.Close()

	if err := conn.AddAudioTransceiver(); err != nil {
		t.Fatalf("AddAudioTransceiver: %v", err)
	}
	offer, err := conn.CreateOffer()
	if err != nil {
		t.Fatalf("CreateOffer: %v", err)
	}
	if !strings.Contains(strings.ToLower(offer.SDP), "opus") {
		t.Fatalf("offer carries no opus codec:\n%s", offer.SDP)
	}
}

func TestPendingCandidateBufferIsBounded(t *testing.T) {
	conn, err := NewWebRTCConn()
	if err != nil {
		t.Fatalf("NewWebRTCConn: %v", err)
	}
	defer conn.Close()

	for i := 0; i < maxPendingCandidates*2; i++ {
		if err := conn.AddICECandidate(webrtc.ICECandidateInit{Candidate: "candidate:junk"}); err != nil {
			t.Fatalf("AddICECandidate: %v", err)
		}
	}
	conn.mu.Lock()
	got := len(conn.pending)
	conn.mu.Unlock()
	if got != maxPendingCandidates {
		t.Fatalf("pending buffer = %d, want it capped at %d", got, maxPendingCandidates)
	}
}

// answerTo builds a real answer from a throwaway peer, so the SDP the test
// feeds back is one pion actually accepts.
func answerTo(t *testing.T, offer webrtc.SessionDescription) webrtc.SessionDescription {
	t.Helper()
	peer, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("peer: %v", err)
	}
	defer peer.Close()

	if err := peer.SetRemoteDescription(offer); err != nil {
		t.Fatalf("peer SetRemoteDescription: %v", err)
	}
	answer, err := peer.CreateAnswer(nil)
	if err != nil {
		t.Fatalf("peer CreateAnswer: %v", err)
	}
	if err := peer.SetLocalDescription(answer); err != nil {
		t.Fatalf("peer SetLocalDescription: %v", err)
	}
	return answer
}
