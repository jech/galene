package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Error for incorrect file tag values passed to Write and Delete.
var ErrTagMismatch = errors.New("tag mismatch")

// ConfigFile manages read and write access to configuration files.
// Modifications require passing the correct file tag value.
type ConfigFile struct {
	path string
}

// Create a new ConfigFile for the given file path.
func NewConfigFile(path string) ConfigFile {
	return ConfigFile{path: path}
}

// Return the file path.
func (cf *ConfigFile) Path() string {
	return cf.path
}

// Return the current file tag value.
// The value for missing or otherwise inaccessible files is an empty string.
// The value may be used as an HTTP ETag.
func (cf *ConfigFile) Tag() string {
	fi, err := os.Stat(cf.path)
	if err != nil {
		return ""
	}

	return makeTag(fi.Size(), fi.ModTime())
}

// Read the file using a handler function.
// If the file does not exist, the handler will receive a nil argument.
func (cf *ConfigFile) Read(handler func(r io.Reader) error) error {
	_, err := os.Stat(cf.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return handler(nil)
		}
		return err
	}

	f, err := os.Open(cf.path)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := handler(f); err != nil {
		return err
	}

	return nil
}

// Write the file using a handler function.
// The tag must match the current file tag value.
func (cf *ConfigFile) Write(tag string, handler func(w io.Writer) error) error {
	if cf.Tag() != tag {
		return ErrTagMismatch
	}

	dir := filepath.Dir(cf.path)
	err := os.MkdirAll(dir, 0700)
	if err != nil {
		return err
	}

	f, err := os.CreateTemp(dir, "*.temp")
	if err != nil {
		return err
	}
	temp := f.Name()

	err = handler(f)
	if err == nil {
		err = f.Sync()
	}
	if err != nil {
		f.Close()
		os.Remove(temp)
		return err
	}
	err = f.Close()
	if err != nil {
		os.Remove(temp)
		return err
	}

	err = os.Rename(temp, cf.path)
	if err != nil {
		os.Remove(temp)
		return err
	}

	return nil
}

// Delete the file.
// The tag must match the current file tag value.
func (cf *ConfigFile) Delete(tag string) error {
	if cf.Tag() != tag {
		return ErrTagMismatch
	}

	return os.Remove(cf.path)
}

func makeTag(fileSize int64, modTime time.Time) string {
	return fmt.Sprintf("\"%v-%v\"", fileSize, modTime.UnixNano())
}
