package token

import (
	"testing"
	"path/filepath"
	"os"
)

func TestBad(t *testing.T) {
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
	defer f.Close()

	token, err := Parse("foo", nil)
	if err == nil {
		t.Errorf("Expected error, got %v", token)
	}

	token, err = Parse("foo", []map[string]interface{}{})
	if err == nil {
		t.Errorf("Expected error, got %v", token)
	}
}
