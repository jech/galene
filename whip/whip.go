package whip

import (
	"bytes"
	crand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"

	"github.com/jech/galene/conn"
	"github.com/jech/galene/group"
	"github.com/jech/galene/rtpconn"
	"github.com/pion/webrtc/v3"
)

var PublicServer bool

type Client struct {
	group     *group.Group
	id        string
	username  string

	mu          sync.Mutex
	permissions group.ClientPermissions
	connection  *rtpconn.UpConn
}

func (c *Client) Group() *group.Group {
	return c.group
}

func (c *Client) Id() string {
	return c.id
}

func (c *Client) Username() string {
	return c.username
}

func (c *Client) Permissions() group.ClientPermissions {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.permissions
}

func (c *Client) SetPermissions(perms group.ClientPermissions) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.permissions = perms
}

func (c *Client) Status() map[string]interface{} {
	return nil
}

func (c *Client) PushConn(g *group.Group, id string, conn conn.Up, tracks []conn.UpTrack, replace string) error {
	return nil
}

func (c *Client) RequestConns(target group.Client, g *group.Group, id string) error {
	if g != c.group {
		return nil
	}

	c.mu.Lock()
	conn := c.connection
	c.mu.Unlock()

	if conn == nil {
		return nil
	}
	target.PushConn(g, conn.Id(), conn, conn.GetTracks(), "")
	return nil
}

func (c *Client) Joined(group, kind string) error {
	return nil
}

func (c *Client) PushClient(group, kind, id, username string, permissions group.ClientPermissions, status map[string]interface{}) error {
	return nil
}

func (c *Client) Kick(id, user, message string) error {
	return c.close()
}

func (c *Client) conn() *rtpconn.UpConn {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connection
}

func (c *Client) close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	g := c.group
	if g == nil {
		return nil
	}
	if c.connection != nil {
		id := c.connection.Id()
		c.connection.PC.OnConnectionStateChange(nil)
		c.connection.PC.Close()
		c.connection = nil
		for _, c := range g.GetClients(c) {
			c.PushConn(g, id, nil, nil, "")
		}
		c.connection = nil
	}
	group.DelClient(c)
	c.group = nil
	return nil
}

func newId() string {
	b := make([]byte, 16)
	crand.Read(b)
	return hex.EncodeToString(b)
}

func httpError(w http.ResponseWriter, err error) {
	if os.IsNotExist(err) {
		http.Error(w, "404 not found", http.StatusNotFound)
		return
	}
	if os.IsPermission(err) {
		http.Error(w, "403 forbidden", http.StatusForbidden)
		return
	}
	log.Printf("WHIP: %v", err)
	http.Error(w, "500 Internal Server Error",
		http.StatusInternalServerError)
	return
}

const sdpLimit = 1024 * 1024

func readLimited(r io.Reader) ([]byte, error) {
	v, err := ioutil.ReadAll(io.LimitReader(r, sdpLimit))
	if len(v) == sdpLimit {
		err = errors.New("SDP too large")
	}
	return v, err
}

func Endpoint(g *group.Group, w http.ResponseWriter, r *http.Request) {
	if PublicServer {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	}

	if r.Method == "OPTIONS" {
		w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, POST")
		w.Header().Set("Access-Control-Allow-Headers",
			"Authorization, Content-Type",
		)
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

	body, err := readLimited(r.Body)
	if err != nil {
		httpError(w, err)
		return
	}

	id := newId()
	c := &Client{
		group: g,
		id:    id,
	}

	username, password, _ := r.BasicAuth()
	c.username = username
	creds := group.ClientCredentials{
		Username: username,
		Password: password,
	}

	_, err = group.AddClient(g.Name(), c, creds)
	if err == group.ErrNotAuthorised ||
		err == group.ErrAnonymousNotAuthorised {
		w.Header().Set("www-authenticate",
			fmt.Sprintf("basic realm=\"%v\"",
				path.Join("/whip/", g.Name())))
		http.Error(w, "Authentication failed", http.StatusUnauthorized)
		return
	} else if err != nil {
		log.Printf("WHIP: %v", err)
		http.Error(w,
			"Internal Server Error", http.StatusInternalServerError,
		)
		return
	}

	if !c.Permissions().Present {
		http.Error(w, "Not authorised", http.StatusUnauthorized)
		return
	}

	conn, err := rtpconn.NewUpConn(c, id, "", string(body))
	if err != nil {
		group.DelClient(c)
		httpError(w, err)
		return
	}

	conn.PC.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		switch state {
		case webrtc.ICEConnectionStateFailed,
			webrtc.ICEConnectionStateClosed:
			c.close()
		}
	})

	c.mu.Lock()
	c.connection = conn
	c.mu.Unlock()

	sdp, err := gotOffer(conn, body)
	if err != nil {
		group.DelClient(c)
		httpError(w, err)
		return
	}

	w.Header().Set("Location", path.Join("/whip/", path.Join(g.Name(), id)))
	w.Header().Set("Access-Control-Expose-Headers", "Location, Content-Type")
	w.Header().Set("Content-Type", "application/sdp")
	w.WriteHeader(http.StatusCreated)
	w.Write(sdp)
	return
}

