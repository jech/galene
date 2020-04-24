// Copyright (c) 2020 by Juliusz Chroboczek.

// This is not open source software.  Copy it, and I'll break into your
// house and tell your three year-old that Santa doesn't exist.

package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

var httpAddr string
var staticRoot string
var dataDir string
var iceFilename string

func main() {
	flag.StringVar(&httpAddr, "http", ":8443", "web server `address`")
	flag.StringVar(&staticRoot, "static", "./static/",
		"web server root `directory`")
	flag.StringVar(&dataDir, "data", "./data/",
		"data `directory`")
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
