package pve

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCreateSDNControllerForm verifies the POST body PVE's
// /cluster/sdn/controllers endpoint receives: the id is carried by the
// "controller" form key (what Controllers.pm's create handler extracts via
// extract_param), alongside the type-specific opts.
func TestCreateSDNControllerForm(t *testing.T) {
	var gotPath string
	var gotForm map[string][]string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = r.ParseForm()
		gotForm = map[string][]string(r.Form)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := clientFor(srv, WithInsecureSkipVerify(true))
	err := c.CreateSDNController(context.Background(), "evpn-ctrl", map[string]string{
		"type": "evpn",
		"asn":  "65000",
	})
	if err != nil {
		t.Fatalf("CreateSDNController: %v", err)
	}
	if !strings.HasSuffix(gotPath, "/cluster/sdn/controllers") {
		t.Errorf("path = %q", gotPath)
	}
	want := map[string]string{
		"controller": "evpn-ctrl",
		"type":       "evpn",
		"asn":        "65000",
	}
	for k, v := range want {
		if got := firstOrEmpty(gotForm[k]); got != v {
			t.Errorf("form[%q] = %q, want %q", k, got, v)
		}
	}
	if _, ok := gotForm["id"]; ok {
		t.Error("form contains legacy \"id\" key — PVE's create handler expects \"controller\"")
	}
}

// TestCreateSDNIPAMForm verifies the POST body PVE's /cluster/sdn/ipams
// endpoint receives: the id is carried by the "ipam" form key (what
// Ipams.pm's create handler extracts via extract_param).
func TestCreateSDNIPAMForm(t *testing.T) {
	var gotPath string
	var gotForm map[string][]string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = r.ParseForm()
		gotForm = map[string][]string(r.Form)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := clientFor(srv, WithInsecureSkipVerify(true))
	err := c.CreateSDNIPAM(context.Background(), "netbox1", map[string]string{
		"type": "netbox",
		"url":  "https://netbox.example",
	})
	if err != nil {
		t.Fatalf("CreateSDNIPAM: %v", err)
	}
	if !strings.HasSuffix(gotPath, "/cluster/sdn/ipams") {
		t.Errorf("path = %q", gotPath)
	}
	want := map[string]string{
		"ipam": "netbox1",
		"type": "netbox",
		"url":  "https://netbox.example",
	}
	for k, v := range want {
		if got := firstOrEmpty(gotForm[k]); got != v {
			t.Errorf("form[%q] = %q, want %q", k, got, v)
		}
	}
	if _, ok := gotForm["id"]; ok {
		t.Error("form contains legacy \"id\" key — PVE's create handler expects \"ipam\"")
	}
}
