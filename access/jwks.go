package access

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

const (
	jwksFetchTimeout = 5 * time.Second
	jwksCacheMaxAge  = time.Hour
	jwksCooldown     = 30 * time.Second
	maxJWKSBody      = 1 << 20
)

var errJWKSRefreshCooldown = errors.New("JWKS refresh is cooling down")

// boundedRemoteKeySet implements oidc.KeySet with the same bounded behavior Read had through
// jose.createRemoteJWKSet: a five-second fetch ceiling, one-hour cache lifetime, and a 30-second
// refresh cooldown. The cooldown applies to successful and failed attempts so unknown key IDs
// cannot turn verification into an unbounded upstream request stream.
type boundedRemoteKeySet struct {
	url      string
	client   *http.Client
	maxAge   time.Duration
	cooldown time.Duration
	now      func() time.Time

	mu          sync.Mutex
	keys        []jose.JSONWebKey
	generation  uint64
	fetchedAt   time.Time
	lastAttempt time.Time
	lastError   error
	inflight    chan struct{}
}

func newBoundedRemoteKeySet(url string, client *http.Client) *boundedRemoteKeySet {
	effectiveClient := &http.Client{Timeout: jwksFetchTimeout}
	if client != nil {
		copy := *client
		if copy.Timeout <= 0 || copy.Timeout > jwksFetchTimeout {
			copy.Timeout = jwksFetchTimeout
		}
		effectiveClient = &copy
	}
	return &boundedRemoteKeySet{
		url:      url,
		client:   effectiveClient,
		maxAge:   jwksCacheMaxAge,
		cooldown: jwksCooldown,
		now:      time.Now,
	}
}

// VerifySignature parses only RS256 and verifies against a bounded cached key set. Issuer,
// audience, expiry, and required claims remain the oidc verifier's responsibility.
func (r *boundedRemoteKeySet) VerifySignature(ctx context.Context, raw string) ([]byte, error) {
	jws, err := jose.ParseSigned(raw, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil {
		return nil, fmt.Errorf("parse signed JWT: %w", err)
	}
	if len(jws.Signatures) != 1 {
		return nil, errors.New("JWT must contain exactly one signature")
	}
	keyID := jws.Signatures[0].Header.KeyID
	keys, fresh, generation := r.snapshot()
	return r.verifyWithSnapshot(ctx, jws, keyID, keys, fresh, generation)
}

func (r *boundedRemoteKeySet) verifyWithSnapshot(ctx context.Context, jws *jose.JSONWebSignature, keyID string, keys []jose.JSONWebKey, fresh bool, generation uint64) ([]byte, error) {
	var err error
	if !fresh {
		keys, generation, err = r.refresh(ctx, generation)
		if err != nil {
			return nil, fmt.Errorf("refresh JWKS: %w", err)
		}
	}
	if payload, ok := verifyWithKeys(jws, keyID, keys); ok {
		return payload, nil
	}

	keys, _, err = r.refresh(ctx, generation)
	if err != nil {
		return nil, fmt.Errorf("refresh JWKS after signature miss: %w", err)
	}
	if payload, ok := verifyWithKeys(jws, keyID, keys); ok {
		return payload, nil
	}
	return nil, errors.New("no JWKS key verified the JWT signature")
}

func (r *boundedRemoteKeySet) snapshot() ([]jose.JSONWebKey, bool, uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	keys := append([]jose.JSONWebKey(nil), r.keys...)
	age := r.now().Sub(r.fetchedAt)
	fresh := len(keys) > 0 && !r.fetchedAt.IsZero() && age < r.maxAge
	return keys, fresh, r.generation
}

func (r *boundedRemoteKeySet) refresh(ctx context.Context, observedGeneration uint64) ([]jose.JSONWebKey, uint64, error) {
	r.mu.Lock()
	if r.generation != observedGeneration {
		keys := append([]jose.JSONWebKey(nil), r.keys...)
		generation := r.generation
		r.mu.Unlock()
		return keys, generation, nil
	}
	if r.inflight != nil {
		inflight := r.inflight
		r.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, observedGeneration, ctx.Err()
		case <-inflight:
			r.mu.Lock()
			defer r.mu.Unlock()
			keys := append([]jose.JSONWebKey(nil), r.keys...)
			if r.generation != observedGeneration {
				return keys, r.generation, nil
			}
			return keys, r.generation, r.lastError
		}
	}
	now := r.now()
	if !r.lastAttempt.IsZero() && now.Sub(r.lastAttempt) < r.cooldown {
		keys := append([]jose.JSONWebKey(nil), r.keys...)
		generation := r.generation
		r.mu.Unlock()
		return keys, generation, errJWKSRefreshCooldown
	}
	r.lastAttempt = now
	inflight := make(chan struct{})
	r.inflight = inflight
	r.mu.Unlock()

	// The fetch serves every waiting request and seeds the cache for the next
	// hour, so it must not die with the one request that happened to start it:
	// a browser that aborts its first call (a navigation, an unmounted view)
	// would otherwise fail the refresh and lock every caller out for the
	// cooldown. The client's own timeout still bounds it.
	keys, err := r.fetch(context.WithoutCancel(ctx))
	fetchedAt := r.now()

	r.mu.Lock()
	if err == nil {
		r.keys = keys
		r.fetchedAt = fetchedAt
		r.generation++
	}
	r.lastError = err
	r.inflight = nil
	close(inflight)
	result := append([]jose.JSONWebKey(nil), r.keys...)
	generation := r.generation
	r.mu.Unlock()
	return result, generation, err
}

func (r *boundedRemoteKeySet) fetch(ctx context.Context) ([]jose.JSONWebKey, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, r.url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Cache-Control", "no-cache")
	response, err := r.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("JWKS endpoint returned %s", response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxJWKSBody+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxJWKSBody {
		return nil, errors.New("JWKS response is too large")
	}
	var set jose.JSONWebKeySet
	if err := json.Unmarshal(body, &set); err != nil {
		return nil, fmt.Errorf("decode JWKS: %w", err)
	}
	keys := make([]jose.JSONWebKey, 0, len(set.Keys))
	for _, key := range set.Keys {
		public := key.Public()
		if _, ok := public.Key.(*rsa.PublicKey); !ok || !public.Valid() {
			continue
		}
		if public.Algorithm != "" && public.Algorithm != string(jose.RS256) {
			continue
		}
		if public.Use != "" && public.Use != "sig" {
			continue
		}
		keys = append(keys, public)
	}
	if len(keys) == 0 {
		return nil, errors.New("JWKS response contains no usable RSA signing keys")
	}
	return keys, nil
}

func verifyWithKeys(jws *jose.JSONWebSignature, keyID string, keys []jose.JSONWebKey) ([]byte, bool) {
	for _, key := range keys {
		if keyID != "" && key.KeyID != keyID {
			continue
		}
		if payload, err := jws.Verify(key.Key); err == nil {
			return payload, true
		}
	}
	return nil, false
}
