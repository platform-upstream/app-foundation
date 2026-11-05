// Package googleauth connects a person's Google account to an app (OAuth for web server apps):
// the consent URL, the code exchange that names the account, the refresh token sealed for the
// app's store (Key), and access tokens from it, cached in memory (Client.AccessToken).
//
// It stores nothing. The app keeps the OAuth state and the sealed token in its own database and
// writes its own SQL (Example): its connect route keeps a NewState with the person for StateTTL
// and redirects to AuthCodeURL; its callback takes that state out of the store (once, unexpired,
// the same person) before Exchange, then stores Key.Seal(grant.RefreshToken, grant.Email), the
// e-mail and the granted scopes. Before each Google call it opens the token and asks
// AccessToken; ErrInvalidGrant means the grant is gone and the person connects again. The sealed
// format is the one Omni and Paper already store, so they move here without a new consent.
//
// Several apps may share one OAuth client (one consent screen, one verification). That is why:
//
//   - Disconnecting forgets the app's token and never revokes it at Google. Revocation removes
//     every scope the person granted to the Cloud project, for all of its clients, so one app's
//     "disconnect" would cut the others off too; a person who wants all access gone removes the
//     app in their Google account. There is deliberately no revoke helper.
//   - include_granted_scopes is off (Config.IncludeGrantedScopes): it would add the scopes the
//     person granted the other apps to this app's token.
//   - Each consent issues a new refresh token (prompt=consent) and Google keeps only a limited
//     number per client and account, invalidating the oldest. Keep one token per app and
//     person and replace it on reconnect; an app that piles up grants pushes the others out.
package googleauth

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/endpoints"
)

const (
	// StateTTL is how long an app keeps a state from NewState before its callback refuses it.
	StateTTL = 10 * time.Minute
	// Timeout bounds each request of the default HTTP client and each token refresh.
	Timeout = 10 * time.Second
	// UserInfoURL is Google's OpenID Connect userinfo endpoint, which names the account.
	UserInfoURL = "https://openidconnect.googleapis.com/v1/userinfo"

	maxUserInfoBytes = 64 << 10
)

var (
	// ErrInvalidGrant: Google refused the code or refresh token (invalid_grant). The person
	// revoked access, the token expired or was pushed out by newer ones, or the code was used
	// already. The app forgets the token and offers to connect again.
	ErrInvalidGrant = errors.New("googleauth: grant is no longer valid, connect again")
	// ErrMissingScopes: on the consent screen the person unticked a scope the app requires. The
	// error names the missing scopes; nothing is returned to store, and connecting again asks again.
	ErrMissingScopes = errors.New("googleauth: required scopes not granted")
)

// Config is one OAuth client (type Web application) and what the app asks for.
type Config struct {
	ClientID     string
	ClientSecret string
	// RedirectURL is the app's callback as registered on the client, such as
	// https://app.example.org/api/google/callback.
	RedirectURL string
	// Scopes are the scopes the app needs; Exchange fails with ErrMissingScopes without one of
	// them. openid and email are always asked for and required: they name the account.
	Scopes []string
	// OptionalScopes are asked for too, but the person may untick them; the app looks for them
	// in Grant.Scopes.
	OptionalScopes []string
	// IncludeGrantedScopes asks Google to add every scope the person granted this client before
	// (include_granted_scopes=true). Leave it off when the client is shared with other apps.
	IncludeGrantedScopes bool
	// HTTPClient defaults to one with Timeout. Redirects are never followed.
	HTTPClient *http.Client
	// AuthURL, TokenURL and UserInfoURL replace Google's endpoints, for tests.
	AuthURL, TokenURL, UserInfoURL string
}

// Client runs the consent flow for one OAuth client and caches access tokens. It is safe for
// concurrent use; keep one per app for the life of the process.
type Client struct {
	oauth                oauth2.Config
	required             []string
	includeGrantedScopes bool
	userInfoURL          string
	http                 *http.Client
	now                  func() time.Time

	mu      sync.Mutex
	tokens  map[[32]byte]accessToken
	flights map[[32]byte]*flight
}

