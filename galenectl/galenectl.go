package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/pbkdf2"
	"golang.org/x/term"

	"github.com/jech/galene/group"
	"github.com/jech/galene/token"
)

type configuration struct {
	Server        string `json:"server"`
	AdminUsername string `json:"admin-username,omitempty"`
	AdminPassword string `json:"admin-password,omitempty"`
	AdminToken    string `json:"admin-token,omitempty"`
}

var insecure bool
var serverURL, adminUsername, adminPassword, adminToken string
var configFile string

var client http.Client

type command struct {
	command     func(string, []string)
	description string
}

var commands = map[string]command{
	"hash-password": {
		command:     hashPasswordCmd,
		description: "generate a hashed password",
	},
	"initial-setup": {
		command:     initialSetupCmd,
		description: "initial setup of Galene and galenectl",
	},
	"set-password": {
		command:     setPasswordCmd,
		description: "set a user's password",
	},
	"delete-password": {
		command:     deletePasswordCmd,
		description: "delete a user's password",
	},
	"list-groups": {
		command:     listGroupsCmd,
		description: "list groups",
	},
	"show-group": {
		command:     showGroupCmd,
		description: "show group definition",
	},
	"create-group": {
		command:     createGroupCmd,
		description: "create a group",
	},
	"update-group": {
		command:     updateGroupCmd,
		description: "change a group's definition",
	},
	"delete-group": {
		command:     deleteGroupCmd,
		description: "delete a group",
	},
	"list-users": {
		command:     listUsersCmd,
		description: "list users",
	},
	"create-user": {
		command:     createUserCmd,
		description: "create a user",
	},
	"delete-user": {
		command:     deleteUserCmd,
		description: "delete a user",
	},
	"update-user": {
		command:     updateUserCmd,
		description: "change a user's permissions",
	},
	"list-tokens": {
		command:     listTokensCmd,
		description: "list tokens",
	},
	"create-token": {
		command:     createTokenCmd,
		description: "request a token",
	},
	"revoke-token": {
		command:     revokeTokenCmd,
		description: "revoke a token",
	},
	"delete-token": {
		command:     deleteTokenCmd,
		description: "delete a token",
	},
}

func main() {
	configdir, err := os.UserConfigDir()
	if err != nil {
		log.Fatalf("UserConfigDir: %v", err)
	}
	configFile = filepath.Join(
		filepath.Join(configdir, "galene"),
		"galenectl.json",
	)

	flag.Usage = func() {
		fmt.Fprintf(
			flag.CommandLine.Output(),
			"%s [option...] command [option...] [args...]\n",
			os.Args[0])
		flag.PrintDefaults()
		fmt.Fprintln(flag.CommandLine.Output())
		names := make([]string, 0, len(commands))
		for name := range commands {
			names = append(names, name)
		}
		sort.Slice(names, func(i, j int) bool {
			return names[i] < names[j]
		})
		for _, name := range names {
			fmt.Fprintf(
				flag.CommandLine.Output(),
				"  %-15s    %s\n",
				name, commands[name].description)
		}
		fmt.Fprintln(flag.CommandLine.Output())
		fmt.Fprintf(flag.CommandLine.Output(),
			"See \"%s command -help\" for information on individual commands.\n",
			os.Args[0],
		)
	}
	flag.StringVar(&serverURL, "server", "",
		"server `url`")
	flag.BoolVar(&insecure, "insecure", false,
		"don't check server certificates")
	flag.StringVar(&configFile, "config", configFile,
		"configuration `file`")
	flag.StringVar(&adminUsername, "admin-username", "",
		"administrator `username`")
	flag.StringVar(&adminPassword, "admin-password", "",
		"administrator `password`")
	flag.StringVar(&adminToken, "admin-token",
		"", "administrator `token`")
	flag.Parse()

	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}

	config, err := readConfig(configFile)
	if err != nil {
		log.Fatalf("Failed to read configuration file: %v", err)
	}
	if serverURL == "" {
		serverURL = config.Server
	}
	if serverURL == "" {
		serverURL = "https://localhost:8443"
	}

	if adminUsername == "" {
		adminUsername = config.AdminUsername
	}
	if adminPassword == "" {
		adminPassword = config.AdminPassword
	}
	if adminToken == "" {
		adminToken = config.AdminToken
	}

	if insecure {
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		client.Transport = t
	}

	cmdname := flag.Args()[0]
	command, ok := commands[cmdname]
	if !ok {
		flag.Usage()
		os.Exit(1)
	}
	command.command(cmdname, flag.Args()[1:])
}

func readConfig(filename string) (configuration, error) {
	var config configuration
	f, err := os.Open(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return config, nil
		}
		return config, err
	}
	defer f.Close()
	decoder := json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&config)
	if err != nil {
		return config, err
	}
	return config, nil
}

