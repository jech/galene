package group

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMarshalUserDescription(t *testing.T) {
	tests := []string{
		`{}`,
		`{"permissions":"present"}`,
		`{"password":"secret"}`,
		`{"password":"secret","permissions":"present"}`,
		`{"password":"secret","permissions":["present"]}`,
		`{"password":{"type":"wildcard"},"permissions":"observe"}`,
		`{"password":{"type":"wildcard"},"permissions":[]}`,
	}

	for _, test := range tests {
		var u UserDescription
		err := json.Unmarshal([]byte(test), &u)
		if err != nil {
			t.Errorf("Unmarshal %v: %v", t, err)
			continue
		}
		v, err := json.Marshal(u)
		if err != nil || string(v) != test {
			t.Errorf("Marshal %v: got %v %v", test, string(v), err)
		}
	}
}

func TestEmptyJSON(t *testing.T) {
	type emptyTest struct {
		value  any
		result string
		name   string
	}

	emptyTests := []emptyTest{
		{Password{}, "{}", "password"},
		{Permissions{}, "null", "permissions"},
		{UserDescription{}, "{}", "user description"},
	}

	for _, v := range emptyTests {
		b, err := json.Marshal(v.value)
		if err != nil || string(b) != v.result {
			t.Errorf("Marshal empty %v: %#v %v, expected %#v",
				v.name, string(b), err, v.result)
		}
	}
}

var descJSON = `
{
    "max-history-age": 10,
    "auto-subgroups": true,
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

	if d1.AutoSubgroups != d2.AutoSubgroups ||
		d1.AllowSubgroups != d2.AllowSubgroups {
		t.Errorf("AllowSubgroups not upgraded correctly")
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

	if len(d1.FallbackUsers) != len(d2.FallbackUsers) {
		t.Errorf("length not equal: %v != %v",
			len(d1.FallbackUsers), len(d2.FallbackUsers))
	}

	for k, v1 := range d1.FallbackUsers {
		v2 := d2.FallbackUsers[k]
		if !reflect.DeepEqual(v1.Password, v2.Password) ||
			!permissionsEqual(
				v1.Permissions.Permissions(&d1),
				v2.Permissions.Permissions(&d2),
			) {
			t.Errorf("%v not equal: %v != %v", k, v1, v2)
		}
	}
}

func setupTest(dir, datadir string, writable bool) error {
	Directory = dir
	DataDirectory = datadir
	f, err := os.Create(filepath.Join(datadir, "config.json"))
	if err != nil {
		return err
	}
	defer f.Close()
	conf := `{}`
	if writable {
		conf = `{"writableGroups": true}`
	}
	_, err = f.WriteString(conf)
	return err
}

func TestNonWritableGroups(t *testing.T) {
	err := setupTest(t.TempDir(), t.TempDir(), false)
	if err != nil {
		t.Fatalf("setupTest: %v", err)
	}

	_, err = GetDescription("test")
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("GetDescription: got %#v, expected ErrNotExist", err)
	}

	err = UpdateDescription("test", "", &Description{})
	if !errors.Is(err, ErrDescriptionsNotWritable) {
		t.Errorf("UpdateDescription: got %#v, "+
			"expected ErrDescriptionsNotWritable", err)
	}
}

func TestWritableGroups(t *testing.T) {
	err := setupTest(t.TempDir(), t.TempDir(), true)
	if err != nil {
		t.Fatalf("setupTest: %v", err)
	}

	_, err = GetDescription("test")
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
		t.Fatalf("UpdateDescription: got %v", err)
	}

	_, err = GetDescription("test")
	if err != nil {
		t.Errorf("GetDescription: got %v", err)
	}

	fi, err := os.Stat(filepath.Join(Directory, "test.json"))
	if err != nil {
		t.Errorf("Stat: %v", err)
	}
	if mode := fi.Mode(); mode != 0o600 {
		t.Errorf("Mode is 0o%03o (expected 0o600)\n", mode)
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

	pw := "pw"
	err = SetUserPassword("test", "jch", Password{
		Type: "plain",
		Key:  &pw,
	})
	if err != nil {
		t.Errorf("SetUserPassword: got %v", err)
	}

	desc, err = GetDescription("test")
	if err != nil || *desc.Users["jch"].Password.Key != "pw" {
		t.Errorf("GetDescription: got %v %v, expected %v %v",
			err, desc.Users["jch"].Password.Key, nil, "pw",
		)
	}
}

func TestSubGroup(t *testing.T) {
	err := setupTest(t.TempDir(), t.TempDir(), true)
	if err != nil {
		t.Fatalf("setupTest: %v", err)
	}

	err = UpdateDescription("dir/test", "", &Description{})
	if err != nil {
		t.Fatalf("UpdateDescription: got %v", err)
	}
}
