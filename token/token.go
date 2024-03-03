package token

import (
	"errors"
	"os"
)

var ErrUsernameRequired = errors.New("username required")

type Token interface {
	Check(host, group string, username *string) (string, []string, error)
}

func Parse(token string, keys []map[string]interface{}) (Token, error) {
	// both getStateful and parseJWT may return nil, which we
	// shouldn't cast into an interface.  Be very careful.
	s, err1 := getStateful(token)
	if err1 == nil && s != nil {
		return s, nil
	}

	jwt, err2 := parseJWT(token, keys)
	if err2 == nil && jwt != nil {
		return jwt, nil
	}

	if err1 != nil {
		return nil, err1
	} else if err2 != nil {
		return nil, err2
	}
	return nil, os.ErrNotExist
}
