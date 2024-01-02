package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/pbkdf2"

	"github.com/jech/galene/group"
)

func main() {
	var algorithm string
	var iterations int
	var cost int
	var length int
	var saltLen int
	var username string
	var permissions string
	flag.StringVar(&username, "user", "",
		"generate entry for given `username`")
	flag.StringVar(&permissions, "permissions", "present",
		"`permissions` for user entry")
	flag.StringVar(&algorithm, "hash", "pbkdf2",
		"hashing `algorithm`")
	flag.IntVar(&iterations, "iterations", 4096,
		"`number` of iterations (pbkdf2)")
	flag.IntVar(&cost, "cost", bcrypt.DefaultCost,
		"`cost` (bcrypt)")
	flag.IntVar(&length, "key", 32, "key `length` (pbkdf2)")
	flag.IntVar(&saltLen, "salt", 8, "salt `length` (pbkdf2)")
	flag.Parse()

	if len(flag.Args()) == 0 {
		fmt.Fprintf(
			flag.CommandLine.Output(),
			"Usage: %s [option...] password...\n",
			os.Args[0])
		flag.PrintDefaults()
		os.Exit(2)
	}

	salt := make([]byte, saltLen)

	for _, pw := range flag.Args() {
		_, err := rand.Read(salt)
		if err != nil {
			log.Fatalf("Salt: %v", err)
		}
		var p group.Password
		if strings.EqualFold(algorithm, "pbkdf2") {
			key := pbkdf2.Key(
				[]byte(pw), salt, iterations, length, sha256.New,
			)
			p = group.Password{
				Type:       "pbkdf2",
				Hash:       "sha-256",
				Key:        hex.EncodeToString(key),
				Salt:       hex.EncodeToString(salt),
				Iterations: iterations,
			}
		} else if strings.EqualFold(algorithm, "bcrypt") {
			key, err := bcrypt.GenerateFromPassword(
				[]byte(pw), cost,
			)
			if err != nil {
				log.Fatalf("Couldn't hash password: %v", err)
			}

			p = group.Password{
				Type: "bcrypt",
				Key:  string(key),
			}
		} else {
			log.Fatalf("Unknown hash type %v", algorithm)
		}

		e := json.NewEncoder(os.Stdout)
		if username != "" {
			perms, err := group.NewPermissions(permissions)
			if err != nil {
				log.Fatalf("NewPermissions: %v", err)
			}
			creds := make(map[string]group.UserDescription)
			creds[username] = group.UserDescription{
				Password:    p,
				Permissions: perms,
			}
			err = e.Encode(creds)
		} else {
			err = e.Encode(p)
		}
		if err != nil {
			log.Fatalf("Encode: %v", err)
		}
	}
}
