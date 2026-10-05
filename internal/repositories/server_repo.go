package repositories

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/armonic-tech/armonic-backend/internal/models/server"
)

type ServerRepo struct {
	db DBTX
}

func NewServerRepo(db DBTX) *ServerRepo {
	return &ServerRepo{db: db}
}

func (r *ServerRepo) Create(ctx context.Context, id string, name string, ownerID string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO servers (id, name, owner_id, created_at) VALUES ($1, $2, $3, $4)`,
		id, name, ownerID, time.Now().Unix(),
	)
	return err
}

func (r *ServerRepo) IsOwner(ctx context.Context, serverID, userID string) (bool, error) {
	var ownerID string
	err := r.db.QueryRowContext(ctx, `SELECT owner_id FROM servers WHERE id = $1`, serverID).Scan(&ownerID)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return ownerID == userID, nil
}

func (r *ServerRepo) GetByIDs(ctx context.Context, ids []string) ([]server.ServerInfo, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	query := "SELECT id, name, COALESCE(owner_id, ''), COALESCE(logo_id, '') FROM servers WHERE id IN (" + strings.Join(placeholders, ",") + ")"
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var servers []server.ServerInfo
	for rows.Next() {
		var s server.ServerInfo
		if err := rows.Scan(&s.ID, &s.Name, &s.OwnerID, &s.LogoID); err != nil {
			return nil, err
		}
		servers = append(servers, s)
	}
	return servers, rows.Err()
}

func (r *ServerRepo) OwnerID(ctx context.Context, serverID string) (string, error) {
	var ownerID sql.NullString
	err := r.db.QueryRowContext(ctx, `SELECT owner_id FROM servers WHERE id = $1`, serverID).Scan(&ownerID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return ownerID.String, err
}

func (r *ServerRepo) SetOwner(ctx context.Context, serverID, ownerID string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE servers SET owner_id = $1 WHERE id = $2`,
		ownerID, serverID,
	)
	return err
}

func (r *ServerRepo) SetLogo(ctx context.Context, serverID, attachmentID string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE servers SET logo_id = $1 WHERE id = $2`,
		attachmentID, serverID,
	)
	return err
}

// ClearLogo leaves the server without a logo. The attachment row and its
// blob stay — nothing frees upload quota yet (docs/ai/uploads.md).
func (r *ServerRepo) ClearLogo(ctx context.Context, serverID string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE servers SET logo_id = NULL WHERE id = $1`, serverID)
	return err
}

func (r *ServerRepo) LogoID(ctx context.Context, serverID string) (string, error) {
	var logo sql.NullString
	err := r.db.QueryRowContext(ctx, `SELECT logo_id FROM servers WHERE id = $1`, serverID).Scan(&logo)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return logo.String, err
}
