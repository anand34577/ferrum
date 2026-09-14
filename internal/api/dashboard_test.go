package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// TestUpdateDashboardKeepsLayoutVersion locks in a real bug: saving a
// dashboard's widgets used to rebuild the stored layout as just
// {"widgets": ...} with no version key, so it round-tripped as version 0
// once saved. The frontend correctly treats 0 as "unversioned" and re-runs
// its v1->v2 migration (doubling every widget's height/y) on every single
// load after that — a widget's saved size would grow every time the
// dashboard was reopened. A save must always stamp the current
// layoutVersion, regardless of what (if anything) the client sent.
func TestUpdateDashboardKeepsLayoutVersion(t *testing.T) {
	e := newTestEnv(t)
	cookie := e.loginAs(t, "admin", "admin@example.com", "hunter22", true)

	created := decode[dashboardDTOTest](t, e.do(t, http.MethodPost, "/api/v1/dashboards/", map[string]string{"name": "Test"}, cookie))
	if created.Version != layoutVersion {
		t.Fatalf("freshly created dashboard version = %d, want %d", created.Version, layoutVersion)
	}

	widgets := `[{"id":"w1","type":"fleet-overview","x":0,"y":0,"w":12,"h":8}]`
	updated := decode[dashboardDTOTest](t, e.do(t, http.MethodPut, "/api/v1/dashboards/"+created.ID, map[string]any{
		"widgets": json.RawMessage(widgets),
	}, cookie))
	if updated.Version != layoutVersion {
		t.Fatalf("version after update = %d, want %d (widgets would silently double in size on next load)", updated.Version, layoutVersion)
	}
	if len(updated.Widgets) != 1 || updated.Widgets[0].H != 8 {
		t.Fatalf("widgets not saved as sent: %+v", updated.Widgets)
	}

	// Re-fetching (simulating the next time the dashboard is opened) must
	// still report the real version and the same, unchanged height.
	refetched := decode[dashboardDTOTest](t, e.get(t, "/api/v1/dashboards/"+created.ID, cookie))
	if refetched.Version != layoutVersion {
		t.Fatalf("version on reload = %d, want %d", refetched.Version, layoutVersion)
	}
	if len(refetched.Widgets) != 1 || refetched.Widgets[0].H != 8 {
		t.Fatalf("widget height changed across reload: %+v", refetched.Widgets)
	}
}

// dashboardDTOTest mirrors dashboardDTO's actual wire shape (its MarshalJSON
// output), which the real type can't be decoded back into directly.
type dashboardDTOTest struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version int    `json:"version"`
	Widgets []struct {
		ID string `json:"id"`
		H  int    `json:"h"`
	} `json:"widgets"`
}
