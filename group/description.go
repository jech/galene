package group

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

type Permissions struct {
	// non-empty for a named permissions set
	name string
	// only used when unnamed
	permissions []string
}

var permissionsMap = map[string][]string{
	"op":      []string{"op", "present", "token"},
	"present": []string{"present"},
	"observe": []string{},
	"admin":   []string{"admin"},
}

func NewPermissions(name string) (Permissions, error) {
	_, ok := permissionsMap[name]
	if !ok {
		return Permissions{}, errors.New("unknown permission")
	}
	return Permissions{
		name: name,
	}, nil
}

func (p Permissions) Permissions(desc *Description) []string {
	if p.name == "" {
		return p.permissions
	}

	perms := permissionsMap[p.name]

	op := false
	present := false
	token := false
	record := false
	for _, p := range perms {
		switch p {
		case "op":
			op = true
		case "present":
			present = true
		case "token":
			token = true
		case "record":
			record = true
		}
	}

	if desc != nil && desc.AllowRecording {
		if op && !record {
			// copy the slice
			perms = append([]string{"record"}, perms...)
		}
	}

	if desc != nil && desc.UnrestrictedTokens {
		if present && !token {
			perms = append([]string{"token"}, perms...)
		}
	}

	return perms
}

func (p *Permissions) UnmarshalJSON(b []byte) error {
	var a []string
	err := json.Unmarshal(b, &a)
	if err == nil {
		*p = Permissions{
			permissions: a,
		}
		return nil
	}
	var s string
	err = json.Unmarshal(b, &s)
	if err == nil {
		_, ok := permissionsMap[s]
		if !ok {
			return errors.New("Unknown permission " + s)
		}
		*p = Permissions{
			name: s,
		}
		return nil
	}
	return err
}

func (p Permissions) MarshalJSON() ([]byte, error) {
	if p.name != "" {
		return json.Marshal(p.name)
	}
	return json.Marshal(p.permissions)
}

type UserDescription struct {
	Password    Password    `json:"password"`
	Permissions Permissions `json:"permissions"`
}

// Description represents a group description together with some metadata
// about the JSON file it was deserialised from.
type Description struct {
	// The file this was deserialised from.  This is not necessarily
	// the name of the group, for example in case of a subgroup.
	FileName string `json:"-"`

	// The modtime and size of the file.  These are used to detect
	// when a file has changed on disk.
	modTime  time.Time `json:"-"`
	fileSize int64     `json:"-"`

	// The user-friendly group name
	DisplayName string `json:"displayName,omitempty"`

	// A user-readable description of the group.
	Description string `json:"description,omitempty"`

	// A user-readable contact, typically an e-mail address.
	Contact string `json:"contact,omitempty"`

	// A user-readable comment.  Ignored by the server.
	Comment string `json:"comment,omitempty"`

	// Whether to display the group on the landing page.
	Public bool `json:"public,omitempty"`

	// A URL to redirect the group to.  If this is not empty, most
	// other fields are ignored.
	Redirect string `json:"redirect,omitempty"`

	// The maximum number of simultaneous clients.  Unlimited if 0.
	MaxClients int `json:"max-clients,omitempty"`

	// The time for which history entries are kept.
	MaxHistoryAge int `json:"max-history-age,omitempty"`

	// Whether recording is allowed.
	AllowRecording bool `json:"allow-recording,omitempty"`

	// Whether creating tokens is allowed
	UnrestrictedTokens bool `json:"unrestricted-tokens,omitempty"`

	// Whether subgroups are created on the fly.
	AutoSubgroups bool `json:"auto-subgroups,omitempty"`

	// Whether to lock the group when the last op logs out.
	Autolock bool `json:"autolock,omitempty"`

	// Whether to kick all users when the last op logs out.
	Autokick bool `json:"autokick,omitempty"`

	// Users allowed to login
	Users map[string]UserDescription `json:"users,omitempty"`

	// Credentials for users with arbitrary username
	FallbackUsers []UserDescription `json:"fallback-users,omitempty"`

	// The (public) keys used for token authentication.
	AuthKeys []map[string]interface{} `json:"authKeys,omitempty"`

	// The URL of the authentication server, if any.
	AuthServer string `json:"authServer,omitempty"`

	// The URL of the authentication portal, if any.
	AuthPortal string `json:"authPortal,omitempty"`

	// Codec preferences.  If empty, a suitable default is chosen in
	// the APIFromNames function.
	Codecs []string `json:"codecs,omitempty"`

	// Obsolete fields
	Op             []ClientPattern `json:"op,omitempty"`
	Presenter      []ClientPattern `json:"presenter,omitempty"`
	Other          []ClientPattern `json:"other,omitempty"`
	AllowSubgroups bool            `json:"allow-subgroups,omitempty"`
	AllowAnonymous bool            `json:"allow-anonymous,omitempty"`
}

