package access

import (
	"reflect"
	"strings"
	"testing"
)

func setEnv(t *testing.T, values map[string]string) {
	t.Helper()
	for _, name := range []string{EnvMode, EnvTeamDomain, EnvAudience, EnvServiceTokens} {
		t.Setenv(name, values[name])
	}
}

func TestConfigFromEnvCloudflare(t *testing.T) {
	setEnv(t, map[string]string{
		EnvTeamDomain:    "acme.cloudflareaccess.com",
		EnvAudience:      "aud-123",
		EnvServiceTokens: "one.access, two.access three.access,",
	})
	cfg, err := ConfigFromEnv(":8080")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != ModeCloudflare || cfg.Listener != ":8080" || cfg.TeamDomain != "acme.cloudflareaccess.com" || cfg.Audience != "aud-123" {
		t.Fatalf("config: %+v", cfg)
	}
	if want := []string{"one.access", "two.access", "three.access"}; !reflect.DeepEqual(cfg.ServiceTokenClientIDs, want) {
		t.Fatalf("service tokens = %q", cfg.ServiceTokenClientIDs)
	}
	if cfg.HTTPClient == nil || cfg.HTTPClient.Timeout != JWKSTimeout {
		t.Fatalf("HTTP client: %+v", cfg.HTTPClient)
	}
	if _, err := New(cfg); err != nil {
		t.Fatalf("New rejected the config: %v", err)
	}
}

func TestConfigFromEnvLocal(t *testing.T) {
	setEnv(t, map[string]string{EnvMode: "local"})
	cfg, err := ConfigFromEnv("127.0.0.1:8150")
	if err != nil || cfg.Mode != ModeLocal || cfg.Listener != "127.0.0.1:8150" {
		t.Fatalf("local: %+v %v", cfg, err)
	}
	if _, err := New(cfg); err != nil {
		t.Fatal(err)
	}
}

func TestConfigFromEnvFailsClosed(t *testing.T) {
	for name, test := range map[string]struct {
		env      map[string]string
		listener string
		mention  []string
	}{
		"nothing set":         {map[string]string{}, ":8080", []string{EnvTeamDomain, EnvAudience}},
		"no audience":         {map[string]string{EnvTeamDomain: "acme.cloudflareaccess.com"}, ":8080", []string{EnvAudience}},
		"foreign team domain": {map[string]string{EnvTeamDomain: "acme.example.org", EnvAudience: "aud"}, ":8080", []string{EnvTeamDomain}},
		"spaced audience":     {map[string]string{EnvTeamDomain: "acme.cloudflareaccess.com", EnvAudience: " aud"}, ":8080", []string{EnvAudience}},
		"unknown mode":        {map[string]string{EnvMode: "none"}, ":8080", []string{EnvMode}},
		"local on all hosts":  {map[string]string{EnvMode: "local"}, ":8080", []string{EnvMode}},
		"local on localhost":  {map[string]string{EnvMode: "local"}, "localhost:8080", []string{EnvMode}},
	} {
		setEnv(t, test.env)
		cfg, err := ConfigFromEnv(test.listener)
		if err == nil {
			t.Errorf("%s: accepted %+v", name, cfg)
			continue
		}
		for _, variable := range test.mention {
			if !strings.Contains(err.Error(), variable) {
				t.Errorf("%s: error %q does not name %s", name, err, variable)
			}
		}
	}
}