func Handler(w http.ResponseWriter, r *http.Request) {
	p := path.Dir(r.URL.Path)
	id := path.Base(r.URL.Path)

	if p[:6] != "/whip/" {
		httpError(w, errors.New("bad URL"))
		return
	}
	name := p[6:]

	g := group.Get(name)
	if g == nil {
		http.Error(w, "404 not found", http.StatusNotFound)
		return
	}

	cc := g.GetClient(id)
	if cc == nil {
		http.Error(w, "404 not found", http.StatusNotFound)
		return
	}

	c, ok := cc.(*Client)
	if !ok {
		httpError(w, errors.New("unexpected type for client"))
		return
	}

	if PublicServer {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	}

	if r.Method == "OPTIONS" {
		w.Header().Set("Access-Control-Allow-Methods",
			"GET, HEAD, PATCH, DELETE",
		)
		w.Header().Set("Access-Control-Allow-Headers",
			"Authorization, Content-Type",
		)
		return
	}

	username, password, _ := r.BasicAuth()
	if username != c.username {
		http.Error(w, "Client changed username", http.StatusUnauthorized)
		return
	}
	creds := group.ClientCredentials{
		Username: username,
		Password: password,
	}
	perms, err := g.Description().GetPermission(name, creds)
	if err == group.ErrNotAuthorised ||
		err == group.ErrAnonymousNotAuthorised {
		w.Header().Set("www-authenticate",
			fmt.Sprintf("basic realm=\"%v\"",
				path.Join("/whip/", g.Name())))
		http.Error(w, "Authentication failed", http.StatusUnauthorized)
		return
	} else if err != nil {
		log.Printf("WHIP: %v", err)
		http.Error(w,
			"Internal Server Error", http.StatusInternalServerError,
		)
		return
	}
	if !perms.Present {
		http.Error(w, "Not authorised", http.StatusUnauthorized)
		return
	}

	if r.Method == "DELETE" {
		c.close()
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

	conn := c.conn()
	if conn == nil {
		http.Error(w, "connection closed", http.StatusNotFound)
		return
	}

	body, err := readLimited(r.Body)
	if err != nil {
		httpError(w, err)
		return
	}

	if len(body) < 2 {
		http.Error(w, "SDP truncated", http.StatusBadRequest)
		return
	}

	if string(body[:2]) == "v=" {
		answer, err := gotOffer(conn, body)
		if err != nil {
			httpError(w, err)
			return
		}

		w.Header().Set("Content-Type", "application/sdp")
		w.Write(answer)
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
			err := gotCandidate(conn, l[2:], ufrag)
			if err != nil {
				log.Printf("WHIP candidate: %v", err)
			}
		} else if bytes.Equal(l, []byte("a=end-of-candidates")) {
			gotCandidate(conn, nil, ufrag)
		}
	}
	w.WriteHeader(http.StatusNoContent)
	return
}

func gotOffer(conn *rtpconn.UpConn, offer []byte) ([]byte, error) {
	err := conn.PC.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  string(offer),
	})
	if err != nil {
		return nil, err
	}

	answer, err := conn.PC.CreateAnswer(nil)
	if err != nil {
		return nil, err
	}

	gatherComplete := webrtc.GatheringCompletePromise(conn.PC)

	err = conn.PC.SetLocalDescription(answer)
	if err != nil {
		return nil, err
	}

	<-gatherComplete

	return []byte(conn.PC.CurrentLocalDescription().SDP), nil
}

func gotCandidate(conn *rtpconn.UpConn, candidate, ufrag []byte) error {
	zero := uint16(0)
	init := webrtc.ICECandidateInit{
		Candidate:     string(candidate),
		SDPMLineIndex: &zero,
	}
	if ufrag != nil {
		u := string(ufrag)
		init.UsernameFragment = &u
	}

	err := conn.PC.AddICECandidate(init)
	return err
}
