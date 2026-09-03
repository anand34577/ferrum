package pve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// clientFor points a pve.Client at an httptest TLS server, skipping cert
// verification (the server's self-signed like an out-of-the-box PVE host).
func clientFor(srv *httptest.Server, opts ...Option) *Client {
	hostPort := strings.TrimPrefix(srv.URL, "https://")
	host, portStr, _ := strings.Cut(hostPort, ":")
	port, _ := strconv.Atoi(portStr)
	return New(host, port, opts...)
}

// PVE renders cpuinfo.mhz as a string on some versions and a bare number on
// others — both must decode, and the numeric form must not fail the whole
// node-status request (seen live: "cannot unmarshal number into Go struct
// field .data.cpuinfo.mhz of type string").
func TestNodeStatusMHzStringOrNumber(t *testing.T) {
	for name, raw := range map[string]string{
		"string": `{"data":{"cpuinfo":{"model":"Xeon","cores":8,"sockets":1,"mhz":"2400.022"},"kversion":"Linux 6.8"}}`,
		"number": `{"data":{"cpuinfo":{"model":"Xeon","cores":8,"sockets":1,"mhz":2400.022},"kversion":"Linux 6.8"}}`,
		"absent": `{"data":{"cpuinfo":{"model":"Xeon","cores":8,"sockets":1}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(raw))
			}))
			defer srv.Close()

			c := clientFor(srv, WithInsecureSkipVerify(true))
			st, err := c.NodeStatus(context.Background(), "pve1")
			if err != nil {
				t.Fatalf("NodeStatus: %v", err)
			}
			if st.CPUInfo.Cores != 8 {
				t.Errorf("cores = %d", st.CPUInfo.Cores)
			}
			if name == "string" && st.CPUInfo.MHz != "2400.022" {
				t.Errorf("mhz string = %q", st.CPUInfo.MHz)
			}
			if name == "number" && st.CPUInfo.MHz != "2400.022" {
				t.Errorf("mhz numeric should keep the literal text, got %q", st.CPUInfo.MHz)
			}
		})
	}
}

// FlexString must round-trip through re-marshal as a plain JSON string so
// API consumers (the web UI types it as `mhz?: string`) see the same shape.
func TestFlexStringMarshal(t *testing.T) {
	out, err := json.Marshal(struct {
		MHz FlexString `json:"mhz"`
	}{MHz: "2400.022"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(out) != `{"mhz":"2400.022"}` {
		t.Errorf("marshaled %s", out)
	}
}

// Ceph endpoints on a cluster without Ceph answer in three shapes: 500
// "binary not installed", 500 {"data":null} with no message at all, and
// 501 "not implemented" on older PVE. All must classify as
// NotAvailableError; a 500 on a non-Ceph endpoint must stay a real error.
func TestCephUnavailableClassification(t *testing.T) {
	for name, resp := range map[string]struct {
		status int
		body   string
	}{
		"ceph missing":  {http.StatusInternalServerError, `{"message":"binary not installed: /usr/bin/ceph-mon\n"}`},
		"bare 500":      {http.StatusInternalServerError, `{"data":null}`},
		"unimplemented": {http.StatusNotImplemented, `{"message":"Method 'GET /nodes/pve1/ceph/pools' not implemented"}`},
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(resp.status)
				_, _ = w.Write([]byte(resp.body))
			}))
			defer srv.Close()

			c := clientFor(srv, WithInsecureSkipVerify(true))
			_, err := c.CephPools(context.Background(), "pve1")
			if err == nil {
				t.Fatal("expected an error")
			}
			if !IsNotAvailable(err) {
				t.Fatalf("expected NotAvailableError, got: %v", err)
			}
		})
	}

	// A failing non-Ceph endpoint stays a plain error.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"disk failure"}`))
	}))
	defer srv.Close()

	c := clientFor(srv, WithInsecureSkipVerify(true))
	if _, err := c.ClusterResources(context.Background()); err == nil || IsNotAvailable(err) {
		t.Fatalf("non-ceph 500 should not classify as unavailable, got: %v", err)
	}
}
