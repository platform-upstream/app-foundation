package access

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

func TestCloudflareGate(t *testing.T) {
	key := generateKey(t)
	const (
		kid    = "test-key"
		issuer = "https://test.cloudflareaccess.com"
		aud    = "test-audience"
	)
	jwks := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJWKS(t, w, kid, &key.PublicKey)
	}))
	defer jwks.Close()

	auth := newCloudflare(issuer, jwks.URL, aud, jwks.Client())
	token := mint(t, key, kid, issuer, aud, "owner", time.Now())
	req := httptest.NewRequest(http.MethodGet, "/api/items", nil)
	req.Header.Set(AssertionHeader, token)
	if caller, err := auth.Identify(req); err != nil || !caller.Person() || caller.Subject != "owner" || caller.Local {
		t.Fatalf("Identify = %+v, %v", caller, err)
	}

	wrongAudience := mint(t, key, kid, issuer, "wrong", "owner", time.Now())
	req.Header.Set(AssertionHeader, wrongAudience)
	if err := auth.Authenticate(req); !errors.Is(err, ErrInvalidAssertion) {
		t.Fatalf("wrong audience error = %v", err)
	}

	invalidClaims := []struct {
		name  string
		token string
	}{
		{"expired", mint(t, key, kid, issuer, aud, "owner", time.Now().Add(-10*time.Minute))},
		{"missing subject", mint(t, key, kid, issuer, aud, "", time.Now())},
		{"missing issued-at", sign(t, key, kid, map[string]any{
			"iss": issuer, "aud": aud, "sub": "owner", "exp": time.Now().Add(5 * time.Minute).Unix(),
		})},
		{"missing expiry", sign(t, key, kid, map[string]any{
			"iss": issuer, "aud": aud, "sub": "owner", "iat": time.Now().Unix(),
		})},
		{"wrong issuer", mint(t, key, kid, "https://evil.example", aud, "owner", time.Now())},
	}
	for _, tc := range invalidClaims {
		t.Run(tc.name, func(t *testing.T) {
			req.Header.Set(AssertionHeader, tc.token)
			if err := auth.Authenticate(req); !errors.Is(err, ErrInvalidAssertion) {
				t.Fatalf("Authenticate error = %v", err)
			}
		})
	}

	req.Header.Del(AssertionHeader)
	if err := auth.Authenticate(req); !errors.Is(err, ErrMissingAssertion) {
		t.Fatalf("missing assertion error = %v", err)
	}
	req.Header.Set(AssertionHeader, strings.Repeat("x", maxAssertionSize+1))
	if err := auth.Authenticate(req); !errors.Is(err, ErrAssertionTooLarge) {
		t.Fatalf("oversized assertion error = %v", err)
	}
	req.Header[AssertionHeader] = []string{token, token}
	if err := auth.Authenticate(req); !errors.Is(err, ErrInvalidAssertion) {
		t.Fatalf("duplicate assertion error = %v", err)
	}
}

