// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package convert_test

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/cmd/nomos/commands/convert"

	coreio "github.com/altessa-s/go-atlas-core/io"
	httpclient "github.com/altessa-s/go-atlas-net/http/client"
)

func TestDescriptionTimeout(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		server := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-release:
			}
		}))
		defer close(release)
		client := convert.NewClaudeClientWithOptions("key", convert.WithEndpoint(server.URL), convert.WithHTTPClient(server.Client()), convert.WithRequestTimeout(time.Second))
		_, err := client.Describe(t.Context(), map[string]string{"A": "string"}, "A")
		require.ErrorIs(t, err, context.DeadlineExceeded)
	})
}
func TestDescriptionCancellation(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		started := make(chan struct{})
		release := make(chan struct{})
		server := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(started)
			select {
			case <-r.Context().Done():
			case <-release:
			}
		}))
		defer close(release)
		client := convert.NewClaudeClientWithOptions("key", convert.WithEndpoint(server.URL), convert.WithHTTPClient(server.Client()))
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan error, 1)
		go func() { _, err := client.Describe(ctx, nil, "A"); done <- err }()
		<-started
		cancel()
		synctest.Wait()
		require.ErrorIs(t, <-done, context.Canceled)
	})
}
func TestDescriptionResponseLimit(t *testing.T) {
	t.Parallel()
	server := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("response too large")) }))
	client := convert.NewClaudeClientWithOptions("key", convert.WithEndpoint(server.URL), convert.WithHTTPClient(server.Client()), convert.WithMaxResponseBytes(4))
	_, err := client.Describe(t.Context(), nil, "A")
	require.ErrorIs(t, err, convert.ErrAPIResponse)
	require.ErrorIs(t, err, coreio.ErrReadLimitExceeded)
}

func TestCanceledConversionDoesNotWrite(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, source, target string }{
		{"environment", "env", "yaml"}, {"structured", "yaml", "toml"}, {"flatten", "yaml", "env"}, {"markdown", "yaml", "md"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			input := filepath.Join(dir, "input."+tc.source)
			output := filepath.Join(dir, "output."+tc.target)
			content := "name: test\n"
			if tc.source == "env" {
				content = "NAME=test\n"
			}
			require.NoError(t, os.WriteFile(input, []byte(content), 0o600))
			require.NoError(t, os.WriteFile(output, []byte("keep"), 0o600))
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			cmd := convert.New()
			cmd.SetArgs([]string{"--from", input, "--to", output})
			require.ErrorIs(t, cmd.ExecuteContext(ctx), context.Canceled)
			got, err := os.ReadFile(output)
			require.NoError(t, err)
			require.Equal(t, "keep", string(got))
		})
	}
}

func TestDescriptionMaximumLimit(t *testing.T) {
	t.Parallel()
	server := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"{}"}]}`))
	}))
	client := convert.NewClaudeClientWithOptions("key", convert.WithEndpoint(server.URL), convert.WithHTTPClient(server.Client()), convert.WithMaxResponseBytes(math.MaxInt64))
	got, err := client.Describe(t.Context(), nil, "A")
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestDescriptionInvalidLimitPreservesLastValidOption(t *testing.T) {
	t.Parallel()
	server := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"{}"}]}`))
	}))
	client := convert.NewClaudeClientWithOptions("key", convert.WithEndpoint(server.URL), convert.WithHTTPClient(server.Client()),
		convert.WithMaxResponseBytes(4), convert.WithMaxResponseBytes(0))
	_, err := client.Describe(t.Context(), nil, "A")
	require.ErrorIs(t, err, coreio.ErrReadLimitExceeded)
}

func TestDefaultDescriptionClientBlocksPrivateEndpoint(t *testing.T) {
	t.Parallel()
	client := convert.NewClaudeClientWithOptions("key", convert.WithEndpoint("http://127.0.0.1:1/messages"))
	_, err := client.Describe(t.Context(), nil, "A")
	require.ErrorIs(t, err, httpclient.ErrSSRFBlocked)
}
