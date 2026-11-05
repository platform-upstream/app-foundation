package googleauth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const calendarScope = "https://www.googleapis.com/auth/calendar.app.created"

// fake answers like Google's token and userinfo endpoints. Handlers see the parsed form.
type fake struct {
	srv       *httptest.Server
	token     func(w http.ResponseWriter, form url.Values)
	userinfo  func(w http.ResponseWriter, r *http.Request)
	refreshes atomic.Int32
	exchanges atomic.Int32
	userinfos atomic.Int32
}

func newFake(t *testing.T) *fake {
	t.Helper()
	f := &fake{
		token: func(w http.ResponseWriter, form url.Values) {
			if form.Get("grant_type") == "refresh_token" {
				writeJSON(w, http.StatusOK, map[string]any{"access_token": "access-" + form.Get("refresh_token"), "expires_in": 3599, "token_type": "Bearer"})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"access_token": "access-from-code", "refresh_token": "1//refresh", "expires_in": 3599, "token_type": "Bearer",
				"scope": "openid " + calendarScope + " https://www.googleapis.com/auth/userinfo.email",
			})
		},
		userinfo: func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{"sub": "1234567890", "email": " Alice@Example.org", "email_verified": true})
		},
	}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			if r.Method != http.MethodPost || r.Header.Get("Authorization") != "" {
				t.Errorf("token request %s with Authorization %q; Google wants the client in the form", r.Method, r.Header.Get("Authorization"))
			}
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			if r.PostForm.Get("client_id") != "client-id" || r.PostForm.Get("client_secret") != "client-secret" {
				writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid_client"})
				return
			}
			if r.PostForm.Get("grant_type") == "refresh_token" {
				f.refreshes.Add(1)
			} else {
				f.exchanges.Add(1)
			}
			f.token(w, r.PostForm)
		case "/userinfo":
			f.userinfos.Add(1)
			if r.Header.Get("Authorization") != "Bearer access-from-code" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			f.userinfo(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fake) config() Config {
	return Config{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		RedirectURL:  "https://app.example.org/api/google/callback",
		Scopes:       []string{calendarScope},
		HTTPClient:   f.srv.Client(),
		AuthURL:      "https://accounts.example.org/o/oauth2/auth",
		TokenURL:     f.srv.URL + "/token",
		UserInfoURL:  f.srv.URL + "/userinfo",
	}
}

func (f *fake) client(t *testing.T, edit func(*Config)) *Client {
	t.Helper()
	cfg := f.config()
	if edit != nil {
		edit(&cfg)
	}
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// clock is a settable test clock.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func TestNewRefusesAnIncompleteClient(t *testing.T) {
	ok := Config{ClientID: "id", ClientSecret: "secret", RedirectURL: "https://app.example.org/api/google/callback"}
	cases := []struct {
		name string
		edit func(*Config)
		want string // in the error; empty means New succeeds
	}{
		{"complete", func(*Config) {}, ""},
		{"local preview", func(c *Config) { c.RedirectURL = "http://127.0.0.1:8124/api/google/callback" }, ""},
		{"no client id", func(c *Config) { c.ClientID = " " }, "client id"},
		{"no secret", func(c *Config) { c.ClientSecret = "" }, "client secret"},
		{"no redirect", func(c *Config) { c.RedirectURL = "" }, "redirect URL"},
		{"relative redirect", func(c *Config) { c.RedirectURL = "/api/google/callback" }, "redirect URL"},
		{"other scheme", func(c *Config) { c.RedirectURL = "app://callback" }, "redirect URL"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := ok
			tc.edit(&cfg)
			_, err := New(cfg)
			if tc.want == "" && err != nil {
				t.Fatalf("New: %v", err)
			}
			if tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("New error = %v, want one about %s", err, tc.want)
			}
		})
	}
}

