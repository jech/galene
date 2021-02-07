package webserver

import (
	"testing"
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
