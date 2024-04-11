package webserver

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"golang.org/x/crypto/pbkdf2"

	"github.com/jech/galene/group"
	"github.com/jech/galene/stats"
)

func parseContentType(ctype string) string {
	return strings.Trim(strings.Split(ctype, ";")[0], " ")
}

// checkAdmin checks whether the client authentifies as an administrator
func checkAdmin(w http.ResponseWriter, r *http.Request) bool {
	username, password, ok := r.BasicAuth()
	if ok {
		ok, _ = adminMatch(username, password)
	}
	if !ok {
		failAuthentication(w, "/galene-api/")
		return false
	}
	return true
}

// checkPasswordAdmin checks whether the client authentifies as either an
// administrator or the given user.  It is used to check whether the
// client has the right to change user's password.
func checkPasswordAdmin(w http.ResponseWriter, r *http.Request, groupname, user string) bool {
	username, password, ok := r.BasicAuth()
	if ok {
		ok, _ := adminMatch(username, password)
		if ok {
			return true
		}
	}
	if ok && username == user {
		desc, err := group.GetDescription(groupname)
		if err == nil && desc.Users != nil {
			u, ok := desc.Users[user]
			if ok {
				ok, _ := u.Password.Match(password)
				if ok {
					return true
				}
			}
		}
	}
	failAuthentication(w, "/galene-api/")
	return false
}

func apiHandler(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/galene-api/") {
		http.NotFound(w, r)
		return
	}

	first, kind, rest := splitPath(r.URL.Path[len("/galene-api"):])
	if first == "/0" && kind == ".stats" && rest == "" {
		if !checkAdmin(w, r) {
			return
		}
		if r.Method != "HEAD" && r.Method != "GET" {
			methodNotAllowed(w, "HEAD", "GET")
			return
		}
		w.Header().Set("content-type", "application/json")
		w.Header().Set("cache-control", "no-cache")
		if r.Method == "HEAD" {
			return
		}

		ss := stats.GetGroups()
		e := json.NewEncoder(w)
		e.Encode(ss)
		return
	} else if first == "/0" && kind == ".groups" {
		apiGroupHandler(w, r, rest)
		return
	}

	http.NotFound(w, r)
	return
}

