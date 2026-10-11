package api

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"
)

type auditRow struct {
	Action    string `json:"action"`
	IP        string `json:"ip"`
	CreatedAt string `json:"createdAt"`
}

func seedAudit(t *testing.T, e *testEnv, id, action, createdAt string) {
	t.Helper()
	if _, err := e.db.Exec(
		`INSERT INTO audit_log (id, user_id, action, category, target, ip, created_at) VALUES (?, NULL, ?, 'test', '', '10.0.0.1', ?)`,
		id, action, createdAt,
	); err != nil {
		t.Fatalf("seeding audit entry: %v", err)
	}
}

func actions(rows []auditRow, only string) []string {
	var out []string
	for _, r := range rows {
		if len(r.Action) >= len(only) && r.Action[:len(only)] == only {
			out = append(out, r.Action)
		}
	}
	return out
}

func TestAuditLogDateRangeAndLimit(t *testing.T) {
	e := newTestEnv(t)
	cookie := e.loginAs(t, "admin", "admin@example.com", "correct-horse-battery", true)

	seedAudit(t, e, "a1", "seed.jan", "2026-01-10T12:00:00Z")
	seedAudit(t, e, "a2", "seed.feb", "2026-02-10T12:00:00Z")
	seedAudit(t, e, "a3", "seed.mar", "2026-03-10T12:00:00Z")

	// from is inclusive, to is exclusive.
	rec := e.get(t, "/api/v1/audit?from=2026-02-10T12:00:00Z&to=2026-03-10T12:00:00Z", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if got := actions(decode[[]auditRow](t, rec), "seed."); len(got) != 1 || got[0] != "seed.feb" {
		t.Fatalf("range filter returned %v, want [seed.feb]", got)
	}

	// A bound given in another zone is the same instant, not a different string.
	from := url.QueryEscape("2026-03-10T17:30:00+05:30") // == 12:00:00Z
	rec = e.get(t, "/api/v1/audit?from="+from, cookie)
	got := actions(decode[[]auditRow](t, rec), "seed.")
	if len(got) != 1 || got[0] != "seed.mar" {
		t.Fatalf("offset bound returned %v, want [seed.mar]", got)
	}

	// Newest first, and limit is honoured.
	rec = e.get(t, "/api/v1/audit?to=2026-12-31T00:00:00Z&limit=2", cookie)
	rows := decode[[]auditRow](t, rec)
	if len(rows) != 2 {
		t.Fatalf("limit=2 returned %d rows", len(rows))
	}
	if rows[0].CreatedAt < rows[1].CreatedAt {
		t.Fatalf("rows not newest-first: %s before %s", rows[0].CreatedAt, rows[1].CreatedAt)
	}

	for _, bad := range []string{"from=yesterday", "to=2026-13-01", "limit=0", "limit=abc"} {
		if rec := e.get(t, "/api/v1/audit?"+bad, cookie); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", bad, rec.Code)
		}
	}
}

func TestAuditLogLimitIsCapped(t *testing.T) {
	e := newTestEnv(t)
	cookie := e.loginAs(t, "admin", "admin@example.com", "correct-horse-battery", true)
	for i := range 5 {
		seedAudit(t, e, fmt.Sprintf("cap-%d", i), "seed.cap", fmt.Sprintf("2026-01-0%dT00:00:00Z", i+1))
	}
	// Far above the cap: must not error, and must not return more than exist.
	rec := e.get(t, "/api/v1/audit?limit=999999", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if got := actions(decode[[]auditRow](t, rec), "seed.cap"); len(got) != 5 {
		t.Fatalf("got %d seeded rows, want 5", len(got))
	}
}

// The login that loginAs performs is itself audited — its recorded address
// must be a bare IP, not Go's "ip:port" RemoteAddr.
func TestAuditRecordsClientIPWithoutPort(t *testing.T) {
	e := newTestEnv(t)
	cookie := e.loginAs(t, "admin", "admin@example.com", "correct-horse-battery", true)

	rows := decode[[]auditRow](t, e.get(t, "/api/v1/audit", cookie))
	var checked int
	for _, r := range rows {
		if r.Action != "auth.login" {
			continue
		}
		checked++
		if net.ParseIP(r.IP) == nil {
			t.Errorf("auth.login recorded ip %q, want a bare IP address", r.IP)
		}
	}
	if checked == 0 {
		t.Fatal("no auth.login entry was recorded")
	}
}

func TestAlertTimedSilence(t *testing.T) {
	e := newTestEnv(t)
	admin := e.loginAs(t, "admin", "admin@example.com", "correct-horse-battery", true)
	seedActiveAlert(t, e.db, "alert-1")

	type alertRow struct {
		ID            string  `json:"id"`
		Status        string  `json:"status"`
		SilencedUntil *string `json:"silencedUntil"`
	}
	silenced := func() alertRow {
		rows := decode[[]alertRow](t, e.get(t, "/api/v1/alerts/?status=silenced", admin))
		if len(rows) != 1 {
			t.Fatalf("got %d silenced alerts, want 1", len(rows))
		}
		return rows[0]
	}

	// A duration sets a deadline roughly that far out.
	if rec := e.do(t, http.MethodPost, "/api/v1/alerts/alert-1/silence", map[string]int{"durationMinutes": 240}, admin); rec.Code != http.StatusOK {
		t.Fatalf("timed silence = %d: %s", rec.Code, rec.Body.String())
	}
	row := silenced()
	if row.SilencedUntil == nil {
		t.Fatal("timed silence recorded no deadline")
	}
	until, err := time.Parse(time.RFC3339, *row.SilencedUntil)
	if err != nil {
		t.Fatalf("silencedUntil %q is not RFC 3339: %v", *row.SilencedUntil, err)
	}
	if d := time.Until(until); d < 239*time.Minute || d > 241*time.Minute {
		t.Errorf("deadline is %v away, want ~4h", d)
	}

	// Silencing again with no duration turns it into an open-ended mute.
	if rec := e.do(t, http.MethodPost, "/api/v1/alerts/alert-1/silence", nil, admin); rec.Code != http.StatusOK {
		t.Fatalf("open-ended silence = %d: %s", rec.Code, rec.Body.String())
	}
	if row := silenced(); row.SilencedUntil != nil {
		t.Errorf("open-ended silence kept deadline %q", *row.SilencedUntil)
	}

	for _, bad := range []int{-5, maxSilenceMinutes + 1} {
		if rec := e.do(t, http.MethodPost, "/api/v1/alerts/alert-1/silence", map[string]int{"durationMinutes": bad}, admin); rec.Code != http.StatusBadRequest {
			t.Errorf("durationMinutes=%d: status %d, want 400", bad, rec.Code)
		}
	}

	// Un-silencing must not leave a stale deadline behind.
	e.do(t, http.MethodPost, "/api/v1/alerts/alert-1/silence", map[string]int{"durationMinutes": 60}, admin)
	if rec := e.do(t, http.MethodPost, "/api/v1/alerts/alert-1/unsilence", nil, admin); rec.Code != http.StatusOK {
		t.Fatalf("unsilence = %d", rec.Code)
	}
	var leftover *string
	if err := e.db.QueryRow(`SELECT silenced_until FROM alert_instances WHERE id = 'alert-1'`).Scan(&leftover); err != nil {
		t.Fatal(err)
	}
	if leftover != nil {
		t.Errorf("silenced_until = %q after un-silence, want NULL", *leftover)
	}
}
