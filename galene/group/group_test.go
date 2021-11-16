package group

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"
)

func TestGroup(t *testing.T) {
	groups.groups = nil
	Add("group", &Description{})
	Add("group/subgroup", &Description{Public: true})
	if len(groups.groups) != 2 {
		t.Errorf("Expected 2, got %v", len(groups.groups))
	}
	g := Get("group")
	g2 := Get("group/subgroup")
	if g == nil {
		t.Fatalf("Couldn't get group")
	}
	if g2 == nil {
		t.Fatalf("Couldn't get group/subgroup")
	}
	if name := g.Name(); name != "group" {
		t.Errorf("Name: expected group1, got %v", name)
	}
	if locked, _ := g.Locked(); locked {
		t.Errorf("Locked: expected false, got %v", locked)
	}
	api, err := g.API()
	if err != nil || api == nil {
		t.Errorf("Couldn't get API: %v", err)
	}

	if names := GetNames(); len(names) != 2 {
		t.Errorf("Expected 2, got %v", names)
	}

	if subs := GetSubGroups("group"); len(subs) != 0 {
		t.Errorf("Expected [], got %v", subs)
	}

	if public := GetPublic(); len(public) != 1 || public[0].Name != "group/subgroup" {
		t.Errorf("Expected group/subgroup, got %v", public)
	}
}

func TestJSTime(t *testing.T) {
	tm := time.Now()
	js := ToJSTime(tm)
	tm2 := FromJSTime(js)
	js2 := ToJSTime(tm2)

	if js != js2 {
		t.Errorf("%v != %v", js, js2)
	}

	delta := tm.Sub(tm2)
	if delta < -time.Millisecond/2 || delta > time.Millisecond/2 {
		t.Errorf("Delta %v, %v, %v", delta, tm, tm2)
	}
}

func TestChatHistory(t *testing.T) {
	g := Group{
		description: &Description{},
	}
	for i := 0; i < 2*maxChatHistory; i++ {
		g.AddToChatHistory("id", "user", ToJSTime(time.Now()), "",
			fmt.Sprintf("%v", i),
		)
	}
	h := g.GetChatHistory()
	if len(h) != maxChatHistory {
		t.Errorf("Expected %v, got %v", maxChatHistory, len(g.history))
	}
	for i, s := range h {
		e := fmt.Sprintf("%v", i+maxChatHistory)
		if s.Value.(string) != e {
			t.Errorf("Expected %v, got %v", e, s)
		}
	}
}

var descJSON = `
{
    "op": [{"username": "jch","password": "topsecret"}],
    "max-history-age": 10,
    "allow-subgroups": true,
    "presenter": [
        {"username": "john", "password": "secret"},
        {"username": "john", "password": "secret2"}
    ],
    "other": [
        {"username": "james", "password": "secret3"},
        {"username": "peter", "password": "secret4"},
        {}
    ]

}`

func TestDescriptionJSON(t *testing.T) {
	var d Description
	err := json.Unmarshal([]byte(descJSON), &d)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	dd, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var ddd Description
	err = json.Unmarshal([]byte(dd), &ddd)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !reflect.DeepEqual(d, ddd) {
		t.Errorf("Got %v, expected %v", ddd, d)
	}
}

var badClients = []ClientCredentials{
	{Username: "jch", Password: "foo"},
	{Username: "john", Password: "foo"},
	{Username: "james", Password: "foo"},
}

type credPerm struct {
	c ClientCredentials
	p ClientPermissions
}

var goodClients = []credPerm{
	{
		ClientCredentials{Username: "jch", Password: "topsecret"},
		ClientPermissions{Op: true, Present: true},
	},
	{
		ClientCredentials{Username: "john", Password: "secret"},
		ClientPermissions{Present: true},
	},
	{
		ClientCredentials{Username: "john", Password: "secret2"},
		ClientPermissions{Present: true},
	},
	{
		ClientCredentials{Username: "james", Password: "secret3"},
		ClientPermissions{},
	},
	{
		ClientCredentials{Username: "paul", Password: "secret3"},
		ClientPermissions{},
	},
}

func TestPermissions(t *testing.T) {
	var d Description
	err := json.Unmarshal([]byte(descJSON), &d)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for _, c := range badClients {
		t.Run("bad "+c.Username, func(t *testing.T) {
			p, err := d.GetPermission("test", c)
			if err != ErrNotAuthorised {
				t.Errorf("GetPermission %v: %v %v", c, err, p)
			}
		})
	}

	for _, cp := range goodClients {
		t.Run("good "+cp.c.Username, func(t *testing.T) {
			p, err := d.GetPermission("test", cp.c)
			if err != nil {
				t.Errorf("GetPermission %v: %v", cp.c, err)
			} else if !reflect.DeepEqual(p, cp.p) {
				t.Errorf("%v: got %v, expected %v",
					cp.c, p, cp.p)
			}
		})
	}

}

func TestFmtpValue(t *testing.T) {
	type fmtpTest struct {
		fmtp  string
		key   string
		value string
	}
	fmtpTests := []fmtpTest{
		{"", "foo", ""},
		{"profile-id=0", "profile-id", "0"},
		{"profile-id=0", "foo", ""},
		{"level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42001f", "profile-level-id", "42001f"},
		{"foo=1;bar=2;quux=3", "foo", "1"},
		{"foo=1;bar=2;quux=3", "bar", "2"},
		{"foo=1;bar=2;quux=3", "fu", ""},
	}

	for _, test := range fmtpTests {
		v := fmtpValue(test.fmtp, test.key)
		if v != test.value {
			t.Errorf("fmtpValue(%v, %v) = %v, expected %v",
				test.fmtp, test.key, v, test.value,
			)
		}
	}
}

func TestValidGroupName(t *testing.T) {
	type nameTest struct {
		name   string
		result bool
	}
	tests := []nameTest{
		{"", false},
		{"/", false},
		{"/foo", false},
		{"foo/", false},
		{"./foo", false},
		{"foo/.", false},
		{"../foo", false},
		{"foo/..", false},
		{"foo/./bar", false},
		{"foo/../bar", false},
		{"foo", true},
		{"foo/bar", true},
	}

	for _, test := range tests {
		r := validGroupName(test.name)
		if r != test.result {
			t.Errorf("Valid %v: got %v, expected %v",
				test.name, r, test.result)
		}
	}
}
