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

	"golang.org/x/crypto/pbkdf2"

	"github.com/jech/galene/group"
)

func main() {
	var iterations int
	var length int
	var saltLen int
	var username string
	flag.StringVar(&username, "user", "",
		"generate entry for given `username`")
	flag.IntVar(&iterations, "iterations", 4096, "`number` of iterations")
	flag.IntVar(&length, "key", 32, "key `length`")
	flag.IntVar(&saltLen, "salt", 8, "salt `length`")
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
		key := pbkdf2.Key(
			[]byte(pw), salt, iterations, length, sha256.New,
		)

		p := group.Password{
			Type:       "pbkdf2",
			Hash:       "sha-256",
			Key:        hex.EncodeToString(key),
			Salt:       hex.EncodeToString(salt),
			Iterations: iterations,
		}
		e := json.NewEncoder(os.Stdout)
		if username != "" {
			creds := group.ClientPattern{
				Username: username,
				Password: &p,
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