func TestAuthCodeURL(t *testing.T) {
	f := newFake(t)
	cases := []struct {
		name      string
		edit      func(*Config)
		loginHint string
		want      url.Values
	}{
		{
			name: "required scopes after openid and email",
			want: url.Values{"scope": {"openid email " + calendarScope}},
		},
		{
			name:      "login hint",
			loginHint: "alice@example.org",
			want:      url.Values{"scope": {"openid email " + calendarScope}, "login_hint": {"alice@example.org"}},
		},
		{
			name: "optional scopes asked too, duplicates once",
			edit: func(c *Config) {
				c.Scopes = []string{"https://www.googleapis.com/auth/userinfo.email", "https://www.googleapis.com/auth/drive", " "}
				c.OptionalScopes = []string{"https://www.googleapis.com/auth/gmail.readonly", "https://www.googleapis.com/auth/drive"}
			},
			want: url.Values{"scope": {"openid email https://www.googleapis.com/auth/drive https://www.googleapis.com/auth/gmail.readonly"}},
		},
		{
			name: "include granted scopes only when switched on",
			edit: func(c *Config) { c.IncludeGrantedScopes = true },
			want: url.Values{"scope": {"openid email " + calendarScope}, "include_granted_scopes": {"true"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := f.client(t, tc.edit)
			state := NewState()
			u, err := url.Parse(c.AuthCodeURL(state, tc.loginHint))
			if err != nil {
				t.Fatal(err)
			}
			if u.Host != "accounts.example.org" || u.Path != "/o/oauth2/auth" {
				t.Fatalf("consent URL %s", u)
			}
			want := url.Values{
				"response_type": {"code"},
				"client_id":     {"client-id"},
				"redirect_uri":  {"https://app.example.org/api/google/callback"},
				"access_type":   {"offline"},
				"prompt":        {"consent"},
				"state":         {state},
			}
			for k, v := range tc.want {
				want[k] = v
			}
			if got := u.Query(); !reflect.DeepEqual(got, want) {
				t.Fatalf("query\n got %v\nwant %v", got, want)
			}
		})
	}
}

func TestNewStateIsFreshText(t *testing.T) {
	a, b := NewState(), NewState()
	if a == b || len(a) < 26 || url.QueryEscape(a) != a {
		t.Fatalf("states %q %q", a, b)
	}
}

func TestExchange(t *testing.T) {
	okToken := func(scope string) func(http.ResponseWriter, url.Values) {
		return func(w http.ResponseWriter, form url.Values) {
			writeJSON(w, http.StatusOK, map[string]any{"access_token": "access-from-code", "refresh_token": "1//refresh", "expires_in": 3599, "scope": scope})
		}
	}
	cases := []struct {
		name     string
		code     string
		edit     func(*Config)
		token    func(http.ResponseWriter, url.Values)
		userinfo func(http.ResponseWriter, *http.Request)
		want     Grant
		wantErr  error  // errors.Is
		errText  string // in the error
		noText   string // never in the error
	}{
		{
			name: "grant with the account in lower case",
			code: "4/code",
			want: Grant{
				RefreshToken: "1//refresh", Subject: "1234567890", Email: "alice@example.org",
				Scopes: []string{"openid", calendarScope, "https://www.googleapis.com/auth/userinfo.email"},
			},
		},
		{
			name:  "optional scope unticked",
			code:  "4/code",
			edit:  func(c *Config) { c.OptionalScopes = []string{"https://www.googleapis.com/auth/gmail.readonly"} },
			token: okToken("openid https://www.googleapis.com/auth/userinfo.email " + calendarScope),
			want: Grant{
				RefreshToken: "1//refresh", Subject: "1234567890", Email: "alice@example.org",
				Scopes: []string{"openid", "https://www.googleapis.com/auth/userinfo.email", calendarScope},
			},
		},
		{
			name:    "required scope unticked",
			code:    "4/code",
			token:   okToken("openid https://www.googleapis.com/auth/userinfo.email"),
			wantErr: ErrMissingScopes,
			errText: calendarScope,
		},
		{
			name:    "email unticked",
			code:    "4/code",
			token:   okToken("openid " + calendarScope),
			wantErr: ErrMissingScopes,
			errText: "email",
		},
		{
			name: "no refresh token",
			code: "4/code",
			token: func(w http.ResponseWriter, _ url.Values) {
				writeJSON(w, http.StatusOK, map[string]any{"access_token": "a", "expires_in": 3599})
			},
			errText: "no refresh token",
		},
		{
			name: "used code",
			code: "4/used",
			token: func(w http.ResponseWriter, _ url.Values) {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant", "error_description": "Bad Request"})
			},
			wantErr: ErrInvalidGrant,
		},
		{
			name:    "wrong client secret",
			code:    "4/code",
			edit:    func(c *Config) { c.ClientSecret = "rotated" },
			errText: "HTTP 401 invalid_client",
		},
		{
			name: "token endpoint failure keeps the body out",
			code: "4/code",
			token: func(w http.ResponseWriter, _ url.Values) {
				w.Header().Set("Content-Type", "text/html")
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte("<html>upstream said something private</html>"))
			},
			errText: "HTTP 502",
			noText:  "private",
		},
		{
			name: "unverified e-mail",
			code: "4/code",
			userinfo: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, http.StatusOK, map[string]any{"sub": "1", "email": "alice@example.org"})
			},
			errText: "not verified",
		},
		{
			name: "no account",
			code: "4/code",
			userinfo: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, http.StatusOK, map[string]any{"email_verified": true})
			},
			errText: "no account",
		},
		{
			name:     "userinfo refused",
			code:     "4/code",
			userinfo: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) },
			errText:  "userinfo: HTTP 403",
		},
		{
			name:    "no code",
			code:    " ",
			errText: "no code",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake(t)
			if tc.token != nil {
				f.token = tc.token
			}
			if tc.userinfo != nil {
				f.userinfo = tc.userinfo
			}
			var form url.Values
			inner := f.token
			f.token = func(w http.ResponseWriter, v url.Values) { form = v; inner(w, v) }
			c := f.client(t, tc.edit)

			grant, err := c.Exchange(context.Background(), tc.code)
			if tc.wantErr == nil && tc.errText == "" {
				if err != nil {
					t.Fatalf("Exchange: %v", err)
				}
				if !reflect.DeepEqual(grant, tc.want) {
					t.Fatalf("grant\n got %+v\nwant %+v", grant, tc.want)
				}
				want := url.Values{"grant_type": {"authorization_code"}, "code": {tc.code}, "redirect_uri": {"https://app.example.org/api/google/callback"},
					"client_id": {"client-id"}, "client_secret": {"client-secret"}}
				if !reflect.DeepEqual(form, want) {
					t.Fatalf("token form %v", form)
				}
				return
			}
			if err == nil {
				t.Fatalf("Exchange succeeded with %+v", grant)
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("error %v, want %v", err, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.errText) || (tc.noText != "" && strings.Contains(err.Error(), tc.noText)) {
				t.Fatalf("error %q", err)
			}
			if !reflect.DeepEqual(grant, Grant{}) {
				t.Fatalf("a failed exchange returned %+v", grant)
			}
			if errors.Is(err, ErrMissingScopes) && f.userinfos.Load() != 0 {
				t.Fatal("asked for the account of a refused grant")
			}
		})
	}
}