func TestServiceTokenAcceptedOnlyWhenListed(t *testing.T) {
	key := generateKey(t)
	const (
		kid    = "test-key"
		issuer = "https://test.cloudflareaccess.com"
		aud    = "test-audience"
	)
	jwks := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJWKS(t, w, kid, &key.PublicKey)
	}))
	defer jwks.Close()
	serviceToken := func(commonName string) string {
		return sign(t, key, kid, map[string]any{
			"iss": issuer, "aud": aud, "sub": "", "common_name": commonName,
			"iat": time.Now().Unix(), "exp": time.Now().Add(5 * time.Minute).Unix(),
		})
	}
	req := httptest.NewRequest(http.MethodGet, "/api/items", nil)

	auth := newCloudflare(issuer, jwks.URL, aud, jwks.Client())
	auth.serviceTokens = map[string]struct{}{"listed.access": {}}
	req.Header.Set(AssertionHeader, serviceToken("listed.access"))
	if caller, err := auth.Identify(req); err != nil || caller.Person() || caller.ServiceToken != "listed.access" {
		t.Fatalf("listed service token = %+v, %v", caller, err)
	}
	for _, commonName := range []string{"other.access", ""} {
		req.Header.Set(AssertionHeader, serviceToken(commonName))
		if err := auth.Authenticate(req); !errors.Is(err, ErrInvalidAssertion) {
			t.Fatalf("common_name %q error = %v", commonName, err)
		}
	}

	if _, err := New(Config{TeamDomain: "test.cloudflareaccess.com", Audience: aud, ServiceTokenClientIDs: []string{" x"}}); err == nil {
		t.Fatal("whitespace client ID was accepted")
	}
	configured, err := New(Config{TeamDomain: "test.cloudflareaccess.com", Audience: aud, ServiceTokenClientIDs: []string{"listed.access"}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, ok := configured.serviceTokens["listed.access"]; !ok || len(configured.serviceTokens) != 1 {
		t.Fatalf("configured service tokens = %v", configured.serviceTokens)
	}
}

func TestCallerCarriesEmailForPeopleOnly(t *testing.T) {
	key := generateKey(t)
	const (
		kid    = "test-key"
		issuer = "https://test.cloudflareaccess.com"
		aud    = "test-audience"
	)
	jwks := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJWKS(t, w, kid, &key.PublicKey)
	}))
	defer jwks.Close()
	auth := newCloudflare(issuer, jwks.URL, aud, jwks.Client())
	auth.serviceTokens = map[string]struct{}{"listed.access": {}}
	claims := func(subject, email, commonName string) string {
		return sign(t, key, kid, map[string]any{
			"iss": issuer, "aud": aud, "sub": subject, "email": email, "common_name": commonName,
			"iat": time.Now().Unix(), "exp": time.Now().Add(5 * time.Minute).Unix(),
		})
	}
	req := httptest.NewRequest(http.MethodGet, "/api/items", nil)

	req.Header.Set(AssertionHeader, claims("owner", "Owner@Example.com", ""))
	if caller, err := auth.Identify(req); err != nil || caller.Email != "Owner@Example.com" || caller.Subject != "owner" {
		t.Fatalf("person = %+v, %v", caller, err)
	}
	req.Header.Set(AssertionHeader, mint(t, key, kid, issuer, aud, "owner", time.Now()))
	if caller, err := auth.Identify(req); err != nil || caller.Email != "" {
		t.Fatalf("person without email claim = %+v, %v", caller, err)
	}
	req.Header.Set(AssertionHeader, claims("", "token@example.com", "listed.access"))
	if caller, err := auth.Identify(req); err != nil || caller.Email != "" || caller.ServiceToken != "listed.access" {
		t.Fatalf("service token = %+v, %v", caller, err)
	}
}

func TestBoundedJWKSRefreshAndCooldown(t *testing.T) {
	key1 := generateKey(t)
	key2 := generateKey(t)
	type servedKey struct {
		id  string
		key *rsa.PublicKey
	}
	var current atomic.Pointer[servedKey]
	current.Store(&servedKey{id: "key-1", key: &key1.PublicKey})
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		value := current.Load()
		writeJWKS(t, w, value.id, value.key)
	}))
	defer server.Close()

	clock := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	keys := newBoundedRemoteKeySet(server.URL, server.Client())
	keys.now = func() time.Time { return clock }
	token1 := sign(t, key1, "key-1", map[string]any{"sub": "one"})
	token2 := sign(t, key2, "key-2", map[string]any{"sub": "two"})

	if _, err := keys.VerifySignature(context.Background(), token1); err != nil {
		t.Fatalf("initial verify: %v", err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("initial JWKS requests = %d, want 1", got)
	}
	if _, err := keys.VerifySignature(context.Background(), token1); err != nil {
		t.Fatalf("cached verify: %v", err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("cached verification fetched JWKS: %d", got)
	}

	current.Store(&servedKey{id: "key-2", key: &key2.PublicKey})
	clock = clock.Add(10 * time.Second)
	for i := 0; i < 4; i++ {
		if _, err := keys.VerifySignature(context.Background(), token2); !errors.Is(err, errJWKSRefreshCooldown) {
			t.Fatalf("unknown key during cooldown error = %v", err)
		}
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("unknown-key requests bypassed cooldown: %d", got)
	}

	clock = clock.Add(21 * time.Second)
	if _, err := keys.VerifySignature(context.Background(), token2); err != nil {
		t.Fatalf("rotated key verify after cooldown: %v", err)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("rotation JWKS requests = %d, want 2", got)
	}

	clock = clock.Add(jwksCacheMaxAge + time.Second)
	if _, err := keys.VerifySignature(context.Background(), token2); err != nil {
		t.Fatalf("verify after cache max age: %v", err)
	}
	if got := requests.Load(); got != 3 {
		t.Fatalf("stale valid key did not refresh: requests = %d", got)
	}
}

func TestConcurrentRefreshUsesNewGenerationDuringCooldown(t *testing.T) {
	key1 := generateKey(t)
	key2 := generateKey(t)
	type servedKey struct {
		id  string
		key *rsa.PublicKey
	}
	var current atomic.Pointer[servedKey]
	current.Store(&servedKey{id: "key-1", key: &key1.PublicKey})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		value := current.Load()
		writeJWKS(t, w, value.id, value.key)
	}))
	defer server.Close()

	clock := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	keys := newBoundedRemoteKeySet(server.URL, server.Client())
	keys.now = func() time.Time { return clock }
	token1 := sign(t, key1, "key-1", map[string]any{"sub": "one"})
	token2 := sign(t, key2, "key-2", map[string]any{"sub": "two"})
	if _, err := keys.VerifySignature(context.Background(), token1); err != nil {
		t.Fatalf("prime old key: %v", err)
	}

	// Request A has already captured the old generation. Hold it at that exact point while
	// request B refreshes the rotated key and starts the cooldown.
	oldKeys, fresh, oldGeneration := keys.snapshot()
	parsed, err := jose.ParseSignedCompact(token2, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := verifyWithKeys(parsed, "key-2", oldKeys); ok {
		t.Fatal("old snapshot unexpectedly verifies rotated token")
	}
	current.Store(&servedKey{id: "key-2", key: &key2.PublicKey})
	clock = clock.Add(jwksCooldown + time.Second)

	bFinished := make(chan struct{})
	bResult := make(chan error, 1)
	go func() {
		_, err := keys.VerifySignature(context.Background(), token2)
		bResult <- err
		close(bFinished)
	}()

	aResult := make(chan error, 1)
	go func() {
		<-bFinished
		_, err := keys.verifyWithSnapshot(context.Background(), parsed, "key-2", oldKeys, fresh, oldGeneration)
		aResult <- err
	}()
	if err := <-bResult; err != nil {
		t.Fatalf("request B refresh: %v", err)
	}
	if err := <-aResult; err != nil {
		t.Fatalf("request A rejected keys from newer generation: %v", err)
	}
}

