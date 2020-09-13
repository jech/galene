package main

type clientCredentials struct {
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

type clientPermissions struct {
	Op      bool `json:"op,omitempty"`
	Present bool `json:"present,omitempty"`
	Record  bool `json:"record,omitempty"`
}

type client interface {
	Group() *group
	Id() string
	Credentials() clientCredentials
	SetPermissions(clientPermissions)
	pushConn(id string, conn upConnection, tracks []upTrack, label string) error
	pushClient(id, username string, add bool) error
}

type kickable interface {
	kick(message string) error
}
