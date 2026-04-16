package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"whatsapp_golang/internal/logger"
)

// APIKeyFunc 動態取得 API Key 的回調
type APIKeyFunc func() string

type Client struct {
	apiKeyFn   APIKeyFunc
	baseURL    string
	model      string
	httpClient *http.Client
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type request struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}

type response struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

const defaultBaseURL = "https://openrouter.ai/api/v1"
const defaultModel = "google/gemini-2.5-flash"

func NewClient(apiKeyFn APIKeyFunc, timeoutSec int) *Client {
	if timeoutSec <= 0 {
		timeoutSec = 30
	}
	return &Client{
		apiKeyFn: apiKeyFn,
		baseURL:  defaultBaseURL,
		model:    defaultModel,
		httpClient: &http.Client{
			Timeout: time.Duration(timeoutSec) * time.Second,
		},
	}
}

// ChatCompletionWithModel sends a chat completion request using the specified model.
// If model is empty, falls back to the client's default model.
func (c *Client) ChatCompletionWithModel(ctx context.Context, model string, messages []Message) (string, error) {
	if model == "" {
		model = c.model
	}
	return c.chatCompletion(ctx, model, messages)
}

// ChatCompletion sends a chat completion request and returns the response content
func (c *Client) ChatCompletion(ctx context.Context, messages []Message) (string, error) {
	return c.chatCompletion(ctx, c.model, messages)
}

// ChatCompletionMultimodal sends a chat completion request that can mix
// text-only Message and vision MultimodalMessage in the same conversation.
// The messages slice accepts both types; json.Marshal handles the polymorphism.
func (c *Client) ChatCompletionMultimodal(ctx context.Context, model string, messages []interface{}) (string, error) {
	if model == "" {
		model = c.model
	}

	apiKey := c.apiKeyFn()
	if apiKey == "" {
		return "", fmt.Errorf("llm.api_key 未配置")
	}

	reqBody := struct {
		Model    string        `json:"model"`
		Messages []interface{} `json:"messages"`
	}{
		Model:    model,
		Messages: messages,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	endpoint := c.baseURL + "/chat/completions"

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(jsonData))
		if err != nil {
			return "", fmt.Errorf("create request: %w", err)
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("read response: %w", err)
			continue
		}

		if resp.StatusCode == 429 {
			lastErr = fmt.Errorf("rate limited (429)")
			logger.Warnw("LLM rate limited", "attempt", attempt+1, "max_attempts", 3)
			time.Sleep(time.Duration(attempt+1) * time.Second)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
		}

		var result response
		if err := json.Unmarshal(body, &result); err != nil {
			return "", fmt.Errorf("unmarshal response: %w", err)
		}

		if len(result.Choices) == 0 {
			return "", fmt.Errorf("empty choices in response")
		}

		return result.Choices[0].Message.Content, nil
	}

	return "", fmt.Errorf("all retries failed: %w", lastErr)
}

func (c *Client) chatCompletion(ctx context.Context, model string, messages []Message) (string, error) {
	apiKey := c.apiKeyFn()
	if apiKey == "" {
		return "", fmt.Errorf("llm.api_key 未配置")
	}

	reqBody := request{
		Model:    model,
		Messages: messages,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	endpoint := c.baseURL + "/chat/completions"

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(jsonData))
		if err != nil {
			return "", fmt.Errorf("create request: %w", err)
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("read response: %w", err)
			continue
		}

		if resp.StatusCode == 429 {
			lastErr = fmt.Errorf("rate limited (429)")
			logger.Warnw("LLM rate limited", "attempt", attempt+1, "max_attempts", 3)
			time.Sleep(time.Duration(attempt+1) * time.Second)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
		}

		var result response
		if err := json.Unmarshal(body, &result); err != nil {
			return "", fmt.Errorf("unmarshal response: %w", err)
		}

		if len(result.Choices) == 0 {
			return "", fmt.Errorf("empty choices in response")
		}

		return result.Choices[0].Message.Content, nil
	}

	return "", fmt.Errorf("all retries failed: %w", lastErr)
}
