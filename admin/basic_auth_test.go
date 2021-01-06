package admin

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jech/galene/group"
	"github.com/stretchr/testify/assert"
	"golang.org/x/crypto/pbkdf2"
)

func TestBasicAuth(t *testing.T) {

	pw := "pazzwordz"
	salt := []byte("zalt")
	iterations := 4096
	length := 32
	key := pbkdf2.Key(
		[]byte(pw), salt, iterations, length, sha256.New,
	)

	p := &group.Password{
		Type:       "pbkdf2",
		Hash:       "sha-256",
		Key:        hex.EncodeToString(key),
		Salt:       hex.EncodeToString(salt),
		Iterations: iterations,
	}
	ts := httptest.NewServer(BasicAuthMiddleware(p, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "Hello, client")
	})))
	defer ts.Close()
	res, err := http.Get(ts.URL)
	assert.NoError(t, err)
	assert.Equal(t, 401, res.StatusCode)
	r, err := http.NewRequest("GET", ts.URL, nil)
	assert.NoError(t, err)
	r.SetBasicAuth("admin", pw)
	client := &http.Client{}
	for i := 0; i < 2; i++ {
		res, err = client.Do(r)
		assert.NoError(t, err)
		assert.Equal(t, 200, res.StatusCode)
	}
	r.SetBasicAuth("admin", "wrong")
	res, err = client.Do(r)
	assert.NoError(t, err)
	assert.Equal(t, 403, res.StatusCode)
}
