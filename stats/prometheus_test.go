package stats

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jech/galene/group"
)

func TestSummary(t *testing.T) {
	var s summary
	for _, v := range []float64{2, 4, 4, 4, 5, 5, 7, 9} {
		s.add(v)
	}
	if s.n != 8 || s.min != 2 || s.max != 9 || s.avg() != 5 {
		t.Errorf("Got %v %v %v %v", s.n, s.min, s.max, s.avg())
	}
	if math.Abs(s.stddev()-2) > 1e-9 {
		t.Errorf("Stddev: expected 2, got %v", s.stddev())
	}
}

type fakeClient struct {
	group.Client
	id    string
	group *group.Group
	stats *Client
}

func (c *fakeClient) Group() *group.Group { return c.group }

func (c *fakeClient) Id() string                      { return c.id }
func (c *fakeClient) Username() string                { return "user-" + c.id }
func (c *fakeClient) Permissions() []string           { return []string{"system"} }
func (c *fakeClient) Data() map[string]interface{}    { return nil }
func (c *fakeClient) Joined(group, kind string) error { return nil }
func (c *fakeClient) PushClient(group, kind, id, username string, perms []string, data map[string]interface{}) error {
	return nil
}
func (c *fakeClient) GetStats() *Client { return c.stats }

func addFakeClient(t *testing.T, g, id string, up, down []Track) {
	c := &fakeClient{
		id: id,
		stats: &Client{
			Id:   id,
			Up:   []Conn{{Id: "conn-up-" + id, Tracks: up}},
			Down: []Conn{{Id: "conn-down-" + id, Tracks: down}},
		},
	}
	gg, err := group.AddClient(g, c, group.ClientCredentials{})
	if err != nil {
		t.Fatalf("AddClient(%v): %v", g, err)
	}
	c.group = gg
}

func TestPrometheus(t *testing.T) {
	ms := 10 * time.Millisecond
	group.Directory = t.TempDir()
	groups := map[string]string{
		"public-group": `{"public": true}`,
		"empty-public": `{"public": true}`,
		"secret-one":   `{}`,
		"secret-two":   `{}`,
		"secret-empty": `{}`,
	}
	for name, desc := range groups {
		err := os.WriteFile(
			filepath.Join(group.Directory, name+".json"),
			[]byte(desc), 0600,
		)
		if err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		_, err = group.Add(name, nil)
		if err != nil {
			t.Fatalf("Add(%v): %v", name, err)
		}
	}
	defer func() {
		for name := range groups {
			g := group.Get(name)
			if g == nil {
				continue
			}
			for _, c := range g.GetClients(nil) {
				group.DelClient(c)
			}
			group.Delete(name)
		}
	}()

	addFakeClient(t, "public-group", "alice",
		[]Track{{Bitrate: 1000, Loss: 0.1, Jitter: Duration(ms)}},
		[]Track{{Bitrate: 3000, Rtt: Duration(2 * ms)}})
	addFakeClient(t, "public-group", "bob", nil, nil)
	addFakeClient(t, "secret-one", "carol",
		[]Track{{Bitrate: 2000}, {Bitrate: 4000}}, nil)
	addFakeClient(t, "secret-two", "dave",
		[]Track{{Bitrate: 6000}}, nil)

	var buf bytes.Buffer
	err := WritePrometheus(&buf)
	if err != nil {
		t.Fatalf("WritePrometheus: %v", err)
	}
	out := buf.String()

	for _, s := range []string{"secret", "alice", "bob", "carol", "dave",
		"user-", "conn-"} {
		if strings.Contains(out, s) {
			t.Errorf("Output contains %q:\n%v", s, out)
		}
	}

	line := regexp.MustCompile(
		`^(# (HELP|TYPE) .*|[a-z_]+\{[^}]*\} [-+0-9.e]+)$`,
	)
	for _, l := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		if !line.MatchString(l) {
			t.Errorf("Bad line %q", l)
		}
	}

	pub := `visibility="public",group="public-group"`
	priv := `visibility="private"`
	for _, s := range []string{
		`galene_groups_active{` + pub + `} 1`,
		`galene_groups_active{visibility="public",group="empty-public"} 0`,
		`galene_groups_active{` + priv + `} 2`,
		`galene_clients{` + pub + `} 2`,
		`galene_clients{` + priv + `} 2`,
		`galene_connections{` + pub + `,direction="up"} 2`,
		`galene_tracks{` + pub + `,direction="up"} 1`,
		`galene_tracks{` + priv + `,direction="up"} 3`,
		`galene_tracks{` + priv + `,direction="down"} 0`,
		`galene_bitrate_bits_per_second{` + priv + `,direction="up"} 12000`,
		`galene_track_bitrate_bits_per_second_min{` + priv + `,direction="up"} 2000`,
		`galene_track_bitrate_bits_per_second_avg{` + priv + `,direction="up"} 4000`,
		`galene_track_bitrate_bits_per_second_max{` + priv + `,direction="up"} 6000`,
		`galene_track_loss_ratio_max{` + pub + `,direction="up"} 0.1`,
		`galene_track_jitter_seconds_avg{` + pub + `,direction="up"} 0.01`,
		`galene_track_rtt_seconds_avg{` + pub + `,direction="down"} 0.02`,
	} {
		if !strings.Contains(out, s+"\n") {
			t.Errorf("Output doesn't contain %q:\n%v", s, out)
		}
	}

	// no summaries for label sets without tracks
	if strings.Contains(out, `_min{`+priv+`,direction="down"}`) {
		t.Errorf("Unexpected summary for empty label set")
	}
	if strings.Contains(out, `galene_track_rtt_seconds_avg{`+pub+`,direction="up"}`) {
		t.Errorf("Unexpected RTT for up tracks")
	}
}
