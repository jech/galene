package ice

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"reflect"
	"strings"
	"testing"

	"github.com/pion/webrtc/v3"
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
