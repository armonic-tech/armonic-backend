package app

import (
	"testing"

	"github.com/armonic-tech/armonic-backend/internal/models/user"
	"github.com/stretchr/testify/require"
)

func TestApp(t *testing.T) {
	app := NewApp()
	sv := app.GetOrCreateServer("test")

	require.NotNil(t, sv)
	require.Equal(t, "test", sv.ID)
	require.NotNil(t, app.Servers["test"])
	require.Len(t, app.Servers, 1)
}

type nopSocket struct{}

func (nopSocket) Send([]byte) error  { return nil }
func (nopSocket) SendJSON(any) error { return nil }
func (nopSocket) Close() error       { return nil }

func TestRemoveConnection(t *testing.T) {
	app := NewApp()
	sv := app.GetOrCreateServer("test")
	sock := &nopSocket{}
	u := &user.User{
		ID:          "test-id",
		DisplayName: "test",
		Signaling:   sock,
	}

	sv.AddConnectedUser(u)
	require.Len(t, sv.ConnectedUsers, 1)

	app.RemoveConnection("test-id", sock)

	require.Len(t, sv.ConnectedUsers, 0)
	require.Nil(t, sv.ConnectedUsers["user-1"])
}
