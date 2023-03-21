package token

type Token interface {
	Check(host, group string, username *string) (string, []string, error)
}

func Parse(token string, keys []map[string]interface{}) (Token, error) {
	return parseJWT(token, keys)
}
