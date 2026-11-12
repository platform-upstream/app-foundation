package push

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"

	"github.com/platform-upstream/app-foundation/access"
	"github.com/platform-upstream/app-foundation/sqlite"
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

// openWithAudience is open plus the optional audience column.
func openWithAudience(t *testing.T) *sql.DB {
	t.Helper()
	db := open(t)
	if _, err := db.Exec("ALTER TABLE push_subscriptions ADD COLUMN audience TEXT NOT NULL DEFAULT ''"); err != nil {
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
		if err := s.SubscribeTo(context.Background(), subscription(t, "https://web.push.apple.com/x"), "", "nik"); !errors.Is(err, ErrDisabled) {
			t.Fatalf("SubscribeTo error = %v", err)
		}
		if n, err := s.SendTo(context.Background(), "nik", []byte("{}")); n != 0 || err != nil {
			t.Fatalf("SendTo = %d, %v", n, err)
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
		"https://web.push.apple.com/abc":            true,
		"https://fcm.googleapis.com/fcm/send/abc":   true,
		"https://x.notify.windows.com/w":            true,
		"https://WEB.PUSH.APPLE.COM/abc":            true,
		"http://web.push.apple.com/abc":             false,
		"https://evilpush.apple.com.example/abc":    false,
		"https://user@web.push.apple.com/abc":       false,
		"https://web.push.apple.com:8443/abc":       false,
		"https://web.push.apple.com/abc#fragment":   false,
		"https://finance.int.example.org/api/banks": false,
		"https://notpush.apple.com/abc":             false,
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

func TestSubscribeToStoresAndMovesAudience(t *testing.T) {
	ctx := context.Background()
	db := openWithAudience(t)
	s := New(db, enabled)
	audience := func(endpoint string) string {
		t.Helper()
		var got string
		if err := db.QueryRow("SELECT audience FROM push_subscriptions WHERE endpoint = ?", endpoint).Scan(&got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	if err := s.SubscribeTo(ctx, subscription(t, "https://example.com/a"), "", "nik"); !errors.Is(err, ErrInvalidSubscription) {
		t.Fatalf("foreign endpoint error = %v", err)
	}
	phone := subscription(t, "https://web.push.apple.com/phone")
	if err := s.SubscribeTo(ctx, phone, "Safari", "nik"); err != nil {
		t.Fatal(err)
	}
	if err := s.SubscribeTo(ctx, subscription(t, "https://fcm.googleapis.com/fcm/send/tablet"), "Chrome", "bob"); err != nil {
		t.Fatal(err)
	}
	if got := audience(phone.Endpoint); got != "nik" {
		t.Fatalf("phone audience = %q", got)
	}
	// The phone now belongs to Bob.
	if err := s.SubscribeTo(ctx, subscription(t, phone.Endpoint), "Safari", "bob"); err != nil {
		t.Fatal(err)
	}
	if got := audience(phone.Endpoint); got != "bob" {
		t.Fatalf("phone audience after move = %q", got)
	}
	if n, _ := s.Subscribed(ctx); n != 2 {
		t.Fatalf("subscribed = %d", n)
	}
	// Subscribe leaves the column alone: the default for a new row, the stored value on refresh.
	if err := s.Subscribe(ctx, subscription(t, phone.Endpoint), "Safari"); err != nil {
		t.Fatal(err)
	}
	if got := audience(phone.Endpoint); got != "bob" {
		t.Fatalf("phone audience after Subscribe = %q", got)
	}
	for i := 2; i < MaxSubscriptions; i++ {
		if err := s.SubscribeTo(ctx, subscription(t, "https://web.push.apple.com/"+strings.Repeat("a", i)), "", "nik"); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SubscribeTo(ctx, subscription(t, "https://web.push.apple.com/new"), "", "bob"); !errors.Is(err, ErrSubscriptionLimit) {
		t.Fatalf("limit error = %v", err)
	}
}

func TestSendToDeliversOnlyToAudience(t *testing.T) {
	ctx := context.Background()
	s := New(openWithAudience(t), enabled)
	for endpoint, person := range map[string]string{"nik-phone": "nik", "nik-laptop": "nik", "bob-phone": "bob"} {
		if err := s.SubscribeTo(ctx, subscription(t, "https://web.push.apple.com/"+endpoint), "", person); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Subscribe(ctx, subscription(t, "https://web.push.apple.com/shared"), ""); err != nil {
		t.Fatal(err)
	}
	var reached []string
	s.send = func(_ context.Context, body []byte, sub *webpush.Subscription) (*http.Response, error) {
		reached = append(reached, sub.Endpoint[strings.LastIndex(sub.Endpoint, "/")+1:])
		return &http.Response{StatusCode: 201, Body: io.NopCloser(strings.NewReader(""))}, nil
	}
	delivered, err := s.SendTo(ctx, "bob", []byte(`{"title":"Nik added an idea"}`))
	if err != nil || delivered != 1 || len(reached) != 1 || reached[0] != "bob-phone" {
		t.Fatalf("SendTo bob = %d, %v, reached %v", delivered, err, reached)
	}
	reached = nil
	if delivered, err := s.SendTo(ctx, "nik", []byte("{}")); err != nil || delivered != 2 || len(reached) != 2 {
		t.Fatalf("SendTo nik = %d, %v, reached %v", delivered, err, reached)
	}
	reached = nil
	if delivered, err := s.SendTo(ctx, "nobody", []byte("{}")); err != nil || delivered != 0 || len(reached) != 0 {
		t.Fatalf("SendTo nobody = %d, %v, reached %v", delivered, err, reached)
	}
	// Send still reaches every device, whatever its audience.
	if delivered, err := s.Send(ctx, []byte("{}")); err != nil || delivered != 4 {
		t.Fatalf("Send = %d, %v", delivered, err)
	}
}

func TestSubscribeAndSendWithoutAudienceColumn(t *testing.T) {
	ctx := context.Background()
	s := New(open(t), enabled)
	if err := s.Subscribe(ctx, subscription(t, "https://web.push.apple.com/a"), ""); err != nil {
		t.Fatal(err)
	}
	s.send = func(context.Context, []byte, *webpush.Subscription) (*http.Response, error) {
		return &http.Response{StatusCode: 201, Body: io.NopCloser(strings.NewReader(""))}, nil
	}
	if delivered, err := s.Send(ctx, []byte("{}")); err != nil || delivered != 1 {
		t.Fatalf("Send = %d, %v", delivered, err)
	}
	// The audience methods need the column; without it they fail rather than send to everyone.
	if _, err := s.SendTo(ctx, "nik", []byte("{}")); err == nil {
		t.Fatal("SendTo without the audience column succeeded")
	}
}

const hubKey = "acme-hub-key-0123456789abcdef-0123456789abcdef"

// hubCall is one request the test hub received.
type hubCall struct {
	method, path, auth, contentType string
	body                            map[string]any
}

// testHub is a hub behind access.Internal with the key of caller "acme", so a passing call carries
// the auth header exactly the way the hub's guard expects it. answer writes the reply; the returned
// func lists the calls so far.
func testHub(t *testing.T, answer func(http.ResponseWriter, *http.Request)) (*httptest.Server, func() []hubCall) {
	t.Helper()
	var calls []hubCall
	var mu sync.Mutex
	guarded := access.Internal(map[string]string{"acme": hubKey}, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if caller, _ := access.InternalCallerFrom(r.Context()); caller != "acme" {
			t.Errorf("caller = %q", caller)
		}
		answer(w, r)
	}))
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := hubCall{method: r.Method, path: r.URL.Path, auth: r.Header.Get("Authorization"), contentType: r.Header.Get("Content-Type")}
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &call.body); err != nil {
			t.Errorf("hub body %q: %v", raw, err)
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		mu.Lock()
		calls = append(calls, call)
		mu.Unlock()
		guarded.ServeHTTP(w, r)
	}))
	t.Cleanup(hub.Close)
	return hub, func() []hubCall {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(calls)
	}
}

func reply(status int, body string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

// withDevice is a Sender over one stored subscription whose web pushes are counted in *pushed.
func withDevice(t *testing.T, cfg Config) (*Sender, *[]string) {
	t.Helper()
	s := New(open(t), cfg)
	if s.Enabled() {
		if err := s.Subscribe(context.Background(), subscription(t, "https://web.push.apple.com/phone"), ""); err != nil {
			t.Fatal(err)
		}
	}
	var pushed []string
	s.send = func(_ context.Context, body []byte, _ *webpush.Subscription) (*http.Response, error) {
		pushed = append(pushed, string(body))
		return &http.Response{StatusCode: 201, Body: io.NopCloser(strings.NewReader(""))}, nil
	}
	return s, &pushed
}

func withHub(url string) Config {
	cfg := enabled
	cfg.Hub = HubConfig{URL: url, Key: hubKey}
	return cfg
}

func TestMinHubKeyIsAccessMinimum(t *testing.T) {
	if minHubKey != access.MinInternalKeyLength {
		t.Fatalf("minHubKey = %d, access.MinInternalKeyLength = %d", minHubKey, access.MinInternalKeyLength)
	}
}

func TestNotifyWithoutHubSendsWebPush(t *testing.T) {
	s, pushed := withDevice(t, enabled)
	if s.HubEnabled() {
		t.Fatal("hub enabled without a URL")
	}
	// A key alone turns nothing on.
	if New(open(t), Config{Hub: HubConfig{Key: hubKey}}).HubEnabled() {
		t.Fatal("hub enabled with a key and no URL")
	}
	delivery, err := s.Notify(context.Background(), Message{Title: "Invoice due", Ref: "acme:invoice/42"}, []byte(`{"title":"Invoice due","url":"/invoices/42"}`))
	if err != nil || delivery != (Delivery{Devices: 1}) || len(*pushed) != 1 || (*pushed)[0] != `{"title":"Invoice due","url":"/invoices/42"}` {
		t.Fatalf("Notify = %+v, %v, pushed %v", delivery, err, *pushed)
	}
	// Both off: nothing to do, as with Send.
	if delivery, err := New(open(t), Config{}).Notify(context.Background(), Message{Title: "x"}, []byte("{}")); err != nil || delivery != (Delivery{}) {
		t.Fatalf("Notify with push off = %+v, %v", delivery, err)
	}
	var nobody *Sender
	if nobody.HubEnabled() {
		t.Fatal("nil sender has a hub")
	}
}

func TestNotifyHandsMessageToHub(t *testing.T) {
	hub, calls := testHub(t, reply(200, `{"id":"m-1"}`))
	s, pushed := withDevice(t, withHub(hub.URL+"/internal/notify"))
	if !s.HubEnabled() || !s.Enabled() {
		t.Fatal("hub and web push should both be on")
	}
	ctx := context.Background()
	full := Message{Title: "Shares free", Body: "40 shares are free to sell from today.", Ref: "acme:account/7", Urgency: UrgencyLater, ID: "shares-free-2026-11-12"}
	if delivery, err := s.Notify(ctx, full, []byte(`{"web":"push"}`)); err != nil || delivery != (Delivery{Hub: true}) {
		t.Fatalf("Notify = %+v, %v", delivery, err)
	}
	if delivery, err := s.Notify(ctx, Message{Title: "Time to fetch"}, []byte(`{"web":"push"}`)); err != nil || delivery != (Delivery{Hub: true}) {
		t.Fatalf("Notify minimal = %+v, %v", delivery, err)
	}
	if len(*pushed) != 0 {
		t.Fatalf("web push sent although the hub took the message: %v", *pushed)
	}
	if len(calls()) != 2 {
		t.Fatalf("hub calls = %d", len(calls()))
	}
	for _, call := range calls() {
		if call.method != http.MethodPost || call.path != "/internal/notify" || call.auth != "Bearer "+hubKey || call.contentType != "application/json" {
			t.Fatalf("hub request = %+v", call)
		}
	}
	want := []map[string]any{
		{"title": "Shares free", "body": "40 shares are free to sell from today.", "ref": "acme:account/7", "urgency": "later", "id": "shares-free-2026-11-12"},
		// Optional fields are left out; urgency is always sent, "now" when unset.
		{"title": "Time to fetch", "urgency": "now"},
	}
	for i := range want {
		if got, _ := json.Marshal(calls()[i].body); string(got) != string(mustJSON(t, want[i])) {
			t.Fatalf("hub body %d = %s, want %s", i, got, mustJSON(t, want[i]))
		}
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// The hub's 429 bounds how much an app tells the owner: the web push must not go around it.
func TestNotifySendsNothingWhenTheHubSaysTooMany(t *testing.T) {
	hub, calls := testHub(t, reply(429, `{"error":"Too many messages."}`))
	s, pushed := withDevice(t, withHub(hub.URL+"/internal/notify"))
	delivery, err := s.Notify(context.Background(), Message{Title: "Pull failed"}, []byte(`{"title":"Pull failed"}`))
	if !errors.Is(err, ErrHubFailed) || !strings.Contains(err.Error(), "HTTP 429: Too many messages.") {
		t.Fatalf("error = %v", err)
	}
	if delivery != (Delivery{}) || len(*pushed) != 0 || len(calls()) != 1 {
		t.Fatalf("delivery = %+v, pushed %v, hub asked %d times", delivery, *pushed, len(calls()))
	}
}

func TestNotifyFallsBackWhenHubFails(t *testing.T) {
	gone := httptest.NewServer(http.NotFoundHandler())
	gone.Close()
	elsewhere := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("the hub's redirect was followed")
	}))
	t.Cleanup(elsewhere.Close)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	for name, answer := range map[string]func(http.ResponseWriter, *http.Request){
		"500": reply(500, `{"error":"database locked"}`),
		"503": reply(503, "upstream unavailable"),
		"403": reply(403, `{"error":"Forbidden."}`),
		"404": reply(404, ""),
		"redirect": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, elsewhere.URL, http.StatusTemporaryRedirect)
		},
		"timeout": func(_ http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-release:
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			hub, calls := testHub(t, answer)
			s, pushed := withDevice(t, withHub(hub.URL+"/internal/notify"))
			s.hub.Timeout = 100 * time.Millisecond
			delivery, err := s.Notify(context.Background(), Message{Title: "Pull failed"}, []byte(`{"title":"Pull failed"}`))
			if !errors.Is(err, ErrHubFailed) || errors.Is(err, ErrInvalidMessage) {
				t.Fatalf("error = %v", err)
			}
			if delivery != (Delivery{Devices: 1}) || len(*pushed) != 1 || (*pushed)[0] != `{"title":"Pull failed"}` {
				t.Fatalf("fallback = %+v, pushed %v", delivery, *pushed)
			}
			if len(calls()) != 1 {
				t.Fatalf("hub asked %d times", len(calls()))
			}
			if strings.Contains(err.Error(), hubKey) {
				t.Fatalf("error carries the key: %v", err)
			}
			if name == "403" && !strings.Contains(err.Error(), "HTTP 403: Forbidden.") {
				t.Fatalf("error lacks the hub's reason: %v", err)
			}
		})
	}
	// A wrong key (401) is the hub refusing, too.
	hub, calls := testHub(t, reply(200, `{"id":"m-1"}`))
	cfg := withHub(hub.URL)
	cfg.Hub.Key = strings.Repeat("x", minHubKey)
	s, pushed := withDevice(t, cfg)
	if delivery, err := s.Notify(context.Background(), Message{Title: "Pull failed"}, []byte("{}")); !errors.Is(err, ErrHubFailed) || !strings.Contains(err.Error(), "HTTP 401") || delivery.Devices != 1 || len(*pushed) != 1 || len(calls()) != 1 {
		t.Fatalf("401: %+v, %v, pushed %d, calls %d", delivery, err, len(*pushed), len(calls()))
	}
	// Unreachable, and misconfigured (never called): the web push goes out with the reason.
	for name, hubCfg := range map[string]HubConfig{
		"unreachable": {URL: gone.URL, Key: hubKey},
		"short key":   {URL: hub.URL, Key: "short"},
		"no key":      {URL: hub.URL},
		"not a URL":   {URL: "hub.example.svc/internal/notify", Key: hubKey},
		"credentials": {URL: "http://user:pass@hub.example.svc/internal/notify", Key: hubKey},
	} {
		cfg := enabled
		cfg.Hub = hubCfg
		s, pushed := withDevice(t, cfg)
		delivery, err := s.Notify(context.Background(), Message{Title: "Pull failed"}, []byte("{}"))
		if !s.HubEnabled() || !errors.Is(err, ErrHubFailed) || delivery != (Delivery{Devices: 1}) || len(*pushed) != 1 {
			t.Fatalf("%s: %+v, %v, pushed %d", name, delivery, err, len(*pushed))
		}
	}
	if len(calls()) != 1 {
		t.Fatalf("a misconfigured hub was called: %d calls", len(calls()))
	}
}

