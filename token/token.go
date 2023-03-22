package token

import (
	"errors"
)

var ErrUsernameRequired = errors.New("username required")

type Token interface {
	Check(host, group string, username *string) (string, []string, error)
}

func Parse(token string, keys []map[string]interface{}) (Token, error) {
	t, err := getStateful(token)
	if err == nil && t != nil {
		return t, nil
	}
	return parseJWT(token, keys)
}
