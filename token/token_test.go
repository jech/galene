package token

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestToken(t *testing.T) {
	d := t.TempDir()
	tokens = state{
		filename: filepath.Join(d, "test.jsonl"),
	}

	f, err := os.OpenFile(tokens.filename,
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600,
	)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	f.Close()

	future := time.Now().Add(time.Hour)
	user := "user"
	_, err = Add(&Stateful{
		Token:       "token",
		Group:       "group",
		Username:    &user,
		Permissions: []string{"present"},
		Expires:     &future,
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	token, err := Parse("token", nil)
	if err != nil || token == nil {
		t.Fatalf("Parse: %v", err)
	}

	_, _, err = token.Check("galene.org:8443", "group", &user)
	if err != nil {
		t.Errorf("Check: %v", err)
	}

	token, err = Parse("bad", nil)
	if err == nil {
		t.Errorf("Expected error, got %v", token)
	}

	token, err = Parse("bad", []map[string]interface{}{})
	if err == nil {
		t.Errorf("Expected error, got %v", token)
	}
}
