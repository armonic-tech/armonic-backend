package repositories

import (
	"database/sql"
)

func Migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id            TEXT PRIMARY KEY,
			display_name  TEXT NOT NULL DEFAULT '',
			username      TEXT NOT NULL DEFAULT '',
			password_hash TEXT NOT NULL DEFAULT ''
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_users_username
			ON users(username) WHERE username <> '';
		CREATE TABLE IF NOT EXISTS servers (
			id         TEXT PRIMARY KEY,
			name       TEXT NOT NULL,
			owner_id   TEXT,
			created_at BIGINT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS channels (
			id        TEXT PRIMARY KEY,
			server_id TEXT NOT NULL,
			name      TEXT NOT NULL,
			type      TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS memberships (
			user_id   TEXT NOT NULL,
			server_id TEXT NOT NULL,
			PRIMARY KEY (user_id, server_id)
		);
		CREATE TABLE IF NOT EXISTS messages (
			id         TEXT PRIMARY KEY,
			server_id  TEXT NOT NULL,
			channel_id TEXT NOT NULL,
			user_id    TEXT NOT NULL,
			content    TEXT NOT NULL,
			created_at BIGINT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS invites (
			token      TEXT PRIMARY KEY,
			server_id  TEXT NOT NULL,
			created_by TEXT NOT NULL,
			expires_at BIGINT NOT NULL,
			used_at    BIGINT
		);
		ALTER TABLE invites ADD COLUMN IF NOT EXISTS used_at BIGINT;
		CREATE TABLE IF NOT EXISTS settings (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);

		CREATE INDEX IF NOT EXISTS idx_messages_channel
			ON messages(server_id, channel_id, created_at DESC);
		CREATE INDEX IF NOT EXISTS idx_memberships_user
			ON memberships(user_id);
		CREATE INDEX IF NOT EXISTS idx_channels_server
			ON channels(server_id);
		CREATE INDEX IF NOT EXISTS idx_invites_expires
			ON invites(expires_at);
	`)
	return err
}