func TestNotifyFailsWhenHubAndFallbackFail(t *testing.T) {
	hub, _ := testHub(t, reply(503, ""))
	// The hub alone, no web push of its own: nothing to fall back to.
	s := New(open(t), Config{Hub: HubConfig{URL: hub.URL, Key: hubKey}})
	if s.Enabled() || !s.HubEnabled() {
		t.Fatal("want hub on, web push off")
	}
	if delivery, err := s.Notify(context.Background(), Message{Title: "Pull failed"}, []byte("{}")); !errors.Is(err, ErrHubFailed) || delivery != (Delivery{}) {
		t.Fatalf("Notify = %+v, %v", delivery, err)
	}
	// The fallback's own error is joined to the hub's.
	s = New(open(t), withHub(hub.URL))
	if _, err := s.db.Exec("DROP TABLE push_subscriptions"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Notify(context.Background(), Message{Title: "Pull failed"}, []byte("{}")); !errors.Is(err, ErrHubFailed) || !strings.Contains(err.Error(), "no such table") {
		t.Fatalf("Notify error = %v", err)
	}
}

func TestNotifyHub400ReturnsErrorWithoutFallback(t *testing.T) {
	hub, calls := testHub(t, reply(400, `{"error":"Unknown ref app."}`))
	s, pushed := withDevice(t, withHub(hub.URL))
	delivery, err := s.Notify(context.Background(), Message{Title: "Shares free", Ref: "other:thing/1"}, []byte("{}"))
	if !errors.Is(err, ErrInvalidMessage) || errors.Is(err, ErrHubFailed) || !strings.Contains(err.Error(), "HTTP 400: Unknown ref app.") {
		t.Fatalf("error = %v", err)
	}
	if delivery != (Delivery{}) || len(*pushed) != 0 || len(calls()) != 1 {
		t.Fatalf("Notify = %+v, pushed %d, hub calls %d", delivery, len(*pushed), len(calls()))
	}
}

func TestNotifyRejectsInvalidMessages(t *testing.T) {
	hub, calls := testHub(t, reply(200, `{"id":"m-1"}`))
	for _, cfg := range []Config{enabled, withHub(hub.URL)} {
		s, pushed := withDevice(t, cfg)
		for _, msg := range []Message{
			{},
			{Title: "  "},
			{Title: strings.Repeat("ä", maxTitle+1)},
			{Title: "x", Body: strings.Repeat("ü", maxBody+1)},
			{Title: "x", Ref: "/#/sync"},
			{Title: "x", Ref: "https://acme.example.org/invoices/42"},
			{Title: "x", Ref: "acme:invoice"},
			{Title: "x", Ref: "acme:invoice/"},
			{Title: "x", Ref: "acme:invoice/4 2"},
			{Title: "x", Urgency: "soon"},
		} {
			if delivery, err := s.Notify(context.Background(), msg, []byte("{}")); !errors.Is(err, ErrInvalidMessage) || delivery != (Delivery{}) {
				t.Fatalf("Notify(%+v) = %+v, %v", msg, delivery, err)
			}
		}
		if len(*pushed) != 0 {
			t.Fatalf("an invalid message was pushed: %v", *pushed)
		}
		// The limits count characters, not bytes.
		edge := Message{Title: strings.Repeat("ä", maxTitle), Body: strings.Repeat("ü", maxBody), Ref: "acme:invoice/2026/42", Urgency: UrgencyNow}
		if _, err := s.Notify(context.Background(), edge, []byte("{}")); err != nil {
			t.Fatalf("Notify at the limits: %v", err)
		}
	}
	if len(calls()) != 1 {
		t.Fatalf("hub calls = %d, want only the valid one", len(calls()))
	}
}

func TestSendAndSendToNeverReachHub(t *testing.T) {
	hub, calls := testHub(t, reply(200, `{"id":"m-1"}`))
	s := New(openWithAudience(t), withHub(hub.URL))
	ctx := context.Background()
	for endpoint, person := range map[string]string{"owner-phone": "owner", "bob-phone": "bob"} {
		if err := s.SubscribeTo(ctx, subscription(t, "https://web.push.apple.com/"+endpoint), "", person); err != nil {
			t.Fatal(err)
		}
	}
	s.send = func(context.Context, []byte, *webpush.Subscription) (*http.Response, error) {
		return &http.Response{StatusCode: 201, Body: io.NopCloser(strings.NewReader(""))}, nil
	}
	if delivered, err := s.SendTo(ctx, "bob", []byte(`{"title":"A new idea"}`)); err != nil || delivered != 1 {
		t.Fatalf("SendTo = %d, %v", delivered, err)
	}
	if delivered, err := s.Send(ctx, []byte("{}")); err != nil || delivered != 2 {
		t.Fatalf("Send = %d, %v", delivered, err)
	}
	if len(calls()) != 0 {
		t.Fatalf("Send or SendTo reached the hub: %d calls", len(calls()))
	}
}
