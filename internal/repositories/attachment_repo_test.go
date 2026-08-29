package repositories

import (
	"context"
	"testing"
	"time"

	"github.com/armonic-tech/armonic-backend/internal/models/attachment"
	"github.com/armonic-tech/armonic-backend/internal/models/message"
	"github.com/stretchr/testify/require"
)

func sampleAttachment(id string) attachment.Attachment {
	return attachment.Attachment{
		ID:          id,
		Hash:        "b5bb9d8014a0f9b1d61e21e796d78dccdf1352f23cd32812f4850b878ae4944c",
		ServerID:    "sv-att",
		UserID:      "user-att",
		Format:      "png",
		ThumbFormat: "png",
		MIME:        "image/png",
		Size:        1234,
		Width:       640,
		Height:      480,
		CreatedAt:   time.Unix(1700000000, 0).UTC(),
	}
}

func TestAttachmentRepo_CreateAndGet(t *testing.T) {
	ctx := context.Background()
	tx, err := testDB.Begin()
	require.NoError(t, err)
	defer tx.Rollback()

	repo := NewAttachmentRepo(tx)
	want := sampleAttachment("att-1")
	require.NoError(t, repo.Create(ctx, want))

	got, err := repo.GetByID(ctx, "att-1")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, want.Hash, got.Hash)
	require.Equal(t, want.ServerID, got.ServerID)
	require.Equal(t, want.UserID, got.UserID)
	require.Equal(t, want.MIME, got.MIME)
	require.Equal(t, want.Size, got.Size)
	require.Equal(t, want.Width, got.Width)
	require.Equal(t, want.Height, got.Height)
	require.Equal(t, want.CreatedAt, got.CreatedAt)
	require.Equal(t, "/attachment/att-1", got.URL)
	require.Equal(t, "/attachment/att-1/thumb", got.ThumbURL)
}

func TestAttachmentRepo_GetUnknownIsNilNotError(t *testing.T) {
	ctx := context.Background()
	tx, err := testDB.Begin()
	require.NoError(t, err)
	defer tx.Rollback()

	got, err := NewAttachmentRepo(tx).GetByID(ctx, "does-not-exist")
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestMembershipRepo_IsMemberByAttachment(t *testing.T) {
	ctx := context.Background()
	tx, err := testDB.Begin()
	require.NoError(t, err)
	defer tx.Rollback()

	attachments := NewAttachmentRepo(tx)
	members := NewMembershipRepo(tx)

	require.NoError(t, attachments.Create(ctx, sampleAttachment("att-2")))
	require.NoError(t, members.Add(ctx, "user-in", "sv-att"))

	ok, err := members.IsMemberByAttachment(ctx, "user-in", "att-2")
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = members.IsMemberByAttachment(ctx, "user-out", "att-2")
	require.NoError(t, err)
	require.False(t, ok)

	ok, err = members.IsMemberByAttachment(ctx, "user-in", "att-unknown")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestMessageRepo_AttachmentIDRoundTrip(t *testing.T) {
	ctx := context.Background()
	tx, err := testDB.Begin()
	require.NoError(t, err)
	defer tx.Rollback()

	repo := NewMessageRepo(tx)
	base := message.Message{ServerID: "sv-m", ChannelID: "ch-m", UserID: "user-m", CreatedAt: time.Now()}

	withAttachment := base
	withAttachment.ID, withAttachment.Content, withAttachment.AttachmentID = "msg-1", "look at this", "att-9"
	require.NoError(t, repo.Save(ctx, withAttachment))

	plain := base
	plain.ID, plain.Content = "msg-2", "just text"
	require.NoError(t, repo.Save(ctx, plain))

	msgs, err := repo.GetByChannel(ctx, "sv-m", "ch-m", 10)
	require.NoError(t, err)
	require.Len(t, msgs, 2)

	byID := map[string]message.Message{}
	for _, m := range msgs {
		byID[m.ID] = m
	}
	require.Equal(t, "att-9", byID["msg-1"].AttachmentID)
	require.Empty(t, byID["msg-2"].AttachmentID)
}

func TestUserRepo_AvatarRoundTrip(t *testing.T) {
	ctx := context.Background()
	tx, err := testDB.Begin()
	require.NoError(t, err)
	defer tx.Rollback()

	users := NewUserRepo(tx)
	require.NoError(t, users.Upsert(ctx, "user-av", "Ada"))

	name, avatarID, err := users.GetProfile(ctx, "user-av")
	require.NoError(t, err)
	require.Equal(t, "Ada", name)
	require.Empty(t, avatarID)

	require.NoError(t, users.SetAvatar(ctx, "user-av", "att-avatar"))

	name, avatarID, err = users.GetProfile(ctx, "user-av")
	require.NoError(t, err)
	require.Equal(t, "Ada", name)
	require.Equal(t, "att-avatar", avatarID)
}

func TestUserRepo_GetProfileUnknownUser(t *testing.T) {
	ctx := context.Background()
	tx, err := testDB.Begin()
	require.NoError(t, err)
	defer tx.Rollback()

	name, avatarID, err := NewUserRepo(tx).GetProfile(ctx, "nobody")
	require.NoError(t, err)
	require.Empty(t, name)
	require.Empty(t, avatarID)
}

func TestMembershipRepo_GetMembers(t *testing.T) {
	ctx := context.Background()
	tx, err := testDB.Begin()
	require.NoError(t, err)
	defer tx.Rollback()

	users := NewUserRepo(tx)
	servers := NewServerRepo(tx)
	members := NewMembershipRepo(tx)

	require.NoError(t, servers.Create(ctx, "sv-roster", "Roster", "owner-1"))
	require.NoError(t, users.Upsert(ctx, "owner-1", "Zoe"))
	require.NoError(t, users.Upsert(ctx, "member-1", "ada"))
	require.NoError(t, users.SetAvatar(ctx, "member-1", "att-av"))
	require.NoError(t, members.Add(ctx, "owner-1", "sv-roster"))
	require.NoError(t, members.Add(ctx, "member-1", "sv-roster"))

	// A member of a different server must not leak into this roster.
	require.NoError(t, users.Upsert(ctx, "outsider", "Eve"))
	require.NoError(t, members.Add(ctx, "outsider", "sv-other"))

	list, err := members.GetMembers(ctx, "sv-roster")
	require.NoError(t, err)
	require.Len(t, list, 2)

	// Ordered case-insensitively by display name: ada before Zoe.
	require.Equal(t, "member-1", list[0].ID)
	require.Equal(t, "ada", list[0].DisplayName)
	require.Equal(t, "att-av", list[0].AvatarID)
	require.False(t, list[0].IsOwner)

	require.Equal(t, "owner-1", list[1].ID)
	require.True(t, list[1].IsOwner)
	require.Empty(t, list[1].AvatarID)
}

func TestMembershipRepo_GetMembersUnownedServer(t *testing.T) {
	ctx := context.Background()
	tx, err := testDB.Begin()
	require.NoError(t, err)
	defer tx.Rollback()

	users := NewUserRepo(tx)
	servers := NewServerRepo(tx)
	members := NewMembershipRepo(tx)

	// owner_id is NULL until the instance is claimed; nobody is the owner and
	// the COALESCE must not turn that into a scan error.
	require.NoError(t, servers.Create(ctx, "sv-unclaimed", "Fresh", ""))
	require.NoError(t, users.Upsert(ctx, "user-u", "Ann"))
	require.NoError(t, members.Add(ctx, "user-u", "sv-unclaimed"))

	list, err := members.GetMembers(ctx, "sv-unclaimed")
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.False(t, list[0].IsOwner)
}
