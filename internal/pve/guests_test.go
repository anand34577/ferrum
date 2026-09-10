package pve

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStripCIDR(t *testing.T) {
	cases := map[string]string{
		"192.168.1.50/24":              "192.168.1.50",
		"fe80::1234:5678:9abc:def0/64": "fe80::1234:5678:9abc:def0",
		"":                             "",
		"no-slash-here":                "no-slash-here",
	}
	for in, want := range cases {
		if got := stripCIDR(in); got != want {
			t.Errorf("stripCIDR(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestRemoteMigrateGuest verifies the form PVE's remote_migrate endpoint
// receives — the target-endpoint string, target vmid/storage/bridge, and
// the online/delete flags — matches what RemoteMigrateOptions describes.
func TestRemoteMigrateGuest(t *testing.T) {
	var gotPath string
	var gotForm map[string][]string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = r.ParseForm()
		gotForm = map[string][]string(r.Form)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":"UPID:pve1:remote-migrate"}`))
	}))
	defer srv.Close()

	c := clientFor(srv, WithInsecureSkipVerify(true))
	upid, err := c.RemoteMigrateGuest(context.Background(), "pve1", 100, RemoteMigrateOptions{
		TargetEndpoint: "apitoken=PVEAPIToken=root@pam!token=secret,host=target.example,port=8006,fingerprint=AA:BB",
		TargetNode:     "pve2",
		TargetVMID:     200,
		TargetStorage:  "local-lvm",
		TargetBridge:   "vmbr1",
		Online:         true,
		DeleteSource:   true,
	})
	if err != nil {
		t.Fatalf("RemoteMigrateGuest: %v", err)
	}
	if upid != "UPID:pve1:remote-migrate" {
		t.Errorf("upid = %q", upid)
	}
	if !strings.HasSuffix(gotPath, "/nodes/pve1/qemu/100/remote_migrate") {
		t.Errorf("path = %q", gotPath)
	}
	want := map[string]string{
		"target-endpoint": "apitoken=PVEAPIToken=root@pam!token=secret,host=target.example,port=8006,fingerprint=AA:BB",
		"target-vmid":     "200",
		"target-storage":  "local-lvm",
		"target-bridge":   "vmbr1",
		"online":          "1",
		"delete":          "1",
	}
	for k, v := range want {
		if got := firstOrEmpty(gotForm[k]); got != v {
			t.Errorf("form[%q] = %q, want %q", k, got, v)
		}
	}
}

func firstOrEmpty(vals []string) string {
	if len(vals) == 0 {
		return ""
	}
	return vals[0]
}

// TestErrLXCRemoteMigrateUnsupported documents that RemoteMigrateGuest is a
// qemu-only capability by convention — callers gate on guest type before
// calling it (see internal/api/guests.go's remoteMigrateGuest), and this
// sentinel error is what they surface for lxc.
func TestErrLXCRemoteMigrateUnsupported(t *testing.T) {
	if ErrLXCRemoteMigrateUnsupported == nil || ErrLXCRemoteMigrateUnsupported.Error() == "" {
		t.Fatal("ErrLXCRemoteMigrateUnsupported must be a non-empty sentinel error")
	}
}
