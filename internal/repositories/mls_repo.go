package repositories

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/armonic-tech/armonic-backend/internal/models/mls"
	"github.com/google/uuid"
)

var (
	ErrStaleEpoch     = errors.New("mls: commit built against a stale epoch")
	ErrNotInGroup     = errors.New("mls: sender device is not in the group")
	ErrInvalidAdd     = errors.New("mls: add references an unavailable key package")
	ErrInvalidRemove  = errors.New("mls: remove references a device not in the group")
	ErrDeviceTaken    = errors.New("mls: device id registered to another user")
	ErrMissingWelcome = errors.New("mls: every added device needs a welcome")
)

type MLSRepo struct {
	db DBTX
}

func NewMLSRepo(db DBTX) *MLSRepo {
	return &MLSRepo{db: db}
}

type txBeginner interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

func (r *MLSRepo) RegisterDevice(ctx context.Context, id, userID, signingKey string) error {
	var owner string
	err := r.db.QueryRowContext(ctx, `SELECT user_id FROM mls_devices WHERE id = $1`, id).Scan(&owner)
	if err == nil {
		if owner != userID {
			return ErrDeviceTaken
		}
		_, err = r.db.ExecContext(ctx, `UPDATE mls_devices SET signing_key = $1 WHERE id = $2`, signingKey, id)
		return err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = r.db.ExecContext(ctx,
		`INSERT INTO mls_devices (id, user_id, signing_key, created_at) VALUES ($1, $2, $3, $4)`,
		id, userID, signingKey, time.Now().Unix(),
	)
	return err
}

func (r *MLSRepo) GetDevice(ctx context.Context, id string) (*mls.Device, error) {
	var d mls.Device
	var createdAt int64
	err := r.db.QueryRowContext(ctx,
		`SELECT id, user_id, signing_key, created_at FROM mls_devices WHERE id = $1`, id,
	).Scan(&d.ID, &d.UserID, &d.SigningKey, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	d.CreatedAt = time.Unix(createdAt, 0).UTC()
	return &d, nil
}

func (r *MLSRepo) AddKeyPackages(ctx context.Context, deviceID, userID string, packages []mls.KeyPackage) error {
	now := time.Now().Unix()
	for _, kp := range packages {
		if _, err := r.db.ExecContext(ctx,
			`INSERT INTO mls_key_packages (id, device_id, user_id, payload, created_at)
			 VALUES ($1, $2, $3, $4, $5) ON CONFLICT (id) DO NOTHING`,
			kp.ID, deviceID, userID, kp.Payload, now,
		); err != nil {
			return err
		}
	}
	return nil
}

func (r *MLSRepo) CountKeyPackages(ctx context.Context, deviceID string) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM mls_key_packages WHERE device_id = $1 AND consumed_at IS NULL`, deviceID,
	).Scan(&n)
	return n, err
}

func (r *MLSRepo) GroupState(ctx context.Context, serverID string) (*mls.GroupState, error) {
	st := &mls.GroupState{Members: []mls.Member{}}
	err := r.db.QueryRowContext(ctx, `SELECT epoch FROM mls_groups WHERE server_id = $1`, serverID).Scan(&st.Epoch)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT device_id, user_id FROM mls_members WHERE server_id = $1 ORDER BY device_id`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var m mls.Member
		if err := rows.Scan(&m.DeviceID, &m.UserID); err != nil {
			return nil, err
		}
		st.Members = append(st.Members, m)
	}
	return st, rows.Err()
}

