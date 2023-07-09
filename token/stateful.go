package token

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// A stateful token
type Stateful struct {
	Token       string     `json:"token"`
	Group       string     `json:"group"`
	Username    *string    `json:"username,omitempty"`
	Permissions []string   `json:"permissions"`
	Expires     *time.Time `json:"expires"`
	NotBefore   *time.Time `json:"not-before,omitempty"`
	IssuedAt    *time.Time `json:"issuedAt,omitempty"`
	IssuedBy    *string    `json:"issuedBy,omitempty"`
}

func (token *Stateful) Clone() *Stateful {
	return &Stateful{
		Token:       token.Token,
		Group:       token.Group,
		Username:    token.Username,
		Permissions: append([]string(nil), token.Permissions...),
		Expires:     token.Expires,
		NotBefore:   token.NotBefore,
		IssuedAt:    token.IssuedAt,
		IssuedBy:    token.IssuedBy,
	}
}

// A set of stateful tokens, kept in sync with a JSONL representation in
// a file.  The synchronisation is slightly racy, so both reading and
// modifying tokens are protected by a mutex.
type state struct {
	filename string
	mu       sync.Mutex
	fileSize int64
	modTime  time.Time
	tokens   map[string]*Stateful
}

var tokens state

func SetStatefulFilename(filename string) {
	tokens.mu.Lock()
	defer tokens.mu.Unlock()
	tokens.filename = filename
	tokens.fileSize = 0
	tokens.modTime = time.Time{}
}

func getStateful(token string) (*Stateful, error) {
	tokens.mu.Lock()
	defer tokens.mu.Unlock()
	err := tokens.load()
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if tokens.tokens == nil {
		return nil, nil
	}
	return tokens.tokens[token], nil
}

func (token *Stateful) Check(host, group string, username *string) (string, []string, error) {
	if token.Group == "" || group != token.Group {
		return "", nil, errors.New("token for bad group")
	}
	now := time.Now()
	if token.Expires == nil || now.After(*token.Expires) {
		return "", nil, errors.New("token has expired")
	}
	if token.NotBefore != nil && now.Before(*token.NotBefore) {
		return "", nil, errors.New("token is in the future")
	}

	// the username from the token overrides the one from the client.
	user := ""
	if token.Username != nil {
		user = *token.Username
	} else if username == nil {
		return "", nil, ErrUsernameRequired
	}

	return user, token.Permissions, nil
}

