package rtpconn

import (
	"context"
	"errors"
	"sync"

	"github.com/jech/galene/conn"
	"github.com/jech/galene/group"
	"github.com/pion/webrtc/v3"
)

type WhipClient struct {
	group    *group.Group
	id       string
	token    string
	username string

	mu          sync.Mutex
	permissions []string
	connection  *rtpUpConnection
}

func NewWhipClient(g *group.Group, id string, token string) *WhipClient {
	return &WhipClient{group: g, id: id, token: token}
}

func (c *WhipClient) Group() *group.Group {
	return c.group
}

func (c *WhipClient) Id() string {
	return c.id
}

func (c *WhipClient) Token() string {
	return c.token
}

func (c *WhipClient) Username() string {
	return c.username
}

func (c *WhipClient) SetUsername(username string) {
	c.username = username
}

func (c *WhipClient) Permissions() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.permissions
}

func (c *WhipClient) SetPermissions(perms []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.permissions = perms
}

func (c *WhipClient) Data() map[string]interface{} {
	return nil
}

func (c *WhipClient) PushConn(g *group.Group, id string, conn conn.Up, tracks []conn.UpTrack, replace string) error {
	return nil
}

func (c *WhipClient) RequestConns(target group.Client, g *group.Group, id string) error {
	if g != c.group {
		return nil
	}

	c.mu.Lock()
	up := c.connection
	c.mu.Unlock()
	if up == nil {
		return nil
	}
	tracks := up.getTracks()
	ts := make([]conn.UpTrack, len(tracks))
	for i, t := range tracks {
		ts[i] = t
	}
	target.PushConn(g, up.Id(), up, ts, "")
	return nil
}

func (c *WhipClient) Joined(group, kind string) error {
	return nil
}

func (c *WhipClient) PushClient(group, kind, id, username string, permissions []string, status map[string]interface{}) error {
	return nil
}

func (c *WhipClient) Kick(id string, user *string, message string) error {
	return c.Close()
}

func (c *WhipClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	g := c.group
	if g == nil {
		return nil
	}
	if c.connection != nil {
		id := c.connection.Id()
		c.connection.pc.OnICEConnectionStateChange(nil)
		c.connection.pc.Close()
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

func (c *WhipClient) NewConnection(ctx context.Context, offer []byte) ([]byte, error) {
	conn, err := newUpConn(c, c.id, "", string(offer))
	if err != nil {
		return nil, err
	}

	conn.pc.OnICEConnectionStateChange(
		func(state webrtc.ICEConnectionState) {
			switch state {
			case webrtc.ICEConnectionStateFailed,
				webrtc.ICEConnectionStateClosed:
				c.Close()
			}
		})

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.connection != nil {
		conn.pc.OnICEConnectionStateChange(nil)
		conn.pc.Close()
		return nil, errors.New("duplicate connection")
	}
	c.connection = conn

	answer, err := c.gotOffer(ctx, offer)
	if err != nil {
		conn.pc.OnICEConnectionStateChange(nil)
		conn.pc.Close()
		return nil, err
	}

	return answer, nil
}

func (c *WhipClient) GotOffer(ctx context.Context, offer []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gotOffer(ctx, offer)
}

// called locked
func (c *WhipClient) gotOffer(ctx context.Context, offer []byte) ([]byte, error) {
	conn := c.connection
	err := conn.pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  string(offer),
	})
	if err != nil {
		return nil, err
	}

	answer, err := conn.pc.CreateAnswer(nil)
	if err != nil {
		return nil, err
	}

	gatherComplete := webrtc.GatheringCompletePromise(conn.pc)

	err = conn.pc.SetLocalDescription(answer)
	if err != nil {
		return nil, err
	}

	conn.flushICECandidates()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-gatherComplete:
	}

	return []byte(conn.pc.CurrentLocalDescription().SDP), nil
}

func (c *WhipClient) GotICECandidate(init webrtc.ICECandidateInit) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.connection == nil {
		return nil
	}
	return c.connection.addICECandidate(&init)
}
