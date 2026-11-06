package alert

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testNotification(kind string) Notification {
	return Notification{
		Kind: kind, At: time.Now(),
		Items: []Item{{HostID: "h1", Hostname: "web01", Service: "nginx", Message: "does not exist", Since: time.Now()}},
	}
}

func TestNtfySend(t *testing.T) {
	var gotPath, gotAuth, gotTitle, gotPrio, gotTags, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		gotTitle, gotPrio, gotTags = r.Header.Get("Title"), r.Header.Get("Priority"), r.Header.Get("Tags")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
	}))
	defer srv.Close()

	cfg := `{"server":"` + srv.URL + `/","topic":"cmonit-alerts","token":"tk_abc"}`
	nt, err := NewNotifier("ntfy", json.RawMessage(cfg))
	if err != nil {
		t.Fatal(err)
	}
	if err := nt.Send(context.Background(), testNotification(KindFailure)); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/cmonit-alerts" || gotAuth != "Bearer tk_abc" || gotPrio != "high" || gotTags != "warning" {
		t.Fatalf("path=%q auth=%q prio=%q tags=%q", gotPath, gotAuth, gotPrio, gotTags)
	}
	if gotTitle != "[cmonit] web01: 1 service failing" || !strings.Contains(gotBody, "nginx@web01") {
		t.Fatalf("title=%q body=%q", gotTitle, gotBody)
	}

	if err := nt.Send(context.Background(), testNotification(KindRecovery)); err != nil {
		t.Fatal(err)
	}
	if gotPrio != "default" || gotTags != "white_check_mark" {
		t.Fatalf("recovery prio=%q tags=%q", gotPrio, gotTags)
	}
}

func TestNtfyBasicAuthAndHTTPError(t *testing.T) {
	var user, pass string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, _ = r.BasicAuth()
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	nt, err := NewNotifier("ntfy", json.RawMessage(`{"server":"`+srv.URL+`","topic":"t","username":"u","password":"p"}`))
	if err != nil {
		t.Fatal(err)
	}
	err = nt.Send(context.Background(), testNotification(KindFailure))
	if err == nil || !strings.Contains(err.Error(), "HTTP 403") {
		t.Fatalf("err = %v, want HTTP 403", err)
	}
	if user != "u" || pass != "p" {
		t.Fatalf("basic auth = %q/%q", user, pass)
	}
}

func TestNtfyValidation(t *testing.T) {
	for _, cfg := range []string{`{}`, `{"topic":"has space"}`, `{"topic":"a/b"}`, `{"topic":"` + strings.Repeat("a", 65) + `"}`} {
		if _, err := NewNotifier("ntfy", json.RawMessage(cfg)); err == nil {
			t.Errorf("config %s accepted, want error", cfg)
		}
	}
	if _, err := NewNotifier("ntfy", json.RawMessage(`{"topic":"ok_topic-1"}`)); err != nil {
		t.Errorf("valid topic rejected: %v", err)
	}
}

func TestDiscordSend(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content-type = %q", ct)
		}
		json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	nt, err := NewNotifier("discord", json.RawMessage(`{"webhook_url":"`+srv.URL+`/webhooks/1/x","username":"cmonit"}`))
	if err != nil {
		t.Fatal(err)
	}
	n := testNotification(KindFailure)
	n.Items[0].Message = "@everyone " + strings.Repeat("x", 3000)
	if err := nt.Send(context.Background(), n); err != nil {
		t.Fatal(err)
	}
	content, _ := got["content"].(string)
	if len([]rune(content)) > discordMaxContent || !strings.HasSuffix(content, "...") {
		t.Fatalf("content length %d, want <= %d and truncated", len([]rune(content)), discordMaxContent)
	}
	mentions, _ := got["allowed_mentions"].(map[string]any)
	if parse, ok := mentions["parse"].([]any); !ok || len(parse) != 0 {
		t.Fatalf("allowed_mentions = %v, want parse: []", got["allowed_mentions"])
	}
	if got["username"] != "cmonit" {
		t.Fatalf("username = %v", got["username"])
	}
}

func TestSecretsMaskedForNewChannelTypes(t *testing.T) {
	got := string(MaskConfig(json.RawMessage(`{"server":"https://ntfy.sh","topic":"t","token":"tk_x","password":"p","webhook_url":"https://d/x"}`)))
	for _, secret := range []string{"tk_x", `"p"`, "https://d/x"} {
		if strings.Contains(got, secret) {
			t.Fatalf("masked config still contains %s: %s", secret, got)
		}
	}
	if !strings.Contains(got, "https://ntfy.sh") {
		t.Fatalf("server should stay visible: %s", got)
	}
}

func TestTruncateBytesKeepsRunes(t *testing.T) {
	s := strings.Repeat("é", 100)
	out := truncateBytes(s, 51)
	if len(out) > 51 || !strings.HasSuffix(out, "...") {
		t.Fatalf("len=%d out=%q", len(out), out)
	}
	if strings.ContainsRune(out, '�') {
		t.Fatalf("cut inside a rune: %q", out)
	}
}
