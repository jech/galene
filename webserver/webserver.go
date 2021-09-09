package webserver

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"github.com/jech/cert"
	"github.com/jech/galene/diskwriter"
	"github.com/jech/galene/group"
	"github.com/jech/galene/rtpconn"
	"github.com/jech/galene/stats"
	"github.com/jech/galene/whip"
)

var server atomic.Value

var StaticRoot string

var Insecure bool

func Serve(address string, dataDir string) error {
	http.Handle("/", &fileHandler{http.Dir(StaticRoot)})
	http.HandleFunc("/group/", groupHandler)
	http.HandleFunc("/whip/", whip.Handler)
	http.HandleFunc("/recordings",
		func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r,
				"/recordings/", http.StatusPermanentRedirect)
		})
	http.HandleFunc("/recordings/", recordingsHandler)
	http.HandleFunc("/ws", wsHandler)
	http.HandleFunc("/public-groups.json", publicHandler)
	http.HandleFunc("/stats.json",
		func(w http.ResponseWriter, r *http.Request) {
			statsHandler(w, r, dataDir)
		})

	s := &http.Server{
		Addr:              address,
		ReadHeaderTimeout: 60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	if !Insecure {
		certificate := cert.New(
			filepath.Join(dataDir, "cert.pem"),
			filepath.Join(dataDir, "key.pem"),
		)
		s.TLSConfig = &tls.Config{
			GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
				return certificate.Get()
			},
		}
	}
	s.RegisterOnShutdown(func() {
		group.Shutdown("server is shutting down")
	})

	server.Store(s)

	var err error

	if !Insecure {
		err = s.ListenAndServeTLS("", "")
	} else {
		err = s.ListenAndServe()
	}

	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func cspHeader(w http.ResponseWriter) {
	w.Header().Add("Content-Security-Policy",
		"connect-src ws: wss: 'self'; img-src data: 'self'; media-src blob: 'self'; default-src 'self'")
}

func notFound(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)

	f, err := os.Open(path.Join(StaticRoot, "404.html"))
	if err != nil {
		fmt.Fprintln(w, "<p>Not found</p>")
		return
	}
	defer f.Close()

	io.Copy(w, f)
}

var ErrIsDirectory = errors.New("is a directory")

func httpError(w http.ResponseWriter, err error) {
	if os.IsNotExist(err) {
		notFound(w)
		return
	}
	if os.IsPermission(err) {
		http.Error(w, "403 forbidden", http.StatusForbidden)
		return
	}
	log.Printf("HTTP server error: %v", err)
	http.Error(w, "500 Internal Server Error",
		http.StatusInternalServerError)
	return
}

const (
	normalCacheControl       = "max-age=1800"
	veryCachableCacheControl = "max-age=86400"
)

func redirect(w http.ResponseWriter, r *http.Request) bool {
	conf, err := group.GetConfiguration()
	if err != nil || conf.CanonicalHost == "" {
		return false
	}

	if strings.EqualFold(r.Host, conf.CanonicalHost) {
		return false
	}

	u := url.URL{
		Scheme: "https",
		Host:   conf.CanonicalHost,
		Path:   r.URL.Path,
	}
	http.Redirect(w, r, u.String(), http.StatusMovedPermanently)
	return true
}

func makeCachable(w http.ResponseWriter, p string, fi os.FileInfo, cachable bool) {
	etag := fmt.Sprintf("\"%v-%v\"", fi.Size(), fi.ModTime().UnixNano())
	w.Header().Set("ETag", etag)
	if !cachable {
		w.Header().Set("cache-control", "no-cache")
		return
	}

	cc := normalCacheControl
	if strings.HasPrefix(p, "/fonts/") ||
		strings.HasPrefix(p, "/scripts/") ||
		strings.HasPrefix(p, "/css/") {
		cc = veryCachableCacheControl
	}

	w.Header().Set("Cache-Control", cc)
}