// New validates cfg and returns a Client. It does not contact Google.
func New(cfg Config) (*Client, error) {
	var problems []error
	if strings.TrimSpace(cfg.ClientID) == "" {
		problems = append(problems, errors.New("googleauth: client id is required"))
	}
	if strings.TrimSpace(cfg.ClientSecret) == "" {
		problems = append(problems, errors.New("googleauth: client secret is required"))
	}
	if u, err := url.Parse(cfg.RedirectURL); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.Fragment != "" {
		problems = append(problems, fmt.Errorf("googleauth: redirect URL %q must be an absolute http(s) URL", cfg.RedirectURL))
	}
	if err := errors.Join(problems...); err != nil {
		return nil, err
	}

	identity := []string{"openid", "email"}
	required := scopeList(identity, cfg.Scopes)
	endpoint := endpoints.Google
	endpoint.AuthStyle = oauth2.AuthStyleInParams // as google.Endpoint; auto-detection tries a header first
	if cfg.AuthURL != "" {
		endpoint.AuthURL = cfg.AuthURL
	}
	if cfg.TokenURL != "" {
		endpoint.TokenURL = cfg.TokenURL
	}
	userInfo := UserInfoURL
	if cfg.UserInfoURL != "" {
		userInfo = cfg.UserInfoURL
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: Timeout}
	}
	// The client secret and bearer tokens never follow a redirect.
	noRedirect := *client
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	return &Client{
		oauth: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Endpoint:     endpoint,
			Scopes:       scopeList(required, cfg.OptionalScopes),
		},
		required:             required,
		includeGrantedScopes: cfg.IncludeGrantedScopes,
		userInfoURL:          userInfo,
		http:                 &noRedirect,
		now:                  time.Now,
		tokens:               make(map[[32]byte]accessToken),
		flights:              make(map[[32]byte]*flight),
	}, nil
}

// NewState returns a fresh OAuth state, 128 random bits as text. The app keeps it with the
// person who started the consent for StateTTL and accepts it in the callback once, for that
// person only.
func NewState() string {
	return rand.Text()
}

// AuthCodeURL is Google's consent page for state: offline access (a refresh token) and the
// consent screen every time (prompt=consent), so the person sees what they grant and a reconnect
// always yields a new refresh token. loginHint, an e-mail address or empty, preselects the
// account. The URL carries a PKCE challenge (S256); the app stores the returned verifier with the
// state and hands it to Exchange, so a code taken from the callback is worthless elsewhere.
func (c *Client) AuthCodeURL(state, loginHint string) (url, verifier string) {
	verifier = oauth2.GenerateVerifier()
	opts := []oauth2.AuthCodeOption{oauth2.AccessTypeOffline, oauth2.ApprovalForce, oauth2.S256ChallengeOption(verifier)}
	if loginHint != "" {
		opts = append(opts, oauth2.SetAuthURLParam("login_hint", loginHint))
	}
	if c.includeGrantedScopes {
		opts = append(opts, oauth2.SetAuthURLParam("include_granted_scopes", "true"))
	}
	return c.oauth.AuthCodeURL(state, opts...), verifier
}

// Grant is what a completed consent gives the app to store.
type Grant struct {
	// RefreshToken is the long-lived credential. Seal it (Key.Seal with Email) before storing.
	RefreshToken string
	// Scopes are the scopes the person granted, as Google names them (email, for example, as
	// https://www.googleapis.com/auth/userinfo.email).
	Scopes []string
	// Subject is Google's stable id of the account; the e-mail may change, Subject does not.
	Subject string
	// Email is the account's verified e-mail in lower case: what the app shows and seals with.
	Email string
}

