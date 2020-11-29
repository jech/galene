package group

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestJSTime(t *testing.T) {
	tm := time.Now()
	js := ToJSTime(tm)
	tm2 := FromJSTime(js)
	js2 := ToJSTime(tm2)

	if js != js2 {
		t.Errorf("%v != %v", js, js2)
	}

	delta := tm.Sub(tm2)
	if delta < -time.Millisecond/2 || delta > time.Millisecond/2 {
		t.Errorf("Delta %v, %v, %v", delta, tm, tm2)
	}
}

func TestDescriptionJSON(t *testing.T) {
	d := `
{
    "op":[{"username": "jch","password": "topsecret"}],
    "max-history-age": 10,
    "allow-subgroups": true,
    "presenter":[
        {"user": "john", "password": "secret"},
        {}
    ]
}`

	var dd description
	err := json.Unmarshal([]byte(d), &dd)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	ddd, err := json.Marshal(dd)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var dddd description
	err = json.Unmarshal([]byte(ddd), &dddd)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !reflect.DeepEqual(dd, dddd) {
		t.Errorf("Got %v, expected %v", dddd, dd)
	}
}