func makePassword(pw string, algorithm string, iterations, length, saltlen, cost int) (group.Password, error) {
	salt := make([]byte, saltlen)
	_, err := rand.Read(salt)
	if err != nil {
		return group.Password{}, err
	}

	switch algorithm {
	case "pbkdf2":
		key := pbkdf2.Key(
			[]byte(pw), salt, iterations, length, sha256.New,
		)
		encoded := hex.EncodeToString(key)
		return group.Password{
			Type:       "pbkdf2",
			Hash:       "sha-256",
			Key:        &encoded,
			Salt:       hex.EncodeToString(salt),
			Iterations: iterations,
		}, nil
	case "bcrypt":
		key, err := bcrypt.GenerateFromPassword(
			[]byte(pw), cost,
		)
		if err != nil {
			return group.Password{}, err
		}

		k := string(key)
		return group.Password{
			Type: "bcrypt",
			Key:  &k,
		}, nil
	case "wildcard":
		if pw != "" {
			log.Fatalf(
				"Wildcard password " +
					"must be the empty string",
			)
		}
		return group.Password{
			Type: "wildcard",
		}, nil
	default:
		return group.Password{}, errors.New("unknown password type")
	}
}

func setUsage(cmd *flag.FlagSet, cmdname string, format string, args ...any) {
	cmd.Usage = func() {
		fmt.Fprintf(cmd.Output(), format, args...)
		cmd.PrintDefaults()
	}
}

func hashPasswordCmd(cmdname string, args []string) {
	var password, algorithm string
	var iterations, cost, length, saltlen int

	cmd := flag.NewFlagSet(cmdname, flag.ExitOnError)
	setUsage(cmd, cmdname,
		"%v [option...] %v [option...]\n",
		os.Args[0], cmdname,
	)
	cmd.StringVar(&password, "password", "", "new `password`")
	cmd.StringVar(&algorithm, "type", "bcrypt",
		"password `type`")
	cmd.IntVar(&iterations, "iterations", 4096,
		"`number` of iterations (pbkdf2)")
	cmd.IntVar(&cost, "cost", 8, "`cost` (bcrypt)")
	cmd.IntVar(&length, "key", 32, "key `length` (pbkdf2)")
	cmd.IntVar(&saltlen, "salt", 8, "salt `length` (pbkdf2)")
	cmd.Parse(args)

	if cmd.NArg() != 0 {
		cmd.Usage()
		os.Exit(1)
	}

	if algorithm != "wildcard" && password == "" {
		fmt.Fprint(os.Stdin, "New password: ")
		pw, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			log.Fatalf("ReadPassword: %v", err)
		}
		password = string(pw)
	}

	p, err := makePassword(
		password, algorithm, iterations, length, saltlen, cost,
	)
	if err != nil {
		log.Fatalf("Make password: %v", err)
	}
	e := json.NewEncoder(os.Stdout)
	err = e.Encode(p)
	if err != nil {
		log.Fatalf("Encode: %v", err)
	}
}

func initialSetupCmd(cmdname string, args []string) {
	var galeneConfigFn string

	cmd := flag.NewFlagSet(cmdname, flag.ExitOnError)
	setUsage(cmd, cmdname,
		"%v [option...] %v [option...]\n",
		os.Args[0], cmdname,
	)
	cmd.StringVar(&galeneConfigFn, "config", "config.json",
		"Galene configuration `file`")
	cmd.Parse(args)

	if cmd.NArg() != 0 {
		cmd.Usage()
		os.Exit(1)
	}

	if adminUsername == "" {
		fmt.Fprintf(cmd.Output(),
			"Option \"-admin-username\" is required.\n",
		)
		os.Exit(1)
	}

	if adminPassword == "" && adminToken == "" {
		fmt.Fprint(os.Stdin, "Administrator password: ")
		pw, err := term.ReadPassword(int(os.Stdin.Fd()))
		if err != nil {
			log.Fatalf("ReadPassword: %v", err)
		}
		adminPassword = string(pw)
		fmt.Fprint(os.Stdin, "\n")
	}

	configDir := filepath.Dir(configFile)
	if configDir != "" {
		err := os.MkdirAll(configDir, 0o700)
		if err != nil {
			log.Fatalf("Create directory %v: %v", configDir)
		}
	}

	galeneConfig, err := os.OpenFile(galeneConfigFn,
		os.O_WRONLY|os.O_CREATE|os.O_CREATE|os.O_EXCL,
		0600)
	if err != nil {
		log.Fatalf("Create %v: %v", galeneConfigFn, err)
	}

	galenectlConfig, err := os.OpenFile(configFile,
		os.O_WRONLY|os.O_CREATE|os.O_CREATE|os.O_EXCL,
		0600)
	if err != nil {
		galeneConfig.Close()
		os.Remove(galeneConfigFn)
		log.Fatalf("Create %v: %v", galeneConfigFn, err)
	}

	defer galeneConfig.Close()
	defer galenectlConfig.Close()

	var users map[string]group.UserDescription
	if adminPassword != "" {
		pw, err := makePassword(adminPassword, "bcrypt", 0, 0, 0, 12)
		if err != nil {
			log.Fatalf("makePassword: %v", err)
		}

		perms, err := group.NewPermissions("admin")
		if err != nil {
			log.Fatalf("NewPermissions: %v", err)
		}
		users = map[string]group.UserDescription{
			adminUsername: {
				Password:    pw,
				Permissions: perms,
			},
		}
	}

	config := group.Configuration{
		WritableGroups: true,
		Users:          users,
	}

	encoder := json.NewEncoder(galeneConfig)
	encoder.SetIndent("", "    ")
	err = encoder.Encode(&config)
	if err != nil {
		log.Fatalf("Encode %v: %v", galeneConfigFn, err)
	}

	ctlConfig := configuration{
		Server:        serverURL,
		AdminUsername: adminUsername,
		AdminPassword: adminPassword,
		AdminToken:    adminToken,
	}

	ctlEncoder := json.NewEncoder(galenectlConfig)
	ctlEncoder.SetIndent("", "    ")
	err = ctlEncoder.Encode(&ctlConfig)
	if err != nil {
		log.Fatalf("Encode %v: %v", configFile, err)
	}

	fmt.Printf("The file %v has been created.  ", galeneConfigFn)
	fmt.Printf("Please copy it into your server's\n\"data/\" directory.\n")
	fmt.Printf("The file %v has been created.  ", configFile)
	fmt.Printf("It contains the administrator's\npassword in cleartext, ")
	fmt.Printf("please keep it secure.\n")
}

