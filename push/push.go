// Package push sends Web Push notifications (VAPID) to the subscriptions an application stores.
//
// The application owns its migration, routes and message copy. It creates this table:
//
//	CREATE TABLE push_subscriptions (
//		endpoint TEXT PRIMARY KEY NOT NULL,
//		p256dh TEXT NOT NULL,
//		auth TEXT NOT NULL,
//		expiration_time INTEGER,
//		user_agent TEXT,
//		created_at TEXT NOT NULL
//	);
//
// and exposes PublicKey, Subscribe and Unsubscribe through its own handlers. Send delivers one
// payload to every stored subscription and is synchronous: an application that must not wait runs
// it in its own worker.
//
// An application with several people may add an audience to the table:
//
//	audience TEXT NOT NULL DEFAULT ''
//
// and store each device with SubscribeTo, then reach one person's devices with SendTo. Only
// SubscribeTo and SendTo read or write the column, so an application without it keeps working
// with Subscribe and Send unchanged.
//
// On a platform where one app, a hub, tells the owner for all apps, an application sets
// Config.Hub and tells the owner with Notify: the message goes to the hub, and to the app's own
// subscriptions only when there is no hub or the hub fails. Send and SendTo never reach the hub.
// The hub tells one person, the owner: set Config.Hub only on a Sender whose subscriptions are the
// owner's. An application with one Sender per person sets it on the owner's Sender and on no other,
// or every person's messages land with the owner.
package push

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	webpush "github.com/SherClockHolmes/webpush-go"
)

const (
	// MaxSubscriptions bounds the stored subscriptions; one owner has a handful of devices.
	MaxSubscriptions = 20
	maxEndpoint      = 4096
	sendTimeout      = 5 * time.Second

	hubTimeout   = 5 * time.Second
	maxHubReply  = 4 << 10
	maxHubReason = 200
	// minHubKey is access.MinInternalKeyLength, the shortest key the hub's guard accepts.
	minHubKey = 32
	maxTitle  = 120
	maxBody   = 500
)

var (
	ErrDisabled            = errors.New("push is not configured")
	ErrInvalidSubscription = errors.New("invalid push subscription")
	ErrSubscriptionLimit   = errors.New("too many push subscriptions")
	// ErrInvalidMessage: Notify sent nothing, because the Message breaks its rules or the hub
	// answered 400. The application fixes the message; nothing retries it.
	ErrInvalidMessage = errors.New("invalid push message")
	// ErrHubFailed: the hub did not take a message (misconfigured, unreachable, timed out, or
	// answered other than 2xx or 400), so Notify sent the web push instead; after a 429 it sent
	// nothing.
	ErrHubFailed = errors.New("push hub did not take the message")
	// errHubLimit marks the hub's 429: the app tells the owner too much, so nothing goes another way.
	errHubLimit = errors.New("too many messages")
)

// refPattern is an app reference, <app>:<noun>/<id>.
var refPattern = regexp.MustCompile(`^[^\s:/]+:[^\s:/]+/\S+$`)

// Config is the VAPID key pair and contact. Push is off unless all three are set.
type Config struct {
	PublicKey  string
	PrivateKey string
	// Subject is the contact address, with or without "mailto:".
	Subject    string
	HTTPClient *http.Client
	// TTL is how long the push service keeps an undelivered message; 0 means one hour.
	TTL time.Duration
	// Hub hands Notify's messages to the platform's hub; off while its URL is empty.
	Hub HubConfig
}

// HubConfig locates the hub, the app that tells the owner for every app on the platform. It
// belongs only on a Sender whose subscriptions are the owner's: the hub cannot tell whose message
// it gets.
type HubConfig struct {
	// URL is the hub's in-cluster notify route, such as http://hub.example.svc/internal/notify.
	URL string
	// Key is this app's bearer key at the hub, at least 32 characters (access.Internal's minimum).
	Key string
}

