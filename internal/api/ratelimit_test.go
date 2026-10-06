package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Reconnecting (new source port) or spoofing a leftmost X-Forwarded-For entry
// must not change the key the login limiter sees.
func TestClientIPResistsLockoutBypass(t *testing.T) {
	a := httptest.NewRequest(http.MethodPost, "/", nil)
	a.RemoteAddr = "203.0.113.7:50001"
	b := httptest.NewRequest(http.MethodPost, "/", nil)
	b.RemoteAddr = "203.0.113.7:50002"
	if clientIP(a) != clientIP(b) || clientIP(a) != "203.0.113.7" {
		t.Fatalf("port leaked into key: %q vs %q", clientIP(a), clientIP(b))
	}

	cases := []struct {
		name    string
		headers map[string]string
		want    string
	}{
		{"rightmost XFF wins", map[string]string{"X-Forwarded-For": "1.1.1.1, 198.51.100.9"}, "198.51.100.9"},
		{"X-Real-IP ignored when XFF present", map[string]string{"X-Forwarded-For": "198.51.100.9", "X-Real-IP": "1.1.1.1"}, "198.51.100.9"},
		{"X-Real-IP fallback", map[string]string{"X-Real-IP": "198.51.100.10"}, "198.51.100.10"},
		{"garbage keeps peer", map[string]string{"X-Forwarded-For": "not-an-ip"}, "10.0.0.1"},
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.RemoteAddr = "10.0.0.1:443"
		for k, v := range c.headers {
			r.Header.Set(k, v)
		}
		var got string
		forwardedFor(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = clientIP(r) })).ServeHTTP(httptest.NewRecorder(), r)
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
