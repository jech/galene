package main

import (
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
	"os"
	"path/filepath"
	"slices"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/pbkdf2"

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
		slices.Sort(names)
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
	var algorithm string
	var iterations int
	var cost int
	var length int
	var saltlen int

	cmd := flag.NewFlagSet(cmdname, flag.ExitOnError)
	setUsage(cmd, cmdname,
		"%v [option...] %v [option...] password...\n",
		os.Args[0], cmdname,
	)
	cmd.StringVar(&algorithm, "hash", "pbkdf2",
		"hashing `algorithm`")
	cmd.IntVar(&iterations, "iterations", 4096,
		"`number` of iterations (pbkdf2)")
	cmd.IntVar(&cost, "cost", bcrypt.DefaultCost,
		"`cost` (bcrypt)")
	cmd.IntVar(&length, "key", 32, "key `length` (pbkdf2)")
	cmd.IntVar(&saltlen, "salt", 8, "salt `length` (pbkdf2)")
	cmd.Parse(args)

	if cmd.NArg() == 0 {
		cmd.Usage()
		os.Exit(1)
	}

	for _, pw := range cmd.Args() {
		p, err := makePassword(
			pw, algorithm, iterations, length, saltlen, cost,
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
}
