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
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestCloudflareIdentity(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	const (
		kid    = "test-key"
		issuer = "https://test.cloudflareaccess.com"
		aud    = "test-audience"
	)
	jwks := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{
			"kty": "RSA", "alg": "RS256", "use": "sig", "kid": kid,
			"n": base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.PublicKey.E)).Bytes()),
		}}})
	}))
	defer jwks.Close()

	auth := newCloudflare(context.Background(), issuer, jwks.URL, aud, jwks.Client())
	token := mint(t, key, kid, issuer, aud, "owner", "owner@example.com", time.Now())
	req := httptest.NewRequest(http.MethodGet, "/api/items", nil)
	req.Header.Set(AssertionHeader, token)

	identity, err := auth.Authenticate(req)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if identity.Subject != "owner" {
		t.Fatalf("identity = %#v", identity)
	}

	wrongAudience := mint(t, key, kid, issuer, "wrong", "owner", "owner@example.com", time.Now())
	req.Header.Set(AssertionHeader, wrongAudience)
	if _, err := auth.Authenticate(req); !errors.Is(err, ErrInvalidAssertion) {
		t.Fatalf("wrong audience error = %v", err)
	}

	invalidClaims := []struct {
		name  string
		token string
	}{
		{"expired", mint(t, key, kid, issuer, aud, "owner", "owner@example.com", time.Now().Add(-10*time.Minute))},
		{"missing subject", mint(t, key, kid, issuer, aud, "", "owner@example.com", time.Now())},
		{"missing issued-at", sign(t, key, kid, jwt.MapClaims{
			"iss": issuer, "aud": aud, "sub": "owner", "exp": time.Now().Add(5 * time.Minute).Unix(),
		})},
		{"wrong issuer", mint(t, key, kid, "https://evil.example", aud, "owner", "owner@example.com", time.Now())},
	}
	for _, tc := range invalidClaims {
		t.Run(tc.name, func(t *testing.T) {
			req.Header.Set(AssertionHeader, tc.token)
			if _, err := auth.Authenticate(req); !errors.Is(err, ErrInvalidAssertion) {
				t.Fatalf("Authenticate error = %v", err)
			}
		})
	}

	req.Header.Del(AssertionHeader)
	if _, err := auth.Authenticate(req); !errors.Is(err, ErrMissingAssertion) {
		t.Fatalf("missing assertion error = %v", err)
	}
}

func TestMiddlewareAddsIdentityAndUsesAppReject(t *testing.T) {
	auth, err := New(context.Background(), Config{Mode: ModeLocal, Listener: "127.0.0.1:8096"})
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
		identity, ok := FromContext(r.Context())
		if !ok || identity.Subject != "local" {
			t.Errorf("context identity = %#v, %v", identity, ok)
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

func TestLocalModeRequiresLoopbackListener(t *testing.T) {
	for _, address := range []string{":8096", "0.0.0.0:8096", "localhost:8096", "bad"} {
		if _, err := New(context.Background(), Config{Mode: ModeLocal, Listener: address}); err == nil {
			t.Errorf("New(local, %q) succeeded", address)
		}
	}
	if _, err := New(context.Background(), Config{Mode: ModeLocal, Listener: "[::1]:8096"}); err == nil {
		t.Fatal("IPv6 listener accepted; local previews must bind literal 127.0.0.1")
	}
}

func mint(t *testing.T, key *rsa.PrivateKey, kid, issuer, audience, subject, email string, issuedAt time.Time) string {
	t.Helper()
	return sign(t, key, kid, jwt.MapClaims{
		"iss": issuer, "aud": audience, "sub": subject, "email": email,
		"iat": issuedAt.Unix(), "exp": issuedAt.Add(5 * time.Minute).Unix(),
	})
}

func sign(t *testing.T, key *rsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid
	raw, err := token.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
