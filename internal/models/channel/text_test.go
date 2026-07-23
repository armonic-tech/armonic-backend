package channel

import (
	"testing"

	"github.com/armonic-tech/armonic-backend/internal/models/message"
	"github.com/armonic-tech/armonic-backend/internal/models/user"
	"github.com/stretchr/testify/require"
)

func TestText(t *testing.T) {
	textCh := NewTextChannel("text", "test-id")
	u := user.User{
		ID:          "user-id",
		DisplayName: "test",
	}

	textCh.AddUser(&u)
	require.NotNil(t, textCh.Users)
	require.Len(t, textCh.Users, 1)

	textCh.AddMessage(&message.Message{
		ID:      "msg-id",
		UserID:  u.ID,
		Content: "test",
	})
	require.Len(t, textCh.Messages, 1)

	textCh.RemoveUser("user-id")
	require.Len(t, textCh.Users, 0)
	require.Nil(t, textCh.Users["user-id"])
}

//

type MockSocket struct {
	messages     [][]byte
	jsonPayloads []any
}

func (m *MockSocket) Send(data []byte) error {
	m.messages = append(m.messages, data)
	return nil
}

func (m *MockSocket) SendJSON(v any) error {
	m.jsonPayloads = append(m.jsonPayloads, v)
	return nil
}

func (m *MockSocket) Close() error {
	return nil
}

func TestTextChannel_Broadcast(t *testing.T) {
	ch := NewTextChannel("ch-1", "sv-1")

	u1 := &user.User{ID: "user-1", Signaling: &MockSocket{}}
	u2 := &user.User{ID: "user-2", Signaling: &MockSocket{}}
	u3 := &user.User{ID: "user-3", Signaling: &MockSocket{}}

	ch.AddUser(u1)
	ch.AddUser(u2)
	ch.AddUser(u3)

	// Broadcast from user-1
	// msg := []byte("hello")  // ["h", "e", "l", "l", "o"]
	// msg := []byte{72, 111, 108, 108, 121}
	msg := []byte("hola")
	ch.Broadcast("user-1", msg)

	// Veirfy u1 (sender) did not receive
	// MockSocket -> multiple msg
	mock1 := u1.Signaling.(*MockSocket)
	require.Len(t, mock1.messages, 0)

	// CHeck u2 & u3 receive msg
	mock2 := u2.Signaling.(*MockSocket)
	require.Len(t, mock2.messages, 1)
	require.Equal(t, msg, mock2.messages[0])

	mock3 := u3.Signaling.(*MockSocket)
	require.Len(t, mock3.messages, 1)
	require.Equal(t, msg, mock3.messages[0])
}