// fileHandler is our custom reimplementation of http.FileServer
type fileHandler struct {
	root http.FileSystem
}

func (fh *fileHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if redirect(w, r) {
		return
	}

	cspHeader(w)
	p := r.URL.Path
	// this ensures any leading .. are removed by path.Clean below
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
		r.URL.Path = p
	}
	p = path.Clean(p)

	f, err := fh.root.Open(p)
	if err != nil {
		httpError(w, err)
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		httpError(w, err)
		return
	}

	if fi.IsDir() {
		u := r.URL.Path
		if u[len(u)-1] != '/' {
			http.Redirect(w, r, u+"/", http.StatusPermanentRedirect)
			return
		}

		index := path.Join(p, "index.html")
		ff, err := fh.root.Open(index)
		if err != nil {
			// return 403 if index.html doesn't exist
			if os.IsNotExist(err) {
				err = os.ErrPermission
			}
			httpError(w, err)
			return
		}
		defer ff.Close()
		dd, err := ff.Stat()
		if err != nil {
			httpError(w, err)
			return
		}
		if dd.IsDir() {
			httpError(w, ErrIsDirectory)
			return
		}
		f, fi = ff, dd
		p = index
	}

	makeCachable(w, p, fi, true)
	http.ServeContent(w, r, fi.Name(), fi.ModTime(), f)
}

// serveFile is similar to http.ServeFile, except that it doesn't check
// for .. and adds cachability headers.
func serveFile(w http.ResponseWriter, r *http.Request, p string) {
	f, err := os.Open(p)
	if err != nil {
		httpError(w, err)
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		httpError(w, err)
		return
	}

	if fi.IsDir() {
		httpError(w, ErrIsDirectory)
		return
	}

	makeCachable(w, p, fi, true)
	http.ServeContent(w, r, fi.Name(), fi.ModTime(), f)
}

func parseGroupName(prefix string, p string) string {
	if !strings.HasPrefix(p, prefix) {
		return ""
	}

	name := p[len(prefix):]
	if name == "" {
		return ""
	}

	if name[0] == '.' {
		return ""
	}

	if filepath.Separator != '/' &&
		strings.ContainsRune(name, filepath.Separator) {
		return ""
	}

	name = path.Clean("/" + name)
	return name[1:]
}

func groupHandler(w http.ResponseWriter, r *http.Request) {
	if redirect(w, r) {
		return
	}

	if strings.HasSuffix(r.URL.Path, "/.status.json") {
		groupStatusHandler(w, r)
		return
	}

	name := parseGroupName("/group/", r.URL.Path)
	if name == "" {
		notFound(w)
		return
	}

	g, err := group.Add(name, nil)
	if err != nil {
		if os.IsNotExist(err) {
			notFound(w)
		} else {
			log.Printf("group.Add: %v", err)
			http.Error(w, "Internal server error",
				http.StatusInternalServerError)
		}
		return
	}

	if r.URL.Path != "/group/"+name+"/" {
		http.Redirect(w, r, "/group/"+name+"/",
			http.StatusPermanentRedirect)
		return
	}

	if redirect := g.Description().Redirect; redirect != "" {
		http.Redirect(w, r, redirect, http.StatusPermanentRedirect)
		return
	}

	if r.Method == "POST" || r.Method == "OPTIONS" {
		whip.Endpoint(g, w, r)
		return
	}

	cspHeader(w)
	serveFile(w, r, filepath.Join(StaticRoot, "galene.html"))
}

func groupStatusHandler(w http.ResponseWriter, r *http.Request) {
	path := path.Dir(r.URL.Path)
	name := parseGroupName("/group/", path)
	if name == "" {
		notFound(w)
		return
	}

	g, err := group.Add(name, nil)
	if err != nil {
		if os.IsNotExist(err) {
			notFound(w)
		} else {
			http.Error(w, "Internal server error",
				http.StatusInternalServerError)
		}
		return
	}

	d := group.GetStatus(g, false)
	w.Header().Set("content-type", "application/json")
	w.Header().Set("cache-control", "no-cache")

	if r.Method == "HEAD" {
		return
	}

	e := json.NewEncoder(w)
	e.Encode(d)
	return
}

func publicHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("content-type", "application/json")
	w.Header().Set("cache-control", "no-cache")

	if r.Method == "HEAD" {
		return
	}

	g := group.GetPublic()
	e := json.NewEncoder(w)
	e.Encode(g)
	return
}

func adminMatch(username, password string) (bool, error) {
	conf, err := group.GetConfiguration()
	if err != nil {
		return false, err
	}

	for _, cred := range conf.Admin {
		if cred.Username == "" || cred.Username == username {
			if ok, _ := cred.Password.Match(password); ok {
				return true, nil
			}
		}
	}
	return false, nil
}

func failAuthentication(w http.ResponseWriter, realm string) {
	w.Header().Set("www-authenticate",
		fmt.Sprintf("basic realm=\"%v\"", realm))
	http.Error(w, "Haha!", http.StatusUnauthorized)
}

func statsHandler(w http.ResponseWriter, r *http.Request, dataDir string) {
	username, password, ok := r.BasicAuth()
	if !ok {
		failAuthentication(w, "stats")
		return
	}

	if ok, err := adminMatch(username, password); !ok {
		if err != nil {
			log.Printf("Administrator password: %v", err)
		}
		failAuthentication(w, "stats")
		return
	}

	w.Header().Set("content-type", "application/json")
	w.Header().Set("cache-control", "no-cache")
	if r.Method == "HEAD" {
		return
	}

	ss := stats.GetGroups()
	e := json.NewEncoder(w)
	err := e.Encode(ss)
	if err != nil {
		log.Printf("stats.json: %v", err)
	}
	return
}

var wsUpgrader = websocket.Upgrader{
	HandshakeTimeout: 30 * time.Second,
}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Websocket upgrade: %v", err)
		return
	}
	go func() {
		err := rtpconn.StartClient(conn)
		if err != nil {
			log.Printf("client: %v", err)
		}
	}()
}

func recordingsHandler(w http.ResponseWriter, r *http.Request) {
	if redirect(w, r) {
		return
	}

	if len(r.URL.Path) < 12 || r.URL.Path[:12] != "/recordings/" {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	p := "/" + r.URL.Path[12:]

	if filepath.Separator != '/' &&
		strings.ContainsRune(p, filepath.Separator) {
		http.Error(w, "bad character in filename",
			http.StatusBadRequest)
		return
	}

	p = path.Clean(p)

	if p == "/" {
		http.Error(w, "nothing to see", http.StatusForbidden)
		return
	}

	f, err := os.Open(filepath.Join(diskwriter.Directory, p))
	if err != nil {
		httpError(w, err)
		return
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		httpError(w, err)
		return
	}

	var group, filename string
	if fi.IsDir() {
		for len(p) > 0 && p[len(p)-1] == '/' {
			p = p[:len(p)-1]
		}
		group = parseGroupName("/", p)
		if group == "" {
			http.Error(w, "bad group name", http.StatusBadRequest)
			return
		}
	} else {
		if p[len(p)-1] == '/' {
			http.Error(w, "bad group name", http.StatusBadRequest)
			return
		}
		group, filename = path.Split(p)
		group = parseGroupName("/", group)
		if group == "" {
			http.Error(w, "bad group name", http.StatusBadRequest)
			return
		}
	}

	u := "/recordings/" + group + "/" + filename
	if r.URL.Path != u {
		http.Redirect(w, r, u, http.StatusPermanentRedirect)
		return
	}

	ok := checkGroupPermissions(w, r, group)
	if !ok {
		failAuthentication(w, "recordings/"+group)
		return
	}

	if filename == "" {
		if r.Method == "POST" {
			handleGroupAction(w, r, group)
		} else {
			serveGroupRecordings(w, r, f, group)
		}
		return
	}

	// Ensure the file is uncachable if it's still recording
	cachable := time.Since(fi.ModTime()) > time.Minute
	makeCachable(w, path.Join("/recordings/", p), fi, cachable)
	http.ServeContent(w, r, fi.Name(), fi.ModTime(), f)
}

func handleGroupAction(w http.ResponseWriter, r *http.Request, group string) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	err := r.ParseForm()
	if err != nil {
		http.Error(w, "couldn't parse request", http.StatusBadRequest)
		return
	}

	q := r.Form.Get("q")

	switch q {
	case "delete":
		filename := r.Form.Get("filename")
		if group == "" || filename == "" {
			http.Error(w, "no filename provided",
				http.StatusBadRequest)
			return
		}
		if strings.ContainsRune(filename, '/') ||
			strings.ContainsRune(filename, filepath.Separator) {
			http.Error(w, "bad character in filename",
				http.StatusBadRequest)
			return
		}
		err := os.Remove(
			filepath.Join(diskwriter.Directory,
				filepath.Join(group,
					path.Clean("/"+filename),
				),
			),
		)
		if err != nil {
			httpError(w, err)
			return
		}
		http.Redirect(w, r, "/recordings/"+group+"/",
			http.StatusSeeOther)
		return
	default:
		http.Error(w, "unknown query", http.StatusBadRequest)
	}
}

