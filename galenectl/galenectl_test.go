package main

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/jech/galene/group"
)

func TestMakePassword(t *testing.T) {
	doit := func(pw group.Password) {
		ok, _ := pw.Match("secret")
		if !ok {
			t.Errorf("%v didn't match", pw)
		}
		ok, _ = pw.Match("notsecret")
		if ok {
			t.Errorf("%v did match", pw)
		}
	}
	pw, err := makePassword("secret", "pbkdf2", 4096, 32, 8, 0)
	if err != nil {
		t.Errorf("PBKDF2: %v", err)
	}
	doit(pw)

	pw, err = makePassword("secret", "bcrypt", 0, 0, 0, 10)
	if err != nil {
		t.Errorf("bcrypt: %v", err)
	}
	doit(pw)

	pw, err = makePassword("", "wildcard", 0, 0, 0, 0)
	if err != nil {
		t.Errorf("Wildcard: %v", err)
	}
	ok, _ := pw.Match("notsecretatall")
	if !ok {
		t.Errorf("Wildcard didn't match")
	}
}

func TestFormatPermissions(t *testing.T) {
	tests := []struct{ j, v, p string }{
		{`"op"`, "op", "[cmopt]"},
		{`"present"`, "present", "[mp]"},
		{`"observe"`, "observe", "[]"},
		{`"admin"`, "admin", "[a]"},
		{`["message", "present", "token"]`, "[mpt]", "[mpt]"},
		{`[]`, "[]", "[]"},
	}
	for _, test := range tests {
		var p group.Permissions
		err := json.Unmarshal([]byte(test.j), &p)
		if err != nil {
			t.Errorf("Unmarshal %#v: %v", test.j, err)
			continue
		}
		v := formatPermissions(p)
		if v != test.v {
			t.Errorf("Expected %v, got %v", test.v, v)
		}
		pp := formatRawPermissions(p.Permissions(nil))
		if pp != test.p {
			t.Errorf("Expected %v, got %v", test.p, pp)
		}
	}
}

func TestMatch(t *testing.T) {
	tests := []struct {
		ps []string
		v  string
		m  bool
	}{
		{[]string{}, "foo", false},
		{[]string{"foo"}, "foo", true},
		{[]string{"foo"}, "bar", false},
		{[]string{"foo*"}, "foobar", true},
		{[]string{"foo*"}, "foo", true},
		{[]string{"foo*"}, "bar", false},
		{[]string{"foo", "bar"}, "foo", true},
		{[]string{"foo", "bar"}, "bar", true},
		{[]string{"foo", "bar"}, "baz", false},
		{[]string{"foo", "[", "bar"}, "bar", false},
	}
	for _, test := range tests {
		m, err := match(test.ps, test.v)
		if m != test.m {
			t.Errorf("Expected %v, got %v (%v)",
				test.m, m, err,
			)
		}
	}
}

func TestParsePermissions(t *testing.T) {
	tests := []struct {
		i string
		e bool
		o any // nil for error
	}{
		{"", false, nil},
		{"   ", false, nil},

		{`["message", "present", "token"]`, false,
			[]string{"message", "present", "token"},
		},
		{`[]`, false, []string{}},
		{`[foo]`, false, nil},
		{`  ["message"]`, false, []string{"message"}},
		{"op", false, "op"},
		{"present", false, "present"},
		{"unknown", false, "unknown"},
		{"  op  ", false, "op"},
		{"op", true, []string{
			"op", "present", "message", "caption", "token",
		}},
		{"present", true, []string{"present", "message"}},
		{"message", true, []string{"message"}},
		{"observe", true, []string{}},
		{"caption", true, []string{"caption"}},
		{"admin", true, []string{"admin"}},
		{"unknown", true, nil},
	}

	for _, test := range tests {
		o, err := parsePermissions(test.i, test.e)
		if (err != nil) != (test.o == nil) {
			t.Errorf("parsePermissions(%v, %v): expected %v, got %v (%v)",
				test.i, test.e, test.o, o, err,
			)
			continue
		}

		if !reflect.DeepEqual(o, test.o) {
			t.Errorf("parsePermissions(%v, %v): expected %v, got %v",
				test.i, test.e, test.o, o,
			)
		}
	}
}
