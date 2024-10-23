package main

import (
	"testing"

	"github.com/jech/galene/group"
)

func TestMakePassword(t *testing.T) {
	doit := func(pw group.Password) {
		ok, _ := pw.Match("secret")
		if !ok {
			t.Errorf("%v didn't match", pw)
		}
		ok, _ = pw.Match("notsecret")
		if ok {
			t.Errorf("%v did match", pw)
		}
	}
	pw, err := makePassword("secret", "pbkdf2", 4096, 32, 8, 0)
	if err != nil {
		t.Errorf("PBKDF2: %v", err)
	}
	doit(pw)

	pw, err = makePassword("secret", "bcrypt", 0, 0, 0, 10)
	if err != nil {
		t.Errorf("bcrypt: %v", err)
	}
	doit(pw)

	pw, err = makePassword("", "wildcard", 0, 0, 0, 0)
	if err != nil {
		t.Errorf("Wildcard: %v", err)
	}
	ok, _ := pw.Match("notsecretatall")
	if !ok {
		t.Errorf("Wildcard didn't match")
	}
}
