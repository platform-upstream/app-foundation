package reference

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var ErrLLMNotConfigured = errors.New("llm_not_configured")

const maxLLMResponseBytes = int64(1 << 20)

type CompletionClient interface {
	Enabled() bool
	Complete(context.Context, string) (string, error)
}

type LLMClient struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

func NewLLMClient(baseURL, apiKey string, client *http.Client) *LLMClient {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	} else {
		copy := *client
		if copy.Timeout <= 0 || copy.Timeout > 5*time.Second {
			copy.Timeout = 5 * time.Second
		}
		client = &copy
	}
	return &LLMClient{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, http: client}
}

func (client *LLMClient) Enabled() bool {
	return client != nil && client.baseURL != "" && client.apiKey != ""
}

func (client *LLMClient) Complete(ctx context.Context, prompt string) (string, error) {
	if !client.Enabled() {
		return "", ErrLLMNotConfigured
	}
	payload := struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}{Model: "local-llm"}
	payload.Messages = append(payload.Messages, struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}{Role: "user", Content: prompt})
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.baseURL+"/chat/completions", bytes.NewReader(encoded))
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", "Bearer "+client.apiKey)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.http.Do(request)
	if err != nil {
		return "", fmt.Errorf("complete: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		return "", fmt.Errorf("complete: HTTP %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxLLMResponseBytes+1))
	if err != nil {
		return "", fmt.Errorf("complete: %w", err)
	}
	if int64(len(raw)) > maxLLMResponseBytes {
		return "", errors.New("complete: response too large")
	}
	var result struct {
		Choices []struct {
			Message struct {
				Content *string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&result); err != nil {
		return "", fmt.Errorf("complete: invalid response: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return "", errors.New("complete: trailing response content")
	}
	if len(result.Choices) != 1 || result.Choices[0].Message.Content == nil || strings.TrimSpace(*result.Choices[0].Message.Content) == "" {
		return "", errors.New("complete: missing content")
	}
	return *result.Choices[0].Message.Content, nil
}
