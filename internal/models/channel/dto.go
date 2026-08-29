package channel

type ChannelInfo struct {
	ID       string `json:"id"`
	ServerID string `json:"serverId"`
	Name     string `json:"name"`
	Type     string `json:"type"`
}

type Member struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	AvatarID    string `json:"avatarId,omitempty"`
	Muted       bool   `json:"muted"`
	Deafened    bool   `json:"deafened"`
}
