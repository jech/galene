package webserver

import (
	"bufio"
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

	"github.com/jech/galene/diskwriter"
	"github.com/jech/galene/group"
	"github.com/jech/galene/rtpconn"
	"github.com/jech/galene/stats"
)

var server atomic.Value

var StaticRoot string

var Redirect string

var Insecure bool

func Serve(address string, dataDir string) error {
	http.Handle("/", &fileHandler{http.Dir(StaticRoot)})
	http.HandleFunc("/group/", groupHandler)
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
		s.TLSConfig = &tls.Config{
			GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
				return getCertificate(dataDir)
			},
		}
	}
	s.RegisterOnShutdown(func() {
		group.Range(func(g *group.Group) bool {
			go g.Shutdown("server is shutting down")
			return true
		})
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

func mungeHeader(w http.ResponseWriter) {
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
	http.Error(w, "500 Internal Server Error",
		http.StatusInternalServerError)
	return
}

const (
	normalCacheControl       = "max-age=1800"
	veryCachableCacheControl = "max-age=86400"
)

func redirect(w http.ResponseWriter, r *http.Request) bool {
	if Redirect == "" || strings.EqualFold(r.Host, Redirect) {
		return false
	}

	u := url.URL{
		Scheme: "https",
		Host:   Redirect,
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

	mungeHeader(w)
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

	name := p[len("/group/"):]
	if name == "" {
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

	mungeHeader(w)
	name := parseGroupName("/group/", r.URL.Path)
	if name == "" {
		notFound(w)
		return
	}

	if r.URL.Path != "/group/"+name {
		http.Redirect(w, r, "/group/"+name,
			http.StatusPermanentRedirect)
		return
	}

	g, err := group.Add(name, nil)
	if err != nil {
		if os.IsNotExist(err) {
			notFound(w)
		} else {
			log.Printf("addGroup: %v", err)
			http.Error(w, "Internal server error",
				http.StatusInternalServerError)
		}
		return
	}

	if redirect := g.Redirect(); redirect != "" {
		http.Redirect(w, r, redirect,
			http.StatusPermanentRedirect)
		return
	}

	serveFile(w, r, filepath.Join(StaticRoot, "galene.html"))
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

func getPassword(dataDir string) (string, string, error) {
	f, err := os.Open(filepath.Join(dataDir, "passwd"))
	if err != nil {
		return "", "", err
	}
	defer f.Close()

	r := bufio.NewReader(f)

	s, err := r.ReadString('\n')
	if err != nil {
		return "", "", err
	}

	l := strings.SplitN(strings.TrimSpace(s), ":", 2)
	if len(l) != 2 {
		return "", "", errors.New("couldn't parse passwords")
	}

	return l[0], l[1], nil
}

func failAuthentication(w http.ResponseWriter, realm string) {
	w.Header().Set("www-authenticate",
		fmt.Sprintf("basic realm=\"%v\"", realm))
	http.Error(w, "Haha!", http.StatusUnauthorized)
}

func statsHandler(w http.ResponseWriter, r *http.Request, dataDir string) {
	u, p, err := getPassword(dataDir)
	if err != nil {
		log.Printf("Passwd: %v", err)
		failAuthentication(w, "stats")
		return
	}

	username, password, ok := r.BasicAuth()
	if !ok || username != u || password != p {
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
	e.Encode(ss)
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

	if p == "/" {
		http.Error(w, "nothing to see", http.StatusForbidden)
		return
	}

	p = path.Clean(p)

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

	group := path.Dir(p[1:])
	if fi.IsDir() {
		u := r.URL.Path
		if u[len(u)-1] != '/' {
			http.Redirect(w, r, u+"/", http.StatusPermanentRedirect)
			return
		}
		group = p[1:]
	}

	ok := checkGroupPermissions(w, r, group)
	if !ok {
		failAuthentication(w, "recordings/"+group)
		return
	}

	if fi.IsDir() {
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

type httpClient struct {
	username string
	password string
}

func (c httpClient) Username() string {
	return c.username
}

func (c httpClient) Challenge(group string, creds group.ClientCredentials) bool {
	if creds.Password == nil {
		return true
	}
	m, err := creds.Password.Match(c.password)
	if err != nil {
		log.Printf("Password match: %v", err)
		return false
	}
	return m
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

	p, err := desc.GetPermission(groupname, httpClient{user, pass})
	if err != nil || !p.Record {
		if err == group.ErrNotAuthorised {
			time.Sleep(200 * time.Millisecond)
		}
		return false
	}

	return true
}

func serveGroupRecordings(w http.ResponseWriter, r *http.Request, f *os.File, group string) {
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
