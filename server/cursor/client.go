package cursor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	DefaultBaseURL = "https://api.cursor.com"
	DefaultTimeout = 30 * time.Second
	maxRetries     = 3
	retryBaseDelay = 1 * time.Second
)

// Logger is an interface for debug logging within the Cursor API client.
// When non-nil, the client logs detailed request/response information.
type Logger interface {
	LogDebug(msg string, keyValuePairs ...any)
}

// Client is the interface for interacting with the Cursor Background Agents API.
type Client interface {
	// LaunchAgent creates a new background agent.
	LaunchAgent(ctx context.Context, req LaunchAgentRequest) (*Agent, error)

	// GetAgent retrieves the status of a specific agent.
	GetAgent(ctx context.Context, id string) (*Agent, error)

	// ListAgents returns a paginated list of agents.
	ListAgents(ctx context.Context, limit int, cursor string) (*ListAgentsResponse, error)

	// AddFollowup sends a follow-up instruction to a running agent.
	AddFollowup(ctx context.Context, id string, req FollowupRequest) (*FollowupResponse, error)

	// GetConversation retrieves the full conversation history for an agent.
	GetConversation(ctx context.Context, id string) (*Conversation, error)

	// StopAgent stops a running agent.
	StopAgent(ctx context.Context, id string) (*StopResponse, error)

	// DeleteAgent permanently deletes an agent.
	DeleteAgent(ctx context.Context, id string) (*DeleteResponse, error)

	// ListModels returns available AI models.
	ListModels(ctx context.Context) (*ListModelsResponse, error)

	// GetMe returns info about the authenticated API key. Used as a health check.
	GetMe(ctx context.Context) (*APIKeyInfo, error)
}

// clientImpl implements the Client interface.
type clientImpl struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	logger     Logger
}

// NewClient creates a new Cursor API client.
func NewClient(apiKey string, opts ...ClientOption) Client {
	c := &clientImpl{
		baseURL: DefaultBaseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: DefaultTimeout,
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// NewClientWithBaseURL creates a client with a custom base URL (useful for testing).
func NewClientWithBaseURL(apiKey, baseURL string, opts ...ClientOption) Client {
	c := &clientImpl{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: DefaultTimeout,
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// ClientOption is a functional option for configuring the Cursor API client.
type ClientOption func(*clientImpl)

// WithLogger returns a ClientOption that sets the logger on the client.
func WithLogger(logger Logger) ClientOption {
	return func(c *clientImpl) {
		c.logger = logger
	}
}

// logDebug logs a debug message if a logger is configured.
func (c *clientImpl) logDebug(msg string, keyValuePairs ...any) {
	if c.logger != nil {
		c.logger.LogDebug(msg, keyValuePairs...)
	}
}

// doRequest performs an HTTP request with retry logic for transient failures.
// It retries on 429 (rate limit) and 5xx errors up to maxRetries times.
func (c *clientImpl) doRequest(ctx context.Context, method, path string, body any) ([]byte, error) {
	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
	}

	fullURL := c.baseURL + path
	c.logDebug("Cursor API request",
		"method", method,
		"url", fullURL,
		"has_body", body != nil,
	)
	if bodyBytes != nil {
		c.logDebug("Cursor API request body",
			"method", method,
			"url", fullURL,
			"body", string(bodyBytes),
		)
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			delay := retryBaseDelay * time.Duration(1<<(attempt-1)) // exponential backoff
			c.logDebug("Cursor API retry",
				"attempt", attempt,
				"max_retries", maxRetries,
				"delay", delay.String(),
				"method", method,
				"url", fullURL,
			)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		var reqBody io.Reader
		if bodyBytes != nil {
			reqBody = bytes.NewReader(bodyBytes)
		}

		req, err := http.NewRequestWithContext(ctx, method, fullURL, reqBody)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		req.SetBasicAuth(c.apiKey, "")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("request failed: %w", err)
			c.logDebug("Cursor API transport error",
				"method", method,
				"url", fullURL,
				"attempt", attempt,
				"error", err.Error(),
			)
			continue
		}

		respBody, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("failed to read response body: %w", err)
			c.logDebug("Cursor API response body read error",
				"method", method,
				"url", fullURL,
				"status", resp.StatusCode,
				"error", err.Error(),
			)
			continue
		}

		c.logDebug("Cursor API response",
			"method", method,
			"url", fullURL,
			"status", resp.StatusCode,
			"body_length", len(respBody),
		)

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			c.logDebug("Cursor API response body",
				"method", method,
				"url", fullURL,
				"body", string(respBody),
			)
			return respBody, nil
		}

		// Always capture the raw response body for error diagnostics.
		rawBody := string(respBody)
		apiErr := &APIError{StatusCode: resp.StatusCode, RawBody: rawBody}
		if jsonErr := json.Unmarshal(respBody, apiErr); jsonErr != nil {
			// JSON parsing failed -- use the raw body as the message.
			apiErr.Message = rawBody
		}

		// If JSON parsed but message is empty, fall back to the raw body.
		if apiErr.Message == "" {
			apiErr.Message = rawBody
		}

		c.logDebug("Cursor API error response",
			"method", method,
			"url", fullURL,
			"status", resp.StatusCode,
			"message", apiErr.Message,
			"raw_body", rawBody,
		)

		// Retry on 429 (rate limited) and 5xx (server error)
		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			lastErr = apiErr
			continue
		}

		// 4xx errors (except 429) are not retryable
		return nil, apiErr
	}

	return nil, fmt.Errorf("request failed after %d retries: %w", maxRetries, lastErr)
}

