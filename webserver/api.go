package webserver

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/jech/galene/group"
	"github.com/jech/galene/stats"
)

func apiHandler(w http.ResponseWriter, r *http.Request) {
	username, password, ok := r.BasicAuth()
	if !ok {
		failAuthentication(w, "galene-api")
		return
	}

	if ok, err := adminMatch(username, password); !ok {
		if err != nil {
			log.Printf("Administrator password: %v", err)
		}
		failAuthentication(w, "galene-api")
		return
	}

	if !strings.HasPrefix(r.URL.Path, "/galene-api/") {
		http.NotFound(w, r)
		return
	}

	pth := r.URL.Path[len("/galene/api"):]

	if pth == "/stats" {
		if r.Method != "HEAD" && r.Method != "GET" {
			http.Error(w, "method not allowed",
				http.StatusMethodNotAllowed)
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
	} else if strings.HasPrefix(pth, "/group/") {
		dir, kind, _ := splitPath(pth)

		if kind == ".user" {
			userHandler(w, r)
			return
		} else if kind != "" {
			notFound(w)
			return
		}

		g := parseGroupName("/group/", dir)
		if g == "" {
			http.NotFound(w, r)
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
		} else if r.Method == "PUT" || r.Method == "DELETE" {
			etag, err := group.GetDescriptionTag(g)
			if r.Method == "PUT" && os.IsNotExist(err) {
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
			if r.Method == "PUT" {
				ctype := r.Header.Get("Content-Type")
				if !strings.EqualFold(
					ctype, "application/json",
				) {
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
			} else if r.Method == "DELETE"{
				err := group.DeleteDescription(g, etag)
				if err != nil {
					httpError(w, err)
					return
				}
				w.WriteHeader(http.StatusNoContent)
				return
			} else {
				http.Error(w, "method not allowed",
					http.StatusMethodNotAllowed)
				return
			}
		}
		http.Error(w, "method not allowed",
			http.StatusMethodNotAllowed)
		return
	}

	http.NotFound(w, r)
	return
}

func userHandler(w http.ResponseWriter, r *http.Request) {
	dir, kind, rest := splitPath(r.URL.Path)

	if kind != ".user" {
		http.Error(w, "Internal server error",
			http.StatusInternalServerError)
		return
	}

	g := parseGroupName("/group/", dir)
	if g == "" {
		http.NotFound(w, r)
		return
	}

	if rest == "" {
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
		w.Header().Set("content-type", "text/plain")
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

	_, kind2, _ := splitPath(rest)
	if kind2 == ".password" {
		http.Error(w, "Not implemented yet",
			http.StatusInternalServerError)
		return
	} else if kind2 != "" {
		notFound(w)
		return
	}

	if r.Method != "HEAD" && r.Method != "GET" {
		http.Error(w, "method not allowed",
			http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("content-type", "application/json")
	user, etag, err := group.GetSanitisedUser(g, rest[1:])
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
}
