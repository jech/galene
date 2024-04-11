package webserver

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/jech/galene/group"
)

var setupOnce = sync.OnceFunc(func() {
	Insecure = true
	go func() {
		err := Serve("localhost:1234", "")
		if err != nil {
			panic("could not start server")
		}
	}()
	time.Sleep(100 * time.Millisecond)
})

func setupTest(dir, datadir string) error {
	setupOnce()

	group.Directory = dir
	group.DataDirectory = datadir
	config := `{
    "writableGroups": true,
    "users": {
        "root": {
            "password": "pw",
            "permissions": "admin"
        }
    }
}`
	f, err := os.Create(filepath.Join(group.DataDirectory, "config.json"))
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(config)
	if err != nil {
		return err
	}
	return nil
}

func TestApi(t *testing.T) {
	err := setupTest(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	client := http.Client{}

	do := func(method, path, ctype, im, inm, body string) (*http.Response, error) {
		req, err := http.NewRequest(method,
			"http://localhost:1234"+path,
			strings.NewReader(body),
		)
		if err != nil {
			return nil, err
		}
		if ctype != "" {
			req.Header.Set("Content-Type", ctype)
		}
		if im != "" {
			req.Header.Set("If-Match", im)
		}
		if inm != "" {
			req.Header.Set("If-None-Match", inm)
		}
		req.SetBasicAuth("root", "pw")
		return client.Do(req)
	}

	getString := func(path string) (string, error) {
		resp, err := do("GET", path, "", "", "", "")
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("Status is %v", resp.StatusCode)
		}
		ctype := parseContentType(resp.Header.Get("Content-Type"))
		if !strings.EqualFold(ctype, "text/plain") {
			return "", errors.New("Unexpected Content-Type")
		}
		b, err := io.ReadAll(resp.Body)
		return string(b), err
	}

	getJSON := func(path string, value any) error {
		resp, err := do("GET", path, "", "", "", "")
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("Status is %v", resp.StatusCode)
		}
		ctype := parseContentType(resp.Header.Get("Content-Type"))
		if !strings.EqualFold(ctype, "application/json") {
			return errors.New("Unexpected")
		}
		d := json.NewDecoder(resp.Body)
		return d.Decode(value)
	}

	s, err := getString("/galene-api/0/.groups/")
	if err != nil || s != "" {
		t.Errorf("Get groups: %v", err)
	}

	resp, err := do("PUT", "/galene-api/0/.groups/test/",
		"application/json", "\"foo\"", "",
		"{}")
	if err != nil || resp.StatusCode != http.StatusPreconditionFailed {
		t.Errorf("Create group (bad ETag): %v %v", err, resp.StatusCode)
	}

	resp, err = do("PUT", "/galene-api/0/.groups/test/",
		"text/plain", "", "",
		"Hello, world!")
	if err != nil || resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("Create group (bad content-type): %v %v",
			err, resp.StatusCode)
	}

	resp, err = do("PUT", "/galene-api/0/.groups/test/",
		"application/json", "", "*",
		"{}")
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Errorf("Create group: %v %v", err, resp.StatusCode)
	}

	var desc *group.Description
	err = getJSON("/galene-api/0/.groups/test/", &desc)
	if err != nil || len(desc.Users) != 0 {
		t.Errorf("Get group: %v", err)
	}

	resp, err = do("PUT", "/galene-api/0/.groups/test/",
		"application/json", "", "*",
		"{}")
	if err != nil || resp.StatusCode != http.StatusPreconditionFailed {
		t.Errorf("Create group (bad ETag): %v %v", err, resp.StatusCode)
	}

	resp, err = do("DELETE", "/galene-api/0/.groups/test/",
		"", "", "*", "")
	if err != nil || resp.StatusCode != http.StatusPreconditionFailed {
		t.Errorf("Delete group (bad ETag): %v %v", err, resp.StatusCode)
	}

	s, err = getString("/galene-api/0/.groups/")
	if err != nil || s != "test\n" {
		t.Errorf("Get groups: %v %#v", err, s)
	}

	resp, err = do("PUT", "/galene-api/0/.groups/test/.keys",
		"application/jwk-set+json", "", "",
		`{"keys": [{
                            "kty": "oct", "alg": "HS256",
                            "k": "4S9YZLHK1traIaXQooCnPfBw_yR8j9VEPaAMWAog_YQ"
                 }]}`)
	if err != nil || resp.StatusCode != http.StatusNoContent {
		t.Errorf("Set key: %v %v", err, resp.StatusCode)
	}

	s, err = getString("/galene-api/0/.groups/test/.users/")
	if err != nil || s != "" {
		t.Errorf("Get users: %v", err)
	}

	resp, err = do("PUT", "/galene-api/0/.groups/test/.users/jch",
		"text/plain", "", "*",
		`hello, world!`)
	if err != nil || resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("Create user (bad content-type): %v %v",
			err, resp.StatusCode)
	}

	resp, err = do("PUT", "/galene-api/0/.groups/test/.users/jch",
		"application/json", "", "*",
		`{"permissions": "present"}`)
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Errorf("Create user: %v %v", err, resp.StatusCode)
	}

	s, err = getString("/galene-api/0/.groups/test/.users/")
	if err != nil || s != "jch\n" {
		t.Errorf("Get users: %v", err)
	}

	resp, err = do("PUT", "/galene-api/0/.groups/test/.users/jch",
		"application/json", "", "*",
		`{"permissions": "present"}`)
	if err != nil || resp.StatusCode != http.StatusPreconditionFailed {
		t.Errorf("Create user (bad ETag): %v %v", err, resp.StatusCode)
	}

	resp, err = do("PUT", "/galene-api/0/.groups/test/.users/jch/.password",
		"application/json", "", "",
		`"toto"`)
	if err != nil || resp.StatusCode != http.StatusNoContent {
		t.Errorf("Set password (PUT): %v %v", err, resp.StatusCode)
	}

	resp, err = do("POST", "/galene-api/0/.groups/test/.users/jch/.password",
		"text/plain", "", "",
		`toto`)
	if err != nil || resp.StatusCode != http.StatusNoContent {
		t.Errorf("Set password (POST): %v %v", err, resp.StatusCode)
	}

	var user group.UserDescription
	err = getJSON("/galene-api/0/.groups/test/.users/jch", &user)
	if err != nil {
		t.Errorf("Get user: %v", err)
	}
	if user.Password.Type != "" && user.Password.Key != "" {
		t.Errorf("User not sanitised properly")
	}

	desc, err = group.GetDescription("test")
	if err != nil {
		t.Errorf("GetDescription: %v", err)
	}

	if len(desc.Users) != 1 {
		t.Errorf("Users: %#v", desc.Users)
	}

	if desc.Users["jch"].Password.Type != "pbkdf2" {
		t.Errorf("Password.Type: %v", desc.Users["jch"].Password.Type)
	}

	resp, err = do("DELETE", "/galene-api/0/.groups/test/.users/jch",
		"", "", "", "")
	if err != nil || resp.StatusCode != http.StatusNoContent {
		t.Errorf("Delete group: %v %v", err, resp.StatusCode)
	}

	desc, err = group.GetDescription("test")
	if err != nil {
		t.Errorf("GetDescription: %v", err)
	}

	if len(desc.Users) != 0 {
		t.Errorf("Users (after delete): %#v", desc.Users)
	}

	if len(desc.AuthKeys) != 1 {
		t.Errorf("Keys: %v", len(desc.AuthKeys))
	}

	resp, err = do("DELETE", "/galene-api/0/.groups/test/.keys",
		"", "", "", "")
	if err != nil || resp.StatusCode != http.StatusNoContent {
		t.Errorf("Delete keys: %v %v", err, resp.StatusCode)
	}

	resp, err = do("DELETE", "/galene-api/0/.groups/test/",
		"", "", "", "")
	if err != nil || resp.StatusCode != http.StatusNoContent {
		t.Errorf("Delete group: %v %v", err, resp.StatusCode)
	}

	_, err = group.GetDescription("test")
	if !os.IsNotExist(err) {
		t.Errorf("Group exists after delete")
	}
}

