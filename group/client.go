package group

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash"

	"golang.org/x/crypto/pbkdf2"

	"github.com/jech/galene/conn"
)

type RawPassword struct {
	Type       string `json:"type,omitempty"`
	Hash       string `json:"hash,omitempty"`
	Key        string `json:"key"`
	Salt       string `json:"salt,omitempty"`
	Iterations int    `json:"iterations,omitempty"`
}

type Password RawPassword

func (p Password) Match(pw string) (bool, error) {
	switch p.Type {
	case "":
		return p.Key == pw, nil
	case "pbkdf2":
		key, err := hex.DecodeString(p.Key)
		if err != nil {
			return false, err
		}
		salt, err := hex.DecodeString(p.Salt)
		if err != nil {
			return false, err
		}
		var h func() hash.Hash
		switch p.Hash {
		case "sha-256":
			h = sha256.New
		default:
			return false, errors.New("unknown hash type")
		}
		theirKey := pbkdf2.Key(
			[]byte(pw), salt, p.Iterations, len(key), h,
		)
		return bytes.Compare(key, theirKey) == 0, nil
	default:
		return false, errors.New("unknown password type")
	}
}

func (p *Password) UnmarshalJSON(b []byte) error {
	var k string
	err := json.Unmarshal(b, &k)
	if err == nil {
		*p = Password{
			Key: k,
		}
		return nil
	}
	var r RawPassword
	err = json.Unmarshal(b, &r)
	if err == nil {
		*p = Password(r)
	}
	return err
}

func (p Password) MarshalJSON() ([]byte, error) {
	if p.Type == "" && p.Hash == "" && p.Salt == "" && p.Iterations == 0 {
		return json.Marshal(p.Key)
	}
	return json.Marshal(RawPassword(p))
}

type ClientCredentials struct {
	Username string    `json:"username,omitempty"`
	Password *Password `json:"password,omitempty"`
}

type ClientPermissions struct {
	Op      bool `json:"op,omitempty"`
	Present bool `json:"present,omitempty"`
	Record  bool `json:"record,omitempty"`
}

type Challengeable interface {
	Username() string
	Challenge(string, ClientCredentials) bool
}

type Client interface {
	Group() *Group
	Id() string
	Challengeable
	Permissions() ClientPermissions
	SetPermissions(ClientPermissions)
	OverridePermissions(*Group) bool
	PushConn(g *Group, id string, conn conn.Up, tracks []conn.UpTrack, replace string) error
	PushClient(id, username string, permissions ClientPermissions, kind string) error
	Kick(id, user, message string) error
}
