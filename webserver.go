package main

import (
	"bufio"
	"context"
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
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"sfu/group"
)

var server *http.Server

func webserver() {
	http.Handle("/", mungeHandler{http.FileServer(http.Dir(staticRoot))})
	http.HandleFunc("/group/", groupHandler)
	http.HandleFunc("/recordings",
		func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r,
				"/recordings/", http.StatusPermanentRedirect)
		})
	http.HandleFunc("/recordings/", recordingsHandler)
	http.HandleFunc("/ws", wsHandler)
	http.HandleFunc("/ice-servers.json",
		func(w http.ResponseWriter, r *http.Request) {
			mungeHeader(w)
			http.ServeFile(w, r,
				filepath.Join(dataDir, "ice-servers.json"))
		})
	http.HandleFunc("/public-groups.json", publicHandler)
	http.HandleFunc("/stats", statsHandler)

	server = &http.Server{
		Addr:              httpAddr,
		ReadHeaderTimeout: 60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	server.RegisterOnShutdown(func() {
		group.Range(func (g *group.Group) bool {
			go g.Shutdown("server is shutting down")
			return true
		})
	})
	go func() {
		var err error
		err = server.ListenAndServeTLS(
			filepath.Join(dataDir, "cert.pem"),
			filepath.Join(dataDir, "key.pem"),
		)
		if err != nil && err != http.ErrServerClosed {
			log.Printf("ListenAndServeTLS: %v", err)
		}
	}()
}

func mungeHeader(w http.ResponseWriter) {
	w.Header().Add("Content-Security-Policy",
		"connect-src ws: wss: 'self'; img-src data: 'self'; default-src 'self'")
}

// mungeResponseWriter redirects 404 replies to our custom 404 page.
type mungeResponseWriter struct {
	http.ResponseWriter
	active bool
}

func (w *mungeResponseWriter) WriteHeader(status int) {
	if status == http.StatusNotFound {
		w.active = true
		notFound(w.ResponseWriter)
		return
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *mungeResponseWriter) Write(p []byte) (int, error) {
	if w.active {
		return len(p), nil
	}
	return w.ResponseWriter.Write(p)
}

func notFound(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)

	f, err := os.Open(path.Join(staticRoot, "404.html"))
	if err != nil {
		fmt.Fprintln(w, "<p>Not found</p>")
		return
	}
	defer f.Close()

	io.Copy(w, f)
}

// mungeHandler adds our custom headers and redirects 404 replies
type mungeHandler struct {
	h http.Handler
}

func (h mungeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	mungeHeader(w)
	h.h.ServeHTTP(&mungeResponseWriter{ResponseWriter: w}, r)
}

func parseGroupName(path string) string {
	if !strings.HasPrefix(path, "/group/") {
		return ""
	}

	name := path[len("/group/"):]
	if name == "" {
		return ""
	}

	if name[len(name)-1] == '/' {
		name = name[:len(name)-1]
	}
	return name
}

