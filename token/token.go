package token

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/base64"
	"errors"
	"math/big"

	"github.com/golang-jwt/jwt/v4"
)

func parseBase64(k string, d map[string]interface{}) ([]byte, error) {
	v, ok := d[k].(string)
	if !ok {
		return nil, errors.New("key " + k + " not found")
	}
	vv, err := base64.RawURLEncoding.DecodeString(v)
	if err != nil {
		return nil, err
	}
	return vv, nil
}

func parseKey(key map[string]interface{}) (interface{}, error) {
	kty, ok := key["kty"].(string)
	if !ok {
		return nil, errors.New("kty not found")
	}
	alg, ok := key["alg"].(string)
	if !ok {
		return nil, errors.New("alg not found")
	}

	switch kty {
	case "oct":
		var length int
		switch(alg) {
		case "HS256":
			length = 32
		case "HS384":
			length = 48
		case "HS512":
			length = 64
		default:
			return nil, errors.New("unknown alg")
		}
		k, err := parseBase64("k", key)
		if err != nil {
			return nil, err
		}
		if len(k) != length {
			return nil, errors.New("bad length for key")
		}
		return k, nil
	case "EC":
		if alg != "ES256" {
			return nil, errors.New("uknown alg")
		}
		crv, ok := key["crv"].(string)
		if !ok {
			return nil, errors.New("crv not found")
		}
		if crv != "P-256" {
			return nil, errors.New("unknown crv")
		}
		curve := elliptic.P256()
		xbytes, err := parseBase64("x", key)
		if err != nil {
			return nil, err
		}
		var x big.Int
		x.SetBytes(xbytes)
		ybytes, err := parseBase64("y", key)
		if err != nil {
			return nil, err
		}
		var y big.Int
		y.SetBytes(ybytes)
		if !curve.IsOnCurve(&x, &y) {
			return nil, errors.New("key is not on curve")
		}
		return &ecdsa.PublicKey{
			Curve: curve,
			X:     &x,
			Y:     &y,
		}, nil
	default:
		return nil, errors.New("unknown key type")
	}
}

func getKey(header map[string]interface{}, keys []map[string]interface{}) (interface{}, error) {
	alg, _ := header["alg"].(string)
	kid, _ := header["kid"].(string)
	for _, k := range keys {
		kid2, _ := k["kid"].(string)
		alg2, _ := k["alg"].(string)
		if (kid == "" || kid == kid2) && alg == alg2 {
			return parseKey(k)
		}
	}
	return nil, errors.New("key not found")
}

func Valid(username, token string, keys []map[string]interface{}, issuer string) ([]string, map[string]interface{}, error) {
	tok, err := jwt.Parse(token, func(t *jwt.Token) (interface{}, error) {
		return getKey(t.Header, keys)
	})
	if err != nil {
		return nil, nil, err
	}
	claims := tok.Claims.(jwt.MapClaims)

	sub, ok := claims["sub"].(string)
	if !ok || sub != username {
		return nil, nil, errors.New("invalid 'sub' field")
	}
	iss, ok := claims["iss"].(string)
	if !ok || iss != issuer {
		return nil, nil, errors.New("invalid 'iss' field")
	}
	aud, ok := claims["aud"]
	var res []string
	if ok {
		switch aud := aud.(type) {
		case string:
			res = []string{aud}
		case []string:
			res = aud
		}
	}
	perms, ok := claims["permissions"].(map[string]interface{})
	if !ok {
		return nil, nil, errors.New("invalid 'permissions' field")
	}
	return res, perms, nil
}
