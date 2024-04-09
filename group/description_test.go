package group

import (
	"encoding/json"
	"errors"
	"os"
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

func TestNonWritableGroups(t *testing.T) {
	Directory = t.TempDir()
	configuration.configuration = &Configuration{}

	_, err := GetDescription("test")
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("GetDescription: got %#v, expected ErrNotExist", err)
	}

	err = UpdateDescription("test", "", &Description{})
	if !errors.Is(err, ErrDescriptionsNotWritable) {
		t.Errorf("UpdateDescription: got %#v, " +
			"expected ErrDescriptionsNotWritable", err)
	}
}

func TestWritableGroups(t *testing.T) {
	Directory = t.TempDir()
	configuration.configuration = &Configuration{
		WritableGroups: true,
	}

	_, err := GetDescription("test")
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("GetDescription: got %v, expected ErrNotExist", err)
	}

	err = UpdateDescription("test", "\"etag\"", &Description{})
	if !errors.Is(err, ErrTagMismatch) {
		t.Errorf("UpdateDescription: got %v, expected ErrTagMismatch",
			err)
	}

	err = UpdateDescription("test", "", &Description{})
	if err != nil {
		t.Errorf("UpdateDescription: got %v", err)
	}

	_, err = GetDescription("test")
	if err != nil {
		t.Errorf("GetDescription: got %v", err)
	}

	desc, token, err := GetSanitisedDescription("test")
	if err != nil || token == "" {
		t.Errorf("GetSanitisedDescription: got %v", err)
	}

	desc.DisplayName = "Test"

	err = UpdateDescription("test", "\"badetag\"", desc)
	if !errors.Is(err, ErrTagMismatch) {
		t.Errorf("UpdateDescription: got %v, expected ErrTagMismatch",
			err)
	}

	err = UpdateDescription("test", token, desc)
	if err != nil {
		t.Errorf("UpdateDescription: got %v", err)
	}

	desc, err = GetDescription("test")
	if err != nil || desc.DisplayName != "Test" {
		t.Errorf("GetDescription: expected %v %v, got %v %v",
			nil, "Test", err, desc.AllowAnonymous,
		)
	}

	_, _, err = GetSanitisedUser("test", "jch")
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("GetSanitisedUser: got %v, expected ErrNotExist", err)
	}

	err = UpdateUser("test", "jch", "", &UserDescription{
		Permissions: Permissions{name: "observe"},
	})
	if err != nil {
		t.Errorf("UpdateUser: got %v", err)
	}

	user, token, err := GetSanitisedUser("test", "jch")
	if err != nil || token == "" || user.Permissions.name != "observe" {
		t.Errorf("GetDescription: got %v %v, expected %v %v",
			err, user.Permissions.name, nil, "observe",
		)
	}

	err = UpdateUser("test", "jch", "", &UserDescription{
		Permissions: Permissions{name: "present"},
	})
	if !errors.Is(err, ErrTagMismatch) {
		t.Errorf("UpdateDescription: got %v, expected ErrTagMismatch",
			err)
	}

	err = UpdateUser("test", "jch", token, &UserDescription{
		Permissions: Permissions{name: "present"},
	})
	if err != nil {
		t.Errorf("UpdateUser: got %v", err)
	}

	err = SetUserPassword("test", "jch", Password{
		Type: "",
		Key: "pw",
	})
	if err != nil {
		t.Errorf("SetUserPassword: got %v", err)
	}

	desc, err = GetDescription("test")
	if err != nil || desc.Users["jch"].Password.Key != "pw" {
		t.Errorf("GetDescription: got %v %v, expected %v %v",
			err, desc.Users["jch"].Password.Key, nil, "pw",
		)
	}
}
