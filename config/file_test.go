package config

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

type configState struct {
	file    ConfigFile
	content string
	tag     string
}

func TestRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.txt")
	state := configState{file: NewConfigFile(path)}
	handler := makeReadHandler(&state)

	if path != state.file.Path() {
		t.Errorf("Path: expected %q, got %q", path, state.file.Path())
	}
	assertState(t, &state, "", true)

	state.tag = state.file.Tag()
	if err := state.file.Read(handler); err != nil {
		t.Fatalf("Read: got %q", err)
	}
	assertState(t, &state, "", true)

	os.WriteFile(path, []byte("test"), 0600)

	state.tag = state.file.Tag()
	if err := state.file.Read(handler); err != nil {
		t.Fatalf("Read: got %q", err)
	}
	assertState(t, &state, "test", false)

	os.Remove(path)

	state.tag = state.file.Tag()
	if err := state.file.Read(handler); err != nil {
		t.Fatalf("Read: got %q", err)
	}
	assertState(t, &state, "", true)
}

func TestWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.txt")
	state := configState{file: NewConfigFile(path)}
	handler := makeReadHandler(&state)

	err := state.file.Write("", func(w io.Writer) error {
		_, err := w.Write([]byte("test"))
		return err
	})
	if err != nil {
		t.Fatalf("Write: got %q", err)
	}

	state.tag = state.file.Tag()
	if err = state.file.Read(handler); err != nil {
		t.Fatalf("Read: got %q", err)
	}
	assertState(t, &state, "test", false)

	err = state.file.Write("", func(w io.Writer) error { return nil })
	if !errors.Is(err, ErrTagMismatch) {
		t.Errorf("Write: expected ErrTagMismatch, got %q", err)
	}

	err = state.file.Write(state.tag, func(w io.Writer) error {
		_, err := w.Write([]byte("test again"))
		return err
	})
	if err != nil {
		t.Fatalf("Write: got %q", err)
	}

	state.tag = state.file.Tag()
	if err = state.file.Read(handler); err != nil {
		t.Fatalf("Read: got %q", err)
	}
	assertState(t, &state, "test again", false)
}

func TestDelete(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.txt")
	file := NewConfigFile(path)

	os.WriteFile(path, []byte("test"), 0600)
	tag := file.Tag()
	if tag == "" {
		t.Error("Tag: expected non-empty tag")
	}

	err := file.Delete("")
	if !errors.Is(err, ErrTagMismatch) {
		t.Errorf("Delete: expected ErrTagMismatch, got %q", err)
	}

	if err = file.Delete(tag); err != nil {
		t.Fatalf("Delete: got %q", err)
	}
	_, err = os.Stat(path)
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Stat: expected ErrNotExist, got %q", err)
	}
}

func makeReadHandler(state *configState) func(r io.Reader) error {
	return func(r io.Reader) error {
		if r != nil {
			content, err := io.ReadAll(r)
			if err != nil {
				return err
			}
			state.content = string(content)
		} else {
			state.content = ""
		}
		return nil
	}
}

func assertState(t *testing.T, state *configState, content string, emptyTag bool) {
	if content != state.content {
		t.Errorf("Expected content %q, got %q", content, state.content)
	}
	if emptyTag && state.tag != "" {
		t.Errorf("Expected empty tag, got %q", state.tag)
	} else if !emptyTag && state.tag == "" {
		t.Error("Expected non-empty tag")
	}
}
