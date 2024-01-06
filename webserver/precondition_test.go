package webserver

import (
	"net/http"
	"testing"
)

type testWriter struct {
	statusCode int
}

func (w *testWriter) Header() http.Header {
	return nil
}

func (w *testWriter) Write(buf []byte) (int, error) {
	return len(buf), nil
}

func (w *testWriter) WriteHeader(statusCode int) {
	if w.statusCode != 0 {
		panic("WriteHeader called twice")
	}
	w.statusCode = statusCode
}

func TestEtagMatch(t *testing.T) {
	type tst struct {
		etag, header string
	}

	var match = []tst{
		{`"foo"`, `"foo"`},
		{`"foo"`, ` "foo"`},
		{`"foo"`, `"foo" `},
		{`"foo"`, ` "foo" `},
		{`"foo"`, `"foo", "bar"`},
		{`"foo"`, `"bar", "foo"`},
		{`W/"foo"`, `W/"foo"`},
	}

	var mismatch = []tst{
		{``, ``},
		{`"foo"`, `"bar"`},
		{`"foo"`, `"bar", "baz"`},
		{`"foo"`, `"baz", "bar"`},
		{`"foo"`, `W/"foo"`},
		{`W/"foo"`, `"foo"`},
	}

	for _, tst := range match {
		m := etagMatch(tst.etag, tst.header)
		if !m {
			t.Errorf("%v %v: got %v, expected true",
				tst.etag, tst.header, m,
			)
		}
	}

	for _, tst := range mismatch {
		m := etagMatch(tst.etag, tst.header)
		if m {
			t.Errorf("%v %v: got %v, expected false",
				tst.etag, tst.header, m,
			)
		}
	}
}

func TestCheckPreconditions(t *testing.T) {
	var tests = []struct {
		method, etag, im, inm string
		result                int
	}{
		{"GET", ``, ``, ``, 0},
		{"GET", ``, `*`, ``, 0},
		{"GET", ``, ``, `*`, 304},
		{"POST", ``, ``, `*`, 412},
		{"GET", `"123"`, ``, ``, 0},
		{"GET", `"123"`, `"123"`, ``, 0},
		{"GET", `"123"`, `"124"`, ``, 412},
		{"POST", `"123"`, `"124"`, ``, 412},
		{"GET", `"123"`, `*`, ``, 0},
		{"GET", `"123"`, ``, `"123"`, 304},
		{"POST", `"123"`, ``, `"123"`, 412},
		{"GET", `"123"`, ``, `"124"`, 0},
		{"GET", `"123"`, ``, `*`, 304},
	}

	for _, tst := range tests {
		var w testWriter
		h := make(http.Header)
		if tst.im != "" {
			h.Set("If-Match", tst.im)
		}
		if tst.inm != "" {
			h.Set("If-None-Match", tst.inm)
		}
		r := http.Request{
			Method: tst.method,
			Header: h,
		}
		done := checkPreconditions(&w, &r, tst.etag)
		if done != (tst.result != 0) || w.statusCode != tst.result {
			t.Errorf("%v %v: got %v, expected %v",
				tst.etag, tst.im, w.statusCode, tst.result)
		}
	}
}
