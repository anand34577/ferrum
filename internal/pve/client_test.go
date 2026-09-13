package pve

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStatusErrorMessage(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "field errors map",
			body: `{"data":null,"errors":{"vmid":"value does not match the regex pattern"}}`,
			want: "vmid: value does not match the regex pattern",
		},
		{
			name: "multiple field errors sorted by key",
			body: `{"data":null,"errors":{"vmid":"bad vmid","node":"bad node"}}`,
			want: "node: bad node; vmid: bad vmid",
		},
		{
			name: "top-level message",
			body: `{"data":null,"message":"storage is not shared"}`,
			want: "storage is not shared",
		},
		{
			name: "not JSON — fall back to raw body",
			body: "502 Bad Gateway",
			want: "502 Bad Gateway",
		},
		{
			name: "JSON with neither field — fall back to raw body",
			body: `{"data":null}`,
			want: `{"data":null}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := &StatusError{Method: "POST", Path: "/nodes/pve1/qemu", StatusCode: 400, Body: tc.body}
			if got := e.Message(); got != tc.want {
				t.Errorf("Message() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestFingerprintMatches covers the pin comparison: case- and colon-insensitive,
// and an empty configured pin never matches (pinning is opt-in).
func TestFingerprintMatches(t *testing.T) {
	cases := []struct {
		name       string
		configured string
		got        string
		want       bool
	}{
		{"exact match", "aabbcc", "aabbcc", true},
		{"case-insensitive", "AABBCC", "aabbcc", true},
		{"colons on both sides", "AA:BB:CC", "aa:bb:cc", true},
		{"colons one side only", "AA:BB:CC", "aabbcc", true},
		{"mismatch", "aabbcc", "aabbcd", false},
		{"empty pin never matches", "", "aabbcc", false},
		{"garbage pin never matches", "::::", "aabbcc", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := fingerprintMatches(tc.configured, tc.got); got != tc.want {
				t.Errorf("fingerprintMatches(%q, %q) = %v, want %v", tc.configured, tc.got, got, tc.want)
			}
		})
	}
}

// TestFingerprintPinning drives a real TLS handshake against an httptest
// server: the matching pin (spelled with colons + uppercase, as an admin
// would paste it) must connect, and a wrong pin must fail the request with
// the mismatch error instead of falling back to "unexpected EOF"-style noise.
func TestFingerprintPinning(t *testing.T) {
	var called bool
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":{"version":"8.3.2"}}`)
	}))
	defer srv.Close()

	want := certFingerprintSHA256(srv.Certificate().Raw)
	if got := normalizeFingerprint("AA:BB"); got != "aabb" { // sanity: normalization used below
		t.Fatalf("normalizeFingerprint sanity: got %q", got)
	}

	// Matching pin — accept, despite the server's cert being untrusted by
	// the system pool (that's the point of pinning).
	c := clientFor(srv, WithFingerprint(strings.ToUpper(want[:2])+":"+want[2:]))
	if _, err := c.Version(context.Background()); err != nil {
		t.Fatalf("Version with matching fingerprint: %v", err)
	}
	if !called {
		t.Fatal("request never reached the server")
	}

	// Wrong pin — reject with the explicit mismatch error.
	called = false
	wrong := clientFor(srv, WithFingerprint("0011223344556677889900aabbccddeeff00112233445566778899aabbccddeeff"))
	_, err := wrong.Version(context.Background())
	if err == nil || !strings.Contains(err.Error(), "fingerprint mismatch") {
		t.Fatalf("expected fingerprint mismatch error, got: %v", err)
	}
	if called {
		t.Error("handler ran despite pinned-certificate rejection (reject must happen during the handshake)")
	}

	// No pin — empty WithFingerprint is a no-op, not a pin against "".
	none := clientFor(srv, WithFingerprint(""))
	if _, err := none.Version(context.Background()); err == nil {
		t.Fatal("expected self-signed cert to fail verification when no pin and no skip-verify")
	}
}
