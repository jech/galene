package token

import (
	"crypto/ecdsa"
	"encoding/json"
	"reflect"
	"testing"
)

func TestJWKHS256(t *testing.T) {
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

func TestJWKES256(t *testing.T) {
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

func TestJWT(t *testing.T) {
	key := `{"alg":"HS256","k":"H7pCkktUl5KyPCZ7CKw09y1j460tfIv4dRcS1XstUKY","key_ops":["sign","verify"],"kty":"oct"}`
	var k map[string]interface{}
	err := json.Unmarshal([]byte(key), &k)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	keys := []map[string]interface{}{k}
	john := "john"
	jack := "jack"

	goodToken := "eyJ0eXAiOiJKV1QiLCJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJqb2huIiwiYXVkIjoiaHR0cHM6Ly9nYWxlbmUub3JnOjg0NDMvZ3JvdXAvYXV0aC8iLCJwZXJtaXNzaW9ucyI6WyJwcmVzZW50Il0sImlhdCI6MTY0NTMxMDI5NCwiZXhwIjoyOTA2NzUwMjk0LCJpc3MiOiJodHRwOi8vbG9jYWxob3N0OjEyMzQvIn0.6xXpgBkBMn4PSBpnwYHb-gRn_Q97Yq9DoKkAf2_6iwc"

	tok, err := Parse(goodToken, keys)
	if err != nil {
		t.Errorf("Couldn't parse goodToken: %v", err)
	}

	username, perms, err := tok.Check("galene.org:8443", "auth", &john)
	if err != nil {
		t.Errorf("goodToken is not valid: %v", err)
	}
	if username != "john" || !reflect.DeepEqual(perms, []string{"present"}) {
		t.Errorf("Expected john, [present], got %v %v", username, perms)
	}

	username, perms, err = tok.Check("galene.org:8443", "auth", &jack)
	if err != nil {
		t.Errorf("goodToken is not valid: %v", err)
	}
	if username != "john" || !reflect.DeepEqual(perms, []string{"present"}) {
		t.Errorf("Expected john, [present], got %v %v", username, perms)
	}

	username, perms, err = tok.Check("", "auth", &john)
	if err != nil {
		t.Errorf("goodToken is not valid: %v", err)
	}

	_, _, err = tok.Check("galene.org", "auth", &john)
	if err == nil {
		t.Errorf("goodToken is valid for wrong hostname")
	}

	_, _, err = tok.Check("galene.org:8443", "not-auth", &john)
	if err == nil {
		t.Errorf("goodToken is valid for wrong group")
	}

	emptySubToken := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIiLCJhdWQiOiJodHRwczovL2dhbGVuZS5vcmc6ODQ0My9ncm91cC9hdXRoLyIsInBlcm1pc3Npb25zIjpbInByZXNlbnQiXSwiaWF0IjoxNjQ1MzEwMjk0LCJleHAiOjI5MDY3NTAyOTQsImlzcyI6Imh0dHA6Ly9sb2NhbGhvc3Q6MTIzNC8ifQo.xwpHIRzKAIgiHKG1pVQyZlXcolmvRwNvBm6FN2gTwZw"

	tok, err = Parse(emptySubToken, keys)
	if err != nil {
		t.Errorf("Couldn't parse emptySubToken: %v", err)
	}
	username, perms, err = tok.Check("galene.org:8443", "auth", &jack)
	if err != nil {
		t.Errorf("anonymousToken is not valid: %v", err)
	}
	if username != "" || !reflect.DeepEqual(perms, []string{"present"}) {
		t.Errorf("Expected \"\", [present], got %v %v", username, perms)
	}

	noSubToken := "eyJhbGciOiJIUzI1NiJ9.eyJhdWQiOiJodHRwczovL2dhbGVuZS5vcmc6ODQ0My9ncm91cC9hdXRoLyIsInBlcm1pc3Npb25zIjpbInByZXNlbnQiXSwiaWF0IjoxNjQ1MzEwMjk0LCJleHAiOjI5MDY3NTAyOTQsImlzcyI6Imh0dHA6Ly9sb2NhbGhvc3Q6MTIzNC8ifQo.7LvoZEKPNVvsRe8SjLxmKa1TgjTA4ZQo2LMPJSXl-ro"

	tok, err = Parse(noSubToken, keys)
	if err != nil {
		t.Errorf("Couldn't parse noSubToken: %v", err)
	}
	username, perms, err = tok.Check("galene.org:8443", "auth", &jack)
	if err != nil {
		t.Errorf("noSubToken is not valid: %v", err)
	}
	if username != "" || !reflect.DeepEqual(perms, []string{"present"}) {
		t.Errorf("Expected \"\", [present], got %v %v", username, perms)
	}

	badToken := "eyJ0eXAiOiJKV1QiLCJhbGciOiJub25lIn0.eyJzdWIiOiJqb2huIiwiYXVkIjoiaHR0cHM6Ly9nYWxlbmUub3JnOjg0NDMvZ3JvdXAvYXV0aC8iLCJwZXJtaXNzaW9ucyI6WyJwcmVzZW50Il0sImlhdCI6MTY0NTMxMDQ2OSwiZXhwIjoyOTA2NzUwNDY5LCJpc3MiOiJodHRwOi8vbG9jYWxob3N0OjEyMzQvIn0."

	_, err = Parse(badToken, keys)
	if err == nil {
		t.Errorf("badToken is good")
	}

	expiredToken := "eyJ0eXAiOiJKV1QiLCJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJqb2huIiwiYXVkIjoiaHR0cHM6Ly9nYWxlbmUub3JnOjg0NDMvZ3JvdXAvYXV0aC8iLCJwZXJtaXNzaW9ucyI6WyJwcmVzZW50Il0sImlhdCI6MTY0NTMxMDMyMiwiZXhwIjoxNjQ1MzEwMzUyLCJpc3MiOiJodHRwOi8vbG9jYWxob3N0OjEyMzQvIn0.jyqRhoV6iK54SvlP33Fy630aDo-sLNmKKi1kcfqs378"

	_, err = Parse(expiredToken, keys)
	if err == nil {
		t.Errorf("expiredToken is good")
	}

	noneToken := "eyJ0eXAiOiJKV1QiLCJhbGciOiJub25lIn0.eyJzdWIiOiJqb2huIiwiYXVkIjoiaHR0cHM6Ly9nYWxlbmUub3JnOjg0NDMvZ3JvdXAvYXV0aC8iLCJwZXJtaXNzaW9ucyI6WyJwcmVzZW50Il0sImlhdCI6MTY0NTMxMDQwMSwiZXhwIjoxNjQ1MzEwNDMxLCJpc3MiOiJodHRwOi8vbG9jYWxob3N0OjEyMzQvIn0."
	_, err = Parse(noneToken, keys)
	if err == nil {
		t.Errorf("noneToken is good")
	}
}
