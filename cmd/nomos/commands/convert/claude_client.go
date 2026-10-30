// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package convert

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/altessa-s/go-atlas-net/httpx"

	coreerrs "github.com/altessa-s/go-atlas-core/errors"
	coreio "github.com/altessa-s/go-atlas-core/io"
	httpclient "github.com/altessa-s/go-atlas-net/http/client"
)

const (
	claudeAPIVersion = "2023-06-01"
	maxTokens        = 4096
)

// ClaudeClient sends batched prompts to the Anthropic Messages API to generate
// short descriptions for environment variables. It is not safe for concurrent use.
type ClaudeClient struct {
	httpClient httpx.Doer
	options    *options
	apiKey     string
}

// NewClaudeClient returns a client configured with apiKey and a default
// go-atlas-net transport (60 s timeout, private-address protection, no retries or redirects).
func NewClaudeClient(apiKey string) *ClaudeClient {
	return NewClaudeClientWithOptions(apiKey)
}

// NewClaudeClientWithOptions constructs a provider adapter with generated settings.
func NewClaudeClientWithOptions(apiKey string, opts ...Option) *ClaudeClient {
	return newClaudeClient(apiKey, opts...)
}

func newClaudeClient(apiKey string, opts ...Option) *ClaudeClient {
	o := newOptions(opts...)
	client := o.httpClient
	if client == nil {
		client = httpclient.New(httpclient.WithClient(&http.Client{Timeout: o.requestTimeout}), httpclient.WithoutRetry(), httpclient.WithoutRedirects())
	}
	return &ClaudeClient{apiKey: apiKey, httpClient: client, options: o}
}

// ErrAPIResponse identifies unsuccessful or malformed provider responses.
var ErrAPIResponse = errors.New("invalid description provider response")

// claudeRequest represents a request to Claude API.
type claudeRequest struct {
	Messages  []claudeMessage `json:"messages"`
	Model     string          `json:"model"`
	MaxTokens int             `json:"max_tokens"`
}

// claudeMessage represents a message in the conversation.
type claudeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// claudeResponse represents a response from Claude API.
type claudeResponse struct {
	Error   *claudeError    `json:"error,omitempty"`
	Content []claudeContent `json:"content"`
}

// claudeContent represents content in the response.
type claudeContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// claudeError represents an error from Claude API.
type claudeError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// Describe implements the rendering port under the caller's context.
func (c *ClaudeClient) Describe(ctx context.Context, envVars map[string]string, sectionName string) (map[string]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Build prompt with all variables in the section
	var promptBuilder strings.Builder
	fmt.Fprintf(&promptBuilder,
		"Generate concise descriptions (max 10 words) for the following %s configuration environment variables.\n\n",
		sectionName)
	promptBuilder.WriteString("For each variable, provide a brief description that explains its purpose.\n")
	promptBuilder.WriteString("Format your response as a JSON object with variable names as keys and descriptions as values.\n\n")
	promptBuilder.WriteString("Environment variables:\n\n")

	keys := make([]string, 0, len(envVars))
	for key := range envVars {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	for _, key := range keys {
		info := envVars[key]
		fmt.Fprintf(&promptBuilder, "- %s (%s)\n", key, info)
	}

	promptBuilder.WriteString("\nProvide ONLY the JSON object, without any additional text or markdown formatting.")

	// Create request
	req := claudeRequest{
		Messages: []claudeMessage{
			{
				Role:    "user",
				Content: promptBuilder.String(),
			},
		},
		Model:     c.options.model,
		MaxTokens: maxTokens,
	}

	// Send request
	responseText, err := c.sendRequest(ctx, req)
	if err != nil {
		return nil, err
	}

	// Clean response text - remove markdown code blocks if present
	cleanedText := strings.TrimSpace(responseText)
	if rest, ok := strings.CutPrefix(cleanedText, "```json"); ok {
		cleanedText = strings.TrimSuffix(rest, "```")
		cleanedText = strings.TrimSpace(cleanedText)
	} else if rest, ok := strings.CutPrefix(cleanedText, "```"); ok {
		cleanedText = strings.TrimSuffix(rest, "```")
		cleanedText = strings.TrimSpace(cleanedText)
	}

	// Parse JSON response
	descriptions := make(map[string]string)
	if err := json.Unmarshal([]byte(cleanedText), &descriptions); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAPIResponse, err)
	}

	return descriptions, nil
}

// sendRequest sends a request to Claude API and returns the response text.
func (c *ClaudeClient) sendRequest(parent context.Context, req claudeRequest) (string, error) {
	// Marshal request body
	body, err := json.Marshal(req)
	if err != nil {
		return "", coreerrs.WrapOperation(err, "marshal request")
	}

	// Create HTTP request with context
	ctx, cancel := context.WithTimeout(parent, c.options.requestTimeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.options.endpoint, bytes.NewReader(body))
	if err != nil {
		return "", coreerrs.WrapOperation(err, "create request")
	}

	// Set headers
	httpReq.Header.Set(httpx.HeaderContentType, "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", claudeAPIVersion)

	// Send request
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", coreerrs.WrapOperation(err, "send request")
	}
	defer func() { _ = resp.Body.Close() }()

	// Read response body
	respBody, err := coreio.ReadAllLimited(resp.Body, c.options.maxResponseBytes)
	if errors.Is(err, coreio.ErrReadLimitExceeded) {
		return "", errors.Join(ErrAPIResponse, err)
	}
	if err != nil {
		return "", coreerrs.WrapOperation(err, "read response")
	}

	// Check for HTTP errors
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: HTTP status %d", ErrAPIResponse, resp.StatusCode)
	}

	// Parse response
	var claudeResp claudeResponse
	if err := json.Unmarshal(respBody, &claudeResp); err != nil {
		return "", fmt.Errorf("%w: %w", ErrAPIResponse, err)
	}

	// Check for API errors
	if claudeResp.Error != nil {
		return "", ErrAPIResponse
	}

	// Extract the first text block; thinking blocks may precede it.
	for _, block := range claudeResp.Content {
		if block.Type == "text" {
			return block.Text, nil
		}
	}
	return "", ErrAPIResponse
}
