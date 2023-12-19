package webserver

import (
	"bytes"
	crand "crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/pion/webrtc/v3"

	"github.com/jech/galene/group"
	"github.com/jech/galene/ice"
	"github.com/jech/galene/rtpconn"
)

func parseWhip(pth string) (string, string) {
	if pth != "/" {
		pth = strings.TrimSuffix(pth, "/")
	}
	dir := path.Dir(pth)
	base := path.Base(pth)
	if base == ".whip" {
		return dir + "/", ""
	}

	if path.Base(dir) == ".whip" {
		return path.Dir(dir) + "/", base
	}

	return "", ""
}

func newId() string {
	b := make([]byte, 16)
	crand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
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

	pth, pthid := parseWhip(r.URL.Path)
	if pthid != "" {
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
		if os.IsNotExist(err) {
			notFound(w)
			return
		}
		log.Printf("group.Add: %v", err)
		http.Error(w, "Internal server error",
			http.StatusInternalServerError)
		return
	}

	conf, err := group.GetConfiguration()
	if err != nil {
		http.Error(w, "Internal server error",
			http.StatusInternalServerError)
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
	c := rtpconn.NewWhipClient(g, id, token)

	_, err = group.AddClient(g.Name(), c, creds)
	if err == group.ErrNotAuthorised ||
		err == group.ErrAnonymousNotAuthorised {
		http.Error(w, "Authentication failed", http.StatusUnauthorized)
		return
	} else if err != nil {
		log.Printf("WHIP: %v", err)
		http.Error(w, "Internal Server Error",
			http.StatusInternalServerError)
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
		http.Error(w, "Internal Server Error",
			http.StatusInternalServerError)
	}

	w.Header().Set("Location", path.Join(r.URL.Path, id))
	w.Header().Set("Access-Control-Expose-Headers",
		"Location, Content-Type, Link")
	whipICEServers(w)
	w.Header().Set("Content-Type", "application/sdp")
	w.WriteHeader(http.StatusCreated)
	w.Write(answer)

	return
}

func whipResourceHandler(w http.ResponseWriter, r *http.Request) {
	pth, id := parseWhip(r.URL.Path)
	if pth == "" || id == "" {
		http.Error(w, "Internal server error",
			http.StatusInternalServerError)
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
		http.Error(w, "Internal server error",
			http.StatusInternalServerError)
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
	var ufrag []byte
	for _, l := range lines {
		l = bytes.TrimRight(l, " \r")
		if bytes.HasPrefix(l, []byte("a=ice-ufrag:")) {
			ufrag = l[len("a=ice-ufrag:"):]
		} else if bytes.HasPrefix(l, []byte("a=candidate:")) {
			err := c.GotICECandidate(l[2:], ufrag)
			if err != nil {
				log.Printf("WHIP candidate: %v", err)
			}
		} else if bytes.Equal(l, []byte("a=end-of-candidates")) {
			c.GotICECandidate(nil, ufrag)
		}
	}
	w.WriteHeader(http.StatusNoContent)
	return
}
