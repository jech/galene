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
		g := parseGroupName("/group/", pg.p)
		if g != pg.g {
			t.Errorf("Path %v, got %v, expected %v",
				pg.p, g, pg.g)
		}
	}
}

func TestParseSplit(t *testing.T) {
	a := []struct{ p, a, b, c string }{
		{"", "", "", ""},
		{"/a", "/a", "", ""},
		{"/a/.b", "/a", ".b", ""},
		{"/a/.b/", "/a", ".b", "/"},
		{"/a/.b/c", "/a", ".b", "/c"},
		{"/a/.b/c/", "/a", ".b", "/c/"},
		{"/a/.b/c/d", "/a", ".b", "/c/d"},
		{"/a/.b/c/d/", "/a", ".b", "/c/d/"},
		{"/a/.b/c/d./", "/a", ".b", "/c/d./"},
	}

	for _, pabc := range(a) {
		a, b, c := splitPath(pabc.p)
		if pabc.a != a || pabc.b != b || pabc.c != c {
			t.Errorf("Path %v, got %v, %v, %v, expected %v, %v, %v",
				pabc.p, a, b, c, pabc.a, pabc.b, pabc.c,
			)
		}
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
		b := parseBearerToken(ab.a)
		if b != ab.b {
			t.Errorf("Bearer token %v, got %v, expected %v",
				ab.a, b, ab.b,
			)
		}
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

func TestObfuscate(t *testing.T) {
	id := newId()
	obfuscated, err := obfuscate(id)
	if err != nil {
		t.Fatalf("obfuscate: %v", err)
	}
	id2, err := deobfuscate(obfuscated)
	if err != nil {
		t.Fatalf("deobfuscate: %v", err)
	}
	if id != id2 {
		t.Errorf("not equal: %v, %v", id, id2)
	}

	_, err = obfuscate("toto")
	if err == nil {
		t.Errorf("obfuscate: no errror")
	}
}
