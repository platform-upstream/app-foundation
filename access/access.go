// Package access verifies Cloudflare Access identity at the application boundary.
package access

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
)

const (
	AssertionHeader  = "Cf-Access-Jwt-Assertion"
	maxAssertionSize = 16_000
)

var (
	ErrMissingAssertion  = errors.New("Cloudflare Access assertion is missing")
	ErrAssertionTooLarge = errors.New("Cloudflare Access assertion is too large")
	ErrInvalidAssertion  = errors.New("Cloudflare Access assertion is invalid")
	ErrNonLoopbackPeer   = errors.New("local authentication request did not originate from loopback")
)

// Mode selects production Cloudflare verification or explicitly local development.
type Mode string

const (
	ModeCloudflare Mode = "cloudflare"
	ModeLocal      Mode = "local"
)

// Config contains only authentication transport configuration. Applications retain authorization
// policy, accepted users, and user-facing failure messages.
type Config struct {
	Mode       Mode
	Listener   string
	TeamDomain string
	Audience   string
	HTTPClient *http.Client
}

// RejectFunc writes an application-owned authentication failure response.
type RejectFunc func(http.ResponseWriter, *http.Request, error)

// Authenticator verifies requests at the application boundary.
type Authenticator struct {
	local    bool
	verifier *oidc.IDTokenVerifier
}

// New constructs a fail-closed authenticator. Local mode is accepted only for an IP loopback
// listener; request peers are checked independently for every request.
func New(cfg Config) (*Authenticator, error) {
	mode := cfg.Mode
	if mode == "" {
		mode = ModeCloudflare
	}
	switch mode {
	case ModeLocal:
		if !loopbackListener(cfg.Listener) {
			return nil, errors.New("local authentication requires an explicit loopback listener")
		}
		return &Authenticator{local: true}, nil
	case ModeCloudflare:
		if !validTeamDomain(cfg.TeamDomain) || cfg.Audience == "" {
			return nil, errors.New("valid Cloudflare team domain and audience are required")
		}
		issuer := "https://" + cfg.TeamDomain
		return newCloudflare(issuer, issuer+"/cdn-cgi/access/certs", cfg.Audience, cfg.HTTPClient), nil
	default:
		return nil, fmt.Errorf("unsupported access mode %q", mode)
	}
}

func newCloudflare(issuer, certsURL, audience string, client *http.Client) *Authenticator {
	keys := newBoundedRemoteKeySet(certsURL, client)
	verifier := oidc.NewVerifier(issuer, keys, &oidc.Config{
		ClientID:             audience,
		SupportedSigningAlgs: []string{"RS256"},
	})
	return &Authenticator{verifier: verifier}
}

// Authenticate verifies one request without writing a response.
func (a *Authenticator) Authenticate(r *http.Request) error {
	if a == nil || (!a.local && a.verifier == nil) {
		return ErrInvalidAssertion
	}
	if a.local {
		if !loopbackPeer(r.RemoteAddr) {
			return ErrNonLoopbackPeer
		}
		return nil
	}
	raw := r.Header.Get(AssertionHeader)
	if raw == "" {
		return ErrMissingAssertion
	}
	if len(raw) > maxAssertionSize {
		return ErrAssertionTooLarge
	}
	token, err := a.verifier.Verify(r.Context(), raw)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidAssertion, err)
	}
	var claims struct {
		IssuedAt int64 `json:"iat"`
	}
	if err := token.Claims(&claims); err != nil || token.Subject == "" || claims.IssuedAt == 0 {
		return ErrInvalidAssertion
	}
	return nil
}

// Middleware gates requests. The reject callback lets each app preserve its existing response
// copy. A small JSON fallback is used when reject is nil.
func (a *Authenticator) Middleware(reject RejectFunc) func(http.Handler) http.Handler {
	if reject == nil {
		reject = func(w http.ResponseWriter, _ *http.Request, _ error) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Sign-in required."})
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := a.Authenticate(r); err != nil {
				reject(w, r, err)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func validTeamDomain(domain string) bool {
	prefix, ok := strings.CutSuffix(domain, ".cloudflareaccess.com")
	return ok && prefix != "" && !strings.ContainsAny(prefix, "/:?# \t\r\n")
}

func loopbackListener(address string) bool {
	host, _, err := net.SplitHostPort(address)
	return err == nil && host == "127.0.0.1"
}

func loopbackPeer(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