func groupHandler(w http.ResponseWriter, r *http.Request) {
	mungeHeader(w)
	name := parseGroupName(r.URL.Path)
	if name == "" {
		notFound(w)
		return
	}

	g, err := group.Add(name, nil)
	if err != nil {
		if os.IsNotExist(err) {
			notFound(w)
		} else {
			log.Println("addGroup: %v", err)
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

	http.ServeFile(w, r, filepath.Join(staticRoot, "sfu.html"))
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

func getPassword() (string, string, error) {
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

func statsHandler(w http.ResponseWriter, r *http.Request) {
	u, p, err := getPassword()
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

	w.Header().Set("content-type", "text/html; charset=utf-8")
	w.Header().Set("cache-control", "no-cache")
	if r.Method == "HEAD" {
		return
	}

	stats := getGroupStats()

	fmt.Fprintf(w, "<!DOCTYPE html>\n<html><head>\n")
	fmt.Fprintf(w, "<title>Stats</title>\n")
	fmt.Fprintf(w, "<link rel=\"stylesheet\" type=\"text/css\" href=\"/common.css\"/>")
	fmt.Fprintf(w, "<head><body>\n")

	printBitrate := func(w io.Writer, rate, maxRate uint64) error {
		var err error
		if maxRate != 0 && maxRate != ^uint64(0) {
			_, err = fmt.Fprintf(w, "%v/%v", rate, maxRate)
		} else {
			_, err = fmt.Fprintf(w, "%v", rate)
		}
		return err
	}

	printTrack := func(w io.Writer, t trackStats) {
		fmt.Fprintf(w, "<tr><td></td><td></td><td></td>")
		fmt.Fprintf(w, "<td>")
		printBitrate(w, t.bitrate, t.maxBitrate)
		fmt.Fprintf(w, "</td>")
		fmt.Fprintf(w, "<td>%d%%</td>",
			t.loss,
		)
		fmt.Fprintf(w, "<td>")
		if t.rtt > 0 {
			fmt.Fprintf(w, "%v", t.rtt)
		}
		if t.jitter > 0 {
			fmt.Fprintf(w, "&#177;%v", t.jitter)
		}
		fmt.Fprintf(w, "</td>")
		fmt.Fprintf(w, "</tr>")
	}

	for _, gs := range stats {
		fmt.Fprintf(w, "<p>%v</p>\n", html.EscapeString(gs.name))
		fmt.Fprintf(w, "<table>")
		for _, cs := range gs.clients {
			fmt.Fprintf(w, "<tr><td>%v</td></tr>\n", cs.id)
			for _, up := range cs.up {
				fmt.Fprintf(w, "<tr><td></td><td>Up</td><td>%v</td>",
					up.id)
				if up.maxBitrate > 0 {
					fmt.Fprintf(w, "<td>%v</td>",
						up.maxBitrate)
				}
				fmt.Fprintf(w, "</tr>\n")
				for _, t := range up.tracks {
					printTrack(w, t)
				}
			}
			for _, down := range cs.down {
				fmt.Fprintf(w, "<tr><td></td><td>Down</td><td> %v</td>",
					down.id)
				if down.maxBitrate > 0 {
					fmt.Fprintf(w, "<td>%v</td>",
						down.maxBitrate)
				}
				fmt.Fprintf(w, "</tr>\n")
				for _, t := range down.tracks {
					printTrack(w, t)
				}
			}
		}
		fmt.Fprintf(w, "</table>\n")
	}
	fmt.Fprintf(w, "</body></html>\n")
}

var upgrader websocket.Upgrader

func wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Websocket upgrade: %v", err)
		return
	}
	go func() {
		err := startClient(conn)
		if err != nil {
			log.Printf("client: %v", err)
		}
	}()
}

func recordingsHandler(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.Path) < 12 || r.URL.Path[:12] != "/recordings/" {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	pth := r.URL.Path[12:]

	if pth == "" {
		http.Error(w, "nothing to see", http.StatusNotImplemented)
		return
	}

	f, err := os.Open(filepath.Join(recordingsDir, pth))
	if err != nil {
		if os.IsNotExist(err) {
			notFound(w)
		} else {
			http.Error(w, "server error",
				http.StatusInternalServerError)
		}
		return
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	if fi.IsDir() {
		if pth[len(pth)-1] != '/' {
			http.Redirect(w, r,
				r.URL.Path+"/", http.StatusPermanentRedirect)
			return
		}
		ok := checkGroupPermissions(w, r, path.Dir(pth))
		if !ok {
			failAuthentication(w, "recordings/"+path.Dir(pth))
			return
		}
		if r.Method == "POST" {
			handleGroupAction(w, r, path.Dir(pth))
		} else {
			serveGroupRecordings(w, r, f, path.Dir(pth))
		}
	} else {
		ok := checkGroupPermissions(w, r, path.Dir(pth))
		if !ok {
			failAuthentication(w, "recordings/"+path.Dir(pth))
			return
		}
		http.ServeContent(w, r, r.URL.Path, fi.ModTime(), f)
	}
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
		err := os.Remove(
			filepath.Join(recordingsDir, group+"/"+filename),
		)
		if err != nil {
			if os.IsPermission(err) {
				http.Error(w, "unauthorized",
					http.StatusForbidden)
			} else {
				http.Error(w, "server error",
					http.StatusInternalServerError)
			}
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

	p, err := desc.GetPermission(group.ClientCredentials{user, pass})
	if err != nil || !p.Record {
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

	w.Header().Set("content-type", "text/html; charset=utf-8")
	w.Header().Set("cache-control", "no-cache")

	if r.Method == "HEAD" {
		return
	}

	fmt.Fprintf(w, "<!DOCTYPE html>\n<html><head>\n")
	fmt.Fprintf(w, "<title>Recordings for group %v</title>\n", group)
	fmt.Fprintf(w, "<link rel=\"stylesheet\" type=\"text/css\" href=\"/common.css\"/>")
	fmt.Fprintf(w, "<head><body>\n")

	fmt.Fprintf(w, "<table>\n")
	for _, fi := range fis {
		if fi.IsDir() {
			continue
		}
		fmt.Fprintf(w, "<tr><td><a href=\"%v\">%v</a></td><td>%d</td>",
			html.EscapeString(fi.Name()),
			html.EscapeString(fi.Name()),
			fi.Size(),
		)
		fmt.Fprintf(w,
			"<td><form action=\"/recordings/%v/?q=delete\" method=\"post\">"+
				"<button type=\"submit\" name=\"filename\" value=\"%v\">Delete</button>"+
				"</form></td></tr>\n",
			url.PathEscape(group), fi.Name())
	}
	fmt.Fprintf(w, "</table>\n")
	fmt.Fprintf(w, "</body></html>\n")
}

func shutdown() {
	ctx, _ := context.WithTimeout(context.Background(), 2*time.Second)
	server.Shutdown(ctx)
}
