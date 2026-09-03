package notify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSplitRecipients(t *testing.T) {
	got := splitRecipients("a@example.com, b@example.com;c@example.com   d@example.com")
	want := []string{"a@example.com", "b@example.com", "c@example.com", "d@example.com"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestBuildMessage(t *testing.T) {
	msg := string(buildMessage("ferrum@example.com", []string{"a@example.com", "b@example.com"}, "Alert", "body text"))
	for _, want := range []string{"From: ferrum@example.com", "To: a@example.com, b@example.com", "Subject: Alert", "body text"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q:\n%s", want, msg)
		}
	}
}

func TestSendGotify(t *testing.T) {
	var gotToken, gotTitle string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.URL.Query().Get("token")
		_ = r.ParseForm()
		gotTitle = r.PostForm.Get("title")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := sendGotify(context.Background(), srv.Client(), GotifyConfig{URL: srv.URL, Token: "tok123"}, "Alert title", "message body")
	if err != nil {
		t.Fatalf("sendGotify: %v", err)
	}
	if gotToken != "tok123" {
		t.Errorf("token = %q, want tok123", gotToken)
	}
	if gotTitle != "Alert title" {
		t.Errorf("title = %q, want %q", gotTitle, "Alert title")
	}
}

func TestSendGotifyRequiresURLAndToken(t *testing.T) {
	if err := sendGotify(context.Background(), http.DefaultClient, GotifyConfig{}, "t", "m"); err == nil {
		t.Fatal("expected error for empty URL/token")
	}
}

func TestSendGotifyErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("invalid token"))
	}))
	defer srv.Close()

	err := sendGotify(context.Background(), srv.Client(), GotifyConfig{URL: srv.URL, Token: "bad"}, "t", "m")
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("expected 401 error, got %v", err)
	}
}
