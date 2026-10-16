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
			id         TEXT PRIMARY KEY,
			server_id  TEXT NOT NULL,
			name       TEXT NOT NULL,
			type       TEXT NOT NULL,
			deleted_at BIGINT
		);
		ALTER TABLE channels ADD COLUMN IF NOT EXISTS deleted_at BIGINT;
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
			created_at BIGINT NOT NULL,
			deleted_at BIGINT,
			deleted_by TEXT
		);
		ALTER TABLE messages ADD COLUMN IF NOT EXISTS deleted_at BIGINT;
		ALTER TABLE messages ADD COLUMN IF NOT EXISTS deleted_by TEXT;
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
		CREATE TABLE IF NOT EXISTS attachments (
			id           TEXT PRIMARY KEY,
			hash         TEXT NOT NULL,
			server_id    TEXT NOT NULL,
			user_id      TEXT NOT NULL,
			format       TEXT NOT NULL,
			thumb_format TEXT NOT NULL,
			mime         TEXT NOT NULL,
			size         BIGINT NOT NULL,
			width        INT NOT NULL,
			height       INT NOT NULL,
			created_at   BIGINT NOT NULL
		);
		ALTER TABLE messages ADD COLUMN IF NOT EXISTS attachment_id TEXT;
		ALTER TABLE messages ADD COLUMN IF NOT EXISTS encrypted BOOLEAN NOT NULL DEFAULT FALSE;
		ALTER TABLE users ADD COLUMN IF NOT EXISTS avatar_id TEXT;
		ALTER TABLE users ADD COLUMN IF NOT EXISTS is_bot BOOLEAN NOT NULL DEFAULT FALSE;
		ALTER TABLE invites ADD COLUMN IF NOT EXISTS bot BOOLEAN NOT NULL DEFAULT FALSE;
		ALTER TABLE servers ADD COLUMN IF NOT EXISTS logo_id TEXT;
		ALTER TABLE attachments ADD COLUMN IF NOT EXISTS encrypted BOOLEAN NOT NULL DEFAULT FALSE;
		ALTER TABLE attachments ADD COLUMN IF NOT EXISTS thumb_size BIGINT NOT NULL DEFAULT 0;

		CREATE TABLE IF NOT EXISTS mls_devices (
			id          TEXT PRIMARY KEY,
			user_id     TEXT NOT NULL,
			signing_key TEXT NOT NULL,
			created_at  BIGINT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS mls_key_packages (
			id          TEXT PRIMARY KEY,
			device_id   TEXT NOT NULL,
			user_id     TEXT NOT NULL,
			payload     TEXT NOT NULL,
			created_at  BIGINT NOT NULL,
			consumed_at BIGINT
		);
		CREATE TABLE IF NOT EXISTS mls_groups (
			server_id TEXT PRIMARY KEY,
			epoch     BIGINT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS mls_members (
			server_id TEXT NOT NULL,
			device_id TEXT NOT NULL,
			user_id   TEXT NOT NULL,
			PRIMARY KEY (server_id, device_id)
		);
		CREATE TABLE IF NOT EXISTS mls_commits (
			server_id        TEXT NOT NULL,
			epoch            BIGINT NOT NULL,
			sender_device_id TEXT NOT NULL,
			payload          TEXT NOT NULL,
			created_at       BIGINT NOT NULL,
			PRIMARY KEY (server_id, epoch)
		);
		CREATE TABLE IF NOT EXISTS mls_welcomes (
			id          TEXT PRIMARY KEY,
			server_id   TEXT NOT NULL,
			device_id   TEXT NOT NULL,
			epoch       BIGINT NOT NULL,
			payload     TEXT NOT NULL,
			created_at  BIGINT NOT NULL,
			consumed_at BIGINT
		);

		CREATE INDEX IF NOT EXISTS idx_messages_channel
			ON messages(server_id, channel_id, created_at DESC);
		CREATE INDEX IF NOT EXISTS idx_memberships_user
			ON memberships(user_id);
		CREATE INDEX IF NOT EXISTS idx_channels_server
			ON channels(server_id);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_channels_unique_name
			ON channels(server_id, type, lower(name))
			WHERE deleted_at IS NULL;
		CREATE INDEX IF NOT EXISTS idx_invites_expires
			ON invites(expires_at);
		CREATE INDEX IF NOT EXISTS idx_attachments_hash
			ON attachments(hash);
		CREATE INDEX IF NOT EXISTS idx_attachments_user
			ON attachments(user_id);
		CREATE INDEX IF NOT EXISTS idx_mls_devices_user
			ON mls_devices(user_id);
		CREATE INDEX IF NOT EXISTS idx_mls_key_packages_device
			ON mls_key_packages(device_id) WHERE consumed_at IS NULL;
		CREATE INDEX IF NOT EXISTS idx_mls_members_user
			ON mls_members(server_id, user_id);
		CREATE INDEX IF NOT EXISTS idx_mls_welcomes_device
			ON mls_welcomes(server_id, device_id) WHERE consumed_at IS NULL;
	`)
	return err
}
