package attachment

import "time"

type Attachment struct {
	ID          string    `json:"id"`
	ServerID    string    `json:"serverId"`
	UserID      string    `json:"userId"`
	Hash        string    `json:"-"`
	Format      string    `json:"-"`
	ThumbFormat string    `json:"-"`
	MIME        string    `json:"mime"`
	Size        int64     `json:"size"`
	ThumbSize   int64     `json:"thumbSize,omitempty"`
	Width       int       `json:"width"`
	Height      int       `json:"height"`
	Encrypted   bool      `json:"encrypted,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	URL         string    `json:"url"`
	ThumbURL    string    `json:"thumbUrl"`
}

func URLFor(id string) string      { return "/attachment/" + id }
func ThumbURLFor(id string) string { return "/attachment/" + id + "/thumb" }

func (a *Attachment) WithURLs() *Attachment {
	a.URL = URLFor(a.ID)
	a.ThumbURL = ThumbURLFor(a.ID)
	return a
}
