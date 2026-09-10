package pve

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// pve_addr is a string (host/IP), not a number — declaring it int made any
// response with a populated pve_addr fail to unmarshal (json: cannot
// unmarshal string into Go struct field ClusterConfigNode.data.pve_addr of
// type int), which 502'd the whole "Members" list on ClusterPage.
func TestClusterConfigNodesPveAddrIsString(t *testing.T) {
	raw := `{"data":[{"name":"pve1","nodeid":1,"quorum_votes":1,"pve_addr":"10.0.0.5"}]}`
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(raw))
	}))
	defer srv.Close()

	c := clientFor(srv, WithInsecureSkipVerify(true))
	nodes, err := c.ClusterConfigNodes(context.Background())
	if err != nil {
		t.Fatalf("ClusterConfigNodes: %v", err)
	}
	if len(nodes) != 1 || nodes[0].PveAddr != "10.0.0.5" {
		t.Errorf("nodes = %+v", nodes)
	}
}
