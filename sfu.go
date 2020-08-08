// Copyright (c) 2020 by Juliusz Chroboczek.

// This is not open source software.  Copy it, and I'll break into your
// house and tell your three year-old that Santa doesn't exist.

package main

import (
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"syscall"

	"sfu/disk"
	"sfu/group"
	"sfu/webserver"
)

func main() {
	var cpuprofile, memprofile, mutexprofile, httpAddr, dataDir string
	var tcpPort int

	flag.StringVar(&httpAddr, "http", ":8443", "web server `address`")
	flag.StringVar(&webserver.StaticRoot, "static", "./static/",
		"web server root `directory`")
	flag.IntVar(&tcpPort, "tcp-port", -1,
		"TCP listener `port`.  If 0, an ephemeral port is used.\n"+
			"If -1, the TCP listerer is disabled")
	flag.StringVar(&dataDir, "data", "./data/",
		"data `directory`")
	flag.StringVar(&group.Directory, "groups", "./groups/",
		"group description `directory`")
	flag.StringVar(&disk.Directory, "recordings", "./recordings/",
		"recordings `directory`")
	flag.StringVar(&cpuprofile, "cpuprofile", "",
		"store CPU profile in `file`")
	flag.StringVar(&memprofile, "memprofile", "",
		"store memory profile in `file`")
	flag.StringVar(&mutexprofile, "mutexprofile", "",
		"store mutex profile in `file`")
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

	group.IceFilename = filepath.Join(dataDir, "ice-servers.json")

	if tcpPort >= 0 {
		err := group.StartTCPListener(&net.TCPAddr{
			Port: tcpPort,
		})
		if err != nil {
			log.Fatalf("Couldn't start ICE TCP: %v", err)
		}
	}

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
	select {
	case <-terminate:
		webserver.Shutdown()
	case <-serverDone:
		os.Exit(1)
	}
}