func setAuthorization(req *http.Request) {
	if adminToken != "" {
		req.Header.Set("Authorization", "Bearer "+adminToken)
	} else if adminUsername != "" {
		req.SetBasicAuth(adminUsername, adminPassword)
	}
}

type httpError struct {
	statusCode int
	status     string
}

func (e httpError) Error() string {
	if e.status != "" {
		return fmt.Sprintf("HTTP error: %v", e.status)
	}
	return fmt.Sprintf("HTTP error: %v", e.statusCode)
}

func getJSON(url string, value any) (string, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	setAuthorization(req)

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	etag := resp.Header.Get("ETag")

	if resp.StatusCode >= 300 {
		return etag, httpError{resp.StatusCode, resp.Status}
	}

	decoder := json.NewDecoder(resp.Body)
	return etag, decoder.Decode(value)
}

func putJSON(url string, value any, overwrite bool) error {
	j, err := json.Marshal(value)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("PUT", url, bytes.NewReader(j))
	if err != nil {
		return err
	}
	setAuthorization(req)

	req.Header.Set("Content-Type", "application/json")
	if !overwrite {
		req.Header.Set("If-None-Match", "*")
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return httpError{resp.StatusCode, resp.Status}
	}
	io.Copy(io.Discard, resp.Body)
	return nil
}

func postJSON(url string, value any) (string, error) {
	j, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequest("POST", url, bytes.NewReader(j))
	if err != nil {
		return "", err
	}
	setAuthorization(req)

	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", httpError{resp.StatusCode, resp.Status}
	}
	location := resp.Header.Get("location")
	io.Copy(io.Discard, resp.Body)
	return location, nil
}

func updateJSON[T any](url string, update func(T) T) error {
	var old T
	etag, err := getJSON(url, &old)
	if err != nil {
		return err
	}
	if etag == "" {
		return errors.New("missing ETag")
	}

	value := update(old)

	j, err := json.Marshal(value)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("PUT", url, bytes.NewReader(j))
	setAuthorization(req)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", etag)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return httpError{resp.StatusCode, resp.Status}
	}
	io.Copy(io.Discard, resp.Body)
	return nil
}

func deleteValue(url string) error {
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return err
	}
	setAuthorization(req)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return httpError{resp.StatusCode, resp.Status}
	}
	io.Copy(io.Discard, resp.Body)
	return nil
}

func setPasswordCmd(cmdname string, args []string) {
	var groupname, username string
	var wildcard bool
	var password, algorithm string
	var iterations, cost, length, saltlen int

	cmd := flag.NewFlagSet(cmdname, flag.ExitOnError)
	setUsage(cmd, cmdname,
		"%v [option...] %v [option...]\n",
		os.Args[0], cmdname,
	)
	cmd.StringVar(&groupname, "group", "", "group `name`")
	cmd.StringVar(&username, "user", "", "user `name`")
	cmd.BoolVar(&wildcard, "wildcard", false, "set wildcard user's password")
	cmd.StringVar(&password, "password", "", "new `password`")
	cmd.StringVar(&algorithm, "type", "bcrypt",
		"password `type`")
	cmd.IntVar(&iterations, "iterations", 4096,
		"`number` of iterations (pbkdf2)")
	cmd.IntVar(&cost, "cost", 8,
		"`cost` (bcrypt)")
	cmd.IntVar(&length, "key", 32, "key `length` (pbkdf2)")
	cmd.IntVar(&saltlen, "salt", 8, "salt `length` (pbkdf2)")
	cmd.Parse(args)

	if cmd.NArg() != 0 {
		cmd.Usage()
		os.Exit(1)
	}

	if groupname == "" {
		fmt.Fprintf(cmd.Output(),
			"Option \"-group\" is required\n")
		os.Exit(1)
	}

	if wildcard != (username == "") {
		fmt.Fprintf(cmd.Output(),
			"Exactly one of \"-user\" and \"-wildcard\" "+
				"is required\n")
		os.Exit(1)
	}

	if algorithm != "wildcard" && password == "" {
		fmt.Fprint(os.Stdin, "New password: ")
		pw, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			log.Fatalf("ReadPassword: %v", err)
		}
		password = string(pw)
	}

	pw, err := makePassword(
		password, algorithm, iterations, length, saltlen, cost,
	)
	if err != nil {
		log.Fatalf("Make password: %v", err)
	}

	var u string
	if wildcard {
		u, err = url.JoinPath(
			serverURL, "/galene-api/v0/.groups", groupname,
			".wildcard-user/.password",
		)
	} else {
		u, err = url.JoinPath(
			serverURL, "/galene-api/v0/.groups", groupname,
			".users", username, ".password",
		)
	}
	if err != nil {
		log.Fatalf("Build URL: %v", err)
	}

	err = putJSON(u, pw, true)
	if err != nil {
		log.Fatalf("Set password: %v", err)
	}
}

