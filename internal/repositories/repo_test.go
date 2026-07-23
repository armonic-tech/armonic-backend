package repositories

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/armonic-tech/armonic-backend/internal/models/message"
	db "github.com/armonic-tech/armonic-backend/migrations"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

var testDB *sql.DB

func TestMain(m *testing.M) {
	ctx := context.Background()

	pg, err := postgres.Run(
		ctx,
		"postgres:17",
		postgres.WithDatabase("testdb"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),

		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		panic(err)
	}
	defer pg.Terminate(ctx)

	connStr, err := pg.ConnectionString(ctx)
	if err != nil {
		panic(err)
	}

	testDB, err = sql.Open("pgx", connStr)
	if err != nil {
		panic(err)
	}
	defer testDB.Close()

	if err := db.Migrate(testDB); err != nil {
		panic(err)
	}

	m.Run()
}

func TestUserRepo_Upsert(t *testing.T) {
	tx, err := testDB.Begin()
	require.NoError(t, err)

	defer tx.Rollback()

	repo := NewUserRepo(tx)

	ctx := context.Background()

	err = repo.Upsert(ctx, "1", "Juan")
	require.NoError(t, err)

	name, err := repo.GetName(ctx, "1")

	require.NoError(t, err)
	require.Equal(t, "Juan", name)
}

func TestServerRepo_Create(t *testing.T) {
	tx, err := testDB.Begin()
	require.NoError(t, err)

	defer tx.Rollback()

	repo := NewServerRepo(tx)

	ctx := context.Background()

	err = repo.Create(ctx, "example-id", "example", "")
	require.NoError(t, err)

	isOwner, err := repo.IsOwner(ctx, "example-id", "owner-id")
	require.NoError(t, err)
	require.False(t, isOwner)

	err = repo.SetOwner(ctx, "example-id", "owner-id")
	require.NoError(t, err)

	isOwner, err = repo.IsOwner(ctx, "example-id", "owner-id")
	require.NoError(t, err)
	require.True(t, isOwner)
}

func TestMessageRepo_Create(t *testing.T) {
	tx, err := testDB.Begin()
	require.NoError(t, err)

	defer tx.Rollback()

	repo := NewMessageRepo(tx)
	ctx := context.Background()

	msg := message.Message{
		ID:        "id",
		ChannelID: "channel-id",
		ServerID:  "server-message",
		UserID:    "user-id",
		Content:   "test",
		CreatedAt: time.Now(),
	}

	err = repo.Save(ctx, msg)
	require.NoError(t, err)

	msgs, err := repo.GetByChannel(ctx, "server-message", "channel-id", 1)

	require.NoError(t, err)
	require.Len(t, msgs, 1)
	require.Equal(t, msg.ID, msgs[0].ID)
	require.Equal(t, msg.Content, msgs[0].Content)
	require.Equal(t, msg.UserID, msgs[0].UserID)
}

func TestChannelRepo_Create(t *testing.T) {
	tx, err := testDB.Begin()
	require.NoError(t, err)
	defer tx.Rollback()

	repo := NewChannelRepo(tx)
	ctx := context.Background()

	err = repo.Create(ctx, "ch-1", "sv-1", "general", "text")
	require.NoError(t, err)

	channels, err := repo.GetChannelByServer(ctx, "sv-1")
	require.NoError(t, err)

	require.Len(t, channels, 1)
	require.Equal(t, "ch-1", channels[0].ID)
	require.Equal(t, "general", channels[0].Name)
	require.Equal(t, "text", channels[0].Type)
}

func TestMembershipRepo_IsMemberByChannel(t *testing.T) {
	ctx := context.Background()
	tx, err := testDB.Begin()
	require.NoError(t, err)
	defer tx.Rollback()

	channels := NewChannelRepo(tx)
	members := NewMembershipRepo(tx)

	require.NoError(t, channels.Create(ctx, "ch-1", "sv-1", "general", "text"))
	require.NoError(t, members.Add(ctx, "user-1", "sv-1"))

	// user-1 is a member of sv-1, which owns ch-1: true
	ok, err := members.IsMemberByChannel(ctx, "user-1", "ch-1")
	require.NoError(t, err)
	require.True(t, ok)

	// user-2 is not a member of that server: false
	ok, err = members.IsMemberByChannel(ctx, "user-2", "ch-1")
	require.NoError(t, err)
	require.False(t, ok)

	// unknown channel: false (no row, not an error)
	ok, err = members.IsMemberByChannel(ctx, "user-1", "ch-unknown")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestInviteRepo(t *testing.T) {
	ctx := context.Background()
	tx, err := testDB.Begin()
	require.NoError(t, err)
	defer tx.Rollback()

	repo := NewInviteRepo(tx)

	token, err := repo.Create(ctx, "server-1", "user-1", time.Hour)
	require.NoError(t, err)
	require.NotEmpty(t, token)

	inv, err := repo.Get(ctx, token)
	require.NoError(t, err)
	require.NotNil(t, inv)
	require.Equal(t, "server-1", inv.ServerID)
	require.Equal(t, "user-1", inv.CreatedBy)

	expiredToken, err := repo.Create(ctx, "server-1", "user-1", -time.Hour)
	require.NoError(t, err)

	expired, err := repo.Get(ctx, expiredToken)
	require.NoError(t, err)
	require.Nil(t, expired)

	missing, err := repo.Get(ctx, "does-not-exist")
	require.NoError(t, err)
	require.Nil(t, missing)

	require.NoError(t, repo.MarkUsed(ctx, token))
	used, err := repo.Get(ctx, token)
	require.NoError(t, err)
	require.Nil(t, used)
}
