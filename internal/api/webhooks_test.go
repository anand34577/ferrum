package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"ferrum/internal/notify"
)

func TestWebhookCRUDRequiresAdmin(t *testing.T) {
	e := newTestEnv(t)
	admin := e.loginAs(t, "admin", "admin@example.com", "correct horse battery staple", true)
	_ = e.do(t, http.MethodPost, "/api/v1/users/", map[string]any{
		"username": "user", "email": "user@example.com", "password": "user's own password",
	}, admin)
	user := e.loginAs(t, "user", "user@example.com", "user's own password", false)

	body := map[string]any{"name": "test hook", "url": "https://example.com/hook", "eventTypes": []string{"alert.triggered"}}

	if rec := e.do(t, http.MethodPost, "/api/v1/settings/webhooks/", body, user); rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin create status = %d, want 403, body %s", rec.Code, rec.Body.String())
	}

	rec := e.do(t, http.MethodPost, "/api/v1/settings/webhooks/", body, admin)
	if rec.Code != http.StatusCreated {
		t.Fatalf("admin create status = %d, want 201, body %s", rec.Code, rec.Body.String())
	}
	created := decode[webhookDTO](t, rec)
	if created.Secret == "" {
		t.Fatal("expected a generated secret on create")
	}
	if created.ID == "" {
		t.Fatal("expected a generated id")
	}

	// The list response must never echo the secret back.
	listRec := e.get(t, "/api/v1/settings/webhooks/", admin)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200", listRec.Code)
	}
	list := decode[[]webhookDTO](t, listRec)
	if len(list) != 1 {
		t.Fatalf("expected 1 webhook, got %d", len(list))
	}
	if list[0].Secret != "" {
		t.Fatal("list response must not include the signing secret")
	}
	if !list[0].Active {
		t.Fatal("expected a newly created webhook to default to active")
	}

	// Update.
	updateBody := map[string]any{"name": "renamed", "url": "https://example.com/hook2", "eventTypes": []string{}, "active": false}
	updRec := e.do(t, http.MethodPut, "/api/v1/settings/webhooks/"+created.ID+"/", updateBody, admin)
	if updRec.Code != http.StatusOK {
		t.Fatalf("update status = %d, want 200, body %s", updRec.Code, updRec.Body.String())
	}
	updated := decode[webhookDTO](t, updRec)
	if updated.Name != "renamed" || updated.Active {
		t.Fatalf("update did not apply: %+v", updated)
	}

	// Delete.
	delRec := e.do(t, http.MethodDelete, "/api/v1/settings/webhooks/"+created.ID+"/", nil, admin)
	if delRec.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want 200", delRec.Code)
	}
	if rec := e.get(t, "/api/v1/settings/webhooks/", admin); rec.Code == http.StatusOK {
		if got := decode[[]webhookDTO](t, rec); len(got) != 0 {
			t.Fatalf("expected 0 webhooks after delete, got %d", len(got))
		}
	}

	// Deleting again should 404.
	if rec := e.do(t, http.MethodDelete, "/api/v1/settings/webhooks/"+created.ID+"/", nil, admin); rec.Code != http.StatusNotFound {
		t.Fatalf("re-delete status = %d, want 404", rec.Code)
	}
}

func TestCreateWebhookValidatesInput(t *testing.T) {
	e := newTestEnv(t)
	admin := e.loginAs(t, "admin", "admin@example.com", "correct horse battery staple", true)

	cases := []struct {
		name string
		body map[string]any
	}{
		{"missing name", map[string]any{"url": "https://example.com"}},
		{"missing url", map[string]any{"name": "x"}},
		{"bad url scheme", map[string]any{"name": "x", "url": "ftp://example.com"}},
		{"unknown event type", map[string]any{"name": "x", "url": "https://example.com", "eventTypes": []string{"not.a.real.type"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := e.do(t, http.MethodPost, "/api/v1/settings/webhooks/", tc.body, admin)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400, body %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestTestWebhookDeliversAndLogsDelivery(t *testing.T) {
	var hit bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		if r.Header.Get(notify.SignatureHeader) == "" {
			t.Error("expected a signature header on the test delivery")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	e := newTestEnv(t)
	e.server.SetWebhookDispatcher(notify.NewWebhookDispatcher(e.db))
	admin := e.loginAs(t, "admin", "admin@example.com", "correct horse battery staple", true)

	createRec := e.do(t, http.MethodPost, "/api/v1/settings/webhooks/",
		map[string]any{"name": "test", "url": upstream.URL, "eventTypes": []string{}}, admin)
	created := decode[webhookDTO](t, createRec)

	testRec := e.do(t, http.MethodPost, "/api/v1/settings/webhooks/"+created.ID+"/test", nil, admin)
	if testRec.Code != http.StatusOK {
		t.Fatalf("test status = %d, want 200, body %s", testRec.Code, testRec.Body.String())
	}
	result := decode[map[string]any](t, testRec)
	if delivered, _ := result["delivered"].(bool); !delivered {
		t.Fatalf("expected delivered=true, got %+v", result)
	}
	if !hit {
		t.Fatal("test webhook never reached the upstream server")
	}

	deliveriesRec := e.get(t, "/api/v1/settings/webhooks/"+created.ID+"/deliveries", admin)
	if deliveriesRec.Code != http.StatusOK {
		t.Fatalf("deliveries status = %d, want 200", deliveriesRec.Code)
	}
	deliveries := decode[[]webhookDeliveryDTO](t, deliveriesRec)
	if len(deliveries) != 1 {
		t.Fatalf("expected 1 delivery logged, got %d", len(deliveries))
	}
	if !deliveries[0].Success {
		t.Fatalf("expected the logged delivery to be marked successful: %+v", deliveries[0])
	}
}

func TestTestWebhookWithoutDispatcherIsUnavailable(t *testing.T) {
	e := newTestEnv(t) // no SetWebhookDispatcher call — mirrors a boot path that hasn't wired it yet
	admin := e.loginAs(t, "admin", "admin@example.com", "correct horse battery staple", true)

	createRec := e.do(t, http.MethodPost, "/api/v1/settings/webhooks/",
		map[string]any{"name": "test", "url": "https://example.com/hook", "eventTypes": []string{}}, admin)
	created := decode[webhookDTO](t, createRec)

	rec := e.do(t, http.MethodPost, "/api/v1/settings/webhooks/"+created.ID+"/test", nil, admin)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}
