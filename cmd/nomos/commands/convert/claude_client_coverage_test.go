// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package convert

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-atlas-net/httpx"
)

// newStubClaudeClient returns a ClaudeClient whose transport is replaced by fn,
// so no request ever leaves the process. White-box: the HTTP client is private.
func newStubClaudeClient(t testing.TB, fn httpx.DoerFunc) *ClaudeClient {
	t.Helper()
	c := NewClaudeClient("test-key")
	c.httpClient = fn
	return c
}

func respond(status int, body string) httpx.DoerFunc {
	return func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: status,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	}
}

func textResponse(text string) string {
	b, _ := json.Marshal(map[string]any{"content": []map[string]string{{"type": "text", "text": text}}})
	return string(b)
}

func TestClaudeClient_RequestShape(t *testing.T) {
	t.Parallel()

	var captured *http.Request
	var body claudeRequest
	c := newStubClaudeClient(t, func(r *http.Request) (*http.Response, error) {
		captured = r
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		return respond(http.StatusOK, textResponse(`{"DB__HOST":"Database host"}`))(r)
	})

	got, err := c.Describe(t.Context(), map[string]string{
		"DB__PORT": "type: int",
		"DB__HOST": "type: string",
	}, "DB")
	require.NoError(t, err)
	require.Equal(t, map[string]string{"DB__HOST": "Database host"}, got)

	require.Equal(t, http.MethodPost, captured.Method)
	require.Equal(t, DefaultEndpoint, captured.URL.String())
	require.Equal(t, "test-key", captured.Header.Get("x-api-key"))
	require.Equal(t, claudeAPIVersion, captured.Header.Get("anthropic-version"))
	require.Len(t, body.Messages, 1)
	prompt := body.Messages[0].Content
	require.Less(t, strings.Index(prompt, "DB__HOST"), strings.Index(prompt, "DB__PORT"), "variables are sorted")
	require.Contains(t, prompt, "- DB__PORT (type: int)")
}

func TestClaudeClient_CodeFences(t *testing.T) {
	t.Parallel()

	for name, text := range map[string]string{
		"json fence":  "```json\n{\"A\":\"desc\"}\n```",
		"plain fence": "```\n{\"A\":\"desc\"}\n```",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := newStubClaudeClient(t, respond(http.StatusOK, textResponse(text)))
			got, err := c.Describe(t.Context(), map[string]string{"A": "type: string"}, "A")
			require.NoError(t, err)
			require.Equal(t, map[string]string{"A": "desc"}, got)
		})
	}
}

func TestClaudeClient_ThinkingBlockFirst(t *testing.T) {
	t.Parallel()

	body := `{"content":[{"type":"thinking","thinking":"...","signature":"sig"},{"type":"text","text":"{\"A\":\"desc\"}"}]}`
	c := newStubClaudeClient(t, respond(http.StatusOK, body))
	got, err := c.Describe(t.Context(), map[string]string{"A": "type: string"}, "A")
	require.NoError(t, err)
	require.Equal(t, map[string]string{"A": "desc"}, got)
}

// The client decodes with encoding/json v1 semantics, which Go 1.27 keeps on
// the v2 engine: a duplicated description key resolves to its last value
// instead of failing the whole section.
func TestClaudeClient_DuplicateDescriptionKey(t *testing.T) {
	t.Parallel()

	c := newStubClaudeClient(t, respond(http.StatusOK, textResponse(`{"A":"first","A":"second"}`)))
	got, err := c.Describe(t.Context(), map[string]string{"A": "type: string"}, "A")
	require.NoError(t, err)
	require.Equal(t, map[string]string{"A": "second"}, got)
}

func TestClaudeClient_Errors(t *testing.T) {
	t.Parallel()

	errTransport := errors.New("dial failed")
	tests := []struct {
		name string
		rt   httpx.DoerFunc
		want error
	}{
		{"transport error", func(*http.Request) (*http.Response, error) { return nil, errTransport }, errTransport},
		{"non-200", respond(http.StatusUnauthorized, `{"error":"nope"}`), ErrAPIResponse},
		{"invalid envelope", respond(http.StatusOK, "not json"), ErrAPIResponse},
		{"api error", respond(http.StatusOK, `{"error":{"type":"overloaded","message":"busy"}}`), ErrAPIResponse},
		{"empty content", respond(http.StatusOK, `{"content":[]}`), ErrAPIResponse},
		{"no text block", respond(http.StatusOK, `{"content":[{"type":"thinking","thinking":"..."}]}`), ErrAPIResponse},
		{"non-json text", respond(http.StatusOK, textResponse("sorry, no")), ErrAPIResponse},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := newStubClaudeClient(t, tc.rt)
			_, err := c.Describe(t.Context(), map[string]string{"A": "type: string"}, "A")
			require.ErrorIs(t, err, tc.want)
		})
	}
}
