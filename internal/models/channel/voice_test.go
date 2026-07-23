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

func TestVoice_MembersReflectVoiceState(t *testing.T) {
	vc := NewVoiceChannel("voice-id", "server-id")

	loud := &user.User{ID: "loud", DisplayName: "Loud", Signaling: &MockSocket{}}
	muted := &user.User{ID: "muted", DisplayName: "Muted", Signaling: &MockSocket{}}
	deaf := &user.User{ID: "deaf", DisplayName: "Deaf", Signaling: &MockSocket{}}
	muted.SetVoiceState(true, false)
	deaf.SetVoiceState(false, true) // deafen implies mute

	vc.AddUser(loud)
	vc.AddUser(muted)
	vc.AddUser(deaf)

	byID := map[string]Member{}
	for _, m := range vc.Members() {
		byID[m.ID] = m
	}
	require.Len(t, byID, 3)

	require.False(t, byID["loud"].Muted)
	require.False(t, byID["loud"].Deafened)

	require.True(t, byID["muted"].Muted)
	require.False(t, byID["muted"].Deafened)

	require.True(t, byID["deaf"].Muted)
	require.True(t, byID["deaf"].Deafened)
}

func TestVoice_BroadcastExcludesSender(t *testing.T) {
	vc := NewVoiceChannel("voice-id", "server-id")

	u1 := &user.User{ID: "user-1", Signaling: &MockSocket{}}
	u2 := &user.User{ID: "user-2", Signaling: &MockSocket{}}
	u3 := &user.User{ID: "user-3", Signaling: &MockSocket{}}
	vc.AddUser(u1)
	vc.AddUser(u2)
	vc.AddUser(u3)

	vc.Broadcast("user-1", map[string]any{"type": "voice-state"})

	require.Empty(t, u1.Signaling.(*MockSocket).jsonPayloads)
	require.Len(t, u2.Signaling.(*MockSocket).jsonPayloads, 1)
	require.Len(t, u3.Signaling.(*MockSocket).jsonPayloads, 1)
}
