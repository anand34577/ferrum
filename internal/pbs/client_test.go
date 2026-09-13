package pbs

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// clientFor points a pbs.Client at an httptest TLS server, skipping cert
// verification (the server's self-signed like an out-of-the-box PBS host).
func clientFor(srv *httptest.Server, opts ...Option) *Client {
	hostPort := strings.TrimPrefix(srv.URL, "https://")
	host, portStr, _ := strings.Cut(hostPort, ":")
	port, _ := strconv.Atoi(portStr)
	return New(host, port, opts...)
}

func TestStatusErrorMessage(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "top-level message",
			body: `{"data":null,"message":"datastore 'x' does not exist"}`,
			want: "datastore 'x' does not exist",
		},
		{
			name: "not JSON — fall back to raw body",
			body: "502 Bad Gateway",
			want: "502 Bad Gateway",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := &StatusError{Method: "GET", Path: "/admin/datastore", StatusCode: 400, Body: tc.body}
			if got := e.Message(); got != tc.want {
				t.Errorf("Message() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestWithAPIToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"version":"3.2","release":"1"}}`))
	}))
	defer srv.Close()

	c := clientFor(srv, WithInsecureSkipVerify(true)).WithAPIToken("user@pbs!token", "secret-value")
	v, err := c.Version(context.Background())
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if v.Version != "3.2" {
		t.Errorf("version = %q", v.Version)
	}
	want := "PBSAPIToken=user@pbs!token:secret-value"
	if gotAuth != want {
		t.Errorf("Authorization header = %q, want %q", gotAuth, want)
	}
}

func TestListDatastores(t *testing.T) {
	// GET /admin/datastore returns DataStoreListItem rows — store, comment,
	// maintenance — and nothing else; capacity lives on the per-store status
	// endpoint (see TestDatastoreStatus).
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api2/json/admin/datastore" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"store":"backups","comment":"main store","maintenance":"read-only"},{"store":"archive"}]}`))
	}))
	defer srv.Close()

	c := clientFor(srv, WithInsecureSkipVerify(true)).WithAPIToken("u@pbs!t", "s")
	stores, err := c.ListDatastores(context.Background())
	if err != nil {
		t.Fatalf("ListDatastores: %v", err)
	}
	if len(stores) != 2 || stores[0].Store != "backups" || stores[1].Store != "archive" {
		t.Fatalf("got %+v", stores)
	}
	if stores[0].Comment != "main store" || stores[0].Maintenance != "read-only" {
		t.Fatalf("comment/maintenance not decoded: %+v", stores[0])
	}
	// Usage fields must stay empty — the list endpoint never carries them.
	if stores[0].Total != 0 || stores[0].Used != 0 || stores[0].Avail != 0 || stores[0].GCStatus != nil {
		t.Fatalf("usage decoded from a list response that cannot carry it: %+v", stores[0])
	}
}

func TestDatastoreStatus(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api2/json/admin/datastore/backups/status" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"total":1000,"used":200,"avail":800,"gc-status":{"upid":"UPID:pbs:...","index-file-count":12,"disk-bytes":200,"disk-chunks":9,"still-bad":2},"counts":{"vm":{"groups":3,"snapshots":41}}}}`))
	}))
	defer srv.Close()

	c := clientFor(srv, WithInsecureSkipVerify(true)).WithAPIToken("u@pbs!t", "s")
	st, err := c.DatastoreStatus(context.Background(), "backups")
	if err != nil {
		t.Fatalf("DatastoreStatus: %v", err)
	}
	if st.Total != 1000 || st.Used != 200 || st.Avail != 800 {
		t.Fatalf("usage not decoded: %+v", st)
	}
	if st.GCStatus == nil || st.GCStatus.StillBad != 2 || st.GCStatus.IndexFileCount != 12 {
		t.Fatalf("gc-status not decoded: %+v", st.GCStatus)
	}
	if st.Counts == nil || st.Counts.VM == nil || st.Counts.VM.Groups != 3 || st.Counts.VM.Snapshots != 41 {
		t.Fatalf("counts not decoded: %+v", st.Counts)
	}
}

