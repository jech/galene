package group

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/jech/galene/token"
)

var ErrTagMismatch = errors.New("tag mismatch")
var ErrDescriptionsNotWritable = &NotAuthorisedError{}

type Permissions struct {
	// non-empty for a named permissions set
	name string
	// only used when unnamed
	permissions []string
}

var permissionsMap = map[string][]string{
	"op":      {"op", "present", "message", "token"},
	"present": {"present", "message"},
	"message": {"message"},
	"observe": {},
	"admin":   {"admin"},
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

// Custom MarshalJSON in order to omit ompty fields
func (u UserDescription) MarshalJSON() ([]byte, error) {
	uu := make(map[string]any, 2)
	if u.Password.Type != "" {
		uu["password"] = &u.Password
	}
	if u.Permissions.name != "" || u.Permissions.permissions != nil {
		uu["permissions"] = &u.Permissions
	}
	return json.Marshal(uu)
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

	// Whether this is an automatically generated subgroup
	isSubgroup bool `json:"-"`

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

	// Time after which joining is no longer allowed
	Expires *time.Time `json:"expires,omitempty"`

	// Time before which joining is not allowed
	NotBefore *time.Time `json:"not-before,omitempty"`

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

	// Credentials for user with arbitrary username
	WildcardUser *UserDescription `json:"wildcard-user,omitempty"`

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

func getDescriptionFile[T any](name string, allowSubgroups bool, get func(string) (T, error)) (T, string, bool, error) {
	isSubgroup := false
	for name != "" {
		fileName := filepath.Join(
			Directory, path.Clean("/"+name)+".json",
		)
		r, err := get(fileName)
		if !errors.Is(err, os.ErrNotExist) {
			return r, fileName, isSubgroup, err
		}
		if !allowSubgroups {
			break
		}
		isSubgroup = true
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
	fi, fileName, _, err := getDescriptionFile(name, true, os.Stat)
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

	return readDescription(name, true)
}

// GetSanitisedDescription returns the subset of the description that is
// published on the web interface together with a suitable ETag.
func GetSanitisedDescription(name string) (*Description, string, error) {
	d, err := GetDescription(name)
	if err != nil {
		return nil, "", err
	}
	if d.isSubgroup {
		return nil, "", os.ErrNotExist
	}

	desc := *d
	desc.Users = nil
	desc.WildcardUser = nil
	desc.AuthKeys = nil
	return &desc, makeETag(desc.fileSize, desc.modTime), nil
}

// GetDescriptionTag returns an ETag for a description.
func GetDescriptionTag(name string) (string, error) {
	fi, _, _, err := getDescriptionFile(name, false, os.Stat)
	if err != nil {
		return "", err
	}
	return makeETag(fi.Size(), fi.ModTime()), nil
}

func makeETag(fileSize int64, modTime time.Time) string {
	return fmt.Sprintf("\"%v-%v\"", fileSize, modTime.UnixNano())
}

// DeleteDescription deletes a description (and therefore persistently
// deletes a group) but only if it matches a given ETag.
func DeleteDescription(name, etag string) error {
	groups.mu.Lock()
	defer groups.mu.Unlock()

	fi, fileName, _, err := getDescriptionFile(name, false, os.Stat)
	if err != nil {
		return err
	}
	if etag != makeETag(fi.Size(), fi.ModTime()) {
		return ErrTagMismatch
	}
	return os.Remove(fileName)
}

// UpdateDescription overwrites a description if it matches a given ETag.
// In order to create a new group, pass an empty ETag.
func UpdateDescription(name, etag string, desc *Description) error {
	if desc.Users != nil || desc.WildcardUser != nil || desc.AuthKeys != nil {
		return errors.New("description is not sanitised")
	}

	groups.mu.Lock()
	defer groups.mu.Unlock()

	oldetag := ""
	var filename string
	old, err := readDescription(name, false)
	if err == nil {
		oldetag = makeETag(old.fileSize, old.modTime)
		filename = old.FileName
	} else if errors.Is(err, os.ErrNotExist) {
		old = nil
		filename = filepath.Join(
			Directory, path.Clean("/"+name)+".json",
		)
	} else {
		return err
	}

	if oldetag != etag {
		return ErrTagMismatch
	}

	newdesc := *desc
	if old != nil {
		newdesc.Users = old.Users
		newdesc.WildcardUser = old.WildcardUser
		newdesc.AuthKeys = old.AuthKeys
	}

	return rewriteDescriptionFile(filename, &newdesc)
}

func rewriteDescriptionFile(filename string, desc *Description) error {
	conf, err := GetConfiguration()
	if err != nil {
		return err
	}
	if !conf.WritableGroups {
		return ErrDescriptionsNotWritable
	}

	dir := filepath.Dir(filename)

	err = os.MkdirAll(dir, 0700)
	if err != nil {
		return err
	}

	f, err := os.CreateTemp(dir, "*.temp")
	if err != nil {
		return err
	}
	temp := f.Name()

	encoder := json.NewEncoder(f)
	err = encoder.Encode(desc)
	if err == nil {
		err = f.Sync()
	}
	if err != nil {
		f.Close()
		os.Remove(temp)
		return err
	}
	err = f.Close()
	if err != nil {
		os.Remove(temp)
		return err
	}

	err = os.Rename(temp, filename)
	if err != nil {
		os.Remove(temp)
		return err
	}

	return nil

}

// readDescription reads a group's description from disk
func readDescription(name string, allowSubgroups bool) (*Description, error) {
	r, fileName, isSubgroup, err :=
		getDescriptionFile(name, allowSubgroups, os.Open)
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
	desc.FileName = fileName
	desc.fileSize = fi.Size()
	desc.modTime = fi.ModTime()

	err = upgradeDescription(&desc)
	if err != nil {
		return nil, err
	}

	if isSubgroup {
		if !desc.AutoSubgroups {
			return nil, os.ErrNotExist
		}
		desc.isSubgroup = true
		desc.Public = false
		desc.Description = ""
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
				if desc.WildcardUser != nil {
					log.Printf("%v: duplicate wildcard user",
						desc.FileName)
					continue
				}
				u := upgradeUser(u, p)
				desc.WildcardUser = &u
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
		upgradeUsers(desc.Other, "message")
		desc.Other = nil
	}

	return nil
}

func GetDescriptionNames() ([]string, error) {
	var names []string
	err := filepath.WalkDir(
		Directory,
		func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			base := filepath.Base(path)
			if d.IsDir() {
				if base[0] == '.' {
					return fs.SkipDir
				}
				return nil
			}
			if base[0] == '.' {
				return nil
			}
			p, err := filepath.Rel(Directory, path)
			if err != nil || !strings.HasSuffix(p, ".json") {
				return nil
			}
			names = append(names, strings.TrimSuffix(
				p, ".json",
			))
			return nil
		},
	)
	return names, err
}

func SetKeys(group string, keys []map[string]any) error {
	if keys != nil {
		_, err := token.ParseKeys(keys, "", "")
		if err != nil {
			return err
		}
	}

	groups.mu.Lock()
	defer groups.mu.Unlock()

	desc, err := readDescription(group, false)
	if err != nil {
		return err
	}
	desc.AuthKeys = keys
	return rewriteDescriptionFile(desc.FileName, desc)
}

func GetUsers(group string) ([]string, string, error) {
	desc, err := GetDescription(group)
	if err != nil {
		return nil, "", err
	}

	users := make([]string, 0, len(desc.Users))
	for u := range desc.Users {
		users = append(users, u)
	}

	return users, makeETag(desc.fileSize, desc.modTime), nil
}

func GetSanitisedUser(group, username string, wildcard bool) (UserDescription, string, error) {
	if wildcard && username != "" {
		return UserDescription{}, "",
			errors.New("wildcard with username")
	}

	desc, err := GetDescription(group)
	if err != nil {
		return UserDescription{}, "", err
	}

	var u UserDescription
	if wildcard {
		if desc.WildcardUser == nil {
			return UserDescription{}, "", os.ErrNotExist
		}
		u = *desc.WildcardUser
	} else {
		if desc.Users == nil {
			return UserDescription{}, "", os.ErrNotExist
		}

		ok := false
		u, ok = desc.Users[username]
		if !ok {
			return UserDescription{}, "", os.ErrNotExist
		}
	}

	u.Password = Password{}
	return u, makeETag(desc.fileSize, desc.modTime), nil
}

func GetUserTag(group, username string, wildcard bool) (string, error) {
	_, etag, err := GetSanitisedUser(group, username, wildcard)
	return etag, err
}

func DeleteUser(group, username string, wildcard bool, etag string) error {
	if wildcard && username != "" {
		return errors.New("wildcard with username")
	}

	groups.mu.Lock()
	defer groups.mu.Unlock()

	desc, err := readDescription(group, false)
	if err != nil {
		return err
	}

	if wildcard {
		if desc.WildcardUser == nil {
			return os.ErrNotExist
		}
	} else {
		if desc.Users == nil {
			return os.ErrNotExist
		}
		_, ok := desc.Users[username]
		if !ok {
			return os.ErrNotExist
		}
	}

	oldetag := makeETag(desc.fileSize, desc.modTime)
	if oldetag != etag {
		return ErrTagMismatch
	}

	if wildcard {
		desc.WildcardUser = nil
	} else {
		delete(desc.Users, username)
	}

	return rewriteDescriptionFile(desc.FileName, desc)
}

func UpdateUser(group, username string, wildcard bool, etag string, user *UserDescription) error {
	if wildcard && username != "" {
		return errors.New("wildcard with username")
	}
	if user.Password.Type != "" || user.Password.Key != nil {
		return errors.New("user description is not sanitised")
	}

	groups.mu.Lock()
	defer groups.mu.Unlock()

	desc, err := readDescription(group, false)
	if err != nil {
		return err
	}

	var old UserDescription
	var ok bool
	if wildcard {
		if desc.WildcardUser != nil {
			ok = true
			old = *desc.WildcardUser
		}
	} else {
		if desc.Users == nil {
			desc.Users = make(map[string]UserDescription)
		}
		old, ok = desc.Users[username]
	}

	var oldetag string
	if ok {
		oldetag = makeETag(desc.fileSize, desc.modTime)
	} else {
		oldetag = ""
	}

	if oldetag != etag {
		return ErrTagMismatch
	}

	newuser := *user
	newuser.Password = old.Password

	if wildcard {
		desc.WildcardUser = &newuser
	} else {
		desc.Users[username] = newuser
	}
	return rewriteDescriptionFile(desc.FileName, desc)
}

func SetUserPassword(group, username string, wildcard bool, pw Password) error {
	if wildcard && username != "" {
		return errors.New("wildcard with username")
	}

	groups.mu.Lock()
	defer groups.mu.Unlock()

	desc, err := readDescription(group, false)
	if err != nil {
		return err
	}
	if desc.Users == nil {
		return os.ErrNotExist
	}

	if wildcard {
		if desc.WildcardUser == nil {
			return os.ErrNotExist
		}
		desc.WildcardUser.Password = pw
	} else {
		user, ok := desc.Users[username]
		if !ok {
			return os.ErrNotExist
		}

		user.Password = pw
		desc.Users[username] = user
	}
	return rewriteDescriptionFile(desc.FileName, desc)
}
