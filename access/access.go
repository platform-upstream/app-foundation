// Package access verifies Cloudflare Access assertions at the application boundary.
package access

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
)

const (
	AssertionHeader  = "Cf-Access-Jwt-Assertion"
	maxAssertionSize = 16_000
)

var teamDomainPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.cloudflareaccess\.com$`)

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
	// ServiceTokenClientIDs lists the Cloudflare Access service tokens accepted besides people.
	// Their assertions carry no subject; the client ID arrives as common_name. Empty accepts none.
	ServiceTokenClientIDs []string
}

// RejectFunc writes an application-owned authentication failure response.
type RejectFunc func(http.ResponseWriter, *http.Request, error)

// Authenticator verifies requests at the application boundary.
type Authenticator struct {
	local         bool
	verifier      *oidc.IDTokenVerifier
	serviceTokens map[string]struct{}
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
		if !validTeamDomain(cfg.TeamDomain) || cfg.Audience == "" || strings.TrimSpace(cfg.Audience) != cfg.Audience {
			return nil, errors.New("valid Cloudflare team domain and audience are required")
		}
		issuer := "https://" + cfg.TeamDomain
		authenticator := newCloudflare(issuer, issuer+"/cdn-cgi/access/certs", cfg.Audience, cfg.HTTPClient)
		authenticator.serviceTokens = make(map[string]struct{}, len(cfg.ServiceTokenClientIDs))
		for _, id := range cfg.ServiceTokenClientIDs {
			if id == "" || strings.TrimSpace(id) != id {
				return nil, errors.New("service token client IDs must be non-empty without surrounding whitespace")
			}
			authenticator.serviceTokens[id] = struct{}{}
		}
		return authenticator, nil
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

// Caller is who a verified request came from.
type Caller struct {
	// Local is set in local mode: a loopback request that carried no assertion.
	Local bool
	// Subject is the person's Access subject; empty for a service token.
	Subject string
	// ServiceToken is the accepted service token's client ID; empty for a person.
	ServiceToken string
}

// Person reports whether a person signed in through Access, as opposed to a service token or a
// local request.
func (c Caller) Person() bool { return c.Subject != "" }

type callerKey struct{}

// CallerFrom returns the caller the Middleware verified for this request.
func CallerFrom(ctx context.Context) (Caller, bool) {
	caller, ok := ctx.Value(callerKey{}).(Caller)
	return caller, ok
}

// Authenticate verifies one request without writing a response.
func (a *Authenticator) Authenticate(r *http.Request) error {
	_, err := a.Identify(r)
	return err
}

// Identify verifies one request and returns its caller.
func (a *Authenticator) Identify(r *http.Request) (Caller, error) {
	if a == nil || (!a.local && a.verifier == nil) {
		return Caller{}, ErrInvalidAssertion
	}
	if a.local {
		if !loopbackPeer(r.RemoteAddr) {
			return Caller{}, ErrNonLoopbackPeer
		}
		return Caller{Local: true}, nil
	}
	assertions := r.Header.Values(AssertionHeader)
	if len(assertions) == 0 || (len(assertions) == 1 && assertions[0] == "") {
		return Caller{}, ErrMissingAssertion
	}
	if len(assertions) != 1 {
		return Caller{}, ErrInvalidAssertion
	}
	raw := assertions[0]
	if len(raw) > maxAssertionSize {
		return Caller{}, ErrAssertionTooLarge
	}
	token, err := a.verifier.Verify(r.Context(), raw)
	if err != nil {
		return Caller{}, fmt.Errorf("%w: %v", ErrInvalidAssertion, err)
	}
	var claims struct {
		IssuedAt   int64  `json:"iat"`
		CommonName string `json:"common_name"`
	}
	if err := token.Claims(&claims); err != nil || token.Expiry.IsZero() || claims.IssuedAt == 0 {
		return Caller{}, ErrInvalidAssertion
	}
	if token.Subject == "" {
		if _, accepted := a.serviceTokens[claims.CommonName]; !accepted {
			return Caller{}, ErrInvalidAssertion
		}
		return Caller{ServiceToken: claims.CommonName}, nil
	}
	return Caller{Subject: token.Subject}, nil
}

// Middleware gates requests and puts the verified Caller into the request context (CallerFrom).
// The reject callback lets each app preserve its existing response copy. A small JSON fallback
// is used when reject is nil.
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
			caller, err := a.Identify(r)
			if err != nil {
				reject(w, r, err)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), callerKey{}, caller)))
		})
	}
}

func validTeamDomain(domain string) bool {
	return teamDomainPattern.MatchString(domain)
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
