package repositories

import (
	"context"
	"testing"

	"github.com/armonic-tech/armonic-backend/internal/models/mls"
	"github.com/stretchr/testify/require"
)

func seedMLS(t *testing.T) (*MLSRepo, *MembershipRepo, context.Context, func()) {
	t.Helper()
	tx, err := testDB.Begin()
	require.NoError(t, err)
	ctx := context.Background()
	mr := NewMembershipRepo(tx)
	r := NewMLSRepo(tx)
	for _, u := range []string{"alice", "bob", "carol"} {
		require.NoError(t, NewUserRepo(tx).Upsert(ctx, u, u))
		require.NoError(t, mr.Add(ctx, u, "srv"))
		require.NoError(t, r.RegisterDevice(ctx, "dev-"+u, u, "sig-"+u))
		require.NoError(t, r.AddKeyPackages(ctx, "dev-"+u, u, []mls.KeyPackage{{ID: "kp-" + u, Payload: "kp"}}))
	}
	return r, mr, ctx, func() { _ = tx.Rollback() }
}

func TestMLSRepo_DeviceOwnership(t *testing.T) {
	r, _, ctx, done := seedMLS(t)
	defer done()

	require.NoError(t, r.RegisterDevice(ctx, "dev-alice", "alice", "sig-2"))
	require.ErrorIs(t, r.RegisterDevice(ctx, "dev-alice", "bob", "sig"), ErrDeviceTaken)
	d, err := r.GetDevice(ctx, "dev-alice")
	require.NoError(t, err)
	require.Equal(t, "sig-2", d.SigningKey)
	missing, err := r.GetDevice(ctx, "nope")
	require.NoError(t, err)
	require.Nil(t, missing)
}

func TestMLSRepo_GroupLifecycle(t *testing.T) {
	r, mr, ctx, done := seedMLS(t)
	defer done()

	st, err := r.GroupState(ctx, "srv")
	require.NoError(t, err)
	require.Zero(t, st.Epoch)
	require.Empty(t, st.Members)

	adds, err := r.PendingAdds(ctx, "srv")
	require.NoError(t, err)
	require.Len(t, adds, 3)

	// Alice starts the group, adding bob with his key package.
	epoch, err := r.Commit(ctx, "srv", mls.CommitRequest{
		DeviceID: "dev-alice", Epoch: 0, Payload: "c1",
		Adds:     []mls.AddRequest{{DeviceID: "dev-bob", KeyPackageID: "kp-bob"}},
		Welcomes: map[string]string{"dev-bob": "welcome-bob"},
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), epoch)

	st, err = r.GroupState(ctx, "srv")
	require.NoError(t, err)
	require.Equal(t, int64(1), st.Epoch)
	require.Len(t, st.Members, 2)

	// Bob's package is consumed; carol is the only one left to add.
	adds, err = r.PendingAdds(ctx, "srv")
	require.NoError(t, err)
	require.Len(t, adds, 1)
	require.Equal(t, "dev-carol", adds[0].DeviceID)
	n, err := r.CountKeyPackages(ctx, "dev-bob")
	require.NoError(t, err)
	require.Zero(t, n)

	w, err := r.Welcome(ctx, "srv", "dev-bob")
	require.NoError(t, err)
	require.Equal(t, "welcome-bob", w.Payload)
	require.Equal(t, int64(1), w.Epoch)
	require.NoError(t, r.ConsumeWelcome(ctx, w.ID, "dev-bob"))
	w, err = r.Welcome(ctx, "srv", "dev-bob")
	require.NoError(t, err)
	require.Nil(t, w)

	// A commit against the old epoch is rejected without side effects.
	_, err = r.Commit(ctx, "srv", mls.CommitRequest{DeviceID: "dev-bob", Epoch: 0, Payload: "stale"})
	require.ErrorIs(t, err, ErrStaleEpoch)

	// Carol is not in the group, so she cannot commit.
	_, err = r.Commit(ctx, "srv", mls.CommitRequest{DeviceID: "dev-carol", Epoch: 1, Payload: "x"})
	require.ErrorIs(t, err, ErrNotInGroup)

	// Reusing a consumed key package fails.
	_, err = r.Commit(ctx, "srv", mls.CommitRequest{
		DeviceID: "dev-alice", Epoch: 1, Payload: "x",
		Adds: []mls.AddRequest{{DeviceID: "dev-bob", KeyPackageID: "kp-bob"}}, Welcomes: map[string]string{"dev-bob": "w"},
	})
	require.ErrorIs(t, err, ErrInvalidAdd)

	// Adding without a welcome is refused.
	_, err = r.Commit(ctx, "srv", mls.CommitRequest{
		DeviceID: "dev-alice", Epoch: 1, Payload: "x",
		Adds: []mls.AddRequest{{DeviceID: "dev-carol", KeyPackageID: "kp-carol"}},
	})
	require.ErrorIs(t, err, ErrMissingWelcome)

	// Kick bob: he shows up as a pending remove until a commit evicts him.
	require.NoError(t, mr.Remove(ctx, "bob", "srv"))
	removes, err := r.PendingRemoves(ctx, "srv")
	require.NoError(t, err)
	require.Len(t, removes, 1)
	require.Equal(t, "dev-bob", removes[0].DeviceID)

	epoch, err = r.Commit(ctx, "srv", mls.CommitRequest{
		DeviceID: "dev-alice", Epoch: 1, Payload: "c2", Removes: []string{"dev-bob"},
		Adds: []mls.AddRequest{{DeviceID: "dev-carol", KeyPackageID: "kp-carol"}}, Welcomes: map[string]string{"dev-carol": "w"},
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), epoch)

	removes, err = r.PendingRemoves(ctx, "srv")
	require.NoError(t, err)
	require.Empty(t, removes)
	_, err = r.Commit(ctx, "srv", mls.CommitRequest{DeviceID: "dev-alice", Epoch: 2, Payload: "x", Removes: []string{"dev-bob"}})
	require.ErrorIs(t, err, ErrInvalidRemove)

	commits, err := r.CommitsAfter(ctx, "srv", 0, 10)
	require.NoError(t, err)
	require.Len(t, commits, 2)
	require.Equal(t, []string{"c1", "c2"}, []string{commits[0].Payload, commits[1].Payload})
	commits, err = r.CommitsAfter(ctx, "srv", 1, 10)
	require.NoError(t, err)
	require.Len(t, commits, 1)
	require.Equal(t, int64(2), commits[0].Epoch)

	inGroup, err := r.IsInGroup(ctx, "srv", "dev-bob")
	require.NoError(t, err)
	require.False(t, inGroup)
}

