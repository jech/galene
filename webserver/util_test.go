package webserver

import (
	"testing"
)

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

