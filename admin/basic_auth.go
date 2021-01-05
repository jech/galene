package admin

import (
	"log"
	"net/http"

	"github.com/jech/galene/group"
)

func BasicAuthMiddleware(password *group.Password, handler http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name, pw, ok := r.BasicAuth()
		if !ok || name != "admin" {
			w.Header().Set("WWW-Authenticate", `Basic realm="GaleneAdmin"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		ok, err := password.Match(pw)
		if err != nil {
			log.Println(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if !ok {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		handler.ServeHTTP(w, r)
	}
}
