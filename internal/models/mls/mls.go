// Package mls holds the wire types of the MLS Delivery Service the backend
// exposes. The server never parses the cryptographic payloads: commits,
// welcomes and key packages are opaque strings produced and consumed by the
// clients. See docs/ai/e2ee.md.
package mls

import "time"

type Device struct {
	ID         string    `json:"id"`
	UserID     string    `json:"userId"`
	SigningKey string    `json:"signingKey"`
	CreatedAt  time.Time `json:"createdAt"`
}

type KeyPackage struct {
	ID       string `json:"id"`
	DeviceID string `json:"deviceId"`
	UserID   string `json:"userId"`
	Payload  string `json:"payload"`
}

type Member struct {
	DeviceID string `json:"deviceId"`
	UserID   string `json:"userId"`
}

// PendingAdd is a member device that is not in the group yet and has an
// unconsumed key package the next committer can add it with.
type PendingAdd struct {
	DeviceID     string `json:"deviceId"`
	UserID       string `json:"userId"`
	KeyPackageID string `json:"keyPackageId"`
	KeyPackage   string `json:"keyPackage"`
}

type Commit struct {
	Epoch          int64     `json:"epoch"`
	SenderDeviceID string    `json:"senderDeviceId"`
	Payload        string    `json:"payload"`
	CreatedAt      time.Time `json:"createdAt"`
}

type Welcome struct {
	ID        string    `json:"id"`
	Epoch     int64     `json:"epoch"`
	Payload   string    `json:"payload"`
	CreatedAt time.Time `json:"createdAt"`
}

type AddRequest struct {
	DeviceID     string `json:"deviceId"`
	KeyPackageID string `json:"keyPackageId"`
}

// CommitRequest is what a client posts to advance the group one epoch.
// Epoch is the epoch the commit was built against; the server only accepts
// it when that is still the current one.
type CommitRequest struct {
	DeviceID string            `json:"deviceId"`
	Epoch    int64             `json:"epoch"`
	Payload  string            `json:"payload"`
	Adds     []AddRequest      `json:"adds"`
	Removes  []string          `json:"removes"`
	Welcomes map[string]string `json:"welcomes"`
}

type GroupState struct {
	Epoch   int64    `json:"epoch"`
	Members []Member `json:"members"`
}
