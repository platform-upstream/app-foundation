package push

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	webpush "github.com/SherClockHolmes/webpush-go"

	"github.com/niiku-platform/app-foundation/sqlite"
)

const schema = `CREATE TABLE push_subscriptions (
	endpoint TEXT PRIMARY KEY NOT NULL,
	p256dh TEXT NOT NULL,
	auth TEXT NOT NULL,
	expiration_time INTEGER,
	user_agent TEXT,
	created_at TEXT NOT NULL
)`

func open(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "push.sqlite"), sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	return db
}

func subscription(t *testing.T, endpoint string) Subscription {
	t.Helper()
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	_, _ = rand.Read(auth)
	var sub Subscription
	sub.Endpoint = endpoint
	sub.Keys.P256dh = base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes())
	sub.Keys.Auth = base64.RawURLEncoding.EncodeToString(auth)
	return sub
}

var enabled = Config{PublicKey: "public", PrivateKey: "private", Subject: "mailto:owner@example.com"}

func TestDisabledWithoutFullConfig(t *testing.T) {
	db := open(t)
	for _, cfg := range []Config{{}, {PublicKey: "p", PrivateKey: "k"}, {PublicKey: "p", Subject: "s"}} {
		s := New(db, cfg)
		if s.Enabled() {
			t.Fatalf("%+v enabled", cfg)
		}
		if _, err := s.PublicKey(); !errors.Is(err, ErrDisabled) {
			t.Fatalf("PublicKey error = %v", err)
		}
		if err := s.Subscribe(context.Background(), subscription(t, "https://web.push.apple.com/x"), ""); !errors.Is(err, ErrDisabled) {
			t.Fatalf("Subscribe error = %v", err)
		}
		if n, err := s.Send(context.Background(), []byte("{}")); n != 0 || err != nil {
			t.Fatalf("Send = %d, %v", n, err)
		}
	}
}

func TestSubjectLosesMailtoPrefix(t *testing.T) {
	if s := New(open(t), enabled); s.cfg.Subject != "owner@example.com" {
		t.Fatalf("subject = %q", s.cfg.Subject)
	}
}

func TestRedirectsAreNotFollowed(t *testing.T) {
	caller := &http.Client{}
	s := New(open(t), Config{PublicKey: "p", PrivateKey: "k", Subject: "s", HTTPClient: caller})
	if s.cfg.HTTPClient == caller || s.cfg.HTTPClient.CheckRedirect == nil || caller.CheckRedirect != nil {
		t.Fatal("the sender must use its own copy of the client that refuses redirects")
	}
	if err := s.cfg.HTTPClient.CheckRedirect(nil, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("CheckRedirect = %v", err)
	}
}

func TestEndpointAllowed(t *testing.T) {
	for endpoint, want := range map[string]bool{
		"https://web.push.apple.com/abc":          true,
		"https://fcm.googleapis.com/fcm/send/abc": true,
		"https://x.notify.windows.com/w":          true,
		"https://WEB.PUSH.APPLE.COM/abc":          true,
		"http://web.push.apple.com/abc":           false,
		"https://evilpush.apple.com.example/abc":  false,
		"https://user@web.push.apple.com/abc":     false,
		"https://web.push.apple.com:8443/abc":     false,
		"https://web.push.apple.com/abc#fragment": false,
		"https://finance.int.niiku.xyz/api/banks": false,
		"https://notpush.apple.com/abc":           false,
	} {
		if got := endpointAllowed(endpoint); got != want {
			t.Errorf("endpointAllowed(%q) = %v", endpoint, got)
		}
	}
}

func TestSubscribeValidatesAndBounds(t *testing.T) {
	ctx := context.Background()
	s := New(open(t), enabled)
	bad := subscription(t, "https://web.push.apple.com/a")
	bad.Keys.Auth = "short"
	if err := s.Subscribe(ctx, bad, ""); !errors.Is(err, ErrInvalidSubscription) {
		t.Fatalf("bad auth error = %v", err)
	}
	if err := s.Subscribe(ctx, subscription(t, "https://example.com/a"), ""); !errors.Is(err, ErrInvalidSubscription) {
		t.Fatalf("foreign endpoint error = %v", err)
	}
	for i := range MaxSubscriptions {
		if err := s.Subscribe(ctx, subscription(t, "https://web.push.apple.com/"+strings.Repeat("a", i+1)), "Safari"); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Subscribe(ctx, subscription(t, "https://web.push.apple.com/new"), ""); !errors.Is(err, ErrSubscriptionLimit) {
		t.Fatalf("limit error = %v", err)
	}
	// Refreshing a stored endpoint is allowed at the limit.
	if err := s.Subscribe(ctx, subscription(t, "https://web.push.apple.com/a"), ""); err != nil {
		t.Fatalf("refresh at limit: %v", err)
	}
	if n, _ := s.Subscribed(ctx); n != MaxSubscriptions {
		t.Fatalf("subscribed = %d", n)
	}
	if err := s.Unsubscribe(ctx, "https://web.push.apple.com/a"); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.Subscribed(ctx); n != MaxSubscriptions-1 {
		t.Fatalf("after unsubscribe = %d", n)
	}
}

func TestSendDeliversAndPrunesGone(t *testing.T) {
	ctx := context.Background()
	s := New(open(t), enabled)
	for _, path := range []string{"ok", "gone", "missing", "failing", "error"} {
		if err := s.Subscribe(ctx, subscription(t, "https://web.push.apple.com/"+path), ""); err != nil {
			t.Fatal(err)
		}
	}
	var bodies []string
	s.send = func(_ context.Context, body []byte, sub *webpush.Subscription) (*http.Response, error) {
		bodies = append(bodies, string(body))
		status := map[string]int{"ok": 201, "gone": 410, "missing": 404, "failing": 500}[sub.Endpoint[strings.LastIndex(sub.Endpoint, "/")+1:]]
		if status == 0 {
			return nil, &url.Error{Op: "Post", URL: sub.Endpoint, Err: errors.New("network down")}
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(""))}, nil
	}
	var logged bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	defer slog.SetDefault(previous)
	delivered, err := s.Send(ctx, []byte(`{"title":"Code needed"}`))
	if strings.Contains(logged.String(), "push.apple.com") || !strings.Contains(logged.String(), "network down") {
		t.Fatalf("log = %s", logged.String())
	}
	if err != nil || delivered != 1 || len(bodies) != 5 || bodies[0] != `{"title":"Code needed"}` {
		t.Fatalf("Send = %d, %v, bodies %v", delivered, err, bodies)
	}
	if n, _ := s.Subscribed(ctx); n != 3 {
		t.Fatalf("after pruning 404 and 410: %d subscriptions", n)
	}
}
