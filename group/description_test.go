package group

import (
	"encoding/json"
	"reflect"
	"testing"
)

var descJSON = `
{
    "max-history-age": 10,
    "allow-subgroups": true,
    "users": {
        "jch": {"password": "topsecret", "permissions": "op"},
        "john": {"password": "secret", "permissions": "present"},
        "james": {"password": "secret2", "permissions": "observe"},
        "peter": {"password": "secret4"}
    },
    "fallback-users": [
        {"permissions": "observe", "password": {"type":"wildcard"}}
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

var obsoleteJSON = `
{
   "op": [{"username": "jch","password": "topsecret"}],
   "max-history-age": 10,
   "allow-subgroups": true,
   "presenter": [
       {"username": "john", "password": "secret"}
   ],
   "other": [
       {"username": "james", "password": "secret2"},
       {"username": "peter", "password": "secret4"},
       {}
   ]
}`

func TestUpgradeDescription(t *testing.T) {
	var d1 Description
	err := json.Unmarshal([]byte(descJSON), &d1)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var d2 Description
	err = json.Unmarshal([]byte(obsoleteJSON), &d2)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	err = upgradeDescription(&d2)
	if err != nil {
		t.Fatalf("upgradeDescription: %v", err)
	}

	if d2.Op != nil || d2.Presenter != nil || d2.Other != nil {
		t.Errorf("legacy field is not nil")
	}

	if len(d1.Users) != len(d2.Users) {
		t.Errorf("length not equal: %v != %v",
			len(d1.Users), len(d2.Users))
	}

	for k, v1 := range d1.Users {
		v2 := d2.Users[k]
		if !reflect.DeepEqual(v1.Password, v2.Password) ||
			!permissionsEqual(
				v1.Permissions.Permissions(&d1),
				v2.Permissions.Permissions(&d2),
			) {
			t.Errorf("%v not equal: %v != %v", k, v1, v2)
		}
	}
}