func TestApiBadAuth(t *testing.T) {
	err := setupTest(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	client := http.Client{}

	do := func(method, path string) {
		req, err := http.NewRequest(method,
			"http://localhost:1234"+path,
			nil)
		if err != nil {
			t.Errorf("New request: %v", err)
			return
		}
		req.SetBasicAuth("root", "badpw")
		resp, err := client.Do(req)
		if err != nil {
			t.Errorf("%v %v: %v", method, path, err)
			return
		}
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%v %v: %v", method, path, resp.StatusCode)
		}
	}

	do("GET", "/galene-api/0/.stats")
	do("GET", "/galene-api/0/.groups/")
	do("PUT", "/galene-api/0/.groups/test/")

	f, err := os.Create(filepath.Join(group.Directory, "test.json"))
	if err != nil {
		t.Fatalf("Create(test.json): %v", err)
	}
	f.WriteString(`{
            "users": {"jch": {"permissions": "present", "password": "pw"}}
        }\n`)
	f.Close()

	do("PUT", "/galene-api/0/.groups/test/")
	do("DELETE", "/galene-api/0/.groups/test/")
	do("GET", "/galene-api/0/.groups/test/.users/")
	do("GET", "/galene-api/0/.groups/test/.users/jch")
	do("GET", "/galene-api/0/.groups/test/.users/jch")
	do("PUT", "/galene-api/0/.groups/test/.users/jch")
	do("DELETE", "/galene-api/0/.groups/test/.users/jch")
	do("GET", "/galene-api/0/.groups/test/.users/not-jch")
	do("PUT", "/galene-api/0/.groups/test/.users/not-jch")
	do("PUT", "/galene-api/0/.groups/test/.users/jch/.password")
	do("POST", "/galene-api/0/.groups/test/.users/jch/.password")
}