func checkGroupPermissions(w http.ResponseWriter, r *http.Request, groupname string) bool {
	desc, err := group.GetDescription(groupname)
	if err != nil {
		return false
	}

	user, pass, ok := r.BasicAuth()
	if !ok {
		return false
	}

	p, err := desc.GetPermission(groupname,
		group.ClientCredentials{
			Username: user,
			Password: pass,
		},
	)
	if err != nil || !p.Record {
		if err == group.ErrNotAuthorised {
			time.Sleep(200 * time.Millisecond)
		}
		return false
	}

	return true
}

func serveGroupRecordings(w http.ResponseWriter, r *http.Request, f *os.File, group string) {
	// read early, so we return permission errors to HEAD
	fis, err := f.Readdir(-1)
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	sort.Slice(fis, func(i, j int) bool {
		return fis[i].Name() < fis[j].Name()
	})

	w.Header().Set("content-type", "text/html; charset=utf-8")
	w.Header().Set("cache-control", "no-cache")

	if r.Method == "HEAD" {
		return
	}

	fmt.Fprintf(w, "<!DOCTYPE html>\n<html><head>\n")
	fmt.Fprintf(w, "<title>Recordings for group %v</title>\n", group)
	fmt.Fprintf(w, "<link rel=\"stylesheet\" type=\"text/css\" href=\"/common.css\"/>")
	fmt.Fprintf(w, "</head><body>\n")

	fmt.Fprintf(w, "<table>\n")
	for _, fi := range fis {
		if fi.IsDir() {
			continue
		}
		fmt.Fprintf(w, "<tr><td><a href=\"./%v\">%v</a></td><td>%d</td>",
			html.EscapeString(fi.Name()),
			html.EscapeString(fi.Name()),
			fi.Size(),
		)
		fmt.Fprintf(w,
			"<td><form action=\"/recordings/%v/\" method=\"post\">"+
				"<input type=\"hidden\" name=\"filename\" value=\"%v\">"+
				"<button type=\"submit\" name=\"q\" value=\"delete\">Delete</button>"+
				"</form></td></tr>\n",
			url.PathEscape(group), fi.Name())
	}
	fmt.Fprintf(w, "</table>\n")
	fmt.Fprintf(w, "</body></html>\n")
}

func Shutdown() {
	v := server.Load()
	if v == nil {
		return
	}
	s := v.(*http.Server)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	s.Shutdown(ctx)
}
