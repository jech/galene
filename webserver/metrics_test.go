package webserver

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jech/galene/group"
)

func TestMetricsHandler(t *testing.T) {
	group.Directory = t.TempDir()
	group.DataDirectory = t.TempDir()
	writeConfig := func(metrics string) {
		config := `{
    "users": {
        "root": {"password": "pw", "permissions": "admin"},
        "prom": {"password": "pw", "permissions": "metrics"},
        "hashed": {
            "password": {
                "type": "pbkdf2",
                "hash": "sha-256",
                "key": "fc9d479a791e2997ad2726909cdee0cc64021f75a3afb76522ba6463c98e49de",
                "salt": "a5a713dd64edbcb6",
                "iterations": 4096
            },
            "permissions": ["metrics"]
        },
        "user": {"password": "pw", "permissions": []}
    }`
		if metrics != "" {
			config += `,
    "metrics": ` + metrics
		}
		config += "\n}\n"
		err := os.WriteFile(
			filepath.Join(group.DataDirectory, "config.json"),
			[]byte(config), 0600,
		)
		if err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	get := func(method, username, password string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, "/metrics", nil)
		if username != "" {
			r.SetBasicAuth(username, password)
		}
		metricsHandler(w, r)
		return w
	}

	if w := get("GET", "", ""); w.Code != http.StatusNotFound {
		t.Errorf("No config: expected 404, got %v", w.Code)
	}

	for _, m := range []string{"", "false"} {
		writeConfig(m)
		if w := get("GET", "root", "pw"); w.Code != http.StatusNotFound {
			t.Errorf("Disabled (%q): expected 404, got %v",
				m, w.Code)
		}
	}

	writeConfig(`true`)
	tests := []struct {
		username, password string
		code               int
	}{
		{"", "", http.StatusUnauthorized},
		{"prom", "bad", http.StatusUnauthorized},
		{"user", "pw", http.StatusUnauthorized},
		{"nobody", "pw", http.StatusUnauthorized},
		{"prom", "pw", http.StatusOK},
		{"hashed", "pw", http.StatusOK},
		{"root", "pw", http.StatusOK},
	}
	for _, test := range tests {
		w := get("GET", test.username, test.password)
		if w.Code != test.code {
			t.Errorf("Authenticated (%v, %v): expected %v, got %v",
				test.username, test.password, test.code, w.Code)
		}
	}
	// the cache must not bypass authentication
	if w := get("GET", "", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("Cached: expected 401, got %v", w.Code)
	}

	writeConfig(`"public"`)
	w := get("GET", "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("Public: expected 200, got %v", w.Code)
	}
	if ct := w.Header().Get("content-type"); !strings.HasPrefix(ct, "text/plain; version=0.0.4") {
		t.Errorf("Bad content-type %v", ct)
	}
	if !strings.Contains(w.Body.String(), "galene_clients{visibility=\"private\"} 0\n") {
		t.Errorf("Unexpected body %v", w.Body.String())
	}

	if w := get("POST", "", ""); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: expected 405, got %v", w.Code)
	}

	// a second scrape within the minimum interval is served from cache
	first := metricsCache.time
	get("GET", "", "")
	if !metricsCache.time.Equal(first) {
		t.Errorf("Metrics were recomputed")
	}
}
