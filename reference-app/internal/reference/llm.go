package reference

import (
	"context"
	"errors"
	"time"

	"github.com/castillo-platform/app-foundation/llm"
)

var ErrLLMNotConfigured = errors.New("llm_not_configured")

// CompletionClient is the app's own seam in front of the LLM gateway.
type CompletionClient interface {
	Enabled() bool
	Complete(context.Context, string) (string, error)
}

// LLMClient answers one prompt through the shared gateway client.
type LLMClient struct {
	client *llm.Client
}

// NewLLMClient builds the seam. No API key gives a disabled client; a key without a model alias
// is an error, so a missing alias stops the app at startup.
func NewLLMClient(config llm.Config) (*LLMClient, error) {
	client, err := llm.New(config)
	if err != nil {
		return nil, err
	}
	return &LLMClient{client: client}, nil
}

func (client *LLMClient) Enabled() bool {
	return client != nil && client.client.Enabled()
}

func (client *LLMClient) Complete(ctx context.Context, prompt string) (string, error) {
	if !client.Enabled() {
		return "", ErrLLMNotConfigured
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	response, err := client.client.Complete(ctx, llm.Request{
		Messages:  []llm.Message{{Role: "user", Content: prompt}},
		MaxTokens: 1000,
	})
	if err != nil {
		return "", err
	}
	return response.Content, nil
}