func TestExchangeCachesTheAccessToken(t *testing.T) {
	f := newFake(t)
	c := f.client(t, nil)
	grant, err := c.Exchange(context.Background(), "4/code")
	if err != nil {
		t.Fatal(err)
	}
	token, err := c.AccessToken(context.Background(), grant.RefreshToken)
	if err != nil || token != "access-from-code" {
		t.Fatalf("AccessToken = %q, %v", token, err)
	}
	if n := f.refreshes.Load(); n != 0 {
		t.Fatalf("%d refreshes right after the exchange", n)
	}
}

func TestAccessTokenCachesUntilShortlyBeforeExpiry(t *testing.T) {
	f := newFake(t)
	var answers atomic.Int32
	f.token = func(w http.ResponseWriter, form url.Values) {
		if form.Get("refresh_token") != "1//alice" {
			t.Errorf("refresh form %v", form)
		}
		n := answers.Add(1)
		writeJSON(w, http.StatusOK, map[string]any{"access_token": "access-" + string(rune('0'+n)), "expires_in": 3599})
	}
	c := f.client(t, nil)
	clk := &clock{now: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	c.now = clk.Now
	get := func() string {
		t.Helper()
		token, err := c.AccessToken(context.Background(), "1//alice")
		if err != nil {
			t.Fatal(err)
		}
		return token
	}

	if got := get(); got != "access-1" {
		t.Fatalf("first = %q", got)
	}
	clk.Advance(3599*time.Second - 31*time.Second)
	if got := get(); got != "access-1" || f.refreshes.Load() != 1 {
		t.Fatalf("with 31s left: %q after %d refreshes", got, f.refreshes.Load())
	}
	clk.Advance(2 * time.Second)
	if got := get(); got != "access-2" || f.refreshes.Load() != 2 {
		t.Fatalf("with 29s left: %q after %d refreshes", got, f.refreshes.Load())
	}
}

func TestAccessTokenKeepsGrantsApart(t *testing.T) {
	f := newFake(t)
	c := f.client(t, nil)
	for _, refresh := range []string{"1//alice", "1//bob", "1//alice"} {
		token, err := c.AccessToken(context.Background(), refresh)
		if err != nil || token != "access-"+refresh {
			t.Fatalf("AccessToken(%s) = %q, %v", refresh, token, err)
		}
	}
	if n := f.refreshes.Load(); n != 2 {
		t.Fatalf("%d refreshes for two grants", n)
	}
}

func TestAccessTokenSharesOneRefresh(t *testing.T) {
	f := newFake(t)
	release := blockRefreshes(t, f)
	c := f.client(t, nil)

	const callers = 20
	var wg sync.WaitGroup
	tokens := make([]string, callers)
	errs := make([]error, callers)
	for i := range callers {
		wg.Go(func() { tokens[i], errs[i] = c.AccessToken(context.Background(), "1//alice") })
	}
	waitFor(t, func() bool { return f.refreshes.Load() >= 1 })
	release()
	wg.Wait()
	for i := range callers {
		if errs[i] != nil || tokens[i] != "access-1//alice" {
			t.Fatalf("caller %d: %q, %v", i, tokens[i], errs[i])
		}
	}
	if n := f.refreshes.Load(); n != 1 {
		t.Fatalf("%d refreshes for %d concurrent callers", n, callers)
	}
}

func TestAccessTokenCallerMayGiveUpWithoutFailingOthers(t *testing.T) {
	f := newFake(t)
	release := blockRefreshes(t, f)
	c := f.client(t, nil)

	ctx, cancel := context.WithCancel(context.Background())
	gaveUp := make(chan error, 1)
	go func() {
		_, err := c.AccessToken(ctx, "1//alice")
		gaveUp <- err
	}()
	waitFor(t, func() bool { return f.refreshes.Load() >= 1 })
	cancel()
	if err := <-gaveUp; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled caller got %v", err)
	}

	other := make(chan string, 1)
	go func() {
		token, _ := c.AccessToken(context.Background(), "1//alice")
		other <- token
	}()
	release()
	if token := <-other; token != "access-1//alice" {
		t.Fatalf("second caller got %q", token)
	}
	if n := f.refreshes.Load(); n != 1 {
		t.Fatalf("%d refreshes", n)
	}

	done, stop := context.WithCancel(context.Background())
	stop()
	if _, err := c.AccessToken(done, "1//alice"); !errors.Is(err, context.Canceled) {
		t.Fatalf("a done context got %v", err)
	}
}