// called locked
func (state *state) load() error {
	if state.filename == "" {
		state.modTime = time.Time{}
		state.tokens = nil
		return nil
	}

	fi, err := os.Stat(state.filename)
	if err != nil {
		state.modTime = time.Time{}
		state.fileSize = 0
		state.tokens = nil
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	if state.modTime.Equal(fi.ModTime()) &&
		state.fileSize == fi.Size() {
		return nil
	}

	f, err := os.Open(state.filename)
	if err != nil {
		state.modTime = time.Time{}
		state.fileSize = 0
		state.tokens = nil
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	defer f.Close()

	ts := make(map[string]*Stateful)
	decoder := json.NewDecoder(f)
	for {
		var t Stateful
		err := decoder.Decode(&t)
		if err == io.EOF {
			break
		} else if err != nil {
			state.modTime = time.Time{}
			state.fileSize = 0
			return err
		}
		ts[t.Token] = &t
	}
	state.tokens = ts
	fi, err = f.Stat()
	if err != nil {
		state.modTime = time.Time{}
		state.fileSize = 0
		state.tokens = nil
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	state.modTime = fi.ModTime()
	state.fileSize = fi.Size()
	return nil
}

func (state *state) Add(token *Stateful) (*Stateful, error) {
	tokens.mu.Lock()
	defer tokens.mu.Unlock()

	if state.filename == "" {
		return nil, os.ErrNotExist
	}

	err := state.load()
	if err != nil {
		return nil, err
	}

	if state.tokens != nil {
		if _, ok := state.tokens[token.Token]; ok {
			return nil, os.ErrExist
		}
	}

	err = os.MkdirAll(filepath.Dir(state.filename), 0700)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(state.filename,
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600,
	)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	encoder := json.NewEncoder(f)
	err = encoder.Encode(token)
	if err != nil {
		return nil, err
	}

	if state.tokens == nil {
		state.tokens = make(map[string]*Stateful)
	}
	state.tokens[token.Token] = token.Clone()

	fi, err := f.Stat()
	if err != nil {
		state.modTime = fi.ModTime()
		state.fileSize = fi.Size()
	}
	return token, nil
}

func Add(token *Stateful) (*Stateful, error) {
	return tokens.Add(token)
}

func Edit(group, token string, expires time.Time) (*Stateful, error) {
	return tokens.Edit(group, token, expires)
}

func (state *state) Edit(group, token string, expires time.Time) (*Stateful, error) {
	tokens.mu.Lock()
	defer tokens.mu.Unlock()
	return state.edit(group, token, expires)
}

// called locked
func (state *state) edit(group, token string, expires time.Time) (*Stateful, error) {
	err := state.load()
	if err != nil {
		return nil, err
	}

	if state.tokens == nil {
		return nil, os.ErrNotExist
	}

	old := state.tokens[token]
	if old == nil {
		return nil, os.ErrNotExist
	}
	if old.Group != group {
		return nil, os.ErrPermission
	}

	new := old.Clone()
	new.Expires = &expires
	state.tokens[token] = new

	err = state.rewrite()
	if err != nil {
		state.tokens[token] = old
		return nil, err
	}
	return new, err
}

// called locked
func (state *state) rewrite() error {
	if state.tokens == nil || len(state.tokens) == 0 {
		err := os.Remove(state.filename)
		if err == nil || os.IsNotExist(err) {
			return nil
		}
		return err
	}

	dir := filepath.Dir(state.filename)
	tmpfile, err := os.CreateTemp(dir, "tokens")
	if err != nil {
		return err
	}
	a, err := state.list("")
	if err != nil {
		os.Remove(tmpfile.Name())
		return err
	}
	encoder := json.NewEncoder(tmpfile)
	for _, t := range a {
		err := encoder.Encode(t)
		if err != nil {
			tmpfile.Close()
			os.Remove(tmpfile.Name())
			return err
		}
	}

	err = tmpfile.Close()
	if err != nil {
		os.Remove(tmpfile.Name())
		return err
	}

	err = os.Rename(tmpfile.Name(), state.filename)
	if err != nil {
		os.Remove(tmpfile.Name())
		return err
	}

	fi, err := os.Stat(state.filename)
	if err == nil {
		state.modTime = fi.ModTime()
		state.fileSize = fi.Size()
	} else {
		// force rereading next time
		state.modTime = time.Time{}
		state.fileSize = 0
	}

	return nil
}

// called locked
func (state *state) list(group string) ([]*Stateful, error) {
	err := state.load()
	if err != nil {
		return nil, err
	}

	a := make([]*Stateful, 0)
	if state.tokens == nil {
		return a, nil
	}
	for _, t := range state.tokens {
		if group != "" {
			if t.Group != group {
				continue
			}
		}
		a = append(a, t)
	}
	sort.Slice(a, func(i, j int) bool {
		if a[j].Expires == nil {
			return false
		}
		if a[i].Expires == nil {
			return true
		}
		return (*a[i].Expires).Before(*a[j].Expires)
	})
	return a, nil
}

func (state *state) List(group string) ([]*Stateful, error) {
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.list(group)
}

func List(group string) ([]*Stateful, error) {
	return tokens.List(group)
}

func (state *state) Expire() error {
	state.mu.Lock()
	defer state.mu.Unlock()

	err := state.load()
	if err != nil {
		return err
	}

	now := time.Now()
	cutoff := now.Add(-time.Hour * 24 * 7)

	modified := false
	for k, t := range state.tokens {
		if t.Expires.Before(cutoff) {
			delete(state.tokens, k)
			modified = true
		}
	}

	if modified {
		err := state.rewrite()
		if err != nil {
			return err
		}
	}
	return nil
}

func Expire() error {
	return tokens.Expire()
}