func TestStaleJWKSFailsClosedWhenRefreshFails(t *testing.T) {
	key := generateKey(t)
	var fail atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if fail.Load() {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		writeJWKS(t, w, "key-1", &key.PublicKey)
	}))
	defer server.Close()

	clock := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	keys := newBoundedRemoteKeySet(server.URL, server.Client())
	keys.now = func() time.Time { return clock }
	token := sign(t, key, "key-1", map[string]any{"sub": "one"})
	if _, err := keys.VerifySignature(context.Background(), token); err != nil {
		t.Fatalf("prime key: %v", err)
	}

	clock = clock.Add(jwksCacheMaxAge + time.Second)
	fail.Store(true)
	if _, err := keys.VerifySignature(context.Background(), token); err == nil {
		t.Fatal("stale key was accepted after required refresh failed")
	}
	if _, err := keys.VerifySignature(context.Background(), token); !errors.Is(err, errJWKSRefreshCooldown) {
		t.Fatalf("stale key during failed-refresh cooldown error = %v", err)
	}
}

func TestJWKSClientHasFiveSecondCeiling(t *testing.T) {
	if got := newBoundedRemoteKeySet("https://example.invalid", nil).client.Timeout; got != 5*time.Second {
		t.Fatalf("default timeout = %s", got)
	}
	provided := &http.Client{Timeout: time.Minute}
	if got := newBoundedRemoteKeySet("https://example.invalid", provided).client.Timeout; got != 5*time.Second {
		t.Fatalf("provided timeout ceiling = %s", got)
	}
}

