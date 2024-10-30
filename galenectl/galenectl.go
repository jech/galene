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
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/pbkdf2"
	"golang.org/x/term"

	"github.com/jech/galene/group"
)

type configuration struct {
	Server        string `json:"server"`
	AdminUsername string `json:"admin-username"`
	AdminPassword string `json:"admin-password"`
	AdminToken    string `json:"admin-token"`
}

var insecure bool
var serverURL, adminUsername, adminPassword, adminToken string

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
	"set-password": {
		command:     setPasswordCmd,
		description: "set a user's password",
	},
	"delete-password": {
		command:     deletePasswordCmd,
		description: "delete a user's password",
	},
	"create-group": {
		command:     createGroupCmd,
		description: "create a group",
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
	"list-groups": {
		command:     listGroupsCmd,
		description: "list groups",
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
	configFile := filepath.Join(
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
	cmd.StringVar(&algorithm, "type", "pbkdf2",
		"password `type`")
	cmd.IntVar(&iterations, "iterations", 4096,
		"`number` of iterations (pbkdf2)")
	cmd.IntVar(&cost, "cost", bcrypt.DefaultCost,
		"`cost` (bcrypt)")
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

func setAuthorization(req *http.Request) {
	if adminToken != "" {
		req.Header.Set("Authorization", "Bearer "+adminToken)
	} else if adminUsername != "" {
		req.SetBasicAuth(adminUsername, adminPassword)
	}
}

func getJSON(url string, value any) error {
	req, err := http.NewRequest("GET", url, nil)
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
		return fmt.Errorf("%v %v", resp.StatusCode, resp.Status)
	}

	decoder := json.NewDecoder(resp.Body)
	return decoder.Decode(value)
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
		return errors.New(resp.Status)
	}
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
		return "", errors.New(resp.Status)
	}
	location := resp.Header.Get("location")
	return location, nil
}