func TestAccessTokenErrors(t *testing.T) {
	cases := []struct {
		name    string
		refresh string
		token   func(http.ResponseWriter, url.Values)
		wantErr error
		errText string
	}{
		{
			name:    "revoked grant",
			refresh: "1//revoked",
			token: func(w http.ResponseWriter, _ url.Values) {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant", "error_description": "Token has been expired or revoked."})
			},
			wantErr: ErrInvalidGrant,
		},
		{
			name:    "Google down",
			refresh: "1//alice",
			token:   func(w http.ResponseWriter, _ url.Values) { w.WriteHeader(http.StatusServiceUnavailable) },
			errText: "refresh: HTTP 503",
		},
		{
			name:    "no refresh token",
			refresh: "",
			errText: "no refresh token",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake(t)
			if tc.token != nil {
				f.token = tc.token
			}
			c := f.client(t, nil)
			for range 2 { // a failure is not cached: the second call asks again
				token, err := c.AccessToken(context.Background(), tc.refresh)
				if err == nil || token != "" {
					t.Fatalf("AccessToken = %q, %v", token, err)
				}
				if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
					t.Fatalf("error %v, want %v", err, tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.errText) {
					t.Fatalf("error %q", err)
				}
			}
			if want := int32(2); tc.refresh != "" && f.refreshes.Load() != want {
				t.Fatalf("%d refreshes, want %d", f.refreshes.Load(), want)
			}
		})
	}
}

