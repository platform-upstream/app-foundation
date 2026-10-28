package reference

import (
	"testing"

	"github.com/castillo-platform/app-foundation/access"
)

func TestConfigValidation(t *testing.T) {
	valid := Config{
		Address: "127.0.0.1:8099", DatabasePath: "/tmp/reference.sqlite",
		Origin: "http://127.0.0.1:8099", AuthMode: access.ModeLocal,
	}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "address", mutate: func(value *Config) { value.Address = "bad" }},
		{name: "database", mutate: func(value *Config) { value.DatabasePath = "" }},
		{name: "origin path", mutate: func(value *Config) { value.Origin = "https://example.invalid/path" }},
		{name: "origin userinfo", mutate: func(value *Config) { value.Origin = "https://user@example.invalid" }},
		{name: "auth mode", mutate: func(value *Config) { value.AuthMode = "other" }},
		{name: "Memory partial", mutate: func(value *Config) { value.MemoryBaseURL = "https://memory.invalid" }},
		{name: "LLM partial", mutate: func(value *Config) { value.LLMAPIKey = "key" }},
		{name: "LLM key without alias", mutate: func(value *Config) { value.LLMBaseURL, value.LLMAPIKey = "https://llm.invalid/v1", "key" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := valid
			test.mutate(&value)
			if err := value.Validate(); err == nil {
				t.Fatal("invalid config accepted")
			}
		})
	}
}

func TestEnvironmentDefaultsToLoopbackLocalMode(t *testing.T) {
	for _, name := range []string{
		"APP_ADDR", "APP_DB", "APP_ORIGIN", "APP_AUTH_MODE", "CF_TEAM_DOMAIN", "CF_ACCESS_AUD",
		"MEMORY_BASE_URL", "MEMORY_API_KEY", "LLM_BASE_URL", "LLM_API_KEY", "LLM_MODEL",
	} {
		t.Setenv(name, "")
	}
	config, err := ConfigFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if config.Address != "127.0.0.1:8099" || config.AuthMode != access.ModeLocal || config.Origin != "http://127.0.0.1:8099" {
		t.Fatalf("defaults = %#v", config)
	}
}
