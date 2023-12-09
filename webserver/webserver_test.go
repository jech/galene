package webserver

import (
	"testing"

	"github.com/pion/webrtc/v3"
)

func TestParseGroupName(t *testing.T) {
	a := []struct{ p, g string }{
		{"", ""},
		{"/foo", ""},
		{"foo", ""},
		{"group/foo", ""},
		{"/group", ""},
		{"/group/..", ""},
		{"/group/foo/../bar", "bar"},
		{"/group/foo", "foo"},
		{"/group/foo/", "foo"},
		{"/group/foo/bar", "foo/bar"},
		{"/group/foo/bar/", "foo/bar"},
	}

	for _, pg := range a {
		t.Run(pg.p, func(t *testing.T) {
			g := parseGroupName("/group/", pg.p)
			if g != pg.g {
				t.Errorf("Path %v, got %v, expected %v",
					pg.p, g, pg.g)
			}
		})
	}
}

func TestParseWhip(t *testing.T) {
	a := []struct{ p, d, b string }{
		{"", "", ""},
		{"/", "", ""},
		{"/foo", "", ""},
		{"/foo/", "", ""},
		{"/foo/bar", "", ""},
		{"/foo/bar/", "", ""},
		{"/foo/bar/baz", "", ""},
		{"/foo/bar/baz/", "", ""},
		{"/foo/.whip", "/foo/", ""},
		{"/foo/.whip/", "/foo/", ""},
		{"/foo/.whip/bar", "/foo/", "bar"},
		{"/foo/.whip/bar/", "/foo/", "bar"},
		{"/foo/.whip/bar/baz", "", ""},
		{"/foo/.whip/bar/baz/", "", ""},
	}

	for _, pdb := range a {
		t.Run(pdb.p, func(t *testing.T) {
			d, b := parseWhip(pdb.p)
			if d != pdb.d || b != pdb.b {
				t.Errorf("Path %v, got %v %v, expected %v %v",
					pdb.p, d, b, pdb.d, pdb.b)
			}
		})
	}
}

func TestParseBearerToken(t *testing.T) {
	a := []struct{ a, b string }{
		{"", ""},
		{"foo", ""},
		{"foo bar", ""},
		{" foo bar", ""},
		{"foo bar ", ""},
		{"Bearer", ""},
		{"Bearer ", ""},
		{"Bearer foo", "foo"},
		{"bearer foo", "foo"},
		{" Bearer foo", "foo"},
		{"Bearer foo ", "foo"},
		{" Bearer foo ", "foo"},
		{"Bearer foo bar", ""},
	}

	for _, ab := range a {
		t.Run(ab.a, func(t *testing.T) {
			b := parseBearerToken(ab.a)
			if b != ab.b {
				t.Errorf("Bearer token %v, got %v, expected %v",
					ab.a, b, ab.b,
				)
			}
		})
	}
}

func TestFormatICEServer(t *testing.T) {
	a := []struct {
		s webrtc.ICEServer
		v string
	}{
		{
			webrtc.ICEServer{
				URLs: []string{"stun:stun.example.org:3478"},
			}, "<stun:stun.example.org:3478>; rel=\"ice-server\"",
		},
		{
			webrtc.ICEServer{
				URLs:           []string{"turn:turn.example.org:3478"},
				Username:       "toto",
				Credential:     "titi",
				CredentialType: webrtc.ICECredentialTypePassword,
			}, "<turn:turn.example.org:3478>; rel=\"ice-server\"; " +
				"username=\"toto\"; credential=\"titi\"; " +
				"credential-type=\"password\"",
		},
		{
			webrtc.ICEServer{
				URLs:           []string{"turns:turn.example.org:5349"},
				Username:       "toto",
				Credential:     "titi",
				CredentialType: webrtc.ICECredentialTypePassword,
			}, "<turns:turn.example.org:5349>; rel=\"ice-server\"; " +
				"username=\"toto\"; credential=\"titi\"; " +
				"credential-type=\"password\"",
		},
		{
			webrtc.ICEServer{
				URLs: []string{"https://stun.example.org"},
			}, "",
		},
	}

	for _, sv := range a {
		t.Run(sv.s.URLs[0], func(t *testing.T) {
			v := formatICEServer(sv.s, sv.s.URLs[0])
			if v != sv.v {
				t.Errorf("Got %v, expected %v", v, sv.v)
			}
		})
	}
}
