package ice

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/pion/webrtc/v3"

	"github.com/jech/galene/turnserver"
)

func TestPassword(t *testing.T) {
	s := Server{
		URLs:       []string{"turn:turn.example.org"},
		Username:   "jch",
		Credential: "secret",
	}

	ss := webrtc.ICEServer{
		URLs:       []string{"turn:turn.example.org"},
		Username:   "jch",
		Credential: "secret",
	}

	sss, err := getServer(s)

	if err != nil || !reflect.DeepEqual(sss, ss) {
		t.Errorf("Got %v, expected %v", sss, ss)
	}
}

func TestHMAC(t *testing.T) {
	s := Server{
		URLs:           []string{"turn:turn.example.org"},
		Username:       "jch",
		Credential:     "secret",
		CredentialType: "hmac-sha1",
	}

	ss := webrtc.ICEServer{
		URLs: []string{"turn:turn.example.org"},
	}

	sss, err := getServer(s)

	if !strings.HasSuffix(sss.Username, ":"+s.Username) {
		t.Errorf("username is %v", ss.Username)
	}
	ss.Username = sss.Username

	mac := hmac.New(sha1.New, []byte(s.Credential.(string)))
	mac.Write([]byte(sss.Username))
	buf := bytes.Buffer{}
	e := base64.NewEncoder(base64.StdEncoding, &buf)
	e.Write(mac.Sum(nil))
	e.Close()
	ss.Credential = string(buf.Bytes())

	if err != nil || !reflect.DeepEqual(sss, ss) {
		t.Errorf("Got %v, expected %v", sss, ss)
	}
}

func TestICEConfiguration(t *testing.T) {
	ICEFilename = "/tmp/no/such/file"
	turnserver.Address = ""

	conf := ICEConfiguration()
	if conf == nil {
		t.Errorf("conf is nil")
	}
	conf2 := ICEConfiguration()
	if conf2 != conf {
		t.Errorf("conf2 != conf")
	}

	if len(conf.ICEServers) != 0 {
		t.Errorf("len(ICEServers) = %v", len(conf.ICEServers))
	}
}

func TestRelayTest(t *testing.T) {
	ICEFilename = "/tmp/no/such/file"
	turnserver.Address = ""

	_, err := RelayTest(200 * time.Millisecond)
	if err == nil || !os.IsTimeout(err) {
		t.Errorf("Relay test returned %v", err)
	}
}
