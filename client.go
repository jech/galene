package main

type clientCredentials struct {
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

type client interface {
	Group() *group
	Id() string
	Credentials() clientCredentials
	pushConn(id string, conn upConnection, tracks []upTrack, label string) error
	pushClient(id, username string, add bool) error
}