func deletePasswordCmd(cmdname string, args []string) {
	var groupname, username string
	var wildcard bool

	cmd := flag.NewFlagSet(cmdname, flag.ExitOnError)
	setUsage(cmd, cmdname,
		"%v [option...] %v [option...]\n",
		os.Args[0], cmdname,
	)
	cmd.StringVar(&groupname, "group", "", "group `name`")
	cmd.StringVar(&username, "user", "", "user `name`")
	cmd.BoolVar(&wildcard, "wildcard", false, "set wildcard user's password")
	cmd.Parse(args)

	if cmd.NArg() != 0 {
		cmd.Usage()
		os.Exit(1)
	}

	if groupname == "" {
		fmt.Fprintf(cmd.Output(),
			"Option \"-group\" is required\n")
		os.Exit(1)
	}

	if wildcard != (username == "") {
		fmt.Fprintf(cmd.Output(),
			"Exactly one of \"-user\" and \"-wildcard\" "+
				"is required\n")
		os.Exit(1)
	}

	var u string
	var err error
	if wildcard {
		u, err = url.JoinPath(
			serverURL, "/galene-api/v0/.groups", groupname,
			".wildcard-user/.password",
		)
	} else {
		u, err = url.JoinPath(
			serverURL, "/galene-api/v0/.groups", groupname,
			".users", username, ".password",
		)
	}
	if err != nil {
		log.Fatalf("Build URL: %v", err)
	}

	err = deleteValue(u)
	if err != nil {
		log.Fatalf("Delete password: %v", err)
	}
}

// boolOption represents a boolean command-line option that may be unset
type boolOption struct {
	set   bool
	value bool
}

func (o *boolOption) Set(value string) error {
	v, err := strconv.ParseBool(value)
	if err != nil {
		return err
	}
	o.value = v
	o.set = true
	return nil
}

func (o *boolOption) String() string {
	if o == nil {
		return "(nil)"
	}
	if !o.set {
		return "(unset)"
	}
	return strconv.FormatBool(o.value)
}

func (o *boolOption) IsBoolFlag() bool {
	return true
}

// stringOption represents a command-line option that may be unset
type stringOption struct {
	set   bool
	value string
}

func (o *stringOption) Set(value string) error {
	o.value = value
	o.set = true
	return nil
}

func (o *stringOption) String() string {
	if o == nil {
		return "(nil)"
	}
	if !o.set {
		return "(unset)"
	}
	return o.value
}

// stdinJSON reads a JSON dictionary on standard input if doit is true.
// It always returns a non-nil dictionary in the non-error case.
func stdinJSON(doit bool) (map[string]any, error) {
	data := make(map[string]any)
	if doit {
		decoder := json.NewDecoder(os.Stdin)
		err := decoder.Decode(&data)
		if err != nil {
			return nil, err
		}
	}
	return data, nil
}

func createGroupCmd(cmdname string, args []string) {
	var groupname string
	var unrestrictedTokens, autoSubgroups boolOption
	var doJSON bool
	cmd := flag.NewFlagSet(cmdname, flag.ExitOnError)
	setUsage(cmd, cmdname,
		"%v [option...] %v [option...]\n",
		os.Args[0], cmdname,
	)
	cmd.StringVar(&groupname, "group", "", "group `name`")
	cmd.Var(&unrestrictedTokens, "unrestricted-tokens",
		"allow ordinary users to create tokens",
	)
	cmd.Var(&autoSubgroups, "auto-subgroups",
		"create subgroups automatically",
	)
	cmd.BoolVar(&doJSON, "json", false,
		"read JSON template from standard input",
	)
	cmd.Parse(args)

	if cmd.NArg() != 0 {
		cmd.Usage()
		os.Exit(1)
	}

	if groupname == "" {
		fmt.Fprintf(cmd.Output(),
			"Option \"-group\" is required\n")
		os.Exit(1)
	}

	u, err := url.JoinPath(
		serverURL, "/galene-api/v0/.groups", groupname,
	)
	if err != nil {
		log.Fatalf("Build URL: %v", err)
	}

	data, err := stdinJSON(doJSON)
	if err != nil {
		log.Fatalf("Decode standard input: %v", err)
	}

	if unrestrictedTokens.set {
		data["unrestricted-tokens"] = unrestrictedTokens.value
	}
	if autoSubgroups.set {
		data["auto-subgroups"] = autoSubgroups.value
	}

	err = putJSON(u, data, false)
	if err != nil {
		log.Fatalf("Create group: %v", err)
	}
}