func TestExpiredTokensLeaveTheCache(t *testing.T) {
	f := newFake(t)
	c := f.client(t, nil)
	clk := &clock{now: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	c.now = clk.Now
	if _, err := c.AccessToken(context.Background(), "1//alice"); err != nil {
		t.Fatal(err)
	}
	clk.Advance(2 * time.Hour)
	if _, err := c.AccessToken(context.Background(), "1//bob"); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.tokens[tokenKey("1//alice")]; ok || len(c.tokens) != 1 {
		t.Fatalf("cache holds %d tokens, alice's expired one included: %v", len(c.tokens), ok)
	}
}

func TestSecretsNeverFollowARedirect(t *testing.T) {
	var stolen atomic.Int32
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { stolen.Add(1) }))
	defer elsewhere.Close()
	f := newFake(t)
	f.token = func(w http.ResponseWriter, _ url.Values) {
		w.Header().Set("Location", elsewhere.URL+"/token")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}
	c := f.client(t, nil)
	if _, err := c.Exchange(context.Background(), "4/code"); err == nil {
		t.Fatal("exchange through a redirect succeeded")
	}
	if _, err := c.AccessToken(context.Background(), "1//alice"); err == nil {
		t.Fatal("refresh through a redirect succeeded")
	}
	if stolen.Load() != 0 {
		t.Fatal("a request followed the redirect")
	}
}

func TestMissingScopes(t *testing.T) {
	cases := []struct {
		required, granted, want []string
	}{
		{[]string{"openid", "email"}, []string{"openid", "https://www.googleapis.com/auth/userinfo.email"}, nil},
		{[]string{"openid", "email"}, []string{"https://www.googleapis.com/auth/openid", "email"}, nil},
		{[]string{"openid", "email", calendarScope}, []string{"openid"}, []string{"email", calendarScope}},
		{[]string{"https://www.googleapis.com/auth/calendar"}, []string{calendarScope}, []string{"https://www.googleapis.com/auth/calendar"}},
		{[]string{"profile"}, []string{"https://www.googleapis.com/auth/userinfo.profile"}, nil},
	}
	for _, tc := range cases {
		if got := missingScopes(tc.required, tc.granted); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("missingScopes(%v, %v) = %v, want %v", tc.required, tc.granted, got, tc.want)
		}
	}
}

// blockRefreshes holds the fake's token answers until release is called (at the latest when the
// test ends, so a failing test does not hang in the server's Close).
func blockRefreshes(t *testing.T, f *fake) (release func()) {
	gate := make(chan struct{})
	var once sync.Once
	release = func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release)
	inner := f.token
	f.token = func(w http.ResponseWriter, form url.Values) { <-gate; inner(w, form) }
	return release
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(time.Millisecond)
	}
}
