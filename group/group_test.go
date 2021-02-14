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
	Add("group", &description{})
	Add("group/subgroup", &description{Public: true})
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
	if public := g.Public(); public {
		t.Errorf("Public: expected false, got %v", public)
	}
	if public := g2.Public(); !public {
		t.Errorf("Public: expected true, got %v", public)
	}
	if redirect := g.Redirect(); redirect != "" {
		t.Errorf("Redirect: expected empty, got %v", redirect)
	}
	if ar := g.AllowRecording(); ar {
		t.Errorf("Allow Recording: expected false, got %v", ar)
	}
	api := g.API()
	if api == nil {
		t.Errorf("Couldn't get API")
	}

	if names := GetNames(); len(names) != 2 {
		t.Errorf("Expected 2, got %v", names)
	}

	if subs := GetSubGroups("group"); len(subs) != 0 {
		t.Errorf("Expected [], got %v", subs)
	}

	if public := GetPublic(); len(public) != 1 || public[0].Name != "group/subgroup" {
		t.Errorf("Expeced group/subgroup, got %v", public)
	}

	Expire()

	if names := GetNames(); len(names) != 2 {
		t.Errorf("Expected 2, got %v", names)
	}

	if found := Delete("nosuchgroup"); found || len(GetNames()) != 2 {
		t.Errorf("Expected 2, got %v", GetNames())
	}

	if found := Delete("group/subgroup"); !found {
		t.Errorf("Failed to delete")
	}

	if names := GetNames(); len(names) != 1 || names[0] != "group" {
		t.Errorf("Expected group, got %v", names)
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
		description: &description{},
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
	var d description
	err := json.Unmarshal([]byte(descJSON), &d)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	dd, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var ddd description
	err = json.Unmarshal([]byte(dd), &ddd)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !reflect.DeepEqual(d, ddd) {
		t.Errorf("Got %v, expected %v", ddd, d)
	}
}

type testClient struct {
	username string
	password string
}

func (c testClient) Username() string {
	return c.username
}

func (c testClient) Challenge(g string, creds ClientCredentials) bool {
	if creds.Password == nil {
		return true
	}
	m, err := creds.Password.Match(c.password)
	if err != nil {
		return false
	}
	return m
}

type testClientPerm struct {
	c testClient
	p ClientPermissions
}

var badClients = []testClient{
	testClient{"jch", "foo"},
	testClient{"john", "foo"},
	testClient{"james", "foo"},
}

var goodClients = []testClientPerm{
	{
		testClient{"jch", "topsecret"},
		ClientPermissions{true, true, false},
	},
	{
		testClient{"john", "secret"},
		ClientPermissions{false, true, false},
	},
	{
		testClient{"john", "secret2"},
		ClientPermissions{false, true, false},
	},
	{
		testClient{"james", "secret3"},
		ClientPermissions{false, false, false},
	},
	{
		testClient{"paul", "secret3"},
		ClientPermissions{false, false, false},
	},
}

func TestPermissions(t *testing.T) {
	var d description
	err := json.Unmarshal([]byte(descJSON), &d)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for _, c := range badClients {
		t.Run("bad "+c.Username(), func(t *testing.T) {
			p, err := d.GetPermission("test", c)
			if err != ErrNotAuthorised {
				t.Errorf("GetPermission %v: %v %v", c, err, p)
			}
		})
	}

	for _, cp := range goodClients {
		t.Run("good "+cp.c.Username(), func(t *testing.T) {
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
