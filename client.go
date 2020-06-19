package main

type client interface {
	Group() *group
	Id() string
	Username() string
	pushConn(id string, conn upConnection, tracks []upTrack, label string) error
	pushClient(id, username string, add bool) error
}
