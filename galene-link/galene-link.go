package main

import (
	"flag"
	"fmt"
	"log"
	"net/url"
	"path"
	"time"

	"github.com/golang-jwt/jwt/v4"

	"github.com/jech/galene/group"
	"github.com/jech/galene/token"
)

func main() {
	var username, kid, server string
	var valid int
	var tokenOnly bool
	flag.StringVar(&group.Directory, "groups", "./groups/",
		"group description `directory`")
	flag.StringVar(&username, "user", "", "username")
	flag.StringVar(&kid, "kid", "", "`id` of key to use")
	flag.IntVar(&valid, "valid", 86400, "`seconds` validity")
	flag.StringVar(&server, "server", "https://galene.org:8443",
		"server `url`")
	flag.BoolVar(&tokenOnly, "token", false, "generate token only")
	flag.Parse()

	if flag.NArg() != 1 {
		log.Fatal("One argument (the group URL) required")
	}
	groupname := flag.Arg(0)

	desc, err := group.GetDescription(groupname)
	if err != nil {
		log.Fatal("Get group description: ", err)
	}

	serverURL, err := url.Parse(server)
	if err != nil {
		log.Fatal("Couldn't parse server URL")
	}
	pth := path.Join(path.Join(serverURL.Path, "group"), groupname) + "/"
	groupURL := &url.URL{
		Scheme: serverURL.Scheme,
		Host:   serverURL.Host,
		Path:   pth,
	}

	keys := desc.AuthKeys
	var key map[string]interface{}
	for _, k := range keys {
		kid2, _ := k["kid"].(string)
		if kid == "" || kid == kid2 {
			key = k
			break
		}
	}

	if key == nil {
		log.Fatal("Couldn't find key")
	}

	alg, ok := key["alg"].(string)
	var method jwt.SigningMethod
	if ok {
		method = jwt.GetSigningMethod(alg)
	}
	if method == nil {
		log.Fatal("Couldn't determine key signing method")
	}

	kstring, err := token.ParseKey(key)
	if err != nil {
		log.Fatal("Couldn't parse key")
	}

	now := time.Now()
	end := now.Add(time.Second * time.Duration(valid))
	token := jwt.NewWithClaims(
		method,
		&jwt.MapClaims{
			"sub":         username,
			"aud":         groupURL.String(),
			"exp":         &jwt.NumericDate{end},
			"nbf":         &jwt.NumericDate{now},
			"iat":         &jwt.NumericDate{now},
			"permissions": []string{"present"},
		},
	)

	s, err := token.SignedString(kstring)
	if err != nil {
		log.Fatal("Couldn't sign token: ", err)
	}

	if tokenOnly {
		fmt.Println(s)
	} else {
		query := url.Values{}
		if username != "" {
			query.Add("username", username)
		}
		query.Add("token", s)
		outURL := &url.URL{
			Scheme:   groupURL.Scheme,
			Host:     groupURL.Host,
			Path:     groupURL.Path,
			RawQuery: query.Encode(),
		}
		fmt.Println(outURL.String())
	}
}
