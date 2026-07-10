package server

import (
	"testing"

	"github.com/armonic-tech/armonic-backend/internal/models/user"
	"github.com/stretchr/testify/require"
)

func TestServer(t *testing.T) {
	sv := NewServer("server-id")

	txtch := sv.AddTextChannel("text-ch")
	voicech := sv.AddVoiceChannel("voice-ch")

	require.Equal(t, sv.GetOrCreateTextChannel("text-ch"), txtch)
	require.Equal(t, sv.GetOrCreateVoiceChannel("voice-ch"), voicech)

	u := user.User{
		ID: "user-id",
	}

	sv.AddConnectedUser(&u)
	require.Len(t, sv.ConnectedUsers, 1)

	sv.RemoveConnectedUser(u.ID)
	require.Len(t, sv.ConnectedUsers, 0)
}