func updateJSON[T any](url string, update func(T) T) error {
	req, err := http.NewRequest("GET", url, nil)
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
		return fmt.Errorf("%v %v", resp.StatusCode, resp.Status)
	}
	etag := resp.Header.Get("ETag")
	if etag == "" {
		return errors.New("missing ETag")
	}

	decoder := json.NewDecoder(req.Body)
	var old T
	err = decoder.Decode(&old)
	if err != nil {
		return err
	}

	value := update(old)

	j, err := json.Marshal(value)
	if err != nil {
		return err
	}
	req2, err := http.NewRequest("PUT", url, bytes.NewReader(j))
	setAuthorization(req2)
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("If-Match", etag)

	resp2, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp2.Body.Close()
	if resp2.StatusCode >= 300 {
		return fmt.Errorf("%v %v", resp.StatusCode, resp.Status)
	}
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
		return fmt.Errorf("%v %v", resp.StatusCode, resp.Status)
	}
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
	cmd.StringVar(&algorithm, "type", "pbkdf2",
		"password `type`")
	cmd.IntVar(&iterations, "iterations", 4096,
		"`number` of iterations (pbkdf2)")
	cmd.IntVar(&cost, "cost", bcrypt.DefaultCost,
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

func createGroupCmd(cmdname string, args []string) {
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

	url, err := url.JoinPath(
		serverURL, "/galene-api/v0/.groups", groupname,
	)
	if err != nil {
		log.Fatalf("Build URL: %v", err)
	}

	err = putJSON(url, map[string]any{}, false)
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

	url, err := url.JoinPath(
		serverURL, "/galene-api/v0/.groups", groupname,
	)
	if err != nil {
		log.Fatalf("Build URL: %v", err)
	}

	err = deleteValue(url)
	if err != nil {
		log.Fatalf("Delete group: %v", err)
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

func listUsersCmd(cmdname string, args []string) {
	var groupname string
	var long bool
	cmd := flag.NewFlagSet(cmdname, flag.ExitOnError)
	setUsage(cmd, cmdname,
		"%v [option...] %v [option...]\n",
		os.Args[0], cmdname,
	)
	cmd.StringVar(&groupname, "group", "", "group `name`")
	cmd.BoolVar(&long, "l", false, "display permissions")
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

	u, err := url.JoinPath(serverURL, "/galene-api/v0/.groups/", groupname,
		".users/")
	if err != nil {
		log.Fatalf("Build URL: %v", err)
	}
	var users []string
	err = getJSON(u, &users)
	if err != nil {
		log.Fatalf("Get users: %v", err)
	}
	sort.Slice(users, func(i, j int) bool {
		return users[i] < users[j]
	})
	for _, user := range users {
		if !long {
			fmt.Println(user)
		} else {
			uu, err := url.JoinPath(u, user)
			if err != nil {
				fmt.Printf("%-12s (ERROR=%v)\n", user, err)
				continue
			}
			var d group.UserDescription
			err = getJSON(uu, &d)
			if err != nil {
				fmt.Printf("%-12s (ERROR=%v)\n", user, err)
				continue
			}
			fmt.Printf("%-12s %v\n", user, d.Permissions)
		}
	}
}

func createUserCmd(cmdname string, args []string) {
	var groupname, username string
	var wildcard bool
	var permissions string
	cmd := flag.NewFlagSet(cmdname, flag.ExitOnError)
	setUsage(cmd, cmdname,
		"%v [option...] %v [option...]\n",
		os.Args[0], cmdname,
	)
	cmd.StringVar(&groupname, "group", "", "group `name`")
	cmd.StringVar(&username, "user", "", "user `name`")
	cmd.BoolVar(&wildcard, "wildcard", false, "create the wildcard user")
	cmd.StringVar(&permissions, "permissions", "present", "permissions")
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

	perms, err := parsePermissions(permissions, false)
	if err != nil {
		log.Fatalf("Parse permissions: %v", err)
	}

	var u string
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

	dict := map[string]any{"permissions": perms}
	err = putJSON(u, dict, false)
	if err != nil {
		log.Fatalf("Create user: %v", err)
	}
}

func updateUserCmd(cmdname string, args []string) {
	var groupname, username string
	var wildcard bool
	var permissions string
	cmd := flag.NewFlagSet(cmdname, flag.ExitOnError)
	setUsage(cmd, cmdname,
		"%v [option...] %v [option...]\n",
		os.Args[0], cmdname,
	)
	cmd.StringVar(&groupname, "group", "", "group `name`")
	cmd.StringVar(&username, "user", "", "user `name`")
	cmd.BoolVar(&wildcard, "wildcard", false, "update the wildcard user")
	cmd.StringVar(&permissions, "permissions", "", "permissions")
	cmd.Parse(args)

	if cmd.NArg() != 0 {
		cmd.Usage()
		os.Exit(1)
	}

	perms, err := parsePermissions(permissions, false)
	if err != nil {
		log.Fatalf("Parse permissions: %v", err)
	}

	var u string
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

	err = updateJSON(u, func(m map[string]any) map[string]any {
		m["permissions"] = perms
		return m
	})

	if err != nil {
		log.Fatalf("Create user: %v", err)
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

func listGroupsCmd(cmdname string, args []string) {
	cmd := flag.NewFlagSet(cmdname, flag.ExitOnError)
	setUsage(cmd, cmdname,
		"%v [option...] %v\n",
		os.Args[0], cmdname,
	)
	cmd.Parse(args)

	u, err := url.JoinPath(serverURL, "/galene-api/v0/.groups/")
	if err != nil {
		log.Fatalf("Build URL: %v", err)
	}

	var groups []string
	err = getJSON(u, &groups)
	if err != nil {
		log.Fatalf("Get groups: %v", err)
	}
	sort.Slice(groups, func(i, j int) bool {
		return groups[i] < groups[j]
	})
	for _, g := range groups {
		fmt.Println(g)
	}
}

func createTokenCmd(cmdname string, args []string) {
	var groupname, username, permissions string
	cmd := flag.NewFlagSet(cmdname, flag.ExitOnError)
	setUsage(cmd, cmdname, "%v [option...] %v [option...]\n",
		os.Args[0], cmdname,
	)
	cmd.StringVar(&groupname, "group", "", "group `name`")
	cmd.StringVar(&username, "user", "", "encode user `name` in token")
	cmd.StringVar(&permissions, "permissions", "present", "permissions")
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

	u, err := url.JoinPath(
		serverURL, "/galene-api/v0/.groups/", groupname, ".tokens/",
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
	var groupname, token string
	cmd := flag.NewFlagSet(cmdname, flag.ExitOnError)
	setUsage(cmd, cmdname, "%v [option...] %v [option...]\n",
		os.Args[0], cmdname,
	)
	cmd.StringVar(&groupname, "group", "", "group `name`")
	cmd.StringVar(&token, "token", "", "`token` to delete")
	cmd.Parse(args)

	if cmd.NArg() != 0 {
		cmd.Usage()
		os.Exit(1)
	}

	if groupname == "" || token == "" {
		fmt.Fprintf(cmd.Output(),
			"Options \"-group\" and \"-token\" are required\n")
		os.Exit(1)
	}

	u, err := url.JoinPath(
		serverURL, "/galene-api/v0/.groups/", groupname,
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
	var groupname, token string
	cmd := flag.NewFlagSet(cmdname, flag.ExitOnError)
	setUsage(cmd, cmdname, "%v [option...] %v [option...]\n",
		os.Args[0], cmdname,
	)
	cmd.StringVar(&groupname, "group", "", "group `name`")
	cmd.StringVar(&token, "token", "", "`token` to delete")
	cmd.Parse(args)

	if cmd.NArg() != 0 {
		cmd.Usage()
		os.Exit(1)
	}

	if groupname == "" || token == "" {
		fmt.Fprintf(cmd.Output(),
			"Options \"-group\" and \"-token\" are required\n")
		os.Exit(1)
	}

	u, err := url.JoinPath(
		serverURL, "/galene-api/v0/.groups/", groupname,
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
