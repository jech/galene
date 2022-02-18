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
	k, err := parseKey(j)
	if err != nil {
		t.Fatalf("parseKey: %v", err)
	}
	kk, ok := k.([]byte)
	if !ok || len(kk) != 32 {
		t.Errorf("parseKey: got %v", kk)
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
	k, err := parseKey(j)
	if err != nil {
		t.Fatalf("parseKey: %v", err)
	}
	kk, ok := k.(*ecdsa.PublicKey)
	if !ok || kk.Params().Name != "P-256" {
		t.Errorf("parseKey: got %v", kk)
	}
	if !kk.IsOnCurve(kk.X, kk.Y) {
		t.Errorf("point is not on curve")
	}
}

func TestValid(t *testing.T) {
	key := `{
            "kty":"EC",
            "alg":"ES256",
            "crv":"P-256",
            "x":"CBo2DHISffe8bVr6bNspCiHK3zK9pfMGfWtpHnk9-Lw",
            "y":"sD5dQ-bJu8AfRGLfA6MigQyUIOQHcYx6HQOdfIbLjHo"
        }`
	var k map[string]interface{}
	err := json.Unmarshal([]byte(key), &k)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	keys := []map[string]interface{}{k}

	goodToken := "eyJ0eXAiOiJKV1QiLCJhbGciOiJFUzI1NiJ9.eyJzdWIiOiJqb2huIiwiYXVkIjoiaHR0cHM6Ly9nYWxlbmUub3JnOjg0NDMvZ3JvdXAvYXV0aC8iLCJwZXJtaXNzaW9ucyI6eyJwcmVzZW50Ijp0cnVlfSwiaWF0IjoxNjQ1MTk1MzkxLCJleHAiOjIyNzU5MTUzOTEsImlzcyI6Imh0dHA6Ly9sb2NhbGhvc3Q6MTIzNC8ifQ.PMgfwYwSLSFIfcNJdOEfHEZ41HM2CzbATuS1fTxncbaGyX-xXq7d9V04enXpLOMGnAlsZpOJvd7eJN2mngJMAg"

	aud, perms, err := Valid(
		"john", goodToken, keys, "http://localhost:1234/",
	)

	if err != nil {
		t.Errorf("Token invalid: %v", err)
	} else {
		if !reflect.DeepEqual(aud, []string{"https://galene.org:8443/group/auth/"}) {
			t.Errorf("Unexpected aud: %v", aud)
		}
		if !reflect.DeepEqual(
			perms, map[string]interface{}{"present": true},
		) {
			t.Errorf("Unexpected perms: %v", perms)
		}
	}

	aud, perms, err = Valid(
		"jack", goodToken, keys, "http://localhost:1234/",
	)
	if err != ErrUnexpectedSub {
		t.Errorf("Token should have bad username")
	}

	aud, perms, err = Valid(
		"john", goodToken, keys, "http://localhost:4567/",
	)
	if err != ErrUnexpectedIss {
		t.Errorf("Token should have bad issuer")
	}

	badToken := "eyJ0eXAiOiJKV1QiLCJhbGciOiJFUzI1NiJ9.eyJzdWIiOiJqb2huIiwiYXVkIjoiaHR0cHM6Ly9nYWxlbmUub3JnOjg0NDMvZ3JvdXAvYXV0aC8iLCJwZXJtaXNzaW9ucyI6eyJwcmVzZW50Ijp0cnVlfSwiaWF0IjoxNjQ1MTk2MDE5LCJleHAiOjIyNjAzNjQwMTksImlzcyI6Imh0dHA6Ly9sb2NhbGhvc3Q6MTIzNC8ifQ.4TN5zxzuKeNIw0rX0yirEkVYF1d0FHI_Lezmsa27ayi0R4ocSgTZ3q2bmlACXvyuoBqEEbuP4e77BUbGCHmpSg"

	_, _, err = Valid(
		"john", badToken, keys,
		"https://localhost:1234/group/auth/",
	)

	var verr *jwt.ValidationError
	if !errors.As(err, &verr) {
		t.Errorf("Token should fail")
	}

	expiredToken := "eyJ0eXAiOiJKV1QiLCJhbGciOiJFUzI1NiJ9.eyJzdWIiOiJqb2huIiwiYXVkIjoiaHR0cHM6Ly9nYWxlbmUub3JnOjg0NDMvZ3JvdXAvYXV0aC8iLCJwZXJtaXNzaW9ucyI6eyJwcmVzZW50Ijp0cnVlfSwiaWF0IjoxNjQ1MTk1NTY3LCJleHAiOjE2NDUxOTU1OTcsImlzcyI6Imh0dHA6Ly9sb2NhbGhvc3Q6MTIzNC8ifQ.GXcLeyNVr5cnZjIECENyjMLH1HyNKWKkHMc9onvqA_RVYMyDLeeR_3NKH9Y7eKSXWC8jhatDWtH7Ed3KdsSxAA"

	_, _, err = Valid(
		"john", expiredToken, keys,
		"https://localhost:1234/group/auth/",
	)

	if !errors.As(err, &verr) {
		t.Errorf("Token should be expired")
	}

	noneToken := "eyJ0eXAiOiJKV1QiLCJhbGciOiJub25lIn0.eyJzdWIiOiJqb2huIiwiYXVkIjoiaHR0cHM6Ly9nYWxlbmUub3JnOjg0NDMvZ3JvdXAvYXV0aC8iLCJwZXJtaXNzaW9ucyI6eyJwcmVzZW50Ijp0cnVlfSwiaWF0IjoxNjQ1MTk1NzgyLCJleHAiOjIyNjAzNjM3ODIsImlzcyI6Imh0dHA6Ly9sb2NhbGhvc3Q6MTIzNC8ifQ."

	_, _, err = Valid(
		"john", noneToken, keys,
		"https://localhost:1234/group/auth/",
	)

	if err == nil {
		t.Errorf("Unsigned token should fail")
	}
}
