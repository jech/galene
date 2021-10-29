package token

import (
	"crypto/ecdsa"
	"encoding/json"
	"testing"
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
