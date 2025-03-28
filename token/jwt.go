package token

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/base64"
	"errors"
	"math/big"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type JWT jwt.Token

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

func ParseKey(key map[string]any) (any, error) {
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
		switch alg {
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

func ParseKeys(keys []map[string]any, alg, kid string) ([]jwt.VerificationKey, error) {
	ks := make([]jwt.VerificationKey, 0, len(keys))
	for _, ky := range keys {
		// return all keys if alg and kid are not specified
		if alg != "" && ky["alg"] != alg {
			continue
		}
		if kid != "" && ky["kid"] != kid {
			continue
		}
		k, err := ParseKey(ky)
		if err != nil {
			return nil, err
		}
		ks = append(ks, k)
	}
	return ks, nil
}

func toStringArray(a interface{}) ([]string, bool) {
	aa, ok := a.([]interface{})
	if !ok {
		return nil, false
	}

	b := make([]string, len(aa))
	for i, v := range aa {
		w, ok := v.(string)
		if !ok {
			return nil, false
		}
		b[i] = w
	}
	return b, true
}

// parseJWT tries to parse a string as a JWT.
// It returns (nil, nil) if the string does not look like a JWT.
func parseJWT(token string, keys []map[string]any) (*JWT, error) {
	t, err := jwt.Parse(
		token,
		func(t *jwt.Token) (any, error) {
			alg, _ := t.Header["alg"].(string)
			if alg == "" {
				return nil, errors.New("alg not found")
			}
			kid, _ := t.Header["kid"].(string)
			ks, err := ParseKeys(keys, alg, kid)
			if err != nil {
				return nil, err
			}
			if len(ks) == 1 {
				return ks[0], nil
			}
			return jwt.VerificationKeySet{Keys: ks}, nil
		},
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(5*time.Second),
	)
	if err != nil {
		if errors.Is(err, jwt.ErrTokenMalformed) {
			// assume this is not a JWT
			return nil, nil
		}
		return nil, err
	}
	return (*JWT)(t), nil
}

func (token *JWT) Check(host, group string, username *string) (string, []string, error) {
	sub, err := token.Claims.GetSubject()
	if err != nil {
		return "", nil, err
	}
	// we accept tokens with a different username from the one provided,
	// and use the token's 'sub' field to override the username

	aud, err := token.Claims.GetAudience()
	if err != nil {
		return "", nil, err
	}
	ok := false
	for _, u := range aud {
		url, err := url.Parse(u)
		if err != nil {
			continue
		}
		// if canonicalHost is not set, we allow tokens
		// for any domain name.  Hopefully different
		// servers use distinct keys.
		if host != "" {
			if !strings.EqualFold(url.Host, host) {
				continue
			}
		}
		if url.Path == path.Join("/group", group)+"/" {
			ok = true
			break
		}
	}
	if !ok {
		return "", nil, errors.New("token for wrong group")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "", nil, errors.New("unexpected type for token")
	}

	var perms []string
	if p, ok := claims["permissions"]; ok && p != nil {
		perms, ok = toStringArray(p)
		if !ok {
			return "", nil, errors.New("invalid 'permissions' field")
		}
	}

	return sub, perms, nil
}
