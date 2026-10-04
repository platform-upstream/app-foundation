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
	if err := auth.Authenticate(req); err != nil {
		t.Fatalf("Authenticate: %v", err)
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
	})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
