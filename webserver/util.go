package webserver

import (
	"slices"
	"strings"

	"github.com/jech/galene/group"
	"github.com/jech/galene/token"
)

func parseBearerToken(auth string) string {
	auths := strings.Split(auth, ",")
	for _, a := range auths {
		a = strings.Trim(a, " \t")
		s := strings.Split(a, " ")
		if len(s) == 2 && strings.EqualFold(s[0], "bearer") {
			return s[1]
		}
	}
	return ""
}

func checkGlobalAdminToken(tok string) (bool, error) {
	t, err := token.Parse(tok, nil)
	if err != nil || t == nil {
		return false, err
	}

	conf, err := group.GetConfiguration()
	if err != nil {
		return false, err
	}

	_, perms, err := t.Check(conf.CanonicalHost, "")
	if err != nil {
		return false, err
	}

	return slices.Contains(perms, "admin"), nil
}
