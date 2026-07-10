package invite

import "time"

type Invite struct {
	Token     string    `json:"token"`
	ServerID  string    `json:"serverId"`
	CreatedBy string    `json:"createdBy"`
	ExpiresAt time.Time `json:"expiresAt"`
}
