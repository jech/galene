package token

import (
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/golang-jwt/jwt/v4"
)

func TestHS256(t *testing.T) {
	key := `{
            "kty":"oct",
            "alg":"HS256",
            "k":"4S9YZLHK1traIaXQooCnPfBw_yR8j9VEPaAMWAog_YQ"
        }`
	var j map[string]interface{}
	err := json.Unmarshal([]byte(key), &j)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	k, err := ParseKey(j)
	if err != nil {
		t.Fatalf("ParseKey: %v", err)
	}
	kk, ok := k.([]byte)
	if !ok || len(kk) != 32 {
		t.Errorf("ParseKey: got %v", kk)
	}
}

func TestES256(t *testing.T) {
	key := `{
            "kty":"EC",
            "alg":"ES256",
            "crv":"P-256",
            "x":"dElK9qBNyCpRXdvJsn4GdjrFzScSzpkz_I0JhKbYC88",
            "y":"pBhVb37haKvwEoleoW3qxnT4y5bK35_RTP7_RmFKR6Q"
        }`
	var j map[string]interface{}
	err := json.Unmarshal([]byte(key), &j)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	k, err := ParseKey(j)
	if err != nil {
		t.Fatalf("ParseKey: %v", err)
	}
	kk, ok := k.(*ecdsa.PublicKey)
	if !ok || kk.Params().Name != "P-256" {
		t.Errorf("ParseKey: got %v", kk)
	}
	if !kk.IsOnCurve(kk.X, kk.Y) {
		t.Errorf("point is not on curve")
	}
}

func TestValid(t *testing.T) {
	key := `{"alg":"HS256","k":"H7pCkktUl5KyPCZ7CKw09y1j460tfIv4dRcS1XstUKY","key_ops":["sign","verify"],"kty":"oct"}`
	var k map[string]interface{}
	err := json.Unmarshal([]byte(key), &k)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	keys := []map[string]interface{}{k}

	goodToken := "eyJ0eXAiOiJKV1QiLCJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJqb2huIiwiYXVkIjoiaHR0cHM6Ly9nYWxlbmUub3JnOjg0NDMvZ3JvdXAvYXV0aC8iLCJwZXJtaXNzaW9ucyI6WyJwcmVzZW50Il0sImlhdCI6MTY0NTMxMDI5NCwiZXhwIjoyOTA2NzUwMjk0LCJpc3MiOiJodHRwOi8vbG9jYWxob3N0OjEyMzQvIn0.6xXpgBkBMn4PSBpnwYHb-gRn_Q97Yq9DoKkAf2_6iwc"

	sub, aud, perms, err := Valid(goodToken, keys)

	if err != nil {
		t.Errorf("Token invalid: %v", err)
	} else {
		if sub != "john" {
			t.Errorf("Unexpected sub: %v", sub)
		}
		if !reflect.DeepEqual(aud, []string{"https://galene.org:8443/group/auth/"}) {
			t.Errorf("Unexpected aud: %v", aud)
		}
		if !reflect.DeepEqual(perms, []string{"present"}) {
			t.Errorf("Unexpected perms: %v", perms)
		}
	}

	badToken := "eyJ0eXAiOiJKV1QiLCJhbGciOiJub25lIn0.eyJzdWIiOiJqb2huIiwiYXVkIjoiaHR0cHM6Ly9nYWxlbmUub3JnOjg0NDMvZ3JvdXAvYXV0aC8iLCJwZXJtaXNzaW9ucyI6WyJwcmVzZW50Il0sImlhdCI6MTY0NTMxMDQ2OSwiZXhwIjoyOTA2NzUwNDY5LCJpc3MiOiJodHRwOi8vbG9jYWxob3N0OjEyMzQvIn0."

	_, _, _, err = Valid(badToken, keys)

	var verr *jwt.ValidationError
	if !errors.As(err, &verr) {
		t.Errorf("Token should fail")
	}

	expiredToken := "eyJ0eXAiOiJKV1QiLCJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJqb2huIiwiYXVkIjoiaHR0cHM6Ly9nYWxlbmUub3JnOjg0NDMvZ3JvdXAvYXV0aC8iLCJwZXJtaXNzaW9ucyI6WyJwcmVzZW50Il0sImlhdCI6MTY0NTMxMDMyMiwiZXhwIjoxNjQ1MzEwMzUyLCJpc3MiOiJodHRwOi8vbG9jYWxob3N0OjEyMzQvIn0.jyqRhoV6iK54SvlP33Fy630aDo-sLNmKKi1kcfqs378"

	_, _, _, err = Valid(expiredToken, keys)

	if !errors.As(err, &verr) {
		t.Errorf("Token should be expired")
	}

	noneToken := "eyJ0eXAiOiJKV1QiLCJhbGciOiJub25lIn0.eyJzdWIiOiJqb2huIiwiYXVkIjoiaHR0cHM6Ly9nYWxlbmUub3JnOjg0NDMvZ3JvdXAvYXV0aC8iLCJwZXJtaXNzaW9ucyI6WyJwcmVzZW50Il0sImlhdCI6MTY0NTMxMDQwMSwiZXhwIjoxNjQ1MzEwNDMxLCJpc3MiOiJodHRwOi8vbG9jYWxob3N0OjEyMzQvIn0."
	_, _, _, err = Valid(noneToken, keys)
	if err == nil {
		t.Errorf("Unsigned token should fail")
	}
}
