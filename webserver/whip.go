package webserver

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	crand "crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/pion/webrtc/v3"

	"github.com/jech/galene/group"
	"github.com/jech/galene/ice"
	"github.com/jech/galene/rtpconn"
)

var idSecret []byte
var idCipher cipher.Block

func init() {
	idSecret = make([]byte, 16)
	_, err := crand.Read(idSecret)
	if err != nil {
		log.Fatalf("crand.Read: %v", err)
	}
	idCipher, err = aes.NewCipher(idSecret)
	if err != nil {
		log.Fatalf("NewCipher: %v", err)
	}
}

func newId() string {
	b := make([]byte, idCipher.BlockSize())
	crand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// we obfuscate ids to avoid exposing the WHIP session URL
func obfuscate(id string) (string, error) {
	v, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil {
		return "", err
	}

	if len(v) != idCipher.BlockSize() {
		return "", errors.New("bad length")
	}

	idCipher.Encrypt(v, v)

	return base64.RawURLEncoding.EncodeToString(v), nil
}

func deobfuscate(id string) (string, error) {
	v, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil {
		return "", err
	}

	if len(v) != idCipher.BlockSize() {
		return "", errors.New("bad length")
	}

	idCipher.Decrypt(v, v)

	return base64.RawURLEncoding.EncodeToString(v), nil
}

func canPresent(perms []string) bool {
	for _, p := range perms {
		if p == "present" {
			return true
		}
	}
	return false
}

func parseBearerToken(auth string) string {
	auths := strings.Split(auth, ",")
	for _, a := range auths {
		a = strings.Trim(a, " \t")
		s := strings.Split(a, " ")
		if len(s) == 2 && strings.EqualFold(s[0], "bearer") {
			return s[1]
		}
	}
	return ""
}

var iceServerReplacer = strings.NewReplacer(`\`, `\\`, `"`, `\"`)

func formatICEServer(server webrtc.ICEServer, u string) string {
	quote := func(s string) string {
		return iceServerReplacer.Replace(s)
	}
	uu, err := url.Parse(u)
	if err != nil {
		return ""
	}

	if strings.EqualFold(uu.Scheme, "stun") {
		return fmt.Sprintf("<%v>; rel=\"ice-server\"", u)
	} else if strings.EqualFold(uu.Scheme, "turn") ||
		strings.EqualFold(uu.Scheme, "turns") {
		pw, ok := server.Credential.(string)
		if !ok {
			return ""
		}
		return fmt.Sprintf("<%v>; rel=\"ice-server\"; "+
			"username=\"%v\"; "+
			"credential=\"%v\"; "+
			"credential-type=\"%v\"",
			u,
			quote(server.Username),
			quote(pw),
			quote(server.CredentialType.String()))
	}
	return ""
}

func whipICEServers(w http.ResponseWriter) {
	conf := ice.ICEConfiguration()
	for _, server := range conf.ICEServers {
		for _, u := range server.URLs {
			v := formatICEServer(server, u)
			if v != "" {
				w.Header().Add("Link", v)
			}
		}
	}
}

const sdpLimit = 1024 * 1024

func whipEndpointHandler(w http.ResponseWriter, r *http.Request) {
	if redirect(w, r) {
		return
	}

	pth, kind, pthid := splitPath(r.URL.Path)
	if kind != ".whip" || pthid != "" {
		http.Error(w, "Internal server error",
			http.StatusInternalServerError)
		return
	}

	name := parseGroupName("/group/", pth)
	if name == "" {
		notFound(w)
		return
	}

	g, err := group.Add(name, nil)
	if err != nil {
		httpError(w, err)
		return
	}

	conf, err := group.GetConfiguration()
	if err != nil {
		httpError(w, err)
		return
	}

	if conf.PublicServer {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	}

	if r.Method == "OPTIONS" {
		w.Header().Set("Access-Control-Allow-Methods", "OPTIONS, POST")
		w.Header().Set("Access-Control-Allow-Headers",
			"Authorization, Content-Type",
		)
		w.Header().Set("Access-Control-Expose-Headers", "Link")
		whipICEServers(w)
		return
	}

	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctype := r.Header.Get("content-type")
	if !strings.EqualFold(ctype, "application/sdp") {
		http.Error(w, "bad content type", http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, sdpLimit))
	if err != nil {
		httpError(w, err)
		return
	}

	token := parseBearerToken(r.Header.Get("Authorization"))

	whip := "whip"
	creds := group.ClientCredentials{
		Username: &whip,
		Token:    token,
	}

	id := newId()
	obfuscated, err := obfuscate(id)
	if err != nil {
		httpError(w, err)
		return
	}

	c := rtpconn.NewWhipClient(g, id, token)

	_, err = group.AddClient(g.Name(), c, creds)
	if err != nil {
		log.Printf("WHIP: %v", err)
		httpError(w, err)
		return
	}

	if !canPresent(c.Permissions()) {
		group.DelClient(c)
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	answer, err := c.NewConnection(r.Context(), body)
	if err != nil {
		group.DelClient(c)
		log.Printf("WHIP offer: %v", err)
		httpError(w, err)
		return
	}

	w.Header().Set("Location", path.Join(r.URL.Path, obfuscated))
	w.Header().Set("Access-Control-Expose-Headers",
		"Location, Content-Type, Link")
	whipICEServers(w)
	w.Header().Set("Content-Type", "application/sdp")
	w.WriteHeader(http.StatusCreated)
	w.Write(answer)

	return
}

func whipResourceHandler(w http.ResponseWriter, r *http.Request) {
	pth, kind, rest := splitPath(r.URL.Path)
	if kind != ".whip" || rest == "" {
		http.Error(w, "Internal server error",
			http.StatusInternalServerError)
		return
	}
	id, err := deobfuscate(rest[1:])
	if err != nil {
		httpError(w, err)
		return
	}

	name := parseGroupName("/group/", pth)
	if name == "" {
		notFound(w)
		return
	}

	g := group.Get(name)
	if g == nil {
		notFound(w)
		return
	}

	cc := g.GetClient(id)
	if cc == nil {
		notFound(w)
		return
	}

	c, ok := cc.(*rtpconn.WhipClient)
	if !ok {
		notFound(w)
		return
	}

	if t := c.Token(); t != "" {
		token := parseBearerToken(r.Header.Get("Authorization"))
		if token != t {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
	}

	conf, err := group.GetConfiguration()
	if err != nil {
		httpError(w, err)
		return
	}

	if conf.PublicServer {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	}

	if r.Method == "OPTIONS" {
		w.Header().Set("Access-Control-Allow-Methods",
			"OPTIONS, PATCH, DELETE",
		)
		w.Header().Set("Access-Control-Allow-Headers",
			"Authorization, Content-Type",
		)
		return
	}

	if r.Method == "DELETE" {
		c.Close()
		return
	}

	if r.Method != "PATCH" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return

	}

	ctype := r.Header.Get("content-type")
	if !strings.EqualFold(ctype, "application/trickle-ice-sdpfrag") {
		http.Error(w, "bad content type", http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, sdpLimit))
	if err != nil {
		httpError(w, err)
		return
	}

	if len(body) < 2 {
		http.Error(w, "SDP truncated", http.StatusBadRequest)
		return
	}

	// RFC 8840
	lines := bytes.Split(body, []byte{'\n'})
	mLineIndex := -1
	var mid, ufrag []byte
	for _, l := range lines {
		l = bytes.TrimRight(l, " \r")
		if bytes.HasPrefix(l, []byte("a=ice-ufrag:")) {
			ufrag = l[len("a=ice-ufrag:"):]
		} else if bytes.HasPrefix(l, []byte("m=")) {
			mLineIndex++
			mid = nil
		} else if bytes.HasPrefix(l, []byte("a=mid:")) {
			mid = l[len("a=mid:"):]
		} else if bytes.HasPrefix(l, []byte("a=candidate:")) {
			init := webrtc.ICECandidateInit{
				Candidate: string(l[2:]),
			}
			if len(mid) > 0 {
				s := string(mid)
				init.SDPMid = &s
			}
			if mLineIndex >= 0 {
				i := uint16(mLineIndex)
				init.SDPMLineIndex = &i
			}
			if len(ufrag) > 0 {
				s := string(ufrag)
				init.UsernameFragment = &s
			}
			err := c.GotICECandidate(init)
			if err != nil {
				log.Printf("WHIP candidate: %v", err)
			}
		}
	}
	w.WriteHeader(http.StatusNoContent)
	return
}
