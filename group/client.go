package group

import (
	"sfu/conn"
)

type ClientCredentials struct {
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

type ClientPermissions struct {
	Op      bool `json:"op,omitempty"`
	Present bool `json:"present,omitempty"`
	Record  bool `json:"record,omitempty"`
}

type Client interface {
	Group() *Group
	Id() string
	Credentials() ClientCredentials
	SetPermissions(ClientPermissions)
	PushConn(id string, conn conn.Up, tracks []conn.UpTrack, label string) error
	PushClient(id, username string, add bool) error
}

type Kickable interface {
	Kick(message string) error
}