func deleteGroupCmd(cmdname string, args []string) {
	var groupname string
	cmd := flag.NewFlagSet(cmdname, flag.ExitOnError)
	setUsage(cmd, cmdname,
		"%v [option...] %v [option...]\n",
		os.Args[0], cmdname,
	)
	cmd.StringVar(&groupname, "group", "", "group `name`")
	cmd.Parse(args)

	if cmd.NArg() != 0 {
		cmd.Usage()
		os.Exit(1)
	}

	if groupname == "" {
		fmt.Fprintf(cmd.Output(),
			"Option \"-group\" is required\n")
		os.Exit(1)
	}

	u, err := url.JoinPath(
		serverURL, "/galene-api/v0/.groups", groupname,
	)
	if err != nil {
		log.Fatalf("Build URL: %v", err)
	}

	err = deleteValue(u)
	if err != nil {
		log.Fatalf("Delete group: %v", err)
	}
}

func updateGroupCmd(cmdname string, args []string) {
	var groupname string
	var unrestrictedTokens, autoSubgroups boolOption
	var doJSON bool
	cmd := flag.NewFlagSet(cmdname, flag.ExitOnError)
	setUsage(cmd, cmdname,
		"%v [option...] %v [option...]\n",
		os.Args[0], cmdname,
	)
	cmd.StringVar(&groupname, "group", "", "group `name`")
	cmd.Var(&unrestrictedTokens, "unrestricted-tokens",
		"allow ordinary users to create tokens",
	)
	cmd.Var(&autoSubgroups, "auto-subgroups",
		"create subgroups automatically",
	)
	cmd.BoolVar(&doJSON, "json", false,
		"read JSON template from standard input",
	)
	cmd.Parse(args)

	if cmd.NArg() != 0 {
		cmd.Usage()
		os.Exit(1)
	}

	u, err := url.JoinPath(
		serverURL, "/galene-api/v0/.groups", groupname,
	)
	if err != nil {
		log.Fatalf("Build URL: %v", err)
	}

	data, err := stdinJSON(doJSON)
	if err != nil {
		log.Fatalf("Decode standard input: %v", err)
	}

	err = updateJSON(u, func(m map[string]any) map[string]any {
		// command line, if any, overrides template
		for k, v := range data {
			if v == nil {
				delete(m, k)
			} else {
				m[k] = v
			}
		}
		if unrestrictedTokens.set {
			m["unrestricted-tokens"] = unrestrictedTokens.value
		}
		if autoSubgroups.set {
			m["auto-subgroups"] = autoSubgroups.value
		}
		return m
	})

	if err != nil {
		log.Fatalf("Update group: %v", err)
	}
}

func parsePermissions(p string, expand bool) (any, error) {
	p = strings.TrimSpace(p)
	if len(p) == 0 {
		return nil, errors.New("empty permissions")
	}
	if p[0] == '[' {
		var a []any
		err := json.Unmarshal([]byte(p), &a)
		if err != nil {
			return nil, err
		}
		return a, nil
	}
	if !expand {
		return p, nil
	}
	pp, err := group.NewPermissions(p)
	if err != nil {
		return nil, err
	}
	return pp.Permissions(nil), nil
}

func formatRawPermissions(permissions []string) string {
	var perms []byte
	for _, p := range permissions {
		if len(p) > 0 {
			perms = append(perms, p[0])
		} else {
			perms = append(perms, '?')
		}
	}
	sort.Slice(perms, func(i, j int) bool {
		return perms[i] < perms[j]
	})
	return fmt.Sprintf("[%s]", perms)
}

func formatPermissions(permissions group.Permissions) string {
	s := permissions.String()
	if len(s) > 0 && s[0] != '[' {
		return s
	}
	return formatRawPermissions(permissions.Permissions(nil))
}

func match(patterns []string, value string) (bool, error) {
	for _, p := range patterns {
		found, err := path.Match(p, value)
		if err != nil {
			return false, err
		}
		if found {
			return true, nil
		}
	}
	return false, nil
}

