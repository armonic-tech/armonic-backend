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

func TestRemoveUser(t *testing.T) {
	app := NewApp()
	sv := app.GetOrCreateServer("test")
	u := &user.User{
		ID:          "test-id",
		DisplayName: "test",
	}

	sv.AddConnectedUser(u)
	require.Len(t, sv.ConnectedUsers, 1)

	app.RemoveConnectedUser("test-id")

	require.Len(t, sv.ConnectedUsers, 0)
	require.Nil(t, sv.ConnectedUsers["user-1"])
}
