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

func voiceUser(id string) *user.User {
	return &user.User{ID: id, Signaling: &fakeSocket{}}
}

func TestJoinVoiceReplacesTheUsersPreviousSession(t *testing.T) {
	sv := NewServer("s")
	general := sv.GetOrCreateVoiceChannel("general")
	music := sv.GetOrCreateVoiceChannel("music")

	laptop := voiceUser("ana")
	_, replaced, _ := sv.JoinVoice(VoiceSession{User: laptop, ChannelID: "general"}, general)
	require.False(t, replaced)

	phone := voiceUser("ana")
	prev, replaced, evicted := sv.JoinVoice(VoiceSession{User: phone, ChannelID: "music"}, music)
	require.True(t, replaced)
	require.True(t, evicted)
	require.Same(t, laptop, prev.User)
	require.Equal(t, "general", prev.ChannelID)
	require.Empty(t, general.Members(), "the old device left its channel")
	require.Len(t, music.Members(), 1)
}

func TestJoinVoiceAgainInTheSameChannelKeepsOneEntry(t *testing.T) {
	sv := NewServer("s")
	general := sv.GetOrCreateVoiceChannel("general")
	first, second := voiceUser("ana"), voiceUser("ana")

	sv.JoinVoice(VoiceSession{User: first, ChannelID: "general"}, general)
	_, replaced, evicted := sv.JoinVoice(VoiceSession{User: second, ChannelID: "general"}, general)

	require.True(t, replaced)
	require.True(t, evicted)
	require.Len(t, general.Members(), 1)
	require.False(t, general.RemoveUserIf("ana", first), "the stale session is no longer the member")
	require.True(t, general.RemoveUserIf("ana", second))
}

func TestReleaseVoiceIgnoresAReplacedSession(t *testing.T) {
	sv := NewServer("s")
	general := sv.GetOrCreateVoiceChannel("general")
	first, second := voiceUser("ana"), voiceUser("ana")
	sv.JoinVoice(VoiceSession{User: first, ChannelID: "general"}, general)
	sv.JoinVoice(VoiceSession{User: second, ChannelID: "general"}, general)

	sv.ReleaseVoice(first) // late cleanup of the replaced session
	_, replaced, _ := sv.JoinVoice(VoiceSession{User: voiceUser("ana"), ChannelID: "general"}, general)
	require.True(t, replaced, "second must still have held the claim")

	sv.ReleaseVoice(second)
}

func TestConcurrentJoinsLeaveExactlyOneSession(t *testing.T) {
	sv := NewServer("s")
	a := sv.GetOrCreateVoiceChannel("a")
	b := sv.GetOrCreateVoiceChannel("b")

	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch, id := a, "a"
			if i%2 == 1 {
				ch, id = b, "b"
			}
			sv.JoinVoice(VoiceSession{User: voiceUser("ana"), ChannelID: id}, ch)
		}()
	}
	wg.Wait()

	require.Equal(t, 1, len(a.Members())+len(b.Members()))
}

// The "El mensaje ya no existe" bug: the admin reconnects and the server only
// notices the old socket died later. Its cleanup used to delete the user's
// only entry — the live socket's — so the live client stopped receiving
// message-deleted (and every other push). A delete looked like it did
// nothing; trying again answered "message not found".
func TestLateDisconnectOfAStaleSocketKeepsTheLiveOneSubscribed(t *testing.T) {
	sv := NewServer("s")
	stale, live := &fakeSocket{}, &fakeSocket{}
	sv.AddConnectedUser(&user.User{ID: "admin", Signaling: stale})
	sv.AddConnectedUser(&user.User{ID: "admin", Signaling: live})

	sv.RemoveConnection("admin", stale)
	sv.BroadcastMessage(nil, map[string]any{"type": "message-deleted"})

	require.Equal(t, 1, live.count())
}

// Same bug with two sessions open: only the newest got the broadcast, so the
// session that asked for the delete never saw it confirmed.
func TestEverySessionOfTheDeleterSeesMessageDeleted(t *testing.T) {
	sv := NewServer("s")
	first, second := &fakeSocket{}, &fakeSocket{}
	sv.AddConnectedUser(&user.User{ID: "admin", Signaling: first})
	sv.AddConnectedUser(&user.User{ID: "admin", Signaling: second})

	sv.BroadcastMessage(nil, map[string]any{"type": "message-deleted"})

	require.Equal(t, 1, first.count())
	require.Equal(t, 1, second.count())
}