func listUsersCmd(cmdname string, args []string) {
	var groupname string
	var long bool
	cmd := flag.NewFlagSet(cmdname, flag.ExitOnError)
	setUsage(cmd, cmdname,
		"%v [option...] %v [option...] [pattern...]\n",
		os.Args[0], cmdname,
	)
	cmd.StringVar(&groupname, "group", "", "group `name`")
	cmd.BoolVar(&long, "l", false, "display permissions")
	cmd.Parse(args)
	patterns := cmd.Args()

	if groupname == "" {
		fmt.Fprintf(cmd.Output(),
			"Option \"-group\" is required\n")
		os.Exit(1)
	}

	u, err := url.JoinPath(serverURL, "/galene-api/v0/.groups/", groupname,
		".users/")
	if err != nil {
		log.Fatalf("Build URL: %v", err)
	}
	var users []string
	_, err = getJSON(u, &users)
	if err != nil {
		log.Fatalf("Get users: %v", err)
	}
	sort.Slice(users, func(i, j int) bool {
		return users[i] < users[j]
	})
	for _, user := range users {
		if len(patterns) > 0 {
			found, err := match(patterns, user)
			if err != nil {
				log.Fatalf("Match: %v", err)
			}
			if !found {
				continue
			}
		}
		if !long {
			fmt.Println(user)
		} else {
			uu, err := url.JoinPath(u, user)
			if err != nil {
				fmt.Printf("%-12s (ERROR=%v)\n", user, err)
				continue
			}
			var d group.UserDescription
			_, err = getJSON(uu, &d)
			if err != nil {
				fmt.Printf("%-12s (ERROR=%v)\n", user, err)
				continue
			}
			fmt.Printf("%-12s %v\n",
				user, formatPermissions(d.Permissions),
			)
		}
	}
}

func userURL(wildcard bool, groupname, username string) (string, error) {
	if wildcard {
		return url.JoinPath(
			serverURL, "/galene-api/v0/.groups", groupname,
			".wildcard-user",
		)
	}
	return url.JoinPath(
		serverURL, "/galene-api/v0/.groups", groupname,
		".users", username,
	)
}

func createUserCmd(cmdname string, args []string) {
	var groupname, username string
	var wildcard bool
	var permissions stringOption
	var doJSON bool
	cmd := flag.NewFlagSet(cmdname, flag.ExitOnError)
	setUsage(cmd, cmdname,
		"%v [option...] %v [option...]\n",
		os.Args[0], cmdname,
	)
	cmd.StringVar(&groupname, "group", "", "group `name`")
	cmd.StringVar(&username, "user", "", "user `name`")
	cmd.BoolVar(&wildcard, "wildcard", false, "create the wildcard user")
	cmd.Var(&permissions, "permissions",
		"permissions (default \"present\")")
	cmd.BoolVar(&doJSON, "json", false,
		"read JSON template from standard input",
	)
	cmd.Parse(args)

	if cmd.NArg() != 0 {
		cmd.Usage()
		os.Exit(1)
	}

	if groupname == "" {
		fmt.Fprintf(cmd.Output(),
			"Option \"-group\" is required\n")
		os.Exit(1)
	}

	if wildcard != (username == "") {
		fmt.Fprintf(cmd.Output(),
			"Exactly one of \"-user\" and \"-wildcard\" "+
				"is required\n")
		os.Exit(1)
	}

	var perms any
	if permissions.set {
		var err error
		perms, err = parsePermissions(permissions.value, false)
		if err != nil {
			fmt.Fprintf(cmd.Output(),
				"Could parse \"-permissions\"\n",
			)
		}
	}

	u, err := userURL(wildcard, groupname, username)
	if err != nil {
		log.Fatalf("Build URL: %v", err)
	}

	data, err := stdinJSON(doJSON)
	if err != nil {
		log.Fatalf("Decode standard input: %v", err)
	}

	// command line overrides template.  If neither, default to "present".
	if permissions.set {
		data["permissions"] = perms
	} else if _, ok := data["permissions"]; !ok {
		data["permissions"] = "present"
	}

	err = putJSON(u, data, false)
	if err != nil {
		log.Fatalf("Create user: %v", err)
	}
}

func updateUserCmd(cmdname string, args []string) {
	var groupname, username string
	var wildcard bool
	var permissions stringOption
	var doJSON bool
	cmd := flag.NewFlagSet(cmdname, flag.ExitOnError)
	setUsage(cmd, cmdname,
		"%v [option...] %v [option...]\n",
		os.Args[0], cmdname,
	)
	cmd.StringVar(&groupname, "group", "", "group `name`")
	cmd.StringVar(&username, "user", "", "user `name`")
	cmd.BoolVar(&wildcard, "wildcard", false, "update the wildcard user")
	cmd.Var(&permissions, "permissions", "permissions")
	cmd.Parse(args)

	if cmd.NArg() != 0 {
		cmd.Usage()
		os.Exit(1)
	}

	u, err := userURL(wildcard, groupname, username)
	if err != nil {
		log.Fatalf("Build URL: %v", err)
	}

	var perms any
	if permissions.set {
		var err error
		perms, err = parsePermissions(permissions.value, false)
		if err != nil {
			fmt.Fprintf(cmd.Output(),
				"Could parse \"-permissions\"\n",
			)
		}
	}

	data, err := stdinJSON(doJSON)
	if err != nil {
		log.Fatalf("Decode standard input: %v", err)
	}

	err = updateJSON(u, func(m map[string]any) map[string]any {
		// command line, if any, overrides template
		for k, v := range data {
			if v == nil {
				delete(m, k)
			} else {
				m[k] = v
			}
		}
		if permissions.set {
			m["permissions"] = perms
		}
		return m
	})

	if err != nil {
		log.Fatalf("Update user: %v", err)
	}
}