func TestMLSRepo_AddRequiresMembership(t *testing.T) {
	r, mr, ctx, done := seedMLS(t)
	defer done()

	// Carol has a key package but was removed from the server: she must not
	// be addable, or a kicked user could be smuggled back in.
	require.NoError(t, mr.Remove(ctx, "carol", "srv"))
	_, err := r.Commit(ctx, "srv", mls.CommitRequest{
		DeviceID: "dev-alice", Epoch: 0, Payload: "x",
		Adds: []mls.AddRequest{{DeviceID: "dev-carol", KeyPackageID: "kp-carol"}}, Welcomes: map[string]string{"dev-carol": "w"},
	})
	require.ErrorIs(t, err, ErrInvalidAdd)
}

func TestAttachmentRepo_StorageUsedSumsBlobsAndThumbs(t *testing.T) {
	tx, err := testDB.Begin()
	require.NoError(t, err)
	defer tx.Rollback()
	ctx := context.Background()
	r := NewAttachmentRepo(tx)

	used, err := r.StorageUsed(ctx, "nobody")
	require.NoError(t, err)
	require.Zero(t, used)

	for i, size := range []int64{100, 250} {
		a := sampleAttachment("att-quota-" + string(rune('a'+i)))
		a.UserID, a.Size, a.ThumbSize = "quota-user", size, 10
		require.NoError(t, r.Create(ctx, a))
	}
	used, err = r.StorageUsed(ctx, "quota-user")
	require.NoError(t, err)
	require.Equal(t, int64(370), used)

	a, err := r.GetByID(ctx, "att-quota-a")
	require.NoError(t, err)
	require.Equal(t, int64(10), a.ThumbSize)
	require.False(t, a.Encrypted)
}
