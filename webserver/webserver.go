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
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/jech/cert"
	"github.com/jech/galene/diskwriter"
	"github.com/jech/galene/group"
	"github.com/jech/galene/rtpconn"
)

var server *http.Server

var StaticRoot string

var Insecure bool

func Serve(address string, dataDir string) error {
	serverMux := http.NewServeMux()
	serverMux.Handle("/", &fileHandler{http.Dir(StaticRoot)})
	serverMux.HandleFunc("/group/", groupHandler)
	serverMux.HandleFunc("/recordings",
		func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r,
				"/recordings/", http.StatusPermanentRedirect)
		})
	serverMux.HandleFunc("/recordings/", recordingsHandler)
	serverMux.HandleFunc("/ws", wsHandler)
	serverMux.HandleFunc("/public-groups.json", publicHandler)
	serverMux.HandleFunc("/galene-api/", apiHandler)

	s := &http.Server{
		Addr:              address,
		Handler:           corsHandler(serverMux),
		ReadHeaderTimeout: 60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	if !Insecure {
		certificate := cert.New(
			filepath.Join(dataDir, "cert.pem"),
			filepath.Join(dataDir, "key.pem"),
		)
		s.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
			GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
				return certificate.Get()
			},
		}
	}
	s.RegisterOnShutdown(func() {
		group.Shutdown("server is shutting down")
	})

	server = s

	proto := "tcp"
	if strings.HasPrefix(address, "/") {
		proto = "unix"
	}

	listener, err := net.Listen(proto, address)
	if err != nil {
		return err
	}
	go func() {
		defer listener.Close()
		if !Insecure {
			err = s.ServeTLS(listener, "", "")
		} else {
			err = s.Serve(listener)
		}
	}()
	return nil
}

func corsHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		CheckOrigin(w, r, false)
		next.ServeHTTP(w, r)
	})
}

