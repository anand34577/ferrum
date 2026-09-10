package pbs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
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
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api2/json/admin/datastore" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"store":"backups","total":1000,"used":200,"avail":800,"gc-status":{"status":"ok"}}]}`))
	}))
	defer srv.Close()

	c := clientFor(srv, WithInsecureSkipVerify(true)).WithAPIToken("u@pbs!t", "s")
	stores, err := c.ListDatastores(context.Background())
	if err != nil {
		t.Fatalf("ListDatastores: %v", err)
	}
	if len(stores) != 1 || stores[0].Store != "backups" {
		t.Fatalf("got %+v", stores)
	}
	if stores[0].GCStatus == nil || stores[0].GCStatus.Status != "ok" {
		t.Fatalf("gc status not decoded: %+v", stores[0].GCStatus)
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
	if !IsUnauthorized(err) {
		t.Fatalf("expected IsUnauthorized, got: %v", err)
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
