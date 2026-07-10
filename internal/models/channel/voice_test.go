package channel

import (
	"testing"

	"github.com/armonic-tech/armonic-backend/internal/models/user"
	"github.com/pion/webrtc/v3"
	"github.com/stretchr/testify/require"
)

// mockRTCConn is a no-op user.RTCConn double: KickUser calls Media.Close(),
// which real connections always have (rtc.WebRTCConn), but a bare test user
// otherwise wouldn't.
type mockRTCConn struct{}

func (m *mockRTCConn) Close() error                              { return nil }
func (m *mockRTCConn) IsConnected() bool                         { return false }
func (m *mockRTCConn) AddAudioTransceiver() error                { return nil }
func (m *mockRTCConn) OnICECandidate(func(*webrtc.ICECandidate)) {}
func (m *mockRTCConn) CreateOffer() (webrtc.SessionDescription, error) {
	return webrtc.SessionDescription{}, nil
}
func (m *mockRTCConn) SetLocalDescription(webrtc.SessionDescription) error  { return nil }
func (m *mockRTCConn) SetRemoteDescription(webrtc.SessionDescription) error { return nil }
func (m *mockRTCConn) AddICECandidate(webrtc.ICECandidateInit) error        { return nil }
func (m *mockRTCConn) AddTrack(*webrtc.TrackLocalStaticRTP) (*webrtc.RTPSender, error) {
	return nil, nil
}
func (m *mockRTCConn) OnTrack(func(*webrtc.TrackRemote, *webrtc.RTPReceiver)) {}

func TestVoice(t *testing.T) {
	u := user.User{
		ID:          "id-user",
		DisplayName: "test",
		Signaling:   &MockSocket{},
		Media:       &mockRTCConn{},
	}
	vc := NewVoiceChannel("voice-id", "server-id")

	vc.AddUser(&u)
	require.Len(t, vc.Users, 1)

	vc.RemoveUser(u.ID)
	require.Len(t, vc.Users, 0)

	vc.AddUser(&u)
	require.Len(t, vc.Users, 1)

	vc.KickUser(u.ID)
	require.Len(t, vc.Users, 0)
}