const DefaultMaxHistoryAge = 4 * time.Hour

func maxHistoryAge(desc *Description) time.Duration {
	if desc.MaxHistoryAge != 0 {
		return time.Duration(desc.MaxHistoryAge) * time.Second
	}
	return DefaultMaxHistoryAge
}

func getDescriptionFile[T any](name string, get func(string) (T, error)) (T, string, bool, error) {
	isParent := false
	for name != "" {
		fileName := filepath.Join(
			Directory, path.Clean("/"+name)+".json",
		)
		r, err := get(fileName)
		if !os.IsNotExist(err) {
			return r, fileName, isParent, err
		}
		isParent = true
		name, _ = path.Split(name)
		name = strings.TrimRight(name, "/")
	}
	var zero T
	return zero, "", false, os.ErrNotExist
}

// descriptionMatch returns true if the description hasn't changed between
// d1 and d2
func descriptionMatch(d1, d2 *Description) bool {
	if d1.FileName != d2.FileName {
		return false
	}

	if d1.fileSize != d2.fileSize || !d1.modTime.Equal(d2.modTime) {
		return false
	}
	return true
}

// descriptionUnchanged returns true if a group's description hasn't
// changed since it was last read.
func descriptionUnchanged(name string, desc *Description) bool {
	fi, fileName, _, err := getDescriptionFile(name, os.Stat)
	if err != nil || fileName != desc.FileName {
		return false
	}

	if fi.Size() != desc.fileSize || !fi.ModTime().Equal(desc.modTime) {
		return false
	}
	return true
}

// GetDescription gets a group description, either from cache or from disk
func GetDescription(name string) (*Description, error) {
	g := Get(name)
	if g != nil {
		if descriptionUnchanged(name, g.description) {
			return g.description, nil
		}
	}

	return readDescription(name)
}

// readDescription reads a group's description from disk
func readDescription(name string) (*Description, error) {
	r, fileName, isParent, err := getDescriptionFile(name, os.Open)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	var desc Description

	fi, err := r.Stat()
	if err != nil {
		return nil, err
	}

	d := json.NewDecoder(r)
	d.DisallowUnknownFields()
	err = d.Decode(&desc)
	if err != nil {
		return nil, err
	}
	if isParent {
		if !desc.AutoSubgroups {
			return nil, os.ErrNotExist
		}
		desc.Public = false
		desc.Description = ""
	}

	desc.FileName = fileName
	desc.fileSize = fi.Size()
	desc.modTime = fi.ModTime()

	err = upgradeDescription(&desc)
	if err != nil {
		return nil, err
	}

	return &desc, nil
}

func upgradeDescription(desc *Description) error {
	if desc.AllowAnonymous {
		log.Printf(
			"%v: field allow-anonymous is obsolete, ignored",
			desc.FileName,
		)
		desc.AllowAnonymous = false
	}

	if desc.AllowSubgroups {
		desc.AutoSubgroups = true
		desc.AllowSubgroups = false
	}

	upgradePassword := func(pw *Password) Password {
		if pw == nil {
			return Password{
				Type: "wildcard",
			}
		}
		return *pw
	}

	upgradeUser := func(u ClientPattern, p string) UserDescription {
		return UserDescription{
			Password: upgradePassword(u.Password),
			Permissions: Permissions{
				name: p,
			},
		}
	}

	upgradeUsers := func(ps []ClientPattern, p string) {
		if desc.Users == nil {
			desc.Users = make(map[string]UserDescription)
		}
		for _, u := range ps {
			if u.Username == "" {
				desc.FallbackUsers = append(desc.FallbackUsers,
					upgradeUser(u, p))
				continue
			}
			_, found := desc.Users[u.Username]
			if found {
				log.Printf("%v: duplicate user %v, ignored",
					desc.FileName, u.Username)
				continue
			}
			desc.Users[u.Username] = upgradeUser(u, p)
		}
	}

	if desc.Op != nil {
		upgradeUsers(desc.Op, "op")
		desc.Op = nil
	}
	if desc.Presenter != nil {
		upgradeUsers(desc.Presenter, "present")
		desc.Presenter = nil
	}
	if desc.Other != nil {
		upgradeUsers(desc.Other, "observe")
		desc.Other = nil
	}

	return nil
}
