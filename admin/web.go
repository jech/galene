package admin

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"net/http/pprof"

	"github.com/jech/galene/group"
	"github.com/jech/galene/stats"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func New(auth *group.Password) http.HandlerFunc {
	s := http.NewServeMux()
	s.HandleFunc("/api/groups", GroupsHandler)
	s.HandleFunc("/api/group/", OneGroupHandler) // /api/group/_all /api/group/{group}

	s.HandleFunc("/pprof/cmdline", pprof.Cmdline)
	s.HandleFunc("/pprof/profile", pprof.Profile)
	s.HandleFunc("/pprof/symbol", pprof.Symbol)
	s.HandleFunc("/pprof/trace", pprof.Trace)
	//s.HandleFunc("/pprof/", pprof.Index)

	s.Handle("/metrics", promhttp.Handler())

	s.HandleFunc("/", HomeHandler)

	return BasicAuthMiddleware(auth, s)
}

func OneGroupHandler(w http.ResponseWriter, r *http.Request) {
	slugs := strings.Split(r.URL.Path, "/")
	if len(slugs) > 4 {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	group := slugs[3]
	var err error
	if group == "_all" {
		err = json.NewEncoder(w).Encode(stats.GetGroups())
	} else {
		err = json.NewEncoder(w).Encode(stats.GetGroup(group))
	}
	if err != nil {
		log.Fatal(err)
	}
}

func GroupsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	err := json.NewEncoder(w).Encode(group.GetNames())
	if err != nil {
		log.Fatal(err)
	}
}

func HomeHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(`
    __ _  __ _| | ___ _ __   ___
   / _' |/ _' | |/ _ \ '_ \ / _ \
  | (_| | (_| | |  __/ | | |  __/
   \__, |\__,_|_|\___|_| |_|\___|
   |___/

`))
}
