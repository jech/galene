package token

import (
	"errors"
)

var ErrUsernameRequired = errors.New("username required")

type Token interface {
	Check(host, group string, username *string) (string, []string, error)
}

func Parse(token string, keys []map[string]interface{}) (Token, error) {
	// both getStateful and parseJWT may return nil, which we
	// shouldn't cast into an interface before testing for nil.
	jwt, err := parseJWT(token, keys)
	if err != nil {
		// parses correctly but doesn't validate
		return nil, err
	}
	if jwt != nil {
		return jwt, nil
	}

	return Get(token)
}
