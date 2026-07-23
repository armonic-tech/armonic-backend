package repositories

import "context"

type MembershipRepo struct {
	db DBTX
}

func NewMembershipRepo(db DBTX) *MembershipRepo {
	return &MembershipRepo{db: db}
}

func (r *MembershipRepo) Add(ctx context.Context, userID, serverID string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO memberships (user_id, server_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		userID, serverID,
	)
	return err
}

func (r *MembershipRepo) Remove(ctx context.Context, userID, serverID string) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM memberships WHERE user_id = $1 AND server_id = $2`,
		userID, serverID,
	)
	return err
}

func (r *MembershipRepo) IsMember(ctx context.Context, userID, serverID string) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM memberships WHERE user_id = $1 AND server_id = $2)`,
		userID, serverID,
	).Scan(&exists)
	return exists, err
}

func (r *MembershipRepo) IsMemberByChannel(ctx context.Context, userID, channelID string) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM channels c
			JOIN memberships m ON m.server_id = c.server_id
			WHERE c.id = $1 AND m.user_id = $2
		)`,
		channelID, userID,
	).Scan(&exists)
	return exists, err
}

func (r *MembershipRepo) GetByUser(ctx context.Context, userID string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT server_id FROM memberships WHERE user_id = $1`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var serverIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		serverIDs = append(serverIDs, id)
	}
	return serverIDs, rows.Err()
}

// CountByServer is used by handlers.MemberCounter (wrapped to bind a specific
// server ID) to report the member count for the /info endpoint.
func (r *MembershipRepo) CountByServer(ctx context.Context, serverID string) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memberships WHERE server_id = $1`,
		serverID,
	).Scan(&count)
	return count, err
}
