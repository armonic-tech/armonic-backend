package server

type ServerInfo struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	OwnerID      string `json:"ownerId,omitempty"`
	LogoID       string `json:"logoId,omitempty"`
	LogoURL      string `json:"logoUrl,omitempty"`
	LogoThumbURL string `json:"logoThumbUrl,omitempty"`
}

type MemberInfo struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	AvatarID    string `json:"avatarId,omitempty"`
	AvatarURL   string `json:"avatarUrl,omitempty"`
	IsOwner     bool   `json:"isOwner"`
	Online      bool   `json:"online"`
}

// LogoURLFor is the public path a client fetches a server's logo from. The
// attachment id rides along as ?v= so a changed logo cannot be served from a
// cache keyed on the stable path.
func LogoURLFor(serverID, logoID string) string {
	return "/server/" + serverID + "/logo?v=" + logoID
}

func LogoThumbURLFor(serverID, logoID string) string {
	return "/server/" + serverID + "/logo/thumb?v=" + logoID
}
