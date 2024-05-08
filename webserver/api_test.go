package webserver

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/jech/galene/group"
	"github.com/jech/galene/token"
)

var setupOnce = sync.OnceFunc(func() {
	Insecure = true
	err := Serve("localhost:1234", "")
	if err != nil {
		panic("could not start server")
	}
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

	token.SetStatefulFilename(filepath.Join(datadir, "tokens.jsonl"))
	return nil
}

func marshalToString(v any) string {
	buf, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(buf)
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
			return errors.New("Unexpected content-type")
		}
		d := json.NewDecoder(resp.Body)
		return d.Decode(value)
	}

	var groups []string
	err = getJSON("/galene-api/v0/.groups/", &groups)
	if err != nil || len(groups) != 0 {
		t.Errorf("Get groups: %v", err)
	}

	resp, err := do("PUT", "/galene-api/v0/.groups/test/",
		"application/json", "\"foo\"", "",
		"{}")
	if err != nil || resp.StatusCode != http.StatusPreconditionFailed {
		t.Errorf("Create group (bad ETag): %v %v", err, resp.StatusCode)
	}

	resp, err = do("PUT", "/galene-api/v0/.groups/test/",
		"text/plain", "", "",
		"Hello, world!")
	if err != nil || resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("Create group (bad content-type): %v %v",
			err, resp.StatusCode)
	}

	resp, err = do("PUT", "/galene-api/v0/.groups/test/",
		"application/json", "", "*",
		"{}")
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Errorf("Create group: %v %v", err, resp.StatusCode)
	}

	var desc *group.Description
	err = getJSON("/galene-api/v0/.groups/test/", &desc)
	if err != nil || len(desc.Users) != 0 {
		t.Errorf("Get group: %v", err)
	}

	resp, err = do("PUT", "/galene-api/v0/.groups/test/",
		"application/json", "", "*",
		"{}")
	if err != nil || resp.StatusCode != http.StatusPreconditionFailed {
		t.Errorf("Create group (bad ETag): %v %v", err, resp.StatusCode)
	}

	resp, err = do("DELETE", "/galene-api/v0/.groups/test/",
		"", "", "*", "")
	if err != nil || resp.StatusCode != http.StatusPreconditionFailed {
		t.Errorf("Delete group (bad ETag): %v %v", err, resp.StatusCode)
	}

	err = getJSON("/galene-api/v0/.groups/", &groups)
	if err != nil || len(groups) != 1 || groups[0] != "test" {
		t.Errorf("Get groups: %v %v", err, groups)
	}

	resp, err = do("PUT", "/galene-api/v0/.groups/test/.keys",
		"application/jwk-set+json", "", "",
		`{"keys": [{
                            "kty": "oct", "alg": "HS256",
                            "k": "4S9YZLHK1traIaXQooCnPfBw_yR8j9VEPaAMWAog_YQ"
                 }]}`)
	if err != nil || resp.StatusCode != http.StatusNoContent {
		t.Errorf("Set key: %v %v", err, resp.StatusCode)
	}

	err = getJSON("/galene-api/v0/.groups/test/.users/", &groups)
	if err != nil || len(groups) != 0 {
		t.Errorf("Get users: %v", err)
	}

	resp, err = do("PUT", "/galene-api/v0/.groups/test/.users/jch",
		"text/plain", "", "*",
		`hello, world!`)
	if err != nil || resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("Create user (bad content-type): %v %v",
			err, resp.StatusCode)
	}

	resp, err = do("PUT", "/galene-api/v0/.groups/test/.users/jch",
		"application/json", "", "*",
		`{"permissions": "present"}`)
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Errorf("Create user: %v %v", err, resp.StatusCode)
	}

	var users []string
	err = getJSON("/galene-api/v0/.groups/test/.users/", &users)
	if err != nil || len(users) != 1 || users[0] != "jch"  {
		t.Errorf("Get users: %v %v", err, users)
	}

	resp, err = do("PUT", "/galene-api/v0/.groups/test/.users/jch",
		"application/json", "", "*",
		`{"permissions": "present"}`)
	if err != nil || resp.StatusCode != http.StatusPreconditionFailed {
		t.Errorf("Create user (bad ETag): %v %v", err, resp.StatusCode)
	}

	resp, err = do("PUT", "/galene-api/v0/.groups/test/.users/jch/.password",
		"application/json", "", "",
		`"toto"`)
	if err != nil || resp.StatusCode != http.StatusNoContent {
		t.Errorf("Set password (PUT): %v %v", err, resp.StatusCode)
	}

	resp, err = do("POST", "/galene-api/v0/.groups/test/.users/jch/.password",
		"text/plain", "", "",
		`toto`)
	if err != nil || resp.StatusCode != http.StatusNoContent {
		t.Errorf("Set password (POST): %v %v", err, resp.StatusCode)
	}

	var user group.UserDescription
	err = getJSON("/galene-api/v0/.groups/test/.users/jch", &user)
	if err != nil {
		t.Errorf("Get user: %v", err)
	}
	if user.Password.Type != "" && user.Password.Key != nil {
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

	resp, err = do("DELETE", "/galene-api/v0/.groups/test/.users/jch",
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

	resp, err = do("PUT", "/galene-api/v0/.groups/test/.wildcard-user",
		"application/json", "", "*",
		`{"permissions": "present"}`)
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Errorf("Create wildcard user: %v %v", err, resp.StatusCode)
	}

	err = getJSON("/galene-api/v0/.groups/test/.wildcard-user", &user)
	if err != nil {
		t.Errorf("Get wildcard user: %v", err)
	}

	resp, err = do("DELETE", "/galene-api/v0/.groups/test/.wildcard-user",
		"", "", "", "")
	if err != nil || resp.StatusCode != http.StatusNoContent {
		t.Errorf("Delete wildcard user: %v %v", err, resp.StatusCode)
	}

	if len(desc.AuthKeys) != 1 {
		t.Errorf("Keys: %v", len(desc.AuthKeys))
	}

	resp, err = do("POST", "/galene-api/v0/.groups/test/.tokens/",
		"application/json", "", "", `{"group":"bad"}`)
	if err != nil || resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Create token (bad group): %v %v", err, resp.StatusCode)
	}

	resp, err = do("POST", "/galene-api/v0/.groups/test/.tokens/",
		"application/json", "", "", "{}")
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Errorf("Create token: %v %v", err, resp.StatusCode)
	}

	var toknames []string
	err = getJSON("/galene-api/v0/.groups/test/.tokens/", &toknames)
	if err != nil || len(toknames) != 1 {
		t.Errorf("Get tokens: %v %v", err, toknames)
	}
	tokname := toknames[0]

	tokens, etag, err := token.List("test")
	if err != nil || len(tokens) != 1 || tokens[0].Token != tokname {
		t.Errorf("token.List: %v %v", tokens, err)
	}

	tokenpath := "/galene-api/v0/.groups/test/.tokens/" + tokname
	resp, err = do("GET", tokenpath,
		"", "", "", "")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Errorf("Get token: %v %v", err, resp.StatusCode)
	}

	tok := tokens[0].Clone()
	e := time.Now().Add(time.Hour)
	tok.Expires = &e
	resp, err = do("PUT", tokenpath,
		"application/json", etag, "", marshalToString(tok))
	if err != nil || resp.StatusCode != http.StatusNoContent {
		t.Errorf("Update token: %v %v", err, resp.StatusCode)
	}

	tok.Token = "badtoken"
	resp, err = do("PUT", tokenpath,
		"application/json", "", "", marshalToString(tok))
	if err != nil || resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Update mismatched token: %v %v", err, resp.StatusCode)
	}

	tok.Group = "bad"
	resp, err = do("PUT", tokenpath,
		"application/json", "", "", marshalToString(tok))
	if err != nil || resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Update token (bad group): %v %v", err, resp.StatusCode)
	}

	tokens, etag, err = token.List("test")
	if err != nil || len(tokens) != 1 {
		t.Errorf("Token list: %v %v", tokens, err)
	}
	if !tokens[0].Expires.Equal(e) {
		t.Errorf("Got %v, expected %v", tokens[0].Expires, e)
	}

	resp, err = do("GET", tokenpath, "", "", "", "")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Errorf("Get token: %v %v", err, resp.StatusCode)
	}

	resp, err = do("DELETE", tokenpath, "", "", "", "")
	if err != nil || resp.StatusCode != http.StatusNoContent {
		t.Errorf("Update token: %v %v", err, resp.StatusCode)
	}
	tokens, etag, err = token.List("test")
	if err != nil || len(tokens) != 0 {
		t.Errorf("Token list: %v %v", tokens, err)
	}

	resp, err = do("DELETE", "/galene-api/v0/.groups/test/.keys",
		"", "", "", "")
	if err != nil || resp.StatusCode != http.StatusNoContent {
		t.Errorf("Delete keys: %v %v", err, resp.StatusCode)
	}

	resp, err = do("DELETE", "/galene-api/v0/.groups/test/",
		"", "", "", "")
	if err != nil || resp.StatusCode != http.StatusNoContent {
		t.Errorf("Delete group: %v %v", err, resp.StatusCode)
	}

	_, err = group.GetDescription("test")
	if !errors.Is(err, os.ErrNotExist) {
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

	do("GET", "/galene-api/v0/.stats")
	do("GET", "/galene-api/v0/.groups/")
	do("PUT", "/galene-api/v0/.groups/test/")

	f, err := os.Create(filepath.Join(group.Directory, "test.json"))
	if err != nil {
		t.Fatalf("Create(test.json): %v", err)
	}
	f.WriteString(`{
            "users": {"jch": {"permissions": "present", "password": "pw"}}
        }\n`)
	f.Close()

	do("PUT", "/galene-api/v0/.groups/test/")
	do("DELETE", "/galene-api/v0/.groups/test/")
	do("GET", "/galene-api/v0/.groups/test/.users/")
	do("GET", "/galene-api/v0/.groups/test/.users/jch")
	do("GET", "/galene-api/v0/.groups/test/.users/jch")
	do("PUT", "/galene-api/v0/.groups/test/.users/jch")
	do("DELETE", "/galene-api/v0/.groups/test/.users/jch")
	do("GET", "/galene-api/v0/.groups/test/.users/not-jch")
	do("PUT", "/galene-api/v0/.groups/test/.users/not-jch")
	do("PUT", "/galene-api/v0/.groups/test/.users/jch/.password")
	do("POST", "/galene-api/v0/.groups/test/.users/jch/.password")
	do("GET", "/galene-api/v0/.groups/test/.tokens/")
	do("POST", "/galene-api/v0/.groups/test/.tokens/")
	do("GET", "/galene-api/v0/.groups/test/.tokens/token")
	do("PUT", "/galene-api/v0/.groups/test/.tokens/token")
	do("DELETE", "/galene-api/v0/.groups/test/.tokens/token")
}