// Exchange trades the callback's code for a Grant. The app has already checked the state and
// read the callback's error parameter (access_denied when the person cancels). Exchange fails
// with ErrMissingScopes when a required scope was not granted, with ErrInvalidGrant for a code
// that is expired or used, and when the account has no verified e-mail. verifier is the one
// AuthCodeURL returned for this state. The access token that comes with the grant is cached for
// AccessToken.
func (c *Client) Exchange(ctx context.Context, code, verifier string) (Grant, error) {
	if strings.TrimSpace(code) == "" {
		return Grant{}, errors.New("googleauth: the callback carries no code")
	}
	if verifier == "" {
		return Grant{}, errors.New("googleauth: no PKCE verifier for this state")
	}
	token, err := c.oauth.Exchange(c.withHTTP(ctx), code, oauth2.VerifierOption(verifier))
	if err != nil {
		return Grant{}, tokenError("exchange", err)
	}
	if token.RefreshToken == "" {
		return Grant{}, errors.New("googleauth: exchange: Google sent no refresh token")
	}
	scope, _ := token.Extra("scope").(string)
	granted := strings.Fields(scope)
	if missing := missingScopes(c.required, granted); len(missing) > 0 {
		return Grant{}, fmt.Errorf("%w: %s", ErrMissingScopes, strings.Join(missing, " "))
	}
	subject, email, err := c.account(ctx, token.AccessToken)
	if err != nil {
		return Grant{}, err
	}
	c.mu.Lock()
	c.keep(tokenKey(token.RefreshToken), token.AccessToken, token.ExpiresIn)
	c.mu.Unlock()
	return Grant{RefreshToken: token.RefreshToken, Scopes: granted, Subject: subject, Email: email}, nil
}

// account names the Google account behind an access token (OpenID Connect userinfo).
func (c *Client) account(ctx context.Context, accessToken string) (subject, email string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.userInfoURL, nil)
	if err != nil {
		return "", "", fmt.Errorf("googleauth: userinfo: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("googleauth: userinfo: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("googleauth: userinfo: HTTP %d", resp.StatusCode)
	}
	var info struct {
		Sub      string `json:"sub"`
		Email    string `json:"email"`
		Verified bool   `json:"email_verified"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxUserInfoBytes)).Decode(&info); err != nil {
		return "", "", fmt.Errorf("googleauth: userinfo: %w", err)
	}
	email = strings.ToLower(strings.TrimSpace(info.Email))
	if info.Sub == "" || email == "" {
		return "", "", errors.New("googleauth: userinfo names no account")
	}
	if !info.Verified {
		return "", "", errors.New("googleauth: the account's e-mail is not verified")
	}
	return info.Sub, email, nil
}

// withHTTP hands the Client's HTTP client to x/oauth2.
func (c *Client) withHTTP(ctx context.Context) context.Context {
	return context.WithValue(ctx, oauth2.HTTPClient, c.http)
}

// tokenError keeps token endpoint bodies out of error strings (x/oauth2 prints a body without
// an OAuth error code) and maps invalid_grant to ErrInvalidGrant.
func tokenError(op string, err error) error {
	var retrieve *oauth2.RetrieveError
	if !errors.As(err, &retrieve) {
		return fmt.Errorf("googleauth: %s: %w", op, err)
	}
	if retrieve.ErrorCode == "invalid_grant" {
		return ErrInvalidGrant
	}
	status := 0
	if retrieve.Response != nil {
		status = retrieve.Response.StatusCode
	}
	if retrieve.ErrorCode != "" {
		return fmt.Errorf("googleauth: %s: HTTP %d %s", op, status, retrieve.ErrorCode)
	}
	return fmt.Errorf("googleauth: %s: HTTP %d", op, status)
}

// Google reports the identity scopes in their long form.
var scopeAliases = map[string]string{
	"https://www.googleapis.com/auth/openid":           "openid",
	"https://www.googleapis.com/auth/userinfo.email":   "email",
	"https://www.googleapis.com/auth/userinfo.profile": "profile",
}

func canonicalScope(scope string) string {
	if short, ok := scopeAliases[scope]; ok {
		return short
	}
	return scope
}

// scopeList joins scope lists in order without empty entries and duplicates.
func scopeList(lists ...[]string) []string {
	var out []string
	seen := map[string]bool{}
	for _, list := range lists {
		for _, scope := range list {
			scope = strings.TrimSpace(scope)
			if scope == "" || seen[canonicalScope(scope)] {
				continue
			}
			seen[canonicalScope(scope)] = true
			out = append(out, scope)
		}
	}
	return out
}

// missingScopes returns the required scopes that granted lacks.
func missingScopes(required, granted []string) []string {
	have := map[string]bool{}
	for _, scope := range granted {
		have[canonicalScope(scope)] = true
	}
	var missing []string
	for _, scope := range required {
		if !have[canonicalScope(scope)] {
			missing = append(missing, scope)
		}
	}
	return missing
}
