package group

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestGetGlobalUsernames(t *testing.T) {
	path := setupGlobalUsersTest(t)

	names, etag, err := GetGlobalUsernames()
	if err != nil {
		t.Fatalf("GetGlobalUsernames: got %q", err)
	}
	if len(names) != 0 {
		t.Errorf("GetGlobalUsernames: expected empty list, got %v", names)
	}
	if etag != "" {
		t.Errorf("GetGlobalUsernames: expected empty etag, got %q", etag)
	}

	os.WriteFile(path, []byte("{\"foo\":{},\"bar\":{},\"baz\":{}}"), 0600)
	expected := []string{"bar", "baz", "foo"}

	names, etag, err = GetGlobalUsernames()
	if err != nil {
		t.Fatalf("GetGlobalUsernames: got %q", err)
	}
	if !slices.Equal(expected, names) {
		t.Errorf("GetGlobalUsernames: expected %v, got %v", expected, names)
	}
	if etag == "" {
		t.Errorf("GetGlobalUsernames: expected non-empty etag")
	}
}

func TestGetGlobalUser(t *testing.T) {
	path := setupGlobalUsersTest(t)

	_, etag, err := GetGlobalUser("test")
	if !errors.Is(err, ErrNoSuchUsername) {
		t.Errorf("GetGlobalUser: expected ErrNoSuchUsername, got %q", err)
	}
	if etag != "" {
		t.Errorf("GetGlobalUser: expected empty etag, got %q", etag)
	}

	os.WriteFile(path, []byte("{\"test\":{}}"), 0600)

	_, etag, err = GetGlobalUser("test")
	if err != nil {
		t.Fatalf("GetGlobalUser: got %q", err)
	}
	if etag == "" {
		t.Errorf("GetGlobalUser: expected non-empty etag, got %q", etag)
	}
	_, _, err = GetGlobalUser("invalid")
	if err == nil {
		t.Errorf("GetGlobalUser: expected error for invalid user")
	}
}

func TestAddGlobalUser(t *testing.T) {
	setupGlobalUsersTest(t)

	err := AddGlobalUser("test")
	if err != nil {
		t.Fatalf("AddGlobalUser: got %q", err)
	}
	_, _, err = GetGlobalUser("test")
	if err != nil {
		t.Errorf("GetGlobalUser: got %q", err)
	}

	err = AddGlobalUser("test")
	if !errors.Is(err, ErrDuplicateUsername) {
		t.Errorf("AddGlobalUser: expected ErrDuplicateUsername, got %q", err)
	}

	err = AddGlobalUser("test2")
	if err != nil {
		t.Fatalf("AddGlobalUser: got %q", err)
	}
	_, _, err = GetGlobalUser("test2")
	if err != nil {
		t.Errorf("GetGlobalUser: got %q", err)
	}
	_, _, err = GetGlobalUser("test")
	if err != nil {
		t.Errorf("GetGlobalUser: got %q", err)
	}
}

func TestDeleteGlobalUser(t *testing.T) {
	setupGlobalUsersTest(t)

	AddGlobalUser("test")
	AddGlobalUser("test2")
	_, etag, err := GetGlobalUser("test")
	if err != nil {
		t.Fatalf("GetGlobalUser: got %q", err)
	}

	err = DeleteGlobalUser("test", "")
	if !errors.Is(err, ErrTagMismatch) {
		t.Errorf("DeleteGlobalUser: expected ErrTagMismatch, got %q", err)
	}
	_, _, err = GetGlobalUser("test")
	if err != nil {
		t.Errorf("GetGlobalUser: got %q", err)
	}

	err = DeleteGlobalUser("test", etag)
	if err != nil {
		t.Fatalf("DeleteGlobalUser: got %q", err)
	}
	_, _, err = GetGlobalUser("test")
	if !errors.Is(err, ErrNoSuchUsername) {
		t.Errorf("GetGlobalUser: expected ErrNoSuchUsername, got %q", err)
	}
	_, _, err = GetGlobalUser("test2")
	if err != nil {
		t.Errorf("GetGlobalUser: got %q", err)
	}
}

func TestSetGlobalUserPassword(t *testing.T) {
	setupGlobalUsersTest(t)

	AddGlobalUser("test")
	key := "secret"
	pw := Password{Type: "plain", Key: &key}

	if err := SetGlobalUserPassword("test", pw); err != nil {
		t.Fatalf("SetGlobalUserPassword: got %q", err)
	}

	desc, _, err := GetGlobalUser("test")
	if err != nil {
		t.Fatalf("GetGlobalUser: got %q", err)
	}

	match, err := desc.Password.Match(key)
	if err != nil {
		t.Errorf("Match: got %q", err)
	}
	if !match {
		t.Errorf("Match: expected password match")
	}

	match, err = desc.Password.Match("wrong")
	if err != nil {
		t.Errorf("Match: got %q", err)
	}
	if match {
		t.Errorf("Match: unexpected password match")
	}
}

func setupGlobalUsersTest(t *testing.T) string {
	dir := t.TempDir()
	path := filepath.Join(dir, "users.json")
	SetGlobalUsersPath(path)
	return path
}
