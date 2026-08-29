package server

type ServerInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	OwnerID string `json:"ownerId,omitempty"`
}

type MemberInfo struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	AvatarID    string `json:"avatarId,omitempty"`
	AvatarURL   string `json:"avatarUrl,omitempty"`
	IsOwner     bool   `json:"isOwner"`
	Online      bool   `json:"online"`
}
