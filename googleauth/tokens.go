package googleauth

import (
	"context"
	"crypto/sha256"
	"errors"
	"time"

	"golang.org/x/oauth2"
)

const (
	// earlyRefresh: a cached token this close to its expiry is renewed first, so no Google call
	// starts with a token that expires on the way.
	earlyRefresh = 30 * time.Second
	// maxTokenLife caps what an answer may claim; Google's access tokens last an hour.
	maxTokenLife = time.Hour
)

type accessToken struct {
	value   string
	expires time.Time
}

// flight is one refresh that every caller for the same refresh token waits for.
type flight struct {
	done  chan struct{}
	value string
	err   error
}

// AccessToken returns an access token for refreshToken: the cached one while it has more than
// 30 seconds left, otherwise a new one from Google. Concurrent calls for the same refresh token
// share one refresh, which runs to its end (at most Timeout) even when callers give up, so one
// cancelled request does not fail the others; ctx bounds only this caller's wait.
//
// The cache is in memory and keyed by a hash of the refresh token: the app keeps no token
// source per person, and a reconnect (a new refresh token) starts afresh. ErrInvalidGrant
// means the grant is gone; the app forgets the token and offers to connect again.
func (c *Client) AccessToken(ctx context.Context, refreshToken string) (string, error) {
	if refreshToken == "" {
		return "", errors.New("googleauth: no refresh token")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	key := tokenKey(refreshToken)
	c.mu.Lock()
	if cached, ok := c.tokens[key]; ok && cached.expires.Sub(c.now()) > earlyRefresh {
		c.mu.Unlock()
		return cached.value, nil
	}
	f, ok := c.flights[key]
	if !ok {
		f = &flight{done: make(chan struct{})}
		c.flights[key] = f
		go c.refresh(context.WithoutCancel(ctx), key, refreshToken, f)
	}
	c.mu.Unlock()
	select {
	case <-f.done:
		return f.value, f.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (c *Client) refresh(ctx context.Context, key [32]byte, refreshToken string, f *flight) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	// Google keeps the refresh token on refresh; a new one in the answer would be dropped here.
	token, err := c.oauth.TokenSource(c.withHTTP(ctx), &oauth2.Token{RefreshToken: refreshToken}).Token()
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		f.err = tokenError("refresh", err)
	} else {
		f.value = token.AccessToken
		c.keep(key, token.AccessToken, token.ExpiresIn)
	}
	delete(c.flights, key)
	close(f.done)
}

// keep caches an access token and drops expired ones (the map holds one entry per grant in
// use). c.mu is held.
func (c *Client) keep(key [32]byte, value string, expiresIn int64) {
	now := c.now()
	for k, t := range c.tokens {
		if !t.expires.After(now) {
			delete(c.tokens, k)
		}
	}
	if value == "" || expiresIn <= 0 {
		return
	}
	life := maxTokenLife
	if expiresIn < int64(maxTokenLife/time.Second) {
		life = time.Duration(expiresIn) * time.Second
	}
	c.tokens[key] = accessToken{value: value, expires: now.Add(life)}
}

func tokenKey(refreshToken string) [32]byte {
	return sha256.Sum256([]byte(refreshToken))
}
