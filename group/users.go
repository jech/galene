package group

import (
	"encoding/json"
	"errors"
	"io"
	"slices"
	"sync"

	"github.com/jech/galene/config"
)

var globalUsers struct {
	mu         sync.Mutex
	desc       map[string]UserDescription
	configFile config.ConfigFile
	configTag  string
}

// Initialize the global user state.
func SetGlobalUsersPath(path string) {
	globalUsers.mu.Lock()
	defer globalUsers.mu.Unlock()

	globalUsers.desc = make(map[string]UserDescription)
	globalUsers.configFile = config.NewConfigFile(path)
	globalUsers.configTag = ""
}

// Return a list of usernames and the current global user etag.
func GetGlobalUsernames() ([]string, string, error) {
	globalUsers.mu.Lock()
	defer globalUsers.mu.Unlock()

	if err := refreshGlobalUsers(); err != nil {
		return []string{}, "", err
	}

	names := make([]string, 0, len(globalUsers.desc))
	for name, _ := range globalUsers.desc {
		names = append(names, name)
	}
	slices.Sort(names)
	return names, globalUsers.configTag, nil
}

// Return the UserDescription and the current global user etag.
func GetGlobalUser(username string) (UserDescription, string, error) {
	globalUsers.mu.Lock()
	defer globalUsers.mu.Unlock()

	if err := refreshGlobalUsers(); err != nil {
		return UserDescription{}, "", err
	}

	u, ok := globalUsers.desc[username]
	if !ok {
		return UserDescription{}, "", ErrNoSuchUsername
	}
	return u, globalUsers.configTag, nil
}

// Add the given user, which must not exist yet.
func AddGlobalUser(username string) error {
	globalUsers.mu.Lock()
	defer globalUsers.mu.Unlock()

	if err := refreshGlobalUsers(); err != nil {
		return err
	}
	_, ok := globalUsers.desc[username]
	if ok {
		return ErrDuplicateUsername
	}

	globalUsers.desc[username] = UserDescription{}
	err := writeGlobalUsers(globalUsers.configTag)
	if err != nil {
		delete(globalUsers.desc, username)
		return err
	}
	return nil
}

// Delete the given user, if the current global user etag matches.
func DeleteGlobalUser(username string, etag string) error {
	globalUsers.mu.Lock()
	defer globalUsers.mu.Unlock()

	if err := refreshGlobalUsers(); err != nil {
		return err
	}

	u, existed := globalUsers.desc[username]
	delete(globalUsers.desc, username)
	err := writeGlobalUsers(etag)
	if err != nil {
		if existed {
			globalUsers.desc[username] = u
		}
		return err
	}
	return nil
}

func SetGlobalUserPassword(username string, pw Password) error {
	globalUsers.mu.Lock()
	defer globalUsers.mu.Unlock()

	if err := refreshGlobalUsers(); err != nil {
		return err
	}
	desc, ok := globalUsers.desc[username]
	if !ok {
		return ErrNoSuchUsername
	}

	oldPassword := desc.Password
	desc.Password = pw
	globalUsers.desc[username] = desc
	err := writeGlobalUsers(globalUsers.configTag)
	if err != nil {
		desc.Password = oldPassword
		globalUsers.desc[username] = desc
		return err
	}
	return nil
}

func refreshGlobalUsers() error {
	currentTag := globalUsers.configFile.Tag()
	if currentTag != globalUsers.configTag {
		desc := make(map[string]UserDescription)

		err := globalUsers.configFile.Read(func(r io.Reader) error {
			if r != nil {
				decoder := json.NewDecoder(r)
				return decoder.Decode(&desc)
			}
			return nil
		})
		if err != nil {
			return err
		}

		globalUsers.desc = desc
		globalUsers.configTag = currentTag
	}
	return nil
}

func writeGlobalUsers(etag string) error {
	err := globalUsers.configFile.Write(etag, func(w io.Writer) error {
		encoder := json.NewEncoder(w)
		return encoder.Encode(globalUsers.desc)
	})
	if errors.Is(err, config.ErrTagMismatch) {
		return ErrTagMismatch
	}
	if err != nil {
		return err
	}

	globalUsers.configTag = globalUsers.configFile.Tag()
	return nil
}
