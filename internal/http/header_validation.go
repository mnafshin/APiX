package httputil

import (
	"net/textproto"
	"strings"
)

// IsValidHeaderName reports whether name is a valid HTTP header field-name
// per RFC 7230 token rule (tchar).
func IsValidHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		// token characters are ASCII; check for CTL or separators.
		if r < 0x21 || r > 0x7E {
			return false
		}
		// separators per RFC7230: ()<>@,;:\"/[]?={} \t
		switch r {
		case '(', ')', '<', '>', '@', ',', ';', ':', '\\', '"', '/', '[', ']', '?', '=', '{', '}', ' ', '\t':
			return false
		}
	}
	return true
}

// IsValidHeaderValue performs a conservative validation for header values.
// It ensures there are no control characters except HTAB (0x09) and allows
// quoted-pairs. This is intentionally permissive to avoid rejecting valid
// user-supplied headers while blocking obvious binary/ctl injection.
func IsValidHeaderValue(v string) bool {
	if v == "" {
		return true
	}
	// Trim optional obs-fold (deprecated) whitespace
	v = strings.Trim(v, "\r\n")
	for _, r := range v {
		if r == '\t' {
			continue
		}
		if r < 0x20 || r == 0x7F {
			return false
		}
	}
	return true
}

// CanonicalHeader returns the canonical MIME header key as used in net/http.
// Delegates to textproto.CanonicalMIMEHeaderKey but rejects invalid names.
func CanonicalHeader(name string) (string, bool) {
	if !IsValidHeaderName(name) {
		return "", false
	}
	return textproto.CanonicalMIMEHeaderKey(name), true
}
