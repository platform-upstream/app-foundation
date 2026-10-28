package reference

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"

	"github.com/castillo-platform/app-foundation/access"
)

type Config struct {
	Address       string
	DatabasePath  string
	Origin        string
	AuthMode      access.Mode
	CFTeamDomain  string
	CFAudience    string
	MemoryBaseURL string
	MemoryAPIKey  string
	LLMBaseURL    string
	LLMAPIKey     string
	LLMModel      string
}

func ConfigFromEnvironment() (Config, error) {
	config := Config{
		Address:       valueOr("APP_ADDR", "127.0.0.1:8099"),
		DatabasePath:  valueOr("APP_DB", ".local/reference.sqlite"),
		Origin:        os.Getenv("APP_ORIGIN"),
		AuthMode:      access.Mode(valueOr("APP_AUTH_MODE", string(access.ModeLocal))),
		CFTeamDomain:  os.Getenv("CF_TEAM_DOMAIN"),
		CFAudience:    os.Getenv("CF_ACCESS_AUD"),
		MemoryBaseURL: os.Getenv("MEMORY_BASE_URL"),
		MemoryAPIKey:  os.Getenv("MEMORY_API_KEY"),
		LLMBaseURL:    os.Getenv("LLM_BASE_URL"),
		LLMAPIKey:     os.Getenv("LLM_API_KEY"),
		LLMModel:      os.Getenv("LLM_MODEL"),
	}
	if config.Origin == "" && config.AuthMode == access.ModeLocal {
		config.Origin = "http://" + config.Address
	}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (config Config) Validate() error {
	if _, _, err := net.SplitHostPort(config.Address); err != nil {
		return fmt.Errorf("invalid APP_ADDR: %w", err)
	}
	if config.DatabasePath == "" {
		return errors.New("APP_DB is required")
	}
	origin, err := url.Parse(config.Origin)
	if err != nil || origin.Scheme == "" || origin.Host == "" || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
		return errors.New("APP_ORIGIN must be an origin without a path")
	}
	if config.AuthMode != access.ModeLocal && config.AuthMode != access.ModeCloudflare {
		return fmt.Errorf("unsupported APP_AUTH_MODE %q", config.AuthMode)
	}
	if (config.MemoryBaseURL == "") != (config.MemoryAPIKey == "") {
		return errors.New("MEMORY_BASE_URL and MEMORY_API_KEY must be configured together")
	}
	if (config.LLMBaseURL == "") != (config.LLMAPIKey == "") {
		return errors.New("LLM_BASE_URL and LLM_API_KEY must be configured together")
	}
	if config.LLMAPIKey != "" && config.LLMModel == "" {
		return errors.New("LLM_MODEL is required with LLM_API_KEY")
	}
	return nil
}

func valueOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