func (c *clientImpl) LaunchAgent(ctx context.Context, req LaunchAgentRequest) (*Agent, error) {
	respBody, err := c.doRequest(ctx, http.MethodPost, "/v0/agents", req)
	if err != nil {
		return nil, err
	}
	var agent Agent
	if err := json.Unmarshal(respBody, &agent); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &agent, nil
}

func (c *clientImpl) GetAgent(ctx context.Context, id string) (*Agent, error) {
	respBody, err := c.doRequest(ctx, http.MethodGet, "/v0/agents/"+id, nil)
	if err != nil {
		return nil, err
	}
	var agent Agent
	if err := json.Unmarshal(respBody, &agent); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &agent, nil
}

func (c *clientImpl) ListAgents(ctx context.Context, limit int, cursor string) (*ListAgentsResponse, error) {
	path := fmt.Sprintf("/v0/agents?limit=%d", limit)
	if cursor != "" {
		path += "&cursor=" + url.QueryEscape(cursor)
	}
	respBody, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var resp ListAgentsResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &resp, nil
}

func (c *clientImpl) AddFollowup(ctx context.Context, id string, req FollowupRequest) (*FollowupResponse, error) {
	respBody, err := c.doRequest(ctx, http.MethodPost, "/v0/agents/"+id+"/followup", req)
	if err != nil {
		return nil, err
	}
	var resp FollowupResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &resp, nil
}

func (c *clientImpl) GetConversation(ctx context.Context, id string) (*Conversation, error) {
	respBody, err := c.doRequest(ctx, http.MethodGet, "/v0/agents/"+id+"/conversation", nil)
	if err != nil {
		return nil, err
	}
	var conv Conversation
	if err := json.Unmarshal(respBody, &conv); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &conv, nil
}

func (c *clientImpl) StopAgent(ctx context.Context, id string) (*StopResponse, error) {
	respBody, err := c.doRequest(ctx, http.MethodPost, "/v0/agents/"+id+"/stop", nil)
	if err != nil {
		return nil, err
	}
	var resp StopResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &resp, nil
}

func (c *clientImpl) DeleteAgent(ctx context.Context, id string) (*DeleteResponse, error) {
	respBody, err := c.doRequest(ctx, http.MethodDelete, "/v0/agents/"+id, nil)
	if err != nil {
		return nil, err
	}
	var resp DeleteResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &resp, nil
}

func (c *clientImpl) ListModels(ctx context.Context) (*ListModelsResponse, error) {
	respBody, err := c.doRequest(ctx, http.MethodGet, "/v0/models", nil)
	if err != nil {
		return nil, err
	}
	var resp ListModelsResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &resp, nil
}

func (c *clientImpl) GetMe(ctx context.Context) (*APIKeyInfo, error) {
	respBody, err := c.doRequest(ctx, http.MethodGet, "/v0/me", nil)
	if err != nil {
		return nil, err
	}
	var info APIKeyInfo
	if err := json.Unmarshal(respBody, &info); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &info, nil
}
