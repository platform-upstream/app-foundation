package access

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// Environment variables read by ConfigFromEnv.
const (
	EnvMode          = "AUTH_MODE"
	EnvTeamDomain    = "CF_TEAM_DOMAIN"
	EnvAudience      = "CF_ACCESS_AUD"
	EnvServiceTokens = "CF_ACCESS_SERVICE_TOKENS"
)

// JWKSTimeout bounds each request ConfigFromEnv's HTTP client makes for the Access signing keys.
const JWKSTimeout = 5 * time.Second

// ConfigFromEnv reads the authentication configuration the way every app's chart sets it:
// AUTH_MODE (cloudflare when unset, or local), CF_TEAM_DOMAIN (<team>.cloudflareaccess.com),
// CF_ACCESS_AUD and CF_ACCESS_SERVICE_TOKENS (client IDs separated by commas or spaces). listener
// is the address the server listens on; local mode accepts only 127.0.0.1. It fails closed: a
// missing or malformed value is an error naming the variable, never a weaker mode. The Config
// carries an HTTP client with a JWKSTimeout ceiling; pass it to New.
func ConfigFromEnv(listener string) (Config, error) {
	mode := Mode(strings.TrimSpace(os.Getenv(EnvMode)))
	if mode == "" {
		mode = ModeCloudflare
	}
	switch mode {
	case ModeLocal:
		if !loopbackListener(listener) {
			return Config{}, fmt.Errorf("%s=local requires the listener 127.0.0.1:<port>, not %q", EnvMode, listener)
		}
		return Config{Mode: ModeLocal, Listener: listener}, nil
	case ModeCloudflare:
	default:
		return Config{}, fmt.Errorf("%s=%q is not cloudflare or local", EnvMode, string(mode))
	}

	cfg := Config{
		Mode:       ModeCloudflare,
		Listener:   listener,
		TeamDomain: os.Getenv(EnvTeamDomain),
		Audience:   os.Getenv(EnvAudience),
		HTTPClient: &http.Client{Timeout: JWKSTimeout},
		ServiceTokenClientIDs: strings.FieldsFunc(os.Getenv(EnvServiceTokens), func(r rune) bool {
			return r == ',' || r == ' ' || r == '\t' || r == '\n'
		}),
	}
	var problems []error
	switch {
	case cfg.TeamDomain == "":
		problems = append(problems, fmt.Errorf("%s is required (<team>.cloudflareaccess.com)", EnvTeamDomain))
	case !validTeamDomain(cfg.TeamDomain):
		problems = append(problems, fmt.Errorf("%s=%q must be <team>.cloudflareaccess.com", EnvTeamDomain, cfg.TeamDomain))
	}
	switch {
	case cfg.Audience == "":
		problems = append(problems, fmt.Errorf("%s is required (the Access application's audience tag)", EnvAudience))
	case strings.TrimSpace(cfg.Audience) != cfg.Audience:
		problems = append(problems, fmt.Errorf("%s has surrounding whitespace", EnvAudience))
	}
	if err := errors.Join(problems...); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