// PendingAdds lists member devices that are not in the group and have a key
// package to be added with — one package per device, the oldest first so a
// device's packages are consumed in publication order.
func (r *MLSRepo) PendingAdds(ctx context.Context, serverID string) ([]mls.PendingAdd, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT DISTINCT ON (d.id) d.id, d.user_id, kp.id, kp.payload
		 FROM memberships m
		 JOIN mls_devices d ON d.user_id = m.user_id
		 JOIN mls_key_packages kp ON kp.device_id = d.id AND kp.consumed_at IS NULL
		 LEFT JOIN mls_members g ON g.server_id = m.server_id AND g.device_id = d.id
		 WHERE m.server_id = $1 AND g.device_id IS NULL
		 ORDER BY d.id, kp.created_at, kp.id`,
		serverID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	adds := []mls.PendingAdd{}
	for rows.Next() {
		var a mls.PendingAdd
		if err := rows.Scan(&a.DeviceID, &a.UserID, &a.KeyPackageID, &a.KeyPackage); err != nil {
			return nil, err
		}
		adds = append(adds, a)
	}
	return adds, rows.Err()
}

// PendingRemoves lists group devices whose user is no longer a member of the
// server, i.e. devices a commit still has to evict cryptographically.
func (r *MLSRepo) PendingRemoves(ctx context.Context, serverID string) ([]mls.Member, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT g.device_id, g.user_id
		 FROM mls_members g
		 LEFT JOIN memberships m ON m.server_id = g.server_id AND m.user_id = g.user_id
		 WHERE g.server_id = $1 AND m.user_id IS NULL
		 ORDER BY g.device_id`,
		serverID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []mls.Member{}
	for rows.Next() {
		var m mls.Member
		if err := rows.Scan(&m.DeviceID, &m.UserID); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Commit applies a client's commit atomically: it must target the current
// epoch, come from a device in the group (or start the group at epoch 0),
// consume one unconsumed key package per add and remove only devices present.
// Returns the new epoch.
func (r *MLSRepo) Commit(ctx context.Context, serverID string, req mls.CommitRequest) (int64, error) {
	if b, ok := r.db.(txBeginner); ok {
		tx, err := b.BeginTx(ctx, nil)
		if err != nil {
			return 0, err
		}
		epoch, err := commitIn(ctx, tx, serverID, req)
		if err != nil {
			_ = tx.Rollback()
			return 0, err
		}
		return epoch, tx.Commit()
	}
	return commitIn(ctx, r.db, serverID, req)
}

func commitIn(ctx context.Context, db DBTX, serverID string, req mls.CommitRequest) (int64, error) {
	if _, err := db.ExecContext(ctx,
		`INSERT INTO mls_groups (server_id, epoch) VALUES ($1, 0) ON CONFLICT DO NOTHING`, serverID); err != nil {
		return 0, err
	}
	var epoch int64
	if err := db.QueryRowContext(ctx,
		`SELECT epoch FROM mls_groups WHERE server_id = $1 FOR UPDATE`, serverID).Scan(&epoch); err != nil {
		return 0, err
	}
	if epoch != req.Epoch {
		return 0, ErrStaleEpoch
	}

	var senderUser string
	err := db.QueryRowContext(ctx,
		`SELECT user_id FROM mls_members WHERE server_id = $1 AND device_id = $2`, serverID, req.DeviceID,
	).Scan(&senderUser)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if epoch != 0 {
			return 0, ErrNotInGroup
		}
		// First commit: the creator seats itself in the group.
		var uid string
		if err := db.QueryRowContext(ctx, `SELECT user_id FROM mls_devices WHERE id = $1`, req.DeviceID).Scan(&uid); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return 0, ErrNotInGroup
			}
			return 0, err
		}
		if _, err := db.ExecContext(ctx,
			`INSERT INTO mls_members (server_id, device_id, user_id) VALUES ($1, $2, $3)`, serverID, req.DeviceID, uid); err != nil {
			return 0, err
		}
	case err != nil:
		return 0, err
	}

	now := time.Now().Unix()
	for _, dev := range req.Removes {
		res, err := db.ExecContext(ctx,
			`DELETE FROM mls_members WHERE server_id = $1 AND device_id = $2`, serverID, dev)
		if err != nil {
			return 0, err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return 0, ErrInvalidRemove
		}
	}
	for _, add := range req.Adds {
		if _, ok := req.Welcomes[add.DeviceID]; !ok {
			return 0, ErrMissingWelcome
		}
		var uid string
		err := db.QueryRowContext(ctx,
			`UPDATE mls_key_packages SET consumed_at = $1
			 WHERE id = $2 AND device_id = $3 AND consumed_at IS NULL
			 RETURNING user_id`,
			now, add.KeyPackageID, add.DeviceID,
		).Scan(&uid)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrInvalidAdd
		}
		if err != nil {
			return 0, err
		}
		var isMember bool
		if err := db.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM memberships WHERE user_id = $1 AND server_id = $2)`, uid, serverID,
		).Scan(&isMember); err != nil {
			return 0, err
		}
		if !isMember {
			return 0, ErrInvalidAdd
		}
		if _, err := db.ExecContext(ctx,
			`INSERT INTO mls_members (server_id, device_id, user_id) VALUES ($1, $2, $3)
			 ON CONFLICT (server_id, device_id) DO NOTHING`,
			serverID, add.DeviceID, uid); err != nil {
			return 0, err
		}
		if _, err := db.ExecContext(ctx,
			`INSERT INTO mls_welcomes (id, server_id, device_id, epoch, payload, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6)`,
			uuid.New().String(), serverID, add.DeviceID, epoch+1, req.Welcomes[add.DeviceID], now); err != nil {
			return 0, err
		}
	}

	if _, err := db.ExecContext(ctx,
		`INSERT INTO mls_commits (server_id, epoch, sender_device_id, payload, created_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		serverID, epoch+1, req.DeviceID, req.Payload, now); err != nil {
		return 0, err
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE mls_groups SET epoch = $1 WHERE server_id = $2`, epoch+1, serverID); err != nil {
		return 0, err
	}
	return epoch + 1, nil
}

func (r *MLSRepo) CommitsAfter(ctx context.Context, serverID string, epoch int64, limit int) ([]mls.Commit, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT epoch, sender_device_id, payload, created_at FROM mls_commits
		 WHERE server_id = $1 AND epoch > $2 ORDER BY epoch LIMIT $3`,
		serverID, epoch, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []mls.Commit{}
	for rows.Next() {
		var c mls.Commit
		var createdAt int64
		if err := rows.Scan(&c.Epoch, &c.SenderDeviceID, &c.Payload, &createdAt); err != nil {
			return nil, err
		}
		c.CreatedAt = time.Unix(createdAt, 0).UTC()
		out = append(out, c)
	}
	return out, rows.Err()
}

