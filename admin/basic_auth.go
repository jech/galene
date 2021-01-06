package admin

import (
	"log"
	"net/http"
	"sync"

	"github.com/jech/galene/group"
)

func BasicAuthMiddleware(password *group.Password, handler http.Handler) http.HandlerFunc {
	cachedPw := ""
	lock := sync.RWMutex{}
	return func(w http.ResponseWriter, r *http.Request) {
		name, pw, ok := r.BasicAuth()
		if !ok || name != "admin" {
			w.Header().Set("WWW-Authenticate", `Basic realm="GaleneAdmin"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		lock.RLock()
		if cachedPw == "" || pw != cachedPw {
			ok, err := password.Match(pw)
			if err != nil {
				log.Println(err)
				lock.RUnlock()
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			if !ok {
				lock.RUnlock()
				w.WriteHeader(http.StatusForbidden)
				return
			}
			lock.RUnlock()
			lock.Lock()
			cachedPw = pw
			lock.Unlock()
		} else {
			lock.RUnlock()
		}
		handler.ServeHTTP(w, r)
	}
}