func deleteUserCmd(cmdname string, args []string) {
	var groupname, username string
	var wildcard bool
	cmd := flag.NewFlagSet(cmdname, flag.ExitOnError)
	setUsage(cmd, cmdname, "%v [option...] %v [option...]\n",
		os.Args[0], cmdname,
	)
	cmd.StringVar(&groupname, "group", "", "group `name`")
	cmd.StringVar(&username, "user", "", "user `name`")
	cmd.BoolVar(&wildcard, "wildcard", false, "delete the wildcard user")
	cmd.Parse(args)

	if cmd.NArg() != 0 {
		cmd.Usage()
		os.Exit(1)
	}

	if groupname == "" {
		fmt.Fprintf(cmd.Output(),
			"Option \"-group\" is required\n")
		os.Exit(1)
	}

	if wildcard != (username == "") {
		fmt.Fprintf(cmd.Output(),
			"Exactly one of \"-user\" and \"-wildcard\" "+
				"is required\n")
		os.Exit(1)
	}

	var u string
	var err error
	if wildcard {
		u, err = url.JoinPath(
			serverURL, "/galene-api/v0/.groups", groupname,
			".wildcard-user",
		)
	} else {
		u, err = url.JoinPath(
			serverURL, "/galene-api/v0/.groups", groupname,
			".users", username,
		)
	}
	if err != nil {
		log.Fatalf("Build URL: %v", err)
	}

	err = deleteValue(u)
	if err != nil {
		log.Fatalf("Delete user: %v", err)
	}
}

func showGroupCmd(cmdname string, args []string) {
	var groupname string
	cmd := flag.NewFlagSet(cmdname, flag.ExitOnError)
	setUsage(cmd, cmdname,
		"%v [option...] %v\n",
		os.Args[0], cmdname,
	)
	cmd.StringVar(&groupname, "group", "", "group `name`")
	cmd.Parse(args)

	if cmd.NArg() != 0 {
		cmd.Usage()
		os.Exit(1)
	}

	if groupname == "" {
		log.Fatal("Option \"-group\" is required.")
	}

	u, err := url.JoinPath(serverURL, "/galene-api/v0/.groups/", groupname)
	if err != nil {
		log.Fatalf("Build URL: %v", err)
	}

	var description map[string]any
	_, err = getJSON(u, &description)
	if err != nil {
		log.Fatalf("Get group description: %v", err)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "    ")
	err = encoder.Encode(&description)
	if err != nil {
		log.Fatalf("Encode: %v", err)
	}
}

func listGroupsCmd(cmdname string, args []string) {
	cmd := flag.NewFlagSet(cmdname, flag.ExitOnError)
	setUsage(cmd, cmdname,
		"%v [option...] %v [pattern...]\n",
		os.Args[0], cmdname,
	)
	cmd.Parse(args)
	patterns := cmd.Args()

	u, err := url.JoinPath(serverURL, "/galene-api/v0/.groups/")
	if err != nil {
		log.Fatalf("Build URL: %v", err)
	}

	var groups []string
	_, err = getJSON(u, &groups)
	if err != nil {
		log.Fatalf("Get groups: %v", err)
	}
	sort.Slice(groups, func(i, j int) bool {
		return groups[i] < groups[j]
	})
	for _, g := range groups {
		if len(patterns) > 0 {
			found, err := match(patterns, g)
			if err != nil {
				log.Fatalf("Match: %v", err)
			}
			if !found {
				continue
			}
		}
		fmt.Println(g)
	}
}

func listTokensCmd(cmdname string, args []string) {
	var groupname stringOption
	var long bool
	cmd := flag.NewFlagSet(cmdname, flag.ExitOnError)
	setUsage(cmd, cmdname, "%v [option...] %v [option...]\n",
		os.Args[0], cmdname,
	)
	cmd.Var(&groupname, "group", "group `name`")
	cmd.BoolVar(&long, "l", false, "display token fields")
	cmd.Parse(args)

	if cmd.NArg() != 0 {
		cmd.Usage()
		os.Exit(1)
	}

	if !groupname.set {
		fmt.Fprintf(cmd.Output(),
			"Option \"-group\" is required\n")
		os.Exit(1)
	}

	u, err := url.JoinPath(
		serverURL, "/galene-api/v0/.groups/", groupname.value, ".tokens/",
	)

	if err != nil {
		log.Fatalf("Build URL: %v", err)
	}

	var tokens []string
	_, err = getJSON(u, &tokens)
	if err != nil {
		log.Fatalf("Get tokens: %v", err)
	}
	sort.Slice(tokens, func(i, j int) bool {
		return tokens[i] < tokens[j]
	})
	now := time.Now()
	for _, t := range tokens {
		if !long {
			fmt.Println(t)
		} else {
			uu, err := url.JoinPath(u, t)
			if err != nil {
				fmt.Printf("%-12s (ERROR=%v)\n", t, err)
				continue
			}
			var tt token.Stateful
			_, err = getJSON(uu, &tt)
			if err != nil {
				fmt.Printf("%-12s (ERROR=%v)\n", t, err)
				continue
			}
			var username string
			if tt.Username != nil {
				username = *tt.Username
			}
			var exp string
			if tt.Expires == nil {
				exp = "(no expiration date)"
			} else if tt.Expires.Before(now) {
				exp = "(expired)"
			} else {
				exp = tt.Expires.Format(time.DateTime)
			}
			var perms []byte
			if tt.IncludeSubgroups {
				perms = append(perms, 'H')
			}
			for _, p := range tt.Permissions {
				if len(p) > 0 {
					perms = append(perms, p[0])
				} else {
					perms = append(perms, '?')
				}
			}
			sort.Slice(perms, func(i, j int) bool {
				return perms[i] < perms[j]
			})
			fmt.Printf("%-11s %-20s %-4s %-20s\n", t,
				username, perms, exp,
			)
		}
	}
}

