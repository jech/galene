package webserver

import (
	"bytes"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/jech/galene/group"
	"github.com/jech/galene/stats"
)

// Computing the metrics requires briefly taking the locks of all clients,
// so we serialise scrapes and cache the result in order to bound the
// load caused by frequent or concurrent scrapes.
const metricsMinInterval = 2 * time.Second

var metricsCache struct {
	mu   sync.Mutex
	time time.Time
	data []byte
}

func getMetrics() ([]byte, error) {
	metricsCache.mu.Lock()
	defer metricsCache.mu.Unlock()

	if metricsCache.data != nil &&
		time.Since(metricsCache.time) < metricsMinInterval {
		return metricsCache.data, nil
	}

	var buf bytes.Buffer
	err := stats.WritePrometheus(&buf)
	if err != nil {
		return nil, err
	}
	metricsCache.time = time.Now()
	metricsCache.data = buf.Bytes()
	return metricsCache.data, nil
}

func metricsHandler(w http.ResponseWriter, r *http.Request) {
	conf, err := group.GetConfiguration()
	if err != nil || conf.Metrics == group.MetricsDisabled {
		http.NotFound(w, r)
		return
	}

	// This must be checked on every request, never cached.
	if conf.Metrics != group.MetricsPublic {
		ok := false
		username, password, haveAuth := r.BasicAuth()
		if haveAuth {
			ok, err = globalPermissionMatch(
				username, password, "metrics", "admin",
			)
			if err != nil {
				log.Printf("Metrics: %v", err)
			}
		}
		if !ok {
			failAuthentication(w, "/metrics")
			return
		}
	}

	if r.Method != "HEAD" && r.Method != "GET" {
		methodNotAllowed(w, "HEAD, GET")
		return
	}

	data, err := getMetrics()
	if err != nil {
		log.Printf("Metrics: %v", err)
		httpError(w, err)
		return
	}

	w.Header().Set("content-type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("cache-control", "no-cache")
	if r.Method == "HEAD" {
		return
	}
	w.Write(data)
}
