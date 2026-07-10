package repositories

import (
	"database/sql"
	"fmt"
	"log/slog"

	repositories "github.com/armonic-tech/armonic-backend/migrations"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type Repositories struct {
	msgRepo        *MessageRepo
	svRepo         *ServerRepo
	chRepo         *ChannelRepo
	userRepo       *UserRepo
	membershipRepo *MembershipRepo
	inviteRepo     *InviteRepo
	settingsRepo   *SettingsRepo
}

func InitRepositories(db DBTX) *Repositories {
	return &Repositories{
		msgRepo:        NewMessageRepo(db),
		svRepo:         NewServerRepo(db),
		chRepo:         NewChannelRepo(db),
		userRepo:       NewUserRepo(db),
		membershipRepo: NewMembershipRepo(db),
		inviteRepo:     NewInviteRepo(db),
		settingsRepo:   NewSettingsRepo(db),
	}
}

func (r *Repositories) Messages() *MessageRepo       { return r.msgRepo }
func (r *Repositories) Servers() *ServerRepo         { return r.svRepo }
func (r *Repositories) Channels() *ChannelRepo       { return r.chRepo }
func (r *Repositories) Users() *UserRepo             { return r.userRepo }
func (r *Repositories) Memberships() *MembershipRepo { return r.membershipRepo }
func (r *Repositories) Invites() *InviteRepo         { return r.inviteRepo }
func (r *Repositories) Settings() *SettingsRepo      { return r.settingsRepo }

func NewDB(dsn string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)

	if err := repositories.Migrate(db); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	slog.Info("database connected and migrated")
	return db, nil
}
