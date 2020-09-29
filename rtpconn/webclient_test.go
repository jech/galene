package rtpconn

import (
	"testing"
	"time"
)

func TestJSTime(t *testing.T) {
	tm := time.Now()
	js := toJSTime(tm)
	tm2 := fromJSTime(js)
	js2 := toJSTime(tm2)

	if js != js2 {
		t.Errorf("%v != %v", js, js2)
	}

	delta := tm.Sub(tm2)
	if delta < -time.Millisecond/2 || delta > time.Millisecond/2 {
		t.Errorf("Delta %v, %v, %v", delta, tm, tm2)
	}
}