// check reports why the hub cannot be called; the values are not repeated, the key may be among them.
func (h HubConfig) check() error {
	u, err := url.Parse(h.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil ||
		u.RawQuery != "" || u.Fragment != "" {
		return errors.New("hub URL must be an absolute http(s) URL without credentials, query or fragment")
	}
	if len(h.Key) < minHubKey {
		return fmt.Errorf("hub key must have at least %d characters", minHubKey)
	}
	return nil
}

// Urgency says when the hub tells the owner.
type Urgency string

const (
	// UrgencyNow: the hub pushes at once. It is what the zero value means.
	UrgencyNow Urgency = "now"
	// UrgencyLater: the hub keeps the message and tells the owner with its next digest.
	UrgencyLater Urgency = "later"
)

// Message is what Notify tells the owner, in the hub's terms. Notify checks the rules below with
// or without a hub, so a message that works on one platform works on every one.
type Message struct {
	// Title is required, at most 120 characters.
	Title string
	// Body is optional, at most 500 characters.
	Body string
	// Ref is optional: the thing the message is about as an app reference <app>:<noun>/<id>, such
	// as acme:invoice/42, which the hub links back into the app. Not a URL.
	Ref string
	// Urgency is UrgencyNow (also the zero value) or UrgencyLater.
	Urgency Urgency
	// ID is optional and stable: a message with the same ID replaces the earlier one at the hub.
	ID string
}

func (m Message) validate() error {
	var problem string
	switch {
	case strings.TrimSpace(m.Title) == "":
		problem = "a title is required"
	case utf8.RuneCountInString(m.Title) > maxTitle:
		problem = fmt.Sprintf("the title has more than %d characters", maxTitle)
	case utf8.RuneCountInString(m.Body) > maxBody:
		problem = fmt.Sprintf("the body has more than %d characters", maxBody)
	case m.Ref != "" && !refPattern.MatchString(m.Ref):
		problem = "the ref is not <app>:<noun>/<id>"
	case m.Urgency != "" && m.Urgency != UrgencyNow && m.Urgency != UrgencyLater:
		problem = fmt.Sprintf("unknown urgency %q", string(m.Urgency))
	default:
		return nil
	}
	return fmt.Errorf("%w: %s", ErrInvalidMessage, problem)
}

// Delivery is where Notify's message went.
type Delivery struct {
	// Hub: the hub took the message; no web push was sent.
	Hub bool
	// Devices counts the push services that accepted the app's own web push.
	Devices int
}

// Subscription is what the browser's PushManager.subscribe() returns, as JSON.
type Subscription struct {
	Endpoint       string `json:"endpoint"`
	ExpirationTime *int64 `json:"expirationTime"`
	Keys           struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

// Sender stores subscriptions and delivers notifications.
type Sender struct {
	db   *sql.DB
	cfg  Config
	send func(context.Context, []byte, *webpush.Subscription) (*http.Response, error)
	// hub calls the hub, apart from the push services' client; hubErr is why it cannot.
	hub    *http.Client
	hubErr error
}

// New returns a Sender over the application's database.
func New(db *sql.DB, cfg Config) *Sender {
	// webpush-go prefixes "mailto:" itself; Apple rejects "mailto:mailto:" with 403 BadJwtToken.
	cfg.Subject = strings.TrimPrefix(cfg.Subject, "mailto:")
	if cfg.TTL <= 0 {
		cfg.TTL = time.Hour
	}
	client := http.Client{Timeout: sendTimeout}
	if cfg.HTTPClient != nil {
		client = *cfg.HTTPClient
	}
	// The allowlist checks the stored endpoint only, so a redirect is never followed.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	cfg.HTTPClient = &client
	s := &Sender{db: db, cfg: cfg}
	s.send = func(ctx context.Context, body []byte, sub *webpush.Subscription) (*http.Response, error) {
		return webpush.SendNotificationWithContext(ctx, body, sub, &webpush.Options{
			HTTPClient: cfg.HTTPClient, Subscriber: cfg.Subject, TTL: int(cfg.TTL / time.Second),
			VAPIDPublicKey: cfg.PublicKey, VAPIDPrivateKey: cfg.PrivateKey,
		})
	}
	if cfg.Hub.URL != "" {
		// The key never follows a redirect.
		s.hub = &http.Client{Timeout: hubTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		if s.hubErr = cfg.Hub.check(); s.hubErr != nil {
			// Said once at start: later only Notify's error shows it. The text carries no values.
			slog.Warn("push: hub not usable, Notify falls back to web push", "error", s.hubErr)
		}
	}
	return s
}

// Enabled reports whether the key pair and contact are configured.
func (s *Sender) Enabled() bool {
	return s != nil && s.cfg.PublicKey != "" && s.cfg.PrivateKey != "" && s.cfg.Subject != ""
}

// HubEnabled reports whether a hub URL is configured, so Notify tries the hub first. An app
// whose own web push is off (Enabled false) still tells the owner through Notify when it is on.
func (s *Sender) HubEnabled() bool {
	return s != nil && s.hub != nil
}

// PublicKey is the VAPID public key the page passes to PushManager.subscribe().
func (s *Sender) PublicKey() (string, error) {
	if !s.Enabled() {
		return "", ErrDisabled
	}
	return s.cfg.PublicKey, nil
}

// endpointAllowed accepts only the browser vendors' push services over plain HTTPS, so a stored
// subscription cannot make the server call an arbitrary address.
func endpointAllowed(endpoint string) bool {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, vendor := range []string{"fcm.googleapis.com", "web.push.apple.com", "push.apple.com", "notify.windows.com", "updates.push.services.mozilla.com"} {
		if host == vendor || strings.HasSuffix(host, "."+vendor) {
			return true
		}
	}
	return false
}

// Subscribe stores or refreshes a subscription.
func (s *Sender) Subscribe(ctx context.Context, sub Subscription, userAgent string) error {
	return s.subscribe(ctx, sub, userAgent, func(tx *sql.Tx, userAgent, createdAt string) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO push_subscriptions (endpoint, p256dh, auth, expiration_time, user_agent, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (endpoint) DO UPDATE SET p256dh = excluded.p256dh, auth = excluded.auth,
			expiration_time = excluded.expiration_time, user_agent = excluded.user_agent`,
			sub.Endpoint, sub.Keys.P256dh, sub.Keys.Auth, sub.ExpirationTime, userAgent, createdAt)
		return err
	})
}

// SubscribeTo stores or refreshes a subscription for one audience, such as a person. A stored
// endpoint moves to the new audience: the device now belongs to someone else. The table needs the
// audience column (see the package documentation).
func (s *Sender) SubscribeTo(ctx context.Context, sub Subscription, userAgent, audience string) error {
	return s.subscribe(ctx, sub, userAgent, func(tx *sql.Tx, userAgent, createdAt string) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO push_subscriptions (endpoint, p256dh, auth, expiration_time, user_agent, created_at, audience)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (endpoint) DO UPDATE SET p256dh = excluded.p256dh, auth = excluded.auth,
			expiration_time = excluded.expiration_time, user_agent = excluded.user_agent, audience = excluded.audience`,
			sub.Endpoint, sub.Keys.P256dh, sub.Keys.Auth, sub.ExpirationTime, userAgent, createdAt, audience)
		return err
	})
}

// subscribe validates sub, enforces MaxSubscriptions and runs upsert in one transaction.
func (s *Sender) subscribe(ctx context.Context, sub Subscription, userAgent string, upsert func(tx *sql.Tx, userAgent, createdAt string) error) error {
	if !s.Enabled() {
		return ErrDisabled
	}
	key, keyErr := base64.RawURLEncoding.DecodeString(strings.TrimRight(sub.Keys.P256dh, "="))
	auth, authErr := base64.RawURLEncoding.DecodeString(strings.TrimRight(sub.Keys.Auth, "="))
	if len(sub.Endpoint) > maxEndpoint || !endpointAllowed(sub.Endpoint) || keyErr != nil || authErr != nil ||
		len(key) != 65 || key[0] != 4 || len(auth) != 16 {
		return ErrInvalidSubscription
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count, exists int
	if err := tx.QueryRowContext(ctx, "SELECT count(*), count(CASE WHEN endpoint = ? THEN 1 END) FROM push_subscriptions", sub.Endpoint).Scan(&count, &exists); err != nil {
		return err
	}
	if exists == 0 && count >= MaxSubscriptions {
		return ErrSubscriptionLimit
	}
	if len(userAgent) > 300 {
		userAgent = userAgent[:300]
	}
	if err := upsert(tx, userAgent, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	return tx.Commit()
}

// Unsubscribe forgets a subscription; an unknown endpoint is not an error.
func (s *Sender) Unsubscribe(ctx context.Context, endpoint string) error {
	if endpoint == "" {
		return ErrInvalidSubscription
	}
	_, err := s.db.ExecContext(ctx, "DELETE FROM push_subscriptions WHERE endpoint = ?", endpoint)
	return err
}

// Subscribed counts the stored subscriptions.
func (s *Sender) Subscribed(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM push_subscriptions").Scan(&count)
	return count, err
}

// Send delivers payload to every stored subscription and returns how many push services accepted
// it. A subscription the push service reports gone (404, 410) is deleted; other failures are logged
// without the endpoint and skipped. With push off it does nothing. Delivery is sequential with a
// five-second limit per subscription, so a caller that must not wait runs Send in its own worker.
func (s *Sender) Send(ctx context.Context, payload []byte) (int, error) {
	return s.deliver(ctx, payload, "SELECT endpoint, p256dh, auth FROM push_subscriptions LIMIT ?", MaxSubscriptions)
}

// SendTo is Send for the subscriptions stored with SubscribeTo under audience only. The table needs
// the audience column (see the package documentation).
func (s *Sender) SendTo(ctx context.Context, audience string, payload []byte) (int, error) {
	return s.deliver(ctx, payload, "SELECT endpoint, p256dh, auth FROM push_subscriptions WHERE audience = ? LIMIT ?", audience, MaxSubscriptions)
}

// Notify tells the owner msg. With a hub configured it hands msg to the hub, one POST with a
// five-second limit, and sends no web push. Without one, or when the hub fails, it sends payload,
// the message the app's service worker expects, as Send does: to every stored subscription.
//
// An invalid msg, or one the hub answers 400, sends nothing and returns ErrInvalidMessage. A hub
// that is misconfigured, unreachable, slow, or refuses (401, 403, 5xx, any other status) is
// not asked again: Notify sends the web push and returns ErrHubFailed with the hub's reason,
// joined with Send's error if that failed too. Delivery says what reached the owner: Hub, or
// Devices > 0 even when the error is ErrHubFailed. A hub that answers 429 bounds how much an app
// tells the owner: nothing is sent and ErrHubFailed is returned. Like Send it is synchronous.
//
// Notify has no audience: it is for the owner, whose devices Send must reach on a Sender with a
// hub (see HubConfig). Messages for other people go through SendTo, or through a Sender without a
// hub; neither reaches it.
func (s *Sender) Notify(ctx context.Context, msg Message, payload []byte) (Delivery, error) {
	if err := msg.validate(); err != nil {
		return Delivery{}, err
	}
	var hubErr error
	if s.HubEnabled() {
		if hubErr = s.forward(ctx, msg); hubErr == nil {
			return Delivery{Hub: true}, nil
		}
		if errors.Is(hubErr, ErrInvalidMessage) || errors.Is(hubErr, errHubLimit) {
			return Delivery{}, hubErr
		}
	}
	devices, err := s.Send(ctx, payload)
	return Delivery{Devices: devices}, errors.Join(hubErr, err)
}

// hubMessage is the hub's request body; empty optional fields are left out.
type hubMessage struct {
	Title   string  `json:"title"`
	Body    string  `json:"body,omitempty"`
	Ref     string  `json:"ref,omitempty"`
	Urgency Urgency `json:"urgency"`
	ID      string  `json:"id,omitempty"`
}

// forward posts msg to the hub. Any 2xx means it took the message.
func (s *Sender) forward(ctx context.Context, msg Message) error {
	if s.hubErr != nil {
		return fmt.Errorf("%w: %w", ErrHubFailed, s.hubErr)
	}
	if msg.Urgency == "" {
		msg.Urgency = UrgencyNow
	}
	body, err := json.Marshal(hubMessage{Title: msg.Title, Body: msg.Body, Ref: msg.Ref, Urgency: msg.Urgency, ID: msg.ID})
	if err != nil {
		return fmt.Errorf("%w: %w", ErrHubFailed, err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.Hub.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("%w: %w", ErrHubFailed, err)
	}
	request.Header.Set("Authorization", "Bearer "+s.cfg.Hub.Key)
	request.Header.Set("Content-Type", "application/json")
	response, err := s.hub.Do(request)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrHubFailed, err)
	}
	defer response.Body.Close()
	reply, _ := io.ReadAll(io.LimitReader(response.Body, maxHubReply))
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}
	reason := fmt.Sprintf("HTTP %d", response.StatusCode)
	var answer struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(reply, &answer) == nil && strings.TrimSpace(answer.Error) != "" {
		text := strings.TrimSpace(answer.Error)
		if runes := []rune(text); len(runes) > maxHubReason {
			text = string(runes[:maxHubReason]) + "…"
		}
		reason += ": " + text
	}
	if response.StatusCode == http.StatusBadRequest {
		return fmt.Errorf("%w: hub answered %s", ErrInvalidMessage, reason)
	}
	if response.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("%w: %w: %s", ErrHubFailed, errHubLimit, reason)
	}
	return fmt.Errorf("%w: %s", ErrHubFailed, reason)
}

// deliver sends payload to the subscriptions query selects as (endpoint, p256dh, auth).
func (s *Sender) deliver(ctx context.Context, payload []byte, query string, args ...any) (int, error) {
	if !s.Enabled() {
		return 0, nil
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	var subs []webpush.Subscription
	for rows.Next() {
		var sub webpush.Subscription
		if err := rows.Scan(&sub.Endpoint, &sub.Keys.P256dh, &sub.Keys.Auth); err != nil {
			rows.Close()
			return 0, err
		}
		subs = append(subs, sub)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	delivered := 0
	for i := range subs {
		if ctx.Err() != nil {
			return delivered, ctx.Err()
		}
		if !endpointAllowed(subs[i].Endpoint) {
			slog.Warn("push: stored endpoint rejected")
			continue
		}
		sendCtx, cancel := context.WithTimeout(ctx, sendTimeout)
		response, err := s.send(sendCtx, payload, &subs[i])
		cancel()
		if err != nil {
			// A *url.Error's text carries the endpoint, a per-device capability URL.
			var urlErr *url.Error
			if errors.As(err, &urlErr) {
				err = urlErr.Err
			}
			slog.Warn("push: delivery failed", "error", err)
			continue
		}
		response.Body.Close()
		switch {
		case response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusGone:
			if _, err := s.db.ExecContext(ctx, "DELETE FROM push_subscriptions WHERE endpoint = ?", subs[i].Endpoint); err != nil {
				slog.Warn("push: gone subscription not deleted", "error", err)
			}
		case response.StatusCode >= 300:
			slog.Warn("push: delivery rejected", "status", response.StatusCode)
		default:
			delivered++
		}
	}
	return delivered, nil
}
