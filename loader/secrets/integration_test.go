//go:build integration

// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package secrets_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/loader/secrets"

	coreerrs "github.com/altessa-s/go-atlas-core/errors"
)

const itPlaceholders = 200

var errVaultDown = errors.New("vault unavailable")

// itResolver resolves "app:kN" to a recognizable secret value, fails with
// errVaultDown for every key in failing and with ErrNotFound for unknown
// namespaces. It counts its calls.
type itResolver struct {
	failing map[string]bool
	calls   atomic.Int64
}

func (r *itResolver) Resolve(_ context.Context, ref string) (string, error) {
	r.calls.Add(1)
	ns, key, ok := strings.Cut(ref, ":")
	if !ok || ns != "app" {
		return "", coreerrs.ErrNotFound
	}
	if r.failing[key] {
		// The store names the key in its own error; the expander adds
		// no secret name or value of its own.
		return "", fmt.Errorf("%w: key %s", errVaultDown, key)
	}
	return secretValue(key), nil
}

func secretValue(key string) string { return "S3CR3T-" + key + "-VALUE" }

// itDocument returns a YAML-like document with n placeholders and returns
// the keys it references.
func itDocument(n int) (string, []string) {
	var sb strings.Builder
	keys := make([]string, 0, n)
	for i := range n {
		key := "k" + strconv.Itoa(i)
		keys = append(keys, key)
		fmt.Fprintf(&sb, "field%d: prefix-$__secret{app:%s}-suffix\n", i, key)
	}
	return sb.String(), keys
}

// Every placeholder of a large document is resolved in one pass.
func TestIntegration_ExpandManyPlaceholders(t *testing.T) {
	t.Parallel()

	doc, keys := itDocument(itPlaceholders)
	require.Len(t, secrets.References(doc), itPlaceholders)

	r := &itResolver{}
	out, err := secrets.New(r).Expand(t.Context(), doc)
	require.NoError(t, err)
	require.False(t, secrets.HasSecrets(out))
	require.EqualValues(t, itPlaceholders, r.calls.Load())
	for i, key := range keys {
		require.Contains(t, out, fmt.Sprintf("field%d: prefix-%s-suffix\n", i, secretValue(key)))
	}
}

// When some keys fail, fail-closed expansion returns an error that carries
// the resolver's error but neither a resolved secret value nor the secret
// names of the document.
func TestIntegration_ExpandFailingKeysDoNotLeak(t *testing.T) {
	t.Parallel()

	doc, keys := itDocument(itPlaceholders)
	failing := map[string]bool{keys[50]: true, keys[120]: true, keys[199]: true}

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	r := &itResolver{failing: failing}
	out, err := secrets.New(r, secrets.WithLogger(logger)).Expand(t.Context(), doc)

	require.ErrorIs(t, err, errVaultDown)
	require.Empty(t, out)
	require.EqualValues(t, 51, r.calls.Load(), "expansion stops at the first failing key")
	for _, key := range keys[:50] {
		require.NotContains(t, err.Error(), secretValue(key), "a resolved value never reaches the error")
	}
	require.NotContains(t, logs.String(), "S3CR3T", "nothing at warn level carries a secret value")
	require.NotContains(t, logs.String(), "app:", "nothing at warn level names a secret")
}

// Fail-open expansion blanks the failing placeholders, keeps every other
// value, and logs no secret value or name at warn level.
func TestIntegration_ExpandFailOpen(t *testing.T) {
	t.Parallel()

	doc, keys := itDocument(itPlaceholders)
	failing := map[string]bool{}
	for i := 0; i < len(keys); i += 7 {
		failing[keys[i]] = true
	}
	doc += "unknown: $__secret{other:missing}\n"

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	r := &itResolver{failing: failing}
	out, err := secrets.New(r, secrets.WithFailOnError(false), secrets.WithLogger(logger)).Expand(t.Context(), doc)
	require.NoError(t, err)
	require.False(t, secrets.HasSecrets(out))
	require.EqualValues(t, itPlaceholders+1, r.calls.Load())

	for i, key := range keys {
		want := secretValue(key)
		if failing[key] {
			want = ""
		}
		require.Contains(t, out, fmt.Sprintf("field%d: prefix-%s-suffix\n", i, want))
	}
	require.Contains(t, out, "unknown: \n")
	require.NotContains(t, logs.String(), "S3CR3T")
	require.NotContains(t, logs.String(), "app:")
	require.NotContains(t, logs.String(), "other:missing")
}

// One Expander serves concurrent expansions of large documents.
func TestIntegration_ExpandConcurrent(t *testing.T) {
	t.Parallel()

	doc, _ := itDocument(itPlaceholders)
	e := secrets.New(&itResolver{})
	want, err := e.Expand(t.Context(), doc)
	require.NoError(t, err)

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			got, err := e.Expand(t.Context(), doc)
			if err == nil && got != want {
				err = errors.New("concurrent expansion differs")
			}
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
}
