package signal

import "github.com/pion/webrtc/v3"

// Message is the single envelope type exchanged over the WebSocket connection.
// Not every field is used by every Type; handlers read only the fields relevant
// to the message Type they're processing.
type Message struct {
	Type string `json:"type"`

	// auth
	Token string `json:"token,omitempty"`
	Name  string `json:"name,omitempty"`

	// server / channel scoping
	ServerID  string `json:"serverId,omitempty"`
	ChannelID string `json:"channelId,omitempty"`

	// text-message / create-server
	Content string `json:"content,omitempty"`

	// join-server
	InviteToken string `json:"inviteToken,omitempty"`

	// kick-voice / kick-server
	TargetUserID string `json:"targetUserId,omitempty"`

	// WebRTC signaling (join-voice / answer / candidate)
	SDP       *webrtc.SessionDescription `json:"sdp,omitempty"`
	Candidate *webrtc.ICECandidateInit   `json:"candidate,omitempty"`
}