func cspHeader(w http.ResponseWriter, connect string) {
	c := "connect-src ws: wss: 'self'; "
	if connect != "" {
		c = "connect-src " + connect + " ws: wss: 'self'; "
	}
	w.Header().Add("Content-Security-Policy",
		c+"img-src 'self'; media-src blob: 'self'; script-src 'unsafe-eval' 'self'; default-src 'self'")

	// Make browser stop sending referrer information
	w.Header().Add("Referrer-Policy", "no-referrer")

	// Require correct MIME type to load CSS and JS
	w.Header().Add("X-Content-Type-Options", "nosniff")
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

func internalError(w http.ResponseWriter, format string, args ...any) {
	log.Printf(format, args...)
	http.Error(w, "Internal server error", http.StatusInternalServerError)
}

var ErrIsDirectory = errors.New("is a directory")

func httpError(w http.ResponseWriter, err error) {
	if errors.Is(err, os.ErrNotExist) {
		notFound(w)
		return
	}
	if errors.Is(err, group.ErrUnknownPermission) {
		http.Error(w, "unknown permission", http.StatusBadRequest)
		return
	}
	var autherr *group.NotAuthorisedError
	if errors.As(err, &autherr) {
		log.Printf("HTTP server error: %v", err)
		http.Error(w, "not authorised", http.StatusUnauthorized)
		return
	}
	var mberr *http.MaxBytesError
	if errors.As(err, &mberr) {
		http.Error(w, "Request body too large",
			http.StatusRequestEntityTooLarge)
		return
	}
	internalError(w, "HTTP server error: %v", err)
}

func methodNotAllowed(w http.ResponseWriter, methods string) {
	w.Header().Set("Allow", "OPTIONS, "+methods)
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
	if strings.HasPrefix(p, "/third-party/") {
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

	cspHeader(w, "")
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
			if errors.Is(err, os.ErrNotExist) {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
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

func splitPath(pth string) (string, string, string) {
	index := strings.Index(pth, "/.")
	if index < 0 {
		return pth, "", ""
	}

	index2 := strings.Index(pth[index+1:], "/")
	if index2 < 0 {
		return pth[:index], pth[index+1:], ""
	}
	return pth[:index], pth[index+1 : index+1+index2], pth[index+1+index2:]
}

func groupHandler(w http.ResponseWriter, r *http.Request) {
	if redirect(w, r) {
		return
	}

	dir, kind, rest := splitPath(r.URL.Path)
	if kind == ".status" && rest == "" {
		groupStatusHandler(w, r)
		return
	} else if kind == ".status.json" && rest == "" {
		http.Redirect(w, r, dir+"/"+".status",
			http.StatusPermanentRedirect)
		return
	} else if kind == ".whip" {
		if rest == "" {
			whipEndpointHandler(w, r)
		} else {
			whipResourceHandler(w, r)
		}
		return
	} else if kind != "" {
		notFound(w)
		return
	}

	name := parseGroupName("/group/", r.URL.Path)
	if name == "" {
		notFound(w)
		return
	}

	g, err := group.Add(name, nil)
	if err != nil {
		httpError(w, err)
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

	status := g.Status(false, nil)
	cspHeader(w, status.AuthServer)
	serveFile(w, r, filepath.Join(StaticRoot, "galene.html"))
}

func baseURL(r *http.Request) (*url.URL, error) {
	conf, err := group.GetConfiguration()
	if err != nil {
		return nil, err
	}
	var pu *url.URL
	if conf.ProxyURL != "" {
		pu, err = url.Parse(conf.ProxyURL)
		if err != nil {
			return nil, err
		}
	}
	scheme := "https"
	if r.TLS == nil {
		scheme = "http"
	}
	host := r.Host
	path := ""
	if pu != nil {
		if pu.Scheme != "" {
			scheme = pu.Scheme
		}
		if pu.Host != "" {
			host = pu.Host
		}
		path = pu.Path
	}
	base := url.URL{
		Scheme: scheme,
		Host:   host,
		Path:   path,
	}
	return &base, nil
}

func groupStatusHandler(w http.ResponseWriter, r *http.Request) {
	pth, kind, rest := splitPath(r.URL.Path)
	if kind != ".status" || rest != "" {
		internalError(w, "groupStatusHandler: this shouldn't happen")
		return
	}
	name := parseGroupName("/group/", pth)
	if name == "" {
		notFound(w)
		return
	}

	g, err := group.Add(name, nil)
	if err != nil {
		httpError(w, err)
		return
	}

	base, err := baseURL(r)
	if err != nil {
		internalError(w, "Parse ProxyURL: %v", err)
		return
	}
	d := g.Status(false, base)
	w.Header().Set("content-type", "application/json")
	w.Header().Set("cache-control", "no-cache")

	if r.Method == "HEAD" {
		return
	}

	e := json.NewEncoder(w)
	e.Encode(d)
}

func publicHandler(w http.ResponseWriter, r *http.Request) {
	base, err := baseURL(r)
	if err != nil {
		log.Printf("couldn't determine group base: %v", err)
		httpError(w, err)
		return
	}
	w.Header().Set("content-type", "application/json")
	w.Header().Set("cache-control", "no-cache")

	if r.Method == "HEAD" {
		return
	}

	g := group.GetPublic(base)
	e := json.NewEncoder(w)
	e.Encode(g)
}

func adminMatch(username, password string) (bool, error) {
	conf, err := group.GetConfiguration()
	if err != nil {
		return false, err
	}

	u, found := conf.Users[username]
	if found {
		ok, err := u.Password.Match(password)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
		perms := u.Permissions.Permissions(nil)
		for _, p := range perms {
			if p == "admin" {
				return true, nil
			}
		}
		return false, nil
	}

	return false, nil
}

func failAuthentication(w http.ResponseWriter, realm string) {
	w.Header().Set("www-authenticate",
		fmt.Sprintf("basic realm=\"%v\"", realm))
	http.Error(w, "Haha!", http.StatusUnauthorized)
}

func CheckOrigin(w http.ResponseWriter, r *http.Request, admin bool) bool {
	if w != nil {
		w.Header().Add("Vary", "Origin")
	}

	origins := r.Header["Origin"]
	if len(origins) == 0 {
		return true
	}
	origin := origins[0]

	ok := false
	o, err := url.Parse(origin)
	if err == nil && strings.EqualFold(o.Host, r.Host) {
		ok = true
	} else {
		conf, err := group.GetConfiguration()
		if err != nil {
			return false
		}

		allow := conf.AllowOrigin
		if admin {
			allow = conf.AllowAdminOrigin
		}
		for _, a := range allow {
			if strings.EqualFold(origin, a) {
				ok = true
				break
			}
		}
	}

	if !ok {
		return false
	}

	if w != nil {
		w.Header().Add("Access-Control-Allow-Origin", origin)
	}
	return true
}

var wsUpgrader = websocket.Upgrader{
	HandshakeTimeout: 30 * time.Second,
	CheckOrigin: func(r *http.Request) bool {
		return CheckOrigin(nil, r, false)
	},
}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Websocket upgrade: %v", err)
		return
	}

	var addr net.Addr
	tcpaddr, err := net.ResolveTCPAddr("tcp", r.RemoteAddr)
	if err != nil {
		log.Printf("ResolveTCPAddr: %v", err)
	} else {
		addr = tcpaddr
	}

	go func() {
		err := rtpconn.StartClient(conn, addr)
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
		internalError(w, "reconrdingsHandler: this shouldn't happen")
		return
	}

	p := "/" + r.URL.Path[12:]

	if filepath.Separator != '/' &&
		strings.ContainsRune(p, filepath.Separator) {
		http.Error(w, "Bad character in filename",
			http.StatusBadRequest)
		return
	}

	p = path.Clean(p)

	if p == "/" {
		http.Error(w, "Nothing here", http.StatusForbidden)
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
			http.Error(w, "Bad group name", http.StatusBadRequest)
			return
		}
	} else {
		if p[len(p)-1] == '/' {
			http.Error(w, "Bad group name", http.StatusBadRequest)
			return
		}
		group, filename = path.Split(p)
		group = parseGroupName("/", group)
		if group == "" {
			http.Error(w, "Bad group name", http.StatusBadRequest)
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
		methodNotAllowed(w, "POST")
		return
	}

	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Couldn't parse request", http.StatusBadRequest)
		return
	}

	q := r.Form.Get("q")

	switch q {
	case "delete":
		filename := r.Form.Get("filename")
		if group == "" || filename == "" {
			http.Error(w, "No filename provided",
				http.StatusBadRequest)
			return
		}
		if strings.ContainsRune(filename, '/') ||
			strings.ContainsRune(filename, filepath.Separator) {
			http.Error(w, "Bad character in filename",
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
		http.Error(w, "Unknown query", http.StatusBadRequest)
	}
}

func checkGroupPermissions(w http.ResponseWriter, r *http.Request, groupname string) bool {
	user, pass, ok := r.BasicAuth()
	if !ok {
		return false
	}

	g := group.Get(groupname)
	if g == nil {
		return false
	}

	_, p, err := g.GetPermission(
		group.ClientCredentials{
			Username: &user,
			Password: pass,
		},
	)
	record := false
	if err == nil {
		for _, v := range p {
			if v == "record" {
				record = true
				break
			}
		}
	}
	if err != nil || !record {
		var autherr *group.NotAuthorisedError
		if errors.As(err, &autherr) {
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
		httpError(w, err)
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
	if server == nil {
		log.Printf("Shutting down nonexistent server")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	server.Shutdown(ctx)
	server = nil
}
