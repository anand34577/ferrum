package notify

import (
	"bufio"
	"context"
	"encoding/base64"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
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

// fakeSMTPServer is a minimal in-process stand-in for a real relay — just
// enough of the protocol (EHLO, AUTH PLAIN, MAIL/RCPT/DATA) to prove
// sendPlain actually authenticates and delivers over a real (loopback,
// non-localhost-hostname) TCP connection, not just that the code compiles.
type fakeSMTPServer struct {
	addr           string
	gotAuthPayload string
	gotFrom        string
	gotTo          []string
}

func newFakeSMTPServer(t *testing.T) *fakeSMTPServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	s := &fakeSMTPServer{addr: ln.Addr().String()}

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		w := conn
		reply := func(line string) { _, _ = w.Write([]byte(line + "\r\n")) }

		reply("220 fake.local ESMTP")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			switch {
			case strings.HasPrefix(strings.ToUpper(line), "EHLO"):
				reply("250-fake.local")
				reply("250 AUTH PLAIN")
			case strings.HasPrefix(strings.ToUpper(line), "AUTH PLAIN "):
				payload, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(line, "AUTH PLAIN "))
				s.gotAuthPayload = string(payload)
				reply("235 Authentication successful")
			case strings.HasPrefix(strings.ToUpper(line), "MAIL FROM:"):
				s.gotFrom = line
				reply("250 OK")
			case strings.HasPrefix(strings.ToUpper(line), "RCPT TO:"):
				s.gotTo = append(s.gotTo, line)
				reply("250 OK")
			case strings.ToUpper(line) == "DATA":
				reply("354 End data with <CR><LF>.<CR><LF>")
				for {
					dl, err := r.ReadString('\n')
					if err != nil || strings.TrimRight(dl, "\r\n") == "." {
						break
					}
				}
				reply("250 OK: queued")
			case strings.ToUpper(line) == "QUIT":
				reply("221 Bye")
				return
			default:
				reply("250 OK")
			}
		}
	}()
	return s
}

func (s *fakeSMTPServer) host() string {
	host, _, _ := net.SplitHostPort(s.addr)
	return host
}

func (s *fakeSMTPServer) port() int {
	_, port, _ := net.SplitHostPort(s.addr)
	n, _ := strconv.Atoi(port)
	return n
}

func TestSendPlainAuthenticatesOverUnencryptedConnection(t *testing.T) {
	srv := newFakeSMTPServer(t)

	// smtp.PlainAuth would refuse this outright ("unencrypted connection")
	// since the server is neither TLS nor literally "localhost" — sendPlain
	// must use unencryptedPlainAuth instead, honoring the admin's explicit
	// choice to skip TLS on a trusted internal relay.
	err := sendSMTP(SMTPConfig{
		Host: srv.host(), Port: srv.port(), Username: "alice", Password: "s3cret",
		From: "ferrum@example.com", To: "ops@example.com", UseTLS: false,
	}, "Alert", "body")
	if err != nil {
		t.Fatalf("sendSMTP over plaintext with auth: %v", err)
	}
	if want := "\x00alice\x00s3cret"; srv.gotAuthPayload != want {
		t.Errorf("AUTH PLAIN payload = %q, want %q", srv.gotAuthPayload, want)
	}
	if !strings.Contains(srv.gotFrom, "ferrum@example.com") {
		t.Errorf("MAIL FROM = %q, missing sender", srv.gotFrom)
	}
	if len(srv.gotTo) != 1 || !strings.Contains(srv.gotTo[0], "ops@example.com") {
		t.Errorf("RCPT TO = %v, missing recipient", srv.gotTo)
	}
}
