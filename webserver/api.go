package webserver

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"github.com/jech/galene/group"
	"github.com/jech/galene/stats"
	"github.com/jech/galene/token"
)

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
func checkPasswordAdmin(w http.ResponseWriter, r *http.Request, groupname, user string, wildcard bool) bool {
	username, password, ok := r.BasicAuth()
	if ok {
		ok, _ := adminMatch(username, password)
		if ok {
			return true
		}
	}
	if ok && !wildcard && username == user {
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

func sendJSON(w http.ResponseWriter, r *http.Request, v any) {
	w.Header().Set("content-type", "application/json")
	if r.Method == "HEAD" {
		return
	}
	e := json.NewEncoder(w)
	e.Encode(v)
}

func getText(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	ctype, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(ctype, "text/plain") {
		w.Header().Set("Accept", "text/plain")
		http.Error(w, "unsupported content type",
			http.StatusUnsupportedMediaType)
		return nil, true
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
	if err != nil {
		httpError(w, err)
		return nil, true
	}

	return body, false
}

func getJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	ctype, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(ctype, "application/json") {
		w.Header().Set("Accept", "application/json")
		http.Error(w, "unsupported content type",
			http.StatusUnsupportedMediaType)
		return true
	}

	d := json.NewDecoder(r.Body)
	err = d.Decode(v)
	if err != nil {
		httpError(w, err)
		return true
	}

	return false
}

func apiHandler(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/galene-api/") {
		http.NotFound(w, r)
		return
	}

	first, kind, rest := splitPath(r.URL.Path[len("/galene-api"):])
	if first == "/v0" && kind == ".stats" && rest == "" {
		if !checkAdmin(w, r) {
			return
		}
		if r.Method != "HEAD" && r.Method != "GET" {
			methodNotAllowed(w, "HEAD", "GET")
			return
		}
		w.Header().Set("cache-control", "no-cache")
		sendJSON(w, r, stats.GetGroups())
		return
	} else if first == "/v0" && kind == ".groups" {
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
		sendJSON(w, r, groups)
		return
	}

	if kind == ".users" {
		usersHandler(w, r, g, rest)
		return
	} else if kind == ".empty-user" {
		specialUserHandler(w, r, g, rest, false)
		return
	} else if kind == ".wildcard-user" {
		specialUserHandler(w, r, g, rest, true)
		return
	} else if kind == ".keys" && rest == "" {
		keysHandler(w, r, g)
		return
	} else if kind == ".tokens" {
		tokensHandler(w, r, g, rest)
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

		sendJSON(w, r, desc)
		return
	} else if r.Method == "PUT" {
		etag, err := group.GetDescriptionTag(g)
		if errors.Is(err, os.ErrNotExist) {
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

		var newdesc group.Description
		done = getJSON(w, r, &newdesc)
		if done {
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

func usersHandler(w http.ResponseWriter, r *http.Request, g, pth string) {
	if pth == "" {
		http.NotFound(w, r)
		return
	}
	if pth == "/" {
		if !checkAdmin(w, r) {
			return
		}
		if r.Method != "HEAD" && r.Method != "GET" {
			methodNotAllowed(w, "HEAD", "GET")
			return
		}
		users, etag, err := group.GetUsers(g)
		if err != nil {
			httpError(w, err)
			return
		}
		w.Header().Set("etag", etag)
		done := checkPreconditions(w, r, etag)
		if done {
			return
		}
		sendJSON(w, r, users)
		return
	}

	first2, kind2, rest2 := splitPath(pth)
	if first2 != "" && kind2 == "" {
		userHandler(w, r, g, first2[1:], false)
		return
	} else if first2 != "" && kind2 == ".password" && rest2 == "" {
		passwordHandler(w, r, g, first2[1:], false)
		return
	}
	if !checkAdmin(w, r) {
		return
	}
	notFound(w)
	return
}

func specialUserHandler(w http.ResponseWriter, r *http.Request, g, pth string, wildcard bool) {
	if pth == "" {
		userHandler(w, r, g, "", wildcard)
		return
	} else if pth == "/.password" {
		passwordHandler(w, r, g, "", wildcard)
		return
	}
	if !checkAdmin(w, r) {
		return
	}
	notFound(w)
	return
}

func userHandler(w http.ResponseWriter, r *http.Request, g, user string, wildcard bool) {
	if !checkAdmin(w, r) {
		return
	}

	if r.Method == "HEAD" || r.Method == "GET" {
		user, etag, err := group.GetSanitisedUser(g, user, wildcard)
		if err != nil {
			httpError(w, err)
			return
		}
		w.Header().Set("etag", etag)
		done := checkPreconditions(w, r, etag)
		if done {
			return
		}
		sendJSON(w, r, user)
		return
	} else if r.Method == "PUT" {
		etag, err := group.GetUserTag(g, user, wildcard)
		if errors.Is(err, os.ErrNotExist) {
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

		var newdesc group.UserDescription
		done = getJSON(w, r, &newdesc)
		if done {
			return
		}
		err = group.UpdateUser(g, user, wildcard, etag, &newdesc)
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
		etag, err := group.GetUserTag(g, user, wildcard)
		if err != nil {
			httpError(w, err)
			return
		}

		done := checkPreconditions(w, r, etag)
		if done {
			return
		}

		err = group.DeleteUser(g, user, wildcard, etag)
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

func passwordHandler(w http.ResponseWriter, r *http.Request, g, user string, wildcard bool) {
	if !checkPasswordAdmin(w, r, g, user, wildcard) {
		return
	}

	if r.Method == "PUT" {
		var pw group.Password
		done := getJSON(w, r, &pw)
		if done {
			return
		}
		err := group.SetUserPassword(g, user, wildcard, pw)
		if err != nil {
			httpError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	} else if r.Method == "POST" {
		body, done := getText(w, r)
		if done {
			return
		}
		key, err := bcrypt.GenerateFromPassword(body, 8)
		if err != nil {
			httpError(w, err)
			return
		}

		k := string(key)
		pw := group.Password{
			Type: "bcrypt",
			Key:  &k,
		}
		err = group.SetUserPassword(g, user, wildcard, pw)
		if err != nil {
			httpError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	} else if r.Method == "DELETE" {
		err := group.SetUserPassword(g, user, wildcard, group.Password{})
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

type jwkset = struct {
	Keys []map[string]any `json:"keys"`
}

func keysHandler(w http.ResponseWriter, r *http.Request, g string) {
	if !checkAdmin(w, r) {
		return
	}

	if r.Method == "PUT" {
		// cannot use getJSON due to the weird content-type
		ctype, _, err :=
			mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil ||
			!strings.EqualFold(ctype, "application/jwk-set+json") {
			w.Header().Set("Accept", "application/jwk-set+json")
			http.Error(w, "unsupported content type",
				http.StatusUnsupportedMediaType)
			return
		}
		d := json.NewDecoder(r.Body)
		var keys jwkset
		err = d.Decode(&keys)
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

func tokensHandler(w http.ResponseWriter, r *http.Request, g, pth string) {
	if pth == "" {
		http.NotFound(w, r)
		return
	}
	if !checkAdmin(w, r) {
		return
	}
	// check that the group exists
	_, err := group.GetDescription(g)
	if err != nil {
		httpError(w, err)
		return
	}
	if pth == "/" {
		if r.Method == "HEAD" || r.Method == "GET" {
			tokens, etag, err := token.List(g)
			if err != nil {
				httpError(w, err)
				return
			}
			w.Header().Set("content-type", "application/json")
			if etag != "" {
				w.Header().Set("etag", etag)
			}
			toknames := make([]string, len(tokens))
			for i, t := range tokens {
				toknames[i] = t.Token
			}
			sendJSON(w, r, toknames)
			return
		} else if r.Method == "POST" {
			var newtoken token.Stateful
			done := getJSON(w, r, &newtoken)
			if done {
				return
			}
			if newtoken.Token != "" || newtoken.Group != "" {
				http.Error(w, "overspecified token",
					http.StatusBadRequest)
				return
			}
			buf := make([]byte, 8)
			rand.Read(buf)
			newtoken.Token =
				base64.RawURLEncoding.EncodeToString(buf)
			newtoken.Group = g
			t, err := token.Update(&newtoken, "")
			if err != nil {
				httpError(w, err)
				return
			}
			w.Header().Set("location", t.Token)
			w.WriteHeader(http.StatusCreated)
			return
		}
		methodNotAllowed(w, "HEAD", "GET", "POST")
		return
	}

	if pth[0] != '/' {
		http.NotFound(w, r)
		return
	}
	t := pth[1:]
	if r.Method == "HEAD" || r.Method == "GET" {
		old, etag, err := token.Get(t)
		if err != nil {
			httpError(w, err)
			return
		}
		if old.Group != g {
			http.NotFound(w, r)
			return
		}
		tok := old.Clone()
		tok.Token = ""
		tok.Group = ""
		w.Header().Set("etag", etag)
		done := checkPreconditions(w, r, etag)
		if done {
			return
		}
		sendJSON(w, r, tok)
		return
	} else if r.Method == "PUT" {
		old, etag, err := token.Get(t)
		if errors.Is(err, os.ErrNotExist) {
			etag = ""
			err = nil
		} else if err != nil {
			httpError(w, err)
			return
		}
		if old.Group != g {
			http.Error(w, "token exists in different group",
				http.StatusConflict)
			return
		}

		done := checkPreconditions(w, r, etag)
		if done {
			return
		}

		var newtoken token.Stateful
		done = getJSON(w, r, &newtoken)
		if done {
			return
		}
		if newtoken.Group != "" || newtoken.Token != "" {
			http.Error(w, "overspecified token",
				http.StatusBadRequest)
			return
		}
		newtoken.Group = g
		newtoken.Token = t
		_, err = token.Update(&newtoken, etag)
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
		old, etag, err := token.Get(t)
		if err != nil {
			httpError(w, err)
			return
		}
		if old.Group != g {
			http.NotFound(w, r)
			return
		}

		done := checkPreconditions(w, r, etag)
		if done {
			return
		}

		err = token.Delete(t, etag)
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
