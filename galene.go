package main

import (
	"flag"
	"fmt"
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
	"github.com/jech/galene/ice"
	"github.com/jech/galene/turnserver"
	"github.com/jech/galene/webserver"
)

func main() {
	var cpuprofile, memprofile, mutexprofile, httpAddr, dataDir string
	var udpRange string

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
	flag.StringVar(&udpRange, "udp-range", "",
		"UDP port `range`")
	flag.BoolVar(&group.UseMDNS, "mdns", false, "gather mDNS addresses")
	flag.BoolVar(&ice.ICERelayOnly, "relay-only", false,
		"require use of TURN relays for all media traffic")
	flag.StringVar(&turnserver.Address, "turn", "auto",
		"built-in TURN server `address` (\"\" to disable)")
	flag.Parse()

	if udpRange != "" {
		var min, max uint16
		n, err := fmt.Sscanf(udpRange, "%v-%v", &min, &max)
		if err != nil {
			log.Printf("UDP range: %v", err)
			os.Exit(1)
		}
		if n != 2 || min <= 0 || max <= 0 || min > max {
			log.Printf("UDP range: bad range")
			os.Exit(1)
		}
		group.UDPMin = min
		group.UDPMax = max
	}

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

	ice.ICEFilename = filepath.Join(dataDir, "ice-servers.json")

	go group.ReadPublicGroups()

	// causes the built-in server to start if required
	ice.Update()
	defer turnserver.Stop()

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

	go relayTest()

	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()

	slowTicker := time.NewTicker(12 * time.Hour)
	defer slowTicker.Stop()

	for {
		select {
		case <-ticker.C:
			go group.Expire()
		case <-slowTicker.C:
			go relayTest()
		case <-terminate:
			webserver.Shutdown()
			return
		case <-serverDone:
			os.Exit(1)
		}
	}
}

func relayTest() {
	now := time.Now()
	d, err := ice.RelayTest(20 * time.Second)
	if err != nil {
		log.Printf("Relay test failed: %v", err)
		log.Printf("Perhaps you didn't configure a TURN server?")
		return
	}
	log.Printf("Relay test successful in %v, RTT = %v", time.Since(now), d)
}
