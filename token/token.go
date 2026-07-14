package token

type Token interface {
	Check(host, group string) (string, []string, error)
	NeedsUsername() bool
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

	s, _, err := Get(token)
	return s, err
}
