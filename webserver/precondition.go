package webserver

import (
	"net/http"
	"strings"
)

// This is partly based on the Go standard library file net/http/fs.go.

// scanETag determines if a syntactically valid ETag is present at s. If so,
// the ETag and remaining text after consuming ETag is returned. Otherwise,
// it returns "", "".
func scanETag(s string) (etag string, remain string) {
	s = strings.TrimLeft(s, " \t\n\r")
	start := 0
	if strings.HasPrefix(s, "W/") {
		start = 2
	}
	if len(s[start:]) < 2 || s[start] != '"' {
		return "", ""
	}
	// ETag is either W/"text" or "text".
	// See RFC 7232 2.3.
	for i := start + 1; i < len(s); i++ {
		c := s[i]
		switch {
		// Character values allowed in ETags.
		case c == 0x21 || c >= 0x23 && c <= 0x7E || c >= 0x80:
		case c == '"':
			return s[:i+1], s[i+1:]
		default:
			return "", ""
		}
	}
	return "", ""
}

func etagMatch(etag, header string) bool {
	if header == "" {
		return false
	}
	if header == etag {
		return true
	}

	for {
		header = strings.TrimLeft(header, " \t\n\r")
		if len(header) == 0 {
			break
		}
		if header[0] == ',' {
			header = header[1:]
			continue
		}
		if header[0] == '*' {
			return true
		}
		e, remain := scanETag(header)
		if e == "" {
			break
		}
		if e == etag {
			return true
		}
		header = remain
	}

	return false
}

func writeNotModified(w http.ResponseWriter) {
	// RFC 7232 section 4.1:
	// a sender SHOULD NOT generate representation metadata other than the
	// above listed fields unless said metadata exists for the purpose of
	// guiding cache updates (e.g., Last-Modified might be useful if the
	// response does not have an ETag field).
	h := w.Header()
	delete(h, "Content-Type")
	delete(h, "Content-Length")
	delete(h, "Content-Encoding")
	if h.Get("Etag") != "" {
		delete(h, "Last-Modified")
	}
	w.WriteHeader(http.StatusNotModified)
}

// checkPreconditions evaluates request preconditions and reports whether a precondition
// resulted in sending StatusNotModified or StatusPreconditionFailed.
func checkPreconditions(w http.ResponseWriter, r *http.Request, etag string) (done bool) {
	// RFC 7232 section 6.
	im := r.Header.Get("If-Match")
	if im != "" && !etagMatch(etag, im) {
		w.WriteHeader(http.StatusPreconditionFailed)
		return true
	}
	inm := r.Header.Get("If-None-Match")
	if inm != "" && etagMatch(etag, r.Header.Get("If-None-Match")) {
		if r.Method == "GET" || r.Method == "HEAD" {
			writeNotModified(w)
			return true
		} else {
			w.WriteHeader(http.StatusPreconditionFailed)
			return true
		}
	}

	return false
}