func createTokenCmd(cmdname string, args []string) {
	var groupname stringOption
	var username, permissions string
	var includeSubgroups boolOption
	cmd := flag.NewFlagSet(cmdname, flag.ExitOnError)
	setUsage(cmd, cmdname, "%v [option...] %v [option...]\n",
		os.Args[0], cmdname,
	)
	cmd.Var(&groupname, "group", "group `name`")
	cmd.Var(&includeSubgroups, "include-subgroups", "include subgroups")
	cmd.StringVar(&username, "user", "", "encode user `name` in token")
	cmd.StringVar(&permissions, "permissions", "present", "permissions")
	cmd.Parse(args)

	if cmd.NArg() != 0 {
		cmd.Usage()
		os.Exit(1)
	}

	if !groupname.set {
		fmt.Fprintf(cmd.Output(),
			"Option \"-group\" is required\n")
		os.Exit(1)
	}

	perms, err := parsePermissions(permissions, true)
	if err != nil {
		log.Fatalf("Parse permissions: %v", err)
	}
	t := make(map[string]any)
	t["permissions"] = perms
	t["expires"] = time.Now().Add(24 * time.Hour)
	if username != "" {
		t["username"] = username
	}
	if includeSubgroups.set {
		t["includeSubgroups"] = includeSubgroups.value
	}

	u, err := url.JoinPath(
		serverURL, "/galene-api/v0/.groups/", groupname.value, ".tokens/",
	)
	if err != nil {
		log.Fatalf("Build URL: %v", err)
	}

	location, err := postJSON(u, t)
	if err != nil {
		log.Fatalf("Create token: %v", err)
	}
	fmt.Println(location)
}

func revokeTokenCmd(cmdname string, args []string) {
	var groupname stringOption
	var token string
	cmd := flag.NewFlagSet(cmdname, flag.ExitOnError)
	setUsage(cmd, cmdname, "%v [option...] %v [option...]\n",
		os.Args[0], cmdname,
	)
	cmd.Var(&groupname, "group", "group `name`")
	cmd.StringVar(&token, "token", "", "`token` to delete")
	cmd.Parse(args)

	if cmd.NArg() != 0 {
		cmd.Usage()
		os.Exit(1)
	}

	if !groupname.set || token == "" {
		fmt.Fprintf(cmd.Output(),
			"Options \"-group\" and \"-token\" are required\n")
		os.Exit(1)
	}

	u, err := url.JoinPath(
		serverURL, "/galene-api/v0/.groups/", groupname.value,
		".tokens", token,
	)
	if err != nil {
		log.Fatalf("Build URL: %v", err)
	}

	err = updateJSON(u, func(v map[string]any) map[string]any {
		v["expires"] = time.Now().Add(-time.Minute)
		return v
	})
	if err != nil {
		log.Fatalf("Update token: %v", err)
	}
}

func deleteTokenCmd(cmdname string, args []string) {
	var groupname stringOption
	var token string
	cmd := flag.NewFlagSet(cmdname, flag.ExitOnError)
	setUsage(cmd, cmdname, "%v [option...] %v [option...]\n",
		os.Args[0], cmdname,
	)
	cmd.Var(&groupname, "group", "group `name`")
	cmd.StringVar(&token, "token", "", "`token` to delete")
	cmd.Parse(args)

	if cmd.NArg() != 0 {
		cmd.Usage()
		os.Exit(1)
	}

	if !groupname.set || token == "" {
		fmt.Fprintf(cmd.Output(),
			"Options \"-group\" and \"-token\" are required\n")
		os.Exit(1)
	}

	u, err := url.JoinPath(
		serverURL, "/galene-api/v0/.groups/", groupname.value,
		".tokens", token,
	)
	if err != nil {
		log.Fatalf("Build URL: %v", err)
	}
	err = deleteValue(u)
	if err != nil {
		log.Fatalf("Delete token: %v", err)
	}
}