func TestMiddlewareGatesAndUsesAppReject(t *testing.T) {
	auth, err := New(Config{Mode: ModeLocal, Listener: "127.0.0.1:8096"})
	if err != nil {
		t.Fatal(err)
	}
	rejected := false
	handler := auth.Middleware(func(w http.ResponseWriter, _ *http.Request, err error) {
		rejected = true
		if !errors.Is(err, ErrNonLoopbackPeer) {
			t.Errorf("reject error = %v", err)
		}
		http.Error(w, "local only", http.StatusUnauthorized)
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if caller, ok := CallerFrom(r.Context()); !ok || !caller.Local || caller.Person() {
			t.Errorf("caller in context = %+v, %v", caller, ok)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	local := httptest.NewRequest(http.MethodGet, "/", nil)
	local.RemoteAddr = "127.0.0.1:40000"
	localResult := httptest.NewRecorder()
	handler.ServeHTTP(localResult, local)
	if localResult.Code != http.StatusNoContent || rejected {
		t.Fatalf("local status = %d, rejected = %v", localResult.Code, rejected)
	}

	remote := httptest.NewRequest(http.MethodGet, "/", nil)
	remote.RemoteAddr = "192.0.2.5:40000"
	remoteResult := httptest.NewRecorder()
	handler.ServeHTTP(remoteResult, remote)
	if remoteResult.Code != http.StatusUnauthorized || !rejected {
		t.Fatalf("remote status = %d, rejected = %v", remoteResult.Code, rejected)
	}
}

func TestMiddlewareDefaultRejectUsesStandardEnvelope(t *testing.T) {
	var auth *Authenticator
	handler := auth.Middleware(nil)(http.NotFoundHandler())
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusUnauthorized || w.Header().Get("Content-Type") != "application/json; charset=utf-8" || w.Body.String() != "{\"error\":\"Sign-in required.\"}\n" {
		t.Fatalf("response = %d %q %q", w.Code, w.Header().Get("Content-Type"), w.Body.String())
	}
}

func TestAuthenticatorZeroValuesFailClosed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	var nilAuth *Authenticator
	if err := nilAuth.Authenticate(req); !errors.Is(err, ErrInvalidAssertion) {
		t.Fatalf("nil authenticator error = %v", err)
	}
	if err := (&Authenticator{}).Authenticate(req); !errors.Is(err, ErrInvalidAssertion) {
		t.Fatalf("zero authenticator error = %v", err)
	}
}

func TestLocalModeRequiresLoopbackListener(t *testing.T) {
	for _, address := range []string{":8096", "0.0.0.0:8096", "localhost:8096", "bad", "[::1]:8096"} {
		if _, err := New(Config{Mode: ModeLocal, Listener: address}); err == nil {
			t.Errorf("New(local, %q) succeeded", address)
		}
	}
}

func TestTeamDomainIsLowercaseSingleLabel(t *testing.T) {
	for _, domain := range []string{"n.cloudflareaccess.com", "acme.cloudflareaccess.com", strings.Repeat("a", 63) + ".cloudflareaccess.com"} {
		if !validTeamDomain(domain) {
			t.Errorf("valid team domain rejected: %q", domain)
		}
	}
	for _, domain := range []string{
		"-acme.cloudflareaccess.com",
		"acme-.cloudflareaccess.com",
		strings.Repeat("a", 64) + ".cloudflareaccess.com",
		"Acme.cloudflareaccess.com",
		"acme.dev.cloudflareaccess.com",
		"acme_test.cloudflareaccess.com",
		"acme.cloudflareaccess.com.evil.example",
	} {
		if validTeamDomain(domain) {
			t.Errorf("invalid team domain accepted: %q", domain)
		}
	}
}

func TestCloudflareConfigRejectsWhitespaceAudience(t *testing.T) {
	for _, audience := range []string{"", " audience", "audience ", "\taudience"} {
		if _, err := New(Config{Mode: ModeCloudflare, TeamDomain: "acme.cloudflareaccess.com", Audience: audience}); err == nil {
			t.Errorf("New accepted audience %q", audience)
		}
	}
}

func generateKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func writeJWKS(t *testing.T, w http.ResponseWriter, kid string, key *rsa.PublicKey) {
	t.Helper()
	_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{
		"kty": "RSA", "alg": "RS256", "use": "sig", "kid": kid,
		"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
	}}})
}

func mint(t *testing.T, key *rsa.PrivateKey, kid, issuer, audience, subject string, issuedAt time.Time) string {
	t.Helper()
	return sign(t, key, kid, map[string]any{
		"iss": issuer, "aud": audience, "sub": subject,
		"iat": issuedAt.Unix(), "exp": issuedAt.Add(5 * time.Minute).Unix(),
	})
}

func sign(t *testing.T, key *rsa.PrivateKey, kid string, claims map[string]any) string {
	t.Helper()
	options := (&jose.SignerOptions{}).WithHeader(jose.HeaderKey("kid"), kid)
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, options)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := signer.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := signed.CompactSerialize()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// The first request after a start is the one that fetches the key set. If that
// request is cancelled (the browser navigated away), the fetch must still
// complete: failing it would trip the cooldown and reject every caller for 30s.
func TestCancelledRequestDoesNotPoisonJWKSRefresh(t *testing.T) {
	key := generateKey(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJWKS(t, w, "key-1", &key.PublicKey)
	}))
	defer server.Close()

	keys := newBoundedRemoteKeySet(server.URL, server.Client())
	token := sign(t, key, "key-1", map[string]any{"sub": "one"})

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := keys.VerifySignature(cancelled, token); err != nil {
		t.Fatalf("verify with a cancelled request context: %v", err)
	}
	if _, err := keys.VerifySignature(context.Background(), token); err != nil {
		t.Fatalf("verify after a cancelled first request: %v", err)
	}
}
