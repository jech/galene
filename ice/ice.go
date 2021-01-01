package ice

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"sync/atomic"
	"time"

	"github.com/pion/webrtc/v3"
)

type Server struct {
	URLs           []string    `json:"urls"`
	Username       string      `json:"username,omitempty"`
	Credential     interface{} `json:"credential,omitempty"`
	CredentialType string      `json:"credentialType,omitempty"`
}

func getServer(server Server) (webrtc.ICEServer, error) {
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
	case "hmac-sha1":
		cred, ok := server.Credential.(string)
		if !ok {
			return webrtc.ICEServer{},
				errors.New("credential is not a string")
		}
		ts := time.Now().Unix() + 86400
		var username string
		if server.Username == "" {
			username = fmt.Sprintf("%d", ts)
		} else {
			username = fmt.Sprintf("%d:%s", ts, server.Username)
		}
		mac := hmac.New(sha1.New, []byte(cred))
		mac.Write([]byte(username))
		buf := bytes.Buffer{}
		e := base64.NewEncoder(base64.StdEncoding, &buf)
		e.Write(mac.Sum(nil))
		e.Close()
		s.Username = username
		s.Credential = string(buf.Bytes())
		s.CredentialType = webrtc.ICECredentialTypePassword
	default:
		return webrtc.ICEServer{}, errors.New("unsupported credential type")
	}
	return s, nil
}

var ICEFilename string
var ICERelayOnly bool

type configuration struct {
	conf      webrtc.Configuration
	timestamp time.Time
}

var conf atomic.Value

func updateICEConfiguration() *configuration {
	now := time.Now()
	var cf webrtc.Configuration

	if ICEFilename != "" {
		file, err := os.Open(ICEFilename)
		if err != nil {
			if !os.IsNotExist(err) {
				log.Printf("Open %v: %v", ICEFilename, err)
			}
		} else {
			defer file.Close()
			d := json.NewDecoder(file)
			var servers []Server
			err = d.Decode(&servers)
			if err != nil {
				log.Printf("Get ICE configuration: %v", err)
			}
			for _, s := range servers {
				ss, err := getServer(s)
				if err != nil {
					log.Printf("parse ICE server: %v", err)
					continue
				}
				cf.ICEServers = append(cf.ICEServers, ss)
			}
		}
	}

	if ICERelayOnly {
		cf.ICETransportPolicy = webrtc.ICETransportPolicyRelay
	}

	iceConf := configuration{
		conf:      cf,
		timestamp: now,
	}
	conf.Store(&iceConf)
	return &iceConf
}

func ICEConfiguration() *webrtc.Configuration {
	conf, ok := conf.Load().(*configuration)
	if !ok || time.Since(conf.timestamp) > 5*time.Minute {
		conf = updateICEConfiguration()
	} else if time.Since(conf.timestamp) > 2*time.Minute {
		go updateICEConfiguration()
	}

	return &conf.conf
}
