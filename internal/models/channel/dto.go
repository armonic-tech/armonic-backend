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
}
