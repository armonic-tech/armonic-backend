package server

import (
	"sync"
	"testing"

	"github.com/armonic-tech/armonic-backend/internal/models/user"
	"github.com/stretchr/testify/require"
)

type fakeSocket struct {
	mu   sync.Mutex
	sent []any
}

func (f *fakeSocket) Send([]byte) error { return nil }
func (f *fakeSocket) SendJSON(v any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, v)
	return nil
}
func (f *fakeSocket) Close() error { return nil }
func (f *fakeSocket) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

func TestServer(t *testing.T) {
	sv := NewServer("server-id")

	txtch := sv.AddTextChannel("text-ch")
	voicech := sv.AddVoiceChannel("voice-ch")

	require.Equal(t, sv.GetOrCreateTextChannel("text-ch"), txtch)
	require.Equal(t, sv.GetOrCreateVoiceChannel("voice-ch"), voicech)

	sock := &fakeSocket{}
	u := user.User{ID: "user-id", Signaling: sock}

	sv.AddConnectedUser(&u)
	require.Len(t, sv.ConnectedUsers, 1)

	require.Equal(t, 0, sv.RemoveConnection(u.ID, sock))
	require.Len(t, sv.ConnectedUsers, 0)
}

func TestTwoDevicesStayConnectedIndependently(t *testing.T) {
	sv := NewServer("s")
	laptop, phone := &fakeSocket{}, &fakeSocket{}
	sv.AddConnectedUser(&user.User{ID: "ana", Signaling: laptop})
	sv.AddConnectedUser(&user.User{ID: "ana", Signaling: phone})

	sv.BroadcastMessage(nil, "hello")
	require.Equal(t, 1, laptop.count())
	require.Equal(t, 1, phone.count(), "the second device must not replace the first")

	require.Equal(t, 1, sv.RemoveConnection("ana", laptop))
	require.Equal(t, []string{"ana"}, sv.ConnectedUserIDs(), "closing one device keeps the user online")

	sv.BroadcastMessage(nil, "still there")
	require.Equal(t, 1, laptop.count())
	require.Equal(t, 2, phone.count())
}

func TestBroadcastExcludesOnlyTheSendingConnection(t *testing.T) {
	sv := NewServer("s")
	laptop, phone, bob := &fakeSocket{}, &fakeSocket{}, &fakeSocket{}
	sv.AddConnectedUser(&user.User{ID: "ana", Signaling: laptop})
	sv.AddConnectedUser(&user.User{ID: "ana", Signaling: phone})
	sv.AddConnectedUser(&user.User{ID: "bob", Signaling: bob})

	sv.BroadcastMessage(laptop, "sent from the laptop")

	require.Equal(t, 0, laptop.count())
	require.Equal(t, 1, phone.count(), "the sender's other device sees its own message")
	require.Equal(t, 1, bob.count())
}

func TestRemoveUserDropsEveryConnection(t *testing.T) {
	sv := NewServer("s")
	laptop, phone := &fakeSocket{}, &fakeSocket{}
	sv.AddConnectedUser(&user.User{ID: "ana", Signaling: laptop})
	sv.AddConnectedUser(&user.User{ID: "ana", Signaling: phone})

	require.Equal(t, 2, sv.RemoveUser("ana"))
	sv.BroadcastMessage(nil, "after the kick")
	require.Zero(t, laptop.count())
	require.Zero(t, phone.count())
	require.Empty(t, sv.ConnectedUserIDs())
}
