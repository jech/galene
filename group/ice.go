package group

import (
	"encoding/json"
	"log"
	"os"
	"sync"

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

var iceConf RTCConfiguration
var iceOnce sync.Once

func ICEConfiguration() *RTCConfiguration {
	iceOnce.Do(func() {
		var iceServers []ICEServer
		file, err := os.Open(ICEFilename)
		if err != nil {
			log.Printf("Open %v: %v", ICEFilename, err)
			return
		}
		defer file.Close()
		d := json.NewDecoder(file)
		err = d.Decode(&iceServers)
		if err != nil {
			log.Printf("Get ICE configuration: %v", err)
			return
		}
		iceConf = RTCConfiguration{
			ICEServers: iceServers,
		}
	})

	return &iceConf
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
