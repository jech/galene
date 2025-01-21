package group

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash"
	"net"
	"runtime"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/pbkdf2"

	"github.com/jech/galene/conn"
)

type RawPassword struct {
	Type       string  `json:"type,omitempty"`
	Hash       string  `json:"hash,omitempty"`
	Key        *string `json:"key,omitempty"`
	Salt       string  `json:"salt,omitempty"`
	Iterations int     `json:"iterations,omitempty"`
}

type Password RawPassword

// limit the number of concurrent hashing operations.  This avoids running
// out of memory when doing too many BCrypt hashes at the same time.
var hashSemaphore = make(chan struct{}, runtime.GOMAXPROCS(-1))

// constantTimeCompare compares a and b in time proportional to the length of a.
func constantTimeCompare(a, b string) bool {
	as := []byte(a)
	bs := make([]byte, len(as))
	copy(bs, b)
	equal := subtle.ConstantTimeCompare(as, bs) == 1
	return len(a) == len(b) && equal
}

func (p Password) Match(pw string) (bool, error) {
	switch p.Type {
	case "":
		return false, nil
	case "plain":
		if p.Key == nil {
			return false, errors.New("missing key")
		}
		return constantTimeCompare(pw, *p.Key), nil
	case "wildcard":
		return true, nil
	case "pbkdf2":
		if p.Key == nil {
			return false, errors.New("missing key")
		}
		key, err := hex.DecodeString(*p.Key)
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
		hashSemaphore <- struct{}{}
		defer func() {
			<-hashSemaphore
		}()
		theirKey := pbkdf2.Key(
			[]byte(pw), salt, p.Iterations, len(key), h,
		)
		return bytes.Equal(key, theirKey), nil
	case "bcrypt":
		if p.Key == nil {
			return false, errors.New("missing key")
		}
		hashSemaphore <- struct{}{}
		defer func() {
			<-hashSemaphore
		}()
		err := bcrypt.CompareHashAndPassword([]byte(*p.Key), []byte(pw))
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return false, nil
		}
		return err == nil, err
	default:
		return false, errors.New("unknown password type")
	}
}

func (p *Password) UnmarshalJSON(b []byte) error {
	var k string
	err := json.Unmarshal(b, &k)
	if err == nil {
		*p = Password{
			Type: "plain",
			Key:  &k,
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
	if p.Type == "plain" && p.Hash == "" && p.Salt == "" && p.Iterations == 0 {
		return json.Marshal(p.Key)
	}
	return json.Marshal(RawPassword(p))
}

type ClientPattern struct {
	Username string    `json:"username,omitempty"`
	Password *Password `json:"password,omitempty"`
}

type ClientCredentials struct {
	System   bool
	Username *string
	Password string
	Token    string
}

type Client interface {
	Group() *Group
	Addr() net.Addr
	Id() string
	Username() string
	SetUsername(string)
	Permissions() []string
	SetPermissions([]string)
	Data() map[string]interface{}
	PushConn(g *Group, id string, conn conn.Up, tracks []conn.UpTrack, replace string) error
	RequestConns(target Client, g *Group, id string) error
	Joined(group, kind string) error
	PushClient(group, kind, id, username string, perms []string, data map[string]interface{}) error
	Kick(id string, user *string, message string) error
}
