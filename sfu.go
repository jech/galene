// Copyright (c) 2020 by Juliusz Chroboczek.

// This is not open source software.  Copy it, and I'll break into your
// house and tell your three year-old that Santa doesn't exist.

package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

var httpAddr string
var staticRoot string
var dataDir string
var groupsDir string
var iceFilename string

func main() {
	flag.StringVar(&httpAddr, "http", ":8443", "web server `address`")
	flag.StringVar(&staticRoot, "static", "./static/",
		"web server root `directory`")
	flag.StringVar(&dataDir, "data", "./data/",
		"data `directory`")
	flag.StringVar(&groupsDir, "groups", "./groups/",
		"group description `directory`")
	flag.Parse()
	iceFilename = filepath.Join(staticRoot, "ice-servers.json")

	http.Handle("/", mungeHandler{http.FileServer(http.Dir(staticRoot))})
	http.HandleFunc("/group/",
		func(w http.ResponseWriter, r *http.Request) {
			mungeHeader(w)
			http.ServeFile(w, r, staticRoot+"/sfu.html")
		})
	http.HandleFunc("/ws", wsHandler)
	http.HandleFunc("/public-groups.json", publicHandler)
	http.HandleFunc("/stats", statsHandler)

	go readPublicGroups()

	go func() {
		server := &http.Server{
			Addr:         httpAddr,
			ReadTimeout:  60 * time.Second,
			WriteTimeout: 30 * time.Second,
			IdleTimeout:  120 * time.Second,
		}
		var err error
		log.Printf("Listening on %v", httpAddr)
		err = server.ListenAndServeTLS(
			filepath.Join(dataDir, "cert.pem"),
			filepath.Join(dataDir, "key.pem"),
		)
		log.Fatalf("ListenAndServeTLS: %v", err)
	}()

	terminate := make(chan os.Signal, 1)
	signal.Notify(terminate, syscall.SIGINT)
	<-terminate
}

func mungeHeader(w http.ResponseWriter) {
	w.Header().Add("Content-Security-Policy",
		"connect-src ws: wss: 'self'; img-src data: 'self'; default-src 'self'")
}

type mungeHandler struct {
	h http.Handler
}

func (h mungeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	mungeHeader(w)
	h.h.ServeHTTP(w, r)
}

func publicHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("content-type", "application/json")
	w.Header().Set("cache-control", "no-cache")

	if r.Method == "HEAD" {
		return
	}

	g := getPublicGroups()
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

func statsHandler(w http.ResponseWriter, r *http.Request) {
	bail := func() {
		w.Header().Set("www-authenticate", "basic realm=\"stats\"")
		http.Error(w, "Haha!", http.StatusUnauthorized)
	}

	u, p, err := getPassword()
	if err != nil {
		log.Printf("Passwd: %v", err)
		bail()
		return
	}

	username, password, ok := r.BasicAuth()
	if !ok || username != u || password != p {
		bail()
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
		fmt.Fprintf(w, "<td>%d%%</td></tr>\n",
			t.loss,
		)
	}

	for _, gs := range stats {
		fmt.Fprintf(w, "<p>%v</p>\n", html.EscapeString(gs.name))
		fmt.Fprintf(w, "<table>")
		for _, cs := range gs.clients {
			fmt.Fprintf(w, "<tr><td>%v</td></tr>\n", cs.id)
			for _, up := range cs.up {
				fmt.Fprintf(w, "<tr><td></td><td>Up</td><td>%v</td></tr>\n",
					up.id)
				for _, t := range up.tracks {
					printTrack(w, t)
				}
			}
			for _, up := range cs.down {
				fmt.Fprintf(w, "<tr><td></td><td>Down</td><td> %v</td></tr>\n",
					up.id)
				for _, t := range up.tracks {
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
