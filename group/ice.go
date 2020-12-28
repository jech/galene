package group

import (
	"encoding/json"
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

type RTCConfiguration struct {
	ICEServers         []ICEServer `json:"iceServers,omitempty"`
	ICETransportPolicy string      `json:"iceTransportPolicy,omitempty"`
}

var ICEFilename string
var ICERelayOnly bool

type iceConf struct {
	conf      RTCConfiguration
	timestamp time.Time
}

var iceConfiguration atomic.Value

func updateICEConfiguration() *iceConf {
	now := time.Now()
	var conf RTCConfiguration

	if ICEFilename != "" {
		file, err := os.Open(ICEFilename)
		if err != nil {
			log.Printf("Open %v: %v", ICEFilename, err)
		} else {
			defer file.Close()
			d := json.NewDecoder(file)
			err = d.Decode(&conf.ICEServers)
			if err != nil {
				log.Printf("Get ICE configuration: %v", err)
			}
		}
	}

	if ICERelayOnly {
		conf.ICETransportPolicy = "relay"
	}

	iceConf := iceConf{
		conf:      conf,
		timestamp: now,
	}
	iceConfiguration.Store(&iceConf)
	return &iceConf
}

func ICEConfiguration() *RTCConfiguration {
	conf, ok := iceConfiguration.Load().(*iceConf)
	if !ok || time.Since(conf.timestamp) > 5*time.Minute {
		conf = updateICEConfiguration()
	} else if time.Since(conf.timestamp) > 2*time.Minute {
		go updateICEConfiguration()
	}

	return &conf.conf
}

func ToConfiguration(conf *RTCConfiguration) webrtc.Configuration {
	var iceServers []webrtc.ICEServer
	for _, s := range conf.ICEServers {
		tpe := webrtc.ICECredentialTypePassword
		if s.CredentialType == "oauth" {
			tpe = webrtc.ICECredentialTypeOauth
		}
		iceServers = append(iceServers,
			webrtc.ICEServer{
				URLs:           s.URLs,
				Username:       s.Username,
				Credential:     s.Credential,
				CredentialType: tpe,
			},
		)
	}

	policy := webrtc.ICETransportPolicyAll
	if conf.ICETransportPolicy == "relay" {
		policy = webrtc.ICETransportPolicyRelay
	}

	return webrtc.Configuration{
		ICEServers:         iceServers,
		ICETransportPolicy: policy,
	}
}