func TestListDatastoresWithUsage(t *testing.T) {
	var mu sync.Mutex
	statusCalls := map[string]int{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api2/json/admin/datastore":
			_, _ = w.Write([]byte(`{"data":[{"store":"good"},{"store":"broken"}]}`))
		case "/api2/json/admin/datastore/good/status":
			mu.Lock()
			statusCalls["good"]++
			mu.Unlock()
			_, _ = w.Write([]byte(`{"data":{"total":100,"used":10,"avail":90,"gc-status":{"disk-bytes":10}}}`))
		case "/api2/json/admin/datastore/broken/status":
			mu.Lock()
			statusCalls["broken"]++
			mu.Unlock()
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"data":null,"message":"datastore 'broken' is in maintenance mode"}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := clientFor(srv, WithInsecureSkipVerify(true)).WithAPIToken("u@pbs!t", "s")
	stores, err := c.ListDatastoresWithUsage(context.Background())
	if err != nil {
		t.Fatalf("ListDatastoresWithUsage: %v", err)
	}
	// The listing survives one store's status failure, and each store was
	// probed exactly once.
	if len(stores) != 2 {
		t.Fatalf("got %d stores, want 2", len(stores))
	}
	if len(statusCalls) != 2 || statusCalls["good"] != 1 || statusCalls["broken"] != 1 {
		t.Fatalf("status fan-out calls wrong: %v", statusCalls)
	}
	good := stores[0]
	if good.Store != "good" || good.Total != 100 || good.Used != 10 || good.Avail != 90 {
		t.Fatalf("usage not attached to the healthy store: %+v", good)
	}
	if good.GCStatus == nil || good.GCStatus.DiskBytes != 10 || good.Error != "" {
		t.Fatalf("gc-status/error wrong on the healthy store: %+v", good)
	}
	broken := stores[1]
	if broken.Store != "broken" {
		t.Fatalf("got %+v", broken)
	}
	if broken.Error == "" || !strings.Contains(broken.Error, "maintenance mode") {
		t.Fatalf("per-store error not attached: %+v", broken)
	}
	if broken.Total != 0 || broken.GCStatus != nil {
		t.Fatalf("failed store must not carry stale usage: %+v", broken)
	}
}

func TestPruneDryRun(t *testing.T) {
	var gotForm string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		_ = r.ParseForm()
		gotForm = r.Form.Encode()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"backup-time":1700000000,"keep":true},{"backup-time":1600000000,"keep":false}]}`))
	}))
	defer srv.Close()

	c := clientFor(srv, WithInsecureSkipVerify(true)).WithAPIToken("u@pbs!t", "s")
	results, err := c.Prune(context.Background(), "backups", PruneOptions{
		BackupType: "vm", BackupID: "100", KeepLast: 3, DryRun: true,
	})
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(results) != 2 || results[1].Keep {
		t.Fatalf("got %+v", results)
	}
	if !strings.Contains(gotForm, "dry-run=1") || !strings.Contains(gotForm, "keep-last=3") {
		t.Fatalf("form missing expected fields: %s", gotForm)
	}
}

func TestGCStatusUnauthorized(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"permission denied"}`))
	}))
	defer srv.Close()

	c := clientFor(srv, WithInsecureSkipVerify(true)).WithAPIToken("u@pbs!t", "s")
	_, err := c.GCStatusFor(context.Background(), "backups")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized in the chain, got: %v", err)
	}
}

func TestTaskLogOrder(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"n":1,"t":"starting gc"},{"n":2,"t":"done"}]}`))
	}))
	defer srv.Close()

	c := clientFor(srv, WithInsecureSkipVerify(true)).WithAPIToken("u@pbs!t", "s")
	lines, err := c.TaskLog(context.Background(), "UPID:pbs:...")
	if err != nil {
		t.Fatalf("TaskLog: %v", err)
	}
	if len(lines) != 2 || lines[0] != "starting gc" || lines[1] != "done" {
		t.Fatalf("got %v", lines)
	}
}

func TestNamespaceQueryParam(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("ns"); got != "prod" {
			t.Errorf("ns query = %q, want prod", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c := clientFor(srv, WithInsecureSkipVerify(true)).WithAPIToken("u@pbs!t", "s")
	if _, err := c.ListGroups(context.Background(), "backups", "prod"); err != nil {
		t.Fatalf("ListGroups: %v", err)
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
		_, _ = w.Write([]byte(`{"data":{"version":"3.2.4"}}`))
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

	// A fingerprint wins over skip-verify: the wrong pin must still reject.
	both := clientFor(srv, WithInsecureSkipVerify(true), WithFingerprint("0011223344556677889900aabbccddeeff00112233445566778899aabbccddeeff"))
	if _, err := both.Version(context.Background()); err == nil || !strings.Contains(err.Error(), "fingerprint mismatch") {
		t.Fatalf("expected fingerprint to win over skip-verify, got: %v", err)
	}
}

// TestIsNotAvailable covers the "this PBS doesn't have that feature" shapes:
// 501 outright, or the 400/500 "unknown parameter"/"no such method"
// complaints pre-2.2 servers produce on the namespace endpoints.
func TestIsNotAvailable(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{"501 unimplemented", http.StatusNotImplemented, `{"data":null}`, true},
		{"400 unknown parameter", http.StatusBadRequest, `{"data":null,"message":"parameter verification failed","errors":{"ns":"unknown parameter ns"}}`, true},
		{"500 no such method", http.StatusInternalServerError, `{"data":null,"message":"no such method"}`, true},
		{"400 wrong input", http.StatusBadRequest, `{"data":null,"message":"datastore 'x' does not exist"}`, false},
		{"404 missing route", http.StatusNotFound, `{"data":null,"message":"404 Not Found"}`, false},
		{"500 real failure", http.StatusInternalServerError, `{"data":null,"message":"io error"}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isNotAvailable(tc.status, tc.body); got != tc.want {
				t.Errorf("isNotAvailable(%d, %q) = %v, want %v", tc.status, tc.body, got, tc.want)
			}
		})
	}
}