// Welcome returns the newest unconsumed welcome addressed to a device, or nil.
func (r *MLSRepo) Welcome(ctx context.Context, serverID, deviceID string) (*mls.Welcome, error) {
	var w mls.Welcome
	var createdAt int64
	err := r.db.QueryRowContext(ctx,
		`SELECT id, epoch, payload, created_at FROM mls_welcomes
		 WHERE server_id = $1 AND device_id = $2 AND consumed_at IS NULL
		 ORDER BY epoch DESC LIMIT 1`,
		serverID, deviceID,
	).Scan(&w.ID, &w.Epoch, &w.Payload, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	w.CreatedAt = time.Unix(createdAt, 0).UTC()
	return &w, nil
}

func (r *MLSRepo) ConsumeWelcome(ctx context.Context, id, deviceID string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE mls_welcomes SET consumed_at = $1 WHERE id = $2 AND device_id = $3 AND consumed_at IS NULL`,
		time.Now().Unix(), id, deviceID)
	return err
}

func (r *MLSRepo) IsInGroup(ctx context.Context, serverID, deviceID string) (bool, error) {
	var ok bool
	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM mls_members WHERE server_id = $1 AND device_id = $2)`, serverID, deviceID,
	).Scan(&ok)
	return ok, err
}

func (r *MLSRepo) String() string { return fmt.Sprintf("MLSRepo(%T)", r.db) }
