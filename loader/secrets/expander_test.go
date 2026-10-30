// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package secrets_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/loader/secrets"

	coreerrs "github.com/altessa-s/go-atlas-core/errors"
)

// mockManager is a map-backed Resolver test double.
type mockManager struct {
	values map[string]string
	err    error
}

func (m *mockManager) Resolve(_ context.Context, key string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	v, ok := m.values[key]
	if !ok {
		return "", coreerrs.ErrNotFound
	}
	return strings.Clone(v), nil
}

func newMockManager(vals map[string]string) *mockManager {
	return &mockManager{values: vals}
}

func TestHasSecrets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"no secrets", "plain text", false},
		{"empty string", "", false},
		{"single secret", "$__secret{ns:key}", true},
		{"secret in text", "prefix $__secret{ns:key} suffix", true},
		{"multiple secrets", "$__secret{a:b} and $__secret{c:d}", true},
		{"incomplete pattern", "$__secret{nocolon}", false},
		{"missing closing brace", "$__secret{ns:key", false},
		{"wrong prefix", "$__sec{ns:key}", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, secrets.HasSecrets(tt.content))
		})
	}
}

func TestExpander_Expand(t *testing.T) {
	t.Parallel()

	mgr := newMockManager(map[string]string{
		"app:password":  "s3cret",
		"db:connection": "host=localhost",
	})

	expander := secrets.New(mgr)
	ctx := t.Context()

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{"no placeholders", "plain text", "plain text"},
		{"single placeholder", "$__secret{app:password}", "s3cret"},
		{"placeholder in text", "pass=$__secret{app:password}!", "pass=s3cret!"},
		{"multiple placeholders", "$__secret{app:password} $__secret{db:connection}", "s3cret host=localhost"},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := expander.Expand(ctx, tt.content)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}

	t.Run("missing secret returns error (fail-closed)", func(t *testing.T) {
		t.Parallel()

		_, err := expander.Expand(ctx, "$__secret{app:missing}")
		require.Error(t, err)
	})
}

func TestExpander_Expand_NilResolver(t *testing.T) {
	t.Parallel()

	t.Run("placeholders fail closed", func(t *testing.T) {
		t.Parallel()

		got, err := secrets.New(nil).Expand(t.Context(), "prefix $__secret{ns:key} suffix")
		require.ErrorIs(t, err, secrets.ErrNoResolver)
		require.Empty(t, got)
	})

	t.Run("no placeholders pass through", func(t *testing.T) {
		t.Parallel()

		got, err := secrets.New(nil).Expand(t.Context(), "plain value")
		require.NoError(t, err)
		require.Equal(t, "plain value", got)
	})

	t.Run("fail-open blanks placeholders", func(t *testing.T) {
		t.Parallel()

		got, err := secrets.New(nil, secrets.WithFailOnError(false)).Expand(t.Context(), "prefix $__secret{ns:key} suffix")
		require.NoError(t, err)
		require.Equal(t, "prefix  suffix", got)
	})
}

func TestExpandString(t *testing.T) {
	t.Parallel()

	mgr := newMockManager(map[string]string{"app:key": "value"})
	ctx := t.Context()

	t.Run("with secrets", func(t *testing.T) {
		t.Parallel()

		got, err := secrets.ExpandString(ctx, "$__secret{app:key}", mgr)
		require.NoError(t, err)
		require.Equal(t, "value", got)
	})

	t.Run("nil manager", func(t *testing.T) {
		t.Parallel()

		_, err := secrets.ExpandString(ctx, "$__secret{app:key}", nil)
		require.ErrorIs(t, err, secrets.ErrNoResolver)
	})

	t.Run("nil manager without placeholders", func(t *testing.T) {
		t.Parallel()

		got, err := secrets.ExpandString(ctx, "plain", nil)
		require.NoError(t, err)
		require.Equal(t, "plain", got)
	})

	t.Run("no secrets", func(t *testing.T) {
		t.Parallel()

		got, err := secrets.ExpandString(ctx, "plain", mgr)
		require.NoError(t, err)
		require.Equal(t, "plain", got)
	})
}

func TestExpander_Expand_ManagerError(t *testing.T) {
	t.Parallel()

	mgr := &mockManager{err: errors.New("connection failed")}
	expander := secrets.New(mgr)

	_, err := expander.Expand(t.Context(), "val=$__secret{app:key}")
	require.Error(t, err)
}

func TestExpander_Expand_FailOnErrorDisabled(t *testing.T) {
	t.Parallel()

	mgr := newMockManager(map[string]string{
		"app:password": "s3cret",
	})
	expander := secrets.New(mgr, secrets.WithFailOnError(false))
	ctx := t.Context()

	t.Run("missing secret returns empty string", func(t *testing.T) {
		t.Parallel()

		got, err := expander.Expand(ctx, "$__secret{app:missing}")
		require.NoError(t, err)
		require.Equal(t, "", got)
	})

	t.Run("manager error returns empty string", func(t *testing.T) {
		t.Parallel()

		errMgr := &mockManager{err: errors.New("connection failed")}
		failOpenExpander := secrets.New(errMgr, secrets.WithFailOnError(false))
		got, err := failOpenExpander.Expand(ctx, "val=$__secret{app:key}")
		require.NoError(t, err)
		require.Equal(t, "val=", got)
	})

	t.Run("existing secret still resolves", func(t *testing.T) {
		t.Parallel()

		got, err := expander.Expand(ctx, "$__secret{app:password}")
		require.NoError(t, err)
		require.Equal(t, "s3cret", got)
	})
}

func TestExpander_Expand_FailClosed_MultipleSecrets(t *testing.T) {
	t.Parallel()

	mgr := newMockManager(map[string]string{
		"app:password": "s3cret",
	})
	expander := secrets.New(mgr)

	// First secret resolves but second one doesn't — should fail on the first failure
	_, err := expander.Expand(t.Context(), "$__secret{app:password} $__secret{app:missing}")
	require.Error(t, err)
}

type cycleNode struct {
	Password string
	Next     *cycleNode
	Any      any
}

// Expand resolves in one pass: a secret's value is inserted literally, even
// when it, alone or joined with the text after it, looks like a placeholder,
// and payloads with control characters and dollars survive.
func TestExpander_Expand_SinglePass(t *testing.T) {
	t.Parallel()

	mgr := newMockManager(map[string]string{
		"app:first": "$__secret", "app:self": "$__secret{app:second}", "app:second": "wrong", "app:odd": "a\x00b$c$$",
	})
	e := secrets.New(mgr)
	for in, want := range map[string]string{
		"$__secret{app:first}{app:second}": "$__secret{app:second}",
		"x-$__secret{app:self}":            "x-$__secret{app:second}",
		"$__secret{app:odd}":               "a\x00b$c$$",
	} {
		got, err := e.Expand(t.Context(), in)
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
}
