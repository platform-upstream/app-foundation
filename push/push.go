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
package push

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
)

const (
	// MaxSubscriptions bounds the stored subscriptions; one owner has a handful of devices.
	MaxSubscriptions = 20
	maxEndpoint      = 4096
	sendTimeout      = 5 * time.Second
)

var (
	ErrDisabled            = errors.New("push is not configured")
	ErrInvalidSubscription = errors.New("invalid push subscription")
	ErrSubscriptionLimit   = errors.New("too many push subscriptions")
)

// Config is the VAPID key pair and contact. Push is off unless all three are set.
type Config struct {
	PublicKey  string
	PrivateKey string
	// Subject is the contact address, with or without "mailto:".
	Subject    string
	HTTPClient *http.Client
	// TTL is how long the push service keeps an undelivered message; 0 means one hour.
	TTL time.Duration
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
	return s
}

// Enabled reports whether the key pair and contact are configured.
func (s *Sender) Enabled() bool {
	return s != nil && s.cfg.PublicKey != "" && s.cfg.PrivateKey != "" && s.cfg.Subject != ""
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
