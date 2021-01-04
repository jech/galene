package admin

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/jech/galene/stats"
)

func New() http.Handler {
	s := http.NewServeMux()
	s.HandleFunc("/stats/groups", GroupeHandler)

	return s
}

func GroupeHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	err := json.NewEncoder(w).Encode(stats.GetGroups())
	if err != nil {
		log.Fatal(err)
	}
}
