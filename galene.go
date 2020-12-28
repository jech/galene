package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"syscall"
	"time"

	"github.com/jech/galene/diskwriter"
	"github.com/jech/galene/group"
	"github.com/jech/galene/webserver"
)

func main() {
	var cpuprofile, memprofile, mutexprofile, httpAddr, dataDir string

	flag.StringVar(&httpAddr, "http", ":8443", "web server `address`")
	flag.StringVar(&webserver.StaticRoot, "static", "./static/",
		"web server root `directory`")
	flag.StringVar(&webserver.Redirect, "redirect", "",
		"redirect to canonical `host`")
	flag.BoolVar(&webserver.Insecure, "insecure", false,
		"act as an HTTP server rather than HTTPS")
	flag.StringVar(&dataDir, "data", "./data/",
		"data `directory`")
	flag.StringVar(&group.Directory, "groups", "./groups/",
		"group description `directory`")
	flag.StringVar(&diskwriter.Directory, "recordings", "./recordings/",
		"recordings `directory`")
	flag.StringVar(&cpuprofile, "cpuprofile", "",
		"store CPU profile in `file`")
	flag.StringVar(&memprofile, "memprofile", "",
		"store memory profile in `file`")
	flag.StringVar(&mutexprofile, "mutexprofile", "",
		"store mutex profile in `file`")
	flag.BoolVar(&group.UseMDNS, "mdns", false, "gather mDNS addresses")
	flag.BoolVar(&group.ICERelayOnly, "relay-only", false,
		"require use of TURN relays for all media traffic")
	flag.Parse()

	if cpuprofile != "" {
		f, err := os.Create(cpuprofile)
		if err != nil {
			log.Printf("Create(cpuprofile): %v", err)
			return
		}
		pprof.StartCPUProfile(f)
		defer func() {
			pprof.StopCPUProfile()
			f.Close()
		}()
	}

	if memprofile != "" {
		defer func() {
			f, err := os.Create(memprofile)
			if err != nil {
				log.Printf("Create(memprofile): %v", err)
				return
			}
			pprof.WriteHeapProfile(f)
			f.Close()
		}()
	}

	if mutexprofile != "" {
		runtime.SetMutexProfileFraction(1)
		defer func() {
			f, err := os.Create(mutexprofile)
			if err != nil {
				log.Printf("Create(mutexprofile): %v", err)
				return
			}
			pprof.Lookup("mutex").WriteTo(f, 0)
			f.Close()
		}()
	}

	group.ICEFilename = filepath.Join(dataDir, "ice-servers.json")

	go group.ReadPublicGroups()

	serverDone := make(chan struct{})
	go func() {
		err := webserver.Serve(httpAddr, dataDir)
		if err != nil {
			log.Printf("Server: %v", err)
		}
		close(serverDone)
	}()

	terminate := make(chan os.Signal, 1)
	signal.Notify(terminate, syscall.SIGINT, syscall.SIGTERM)

	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			go group.Expire()
		case <-terminate:
			webserver.Shutdown()
			return
		case <-serverDone:
			os.Exit(1)
		}
	}
}