// TestNotAvailableDegradation checks that namespace queries on a PBS too old
// to know them degrade to "no namespaces"/"no filtered groups" — while real
// failures (unknown store → 404) still surface.
func TestNotAvailableDegradation(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/datastore/no-such-store/"):
			http.NotFound(w, r) // bad store name — a real error, not "feature missing"
		case strings.HasSuffix(r.URL.Path, "/namespace"):
			w.WriteHeader(http.StatusNotImplemented)
			_, _ = w.Write([]byte(`{"data":null}`))
		case strings.HasSuffix(r.URL.Path, "/groups"):
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"data":null,"message":"parameter verification failed","errors":{"ns":"unknown parameter ns"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := clientFor(srv, WithInsecureSkipVerify(true)).WithAPIToken("u@pbs!t", "s")
	ctx := context.Background()

	ns, err := c.ListNamespaces(ctx, "backups", "")
	if err != nil || ns != nil {
		t.Fatalf("ListNamespaces on pre-2.2 PBS: got (%v, %v), want (nil, nil)", ns, err)
	}
	groups, err := c.ListGroups(ctx, "backups", "prod")
	if err != nil || groups != nil {
		t.Fatalf("ListGroups(ns) on pre-2.2 PBS: got (%v, %v), want (nil, nil)", groups, err)
	}
	if _, err := c.ListGroups(ctx, "backups", ""); err == nil {
		t.Fatal("unfiltered ListGroups on a real failure must still error")
	}
	if _, err := c.ListNamespaces(ctx, "no-such-store", ""); err == nil {
		t.Fatal("404 (bad store name) must not be swallowed as not-available")
	}
}

func TestLoginRealmHint(t *testing.T) {
	c := New("localhost", 8007)
	err := c.Login(context.Background(), "root", "secret")
	if err == nil || !strings.Contains(err.Error(), "root@pam") {
		t.Fatalf("expected realm hint naming root@pam, got: %v", err)
	}
}

func TestSnapshotVerificationTyped(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[
			{"backup-type":"vm","backup-id":"100","backup-time":1700000000,"verification":{"upid":"UPID:pbs:...","state":"ok"},"size":123},
			{"backup-type":"vm","backup-id":"101","backup-time":1700000100,"verification":{"state":"failed","upid":"UPID:pbs:..."}},
			{"backup-type":"vm","backup-id":"102","backup-time":1700000200}
		]}`))
	}))
	defer srv.Close()

	c := clientFor(srv, WithInsecureSkipVerify(true)).WithAPIToken("u@pbs!t", "s")
	snaps, err := c.ListSnapshots(context.Background(), "backups", ListSnapshotsOptions{})
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(snaps) != 3 {
		t.Fatalf("got %d snapshots, want 3", len(snaps))
	}
	if snaps[0].Verification == nil || snaps[0].Verification.State != "ok" || snaps[0].Verification.UPID != "UPID:pbs:..." {
		t.Fatalf("verification not decoded: %+v", snaps[0].Verification)
	}
	if snaps[1].Verification == nil || snaps[1].Verification.State != "failed" {
		t.Fatalf("failed verification not decoded: %+v", snaps[1].Verification)
	}
	if snaps[2].Verification != nil {
		t.Fatalf("unverified snapshot must decode to nil, got %+v", snaps[2].Verification)
	}
}
