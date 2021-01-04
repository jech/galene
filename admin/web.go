package admin

import (
	"encoding/json"
	"log"
	"net/http"

	"net/http/pprof"

	"github.com/jech/galene/stats"
)

func New() http.Handler {
	s := http.NewServeMux()
	s.HandleFunc("/stats/groups", GroupeHandler)

	s.HandleFunc("/pprof/cmdline", pprof.Cmdline)
	s.HandleFunc("/pprof/profile", pprof.Profile)
	s.HandleFunc("/pprof/symbol", pprof.Symbol)
	s.HandleFunc("/pprof/trace", pprof.Trace)
	//s.HandleFunc("/pprof/", pprof.Index)

	s.HandleFunc("/", HomeHandler)

	return s
}

func GroupeHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	err := json.NewEncoder(w).Encode(stats.GetGroups())
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