func apiGroupHandler(w http.ResponseWriter, r *http.Request, pth string) {
	first, kind, rest := splitPath(pth)
	if first == "" {
		notFound(w)
		return
	}
	g := first[1:]
	if g == "" && kind == "" {
		if !checkAdmin(w, r) {
			return
		}
		if r.Method != "HEAD" && r.Method != "GET" {
			methodNotAllowed(w, "HEAD", "GET")
			return
		}
		groups, err := group.GetDescriptionNames()
		if err != nil {
			httpError(w, err)
			return
		}
		w.Header().Set("content-type", "text/plain; charset=utf-8")
		if r.Method == "HEAD" {
			return
		}
		for _, g := range groups {
			fmt.Fprintln(w, g)
		}
		return
	}

	if kind == ".users" {
		apiUserHandler(w, r, g, rest)
		return
	} else if kind == ".fallback-users" && rest == "" {
		fallbackUsersHandler(w, r, g)
		return
	} else if kind == ".keys" && rest == "" {
		keysHandler(w, r, g)
		return
	} else if kind != "" {
		if !checkAdmin(w, r) {
			return
		}
		notFound(w)
		return
	}

	if !checkAdmin(w, r) {
		return
	}

	if r.Method == "HEAD" || r.Method == "GET" {
		desc, etag, err := group.GetSanitisedDescription(g)
		if err != nil {
			httpError(w, err)
			return
		}

		w.Header().Set("etag", etag)

		done := checkPreconditions(w, r, etag)
		if done {
			return
		}

		w.Header().Set("content-type", "application/json")
		if r.Method == "HEAD" {
			return
		}

		e := json.NewEncoder(w)
		e.Encode(desc)
		return
	} else if r.Method == "PUT" {
		etag, err := group.GetDescriptionTag(g)
		if os.IsNotExist(err) {
			err = nil
			etag = ""
		} else if err != nil {
			httpError(w, err)
			return
		}

		done := checkPreconditions(w, r, etag)
		if done {
			return
		}

		ctype := parseContentType(
			r.Header.Get("Content-Type"),
		)
		if !strings.EqualFold(ctype, "application/json") {
			http.Error(w, "unsupported content type",
				http.StatusUnsupportedMediaType)
			return
		}
		d := json.NewDecoder(r.Body)
		var newdesc group.Description
		err = d.Decode(&newdesc)
		if err != nil {
			httpError(w, err)
			return
		}
		err = group.UpdateDescription(g, etag, &newdesc)
		if err != nil {
			httpError(w, err)
			return
		}
		if etag == "" {
			w.WriteHeader(http.StatusCreated)
		} else {
			w.WriteHeader(http.StatusNoContent)
		}
		return
	} else if r.Method == "DELETE" {
		etag, err := group.GetDescriptionTag(g)
		if err != nil {
			httpError(w, err)
			return
		}

		done := checkPreconditions(w, r, etag)
		if done {
			return
		}
		err = group.DeleteDescription(g, etag)
		if err != nil {
			httpError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	methodNotAllowed(w, "HEAD", "GET", "PUT", "DELETE")
	return
}

func apiUserHandler(w http.ResponseWriter, r *http.Request, g, pth string) {
	if pth == "/" {
		if !checkAdmin(w, r) {
			return
		}
		if r.Method != "HEAD" && r.Method != "GET" {
			http.Error(w, "method not allowed",
				http.StatusMethodNotAllowed)
			return
		}
		users, etag, err := group.GetUsers(g)
		if err != nil {
			httpError(w, err)
			return
		}
		w.Header().Set("content-type", "text/plain; charset=utf-8")
		w.Header().Set("etag", etag)
		done := checkPreconditions(w, r, etag)
		if done {
			return
		}
		if r.Method == "HEAD" {
			return
		}
		for _, u := range users {
			fmt.Fprintln(w, u)
		}
		return
	}

	first2, kind2, rest2 := splitPath(pth)
	if first2 != "" && kind2 == ".password" && rest2 == "" {
		passwordHandler(w, r, g, first2[1:])
		return
	} else if kind2 != "" || first2 == "" {
		if !checkAdmin(w, r) {
			return
		}
		notFound(w)
		return
	}

	if !checkAdmin(w, r) {
		return
	}

	username := first2[1:]
	if r.Method == "HEAD" || r.Method == "GET" {
		w.Header().Set("content-type", "application/json")
		user, etag, err := group.GetSanitisedUser(g, username)
		if err != nil {
			httpError(w, err)
			return
		}
		w.Header().Set("content-type", "application/json")
		w.Header().Set("etag", etag)
		done := checkPreconditions(w, r, etag)
		if done {
			return
		}
		if r.Method == "HEAD" {
			return
		}
		e := json.NewEncoder(w)
		e.Encode(user)
		return
	} else if r.Method == "PUT" {
		etag, err := group.GetUserTag(g, username)
		if os.IsNotExist(err) {
			etag = ""
			err = nil
		} else if err != nil {
			httpError(w, err)
			return
		}

		done := checkPreconditions(w, r, etag)
		if done {
			return
		}

		ctype := parseContentType(r.Header.Get("Content-Type"))
		if !strings.EqualFold(ctype, "application/json") {
			http.Error(w, "unsupported content type",
				http.StatusUnsupportedMediaType)
			return
		}
		d := json.NewDecoder(r.Body)
		var newdesc group.UserDescription
		err = d.Decode(&newdesc)
		if err != nil {
			httpError(w, err)
			return
		}
		err = group.UpdateUser(g, username, etag, &newdesc)
		if err != nil {
			httpError(w, err)
			return
		}
		if etag == "" {
			w.WriteHeader(http.StatusCreated)
		} else {
			w.WriteHeader(http.StatusNoContent)
		}
		return
	} else if r.Method == "DELETE" {
		etag, err := group.GetUserTag(g, username)
		if err != nil {
			httpError(w, err)
			return
		}

		done := checkPreconditions(w, r, etag)
		if done {
			return
		}

		err = group.DeleteUser(g, username, etag)
		if err != nil {
			httpError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	methodNotAllowed(w, "HEAD", "GET", "PUT", "DELETE")
	return
}

func passwordHandler(w http.ResponseWriter, r *http.Request, g, user string) {
	if !checkPasswordAdmin(w, r, g, user) {
		return
	}

	if r.Method == "PUT" {
		ctype := parseContentType(r.Header.Get("Content-Type"))
		if !strings.EqualFold(ctype, "application/json") {
			http.Error(w, "unsupported content type",
				http.StatusUnsupportedMediaType)
			return
		}
		d := json.NewDecoder(r.Body)
		var pw group.Password
		err := d.Decode(&pw)
		if err != nil {
			httpError(w, err)
			return
		}
		err = group.SetUserPassword(g, user, pw)
		if err != nil {
			httpError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	} else if r.Method == "POST" {
		ctype := parseContentType(r.Header.Get("Content-Type"))
		if !strings.EqualFold(ctype, "text/plain") {
			http.Error(w, "unsupported content type",
				http.StatusUnsupportedMediaType)
			return
		}

		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
		if err != nil {
			httpError(w, err)
			return
		}
		salt := make([]byte, 8)
		_, err = rand.Read(salt)
		if err != nil {
			httpError(w, err)
			return
		}
		iterations := 4096
		key := pbkdf2.Key(body, salt, iterations, 32, sha256.New)
		pw := group.Password{
			Type:       "pbkdf2",
			Hash:       "sha-256",
			Key:        hex.EncodeToString(key),
			Salt:       hex.EncodeToString(salt),
			Iterations: iterations,
		}
		err = group.SetUserPassword(g, user, pw)
		if err != nil {
			httpError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	} else if r.Method == "DELETE" {
		err := group.SetUserPassword(g, user, group.Password{})
		if err != nil {
			httpError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	methodNotAllowed(w, "PUT", "POST", "DELETE")
	return
}

func fallbackUsersHandler(w http.ResponseWriter, r *http.Request, g string) {
	if !checkAdmin(w, r) {
		return
	}

	if r.Method == "PUT" {
		ctype := parseContentType(r.Header.Get("Content-Type"))
		if !strings.EqualFold(ctype, "application/json") {
			http.Error(w, "unsupported content type",
				http.StatusUnsupportedMediaType)
			return
		}
		d := json.NewDecoder(r.Body)
		var users []group.UserDescription
		err := d.Decode(&users)
		if err != nil {
			httpError(w, err)
			return
		}
		err = group.SetFallbackUsers(g, users)
		if err != nil {
			httpError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	} else if r.Method == "DELETE" {
		err := group.SetFallbackUsers(g, nil)
		if err != nil {
			httpError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	methodNotAllowed(w, "PUT", "DELETE")
	return
}

type jwkset = struct {
	Keys []map[string]any `json:"keys"`
}

func keysHandler(w http.ResponseWriter, r *http.Request, g string) {
	if !checkAdmin(w, r) {
		return
	}

	if r.Method == "PUT" {
		ctype := parseContentType(r.Header.Get("Content-Type"))
		if !strings.EqualFold(ctype, "application/jwk-set+json") {
			http.Error(w, "unsupported content type",
				http.StatusUnsupportedMediaType)
			return
		}
		d := json.NewDecoder(r.Body)
		var keys jwkset
		err := d.Decode(&keys)
		if err != nil {
			httpError(w, err)
			return
		}
		err = group.SetKeys(g, keys.Keys)
		if err != nil {
			httpError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	} else if r.Method == "DELETE" {
		err := group.SetKeys(g, nil)
		if err != nil {
			httpError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	methodNotAllowed(w, "PUT", "DELETE")
	return
}
