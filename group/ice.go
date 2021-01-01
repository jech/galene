package group

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"sync/atomic"
	"time"

	"github.com/pion/webrtc/v3"
)

type ICEServer struct {
	URLs           []string    `json:"urls"`
	Username       string      `json:"username,omitempty"`
	Credential     interface{} `json:"credential,omitempty"`
	CredentialType string      `json:"credentialType,omitempty"`
}

func getICEServer(server ICEServer) (webrtc.ICEServer, error) {
	s := webrtc.ICEServer{
		URLs:       server.URLs,
		Username:   server.Username,
		Credential: server.Credential,
	}
	switch server.CredentialType {
	case "", "password":
		s.CredentialType = webrtc.ICECredentialTypePassword
	case "oauth":
		s.CredentialType = webrtc.ICECredentialTypeOauth
	default:
		return webrtc.ICEServer{}, errors.New("unsupported credential type")
	}
	return s, nil
}

var ICEFilename string
var ICERelayOnly bool

type iceConf struct {
	conf      webrtc.Configuration
	timestamp time.Time
}

var iceConfiguration atomic.Value

func updateICEConfiguration() *iceConf {
	now := time.Now()
	var conf webrtc.Configuration

	if ICEFilename != "" {
		file, err := os.Open(ICEFilename)
		if err != nil {
			log.Printf("Open %v: %v", ICEFilename, err)
		} else {
			defer file.Close()
			d := json.NewDecoder(file)
			var servers []ICEServer
			err = d.Decode(&servers)
			if err != nil {
				log.Printf("Get ICE configuration: %v", err)
			}
			for _, s := range servers {
				ss, err := getICEServer(s)
				if err != nil {
					log.Printf("parse ICE server: %v", err)
					continue
				}
				conf.ICEServers = append(conf.ICEServers, ss)
			}
		}
	}

	if ICERelayOnly {
		conf.ICETransportPolicy = webrtc.ICETransportPolicyRelay
	}

	iceConf := iceConf{
		conf:      conf,
		timestamp: now,
	}
	iceConfiguration.Store(&iceConf)
	return &iceConf
}

func ICEConfiguration() *webrtc.Configuration {
	conf, ok := iceConfiguration.Load().(*iceConf)
	if !ok || time.Since(conf.timestamp) > 5*time.Minute {
		conf = updateICEConfiguration()
	} else if time.Since(conf.timestamp) > 2*time.Minute {
		go updateICEConfiguration()
	}

	return &conf.conf
}
