// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package secrets_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/loader/secrets"
)

func TestReferences(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{name: "empty", content: "", want: []string{}},
		{name: "no placeholders", content: "password: plain", want: []string{}},
		{name: "one", content: "pw: $__secret{db:password}", want: []string{"db:password"}},
		{
			name:    "in order with duplicates",
			content: "$__secret{b:2} x $__secret{a:1} y $__secret{b:2}",
			want:    []string{"b:2", "a:1", "b:2"},
		},
		{name: "key keeps later colons", content: "$__secret{ns:a:b}", want: []string{"ns:a:b"}},
		{name: "adjacent", content: "$__secret{a:1}$__secret{b:2}", want: []string{"a:1", "b:2"}},
		{
			name:    "malformed placeholders",
			content: "$__secret{nokey} $__secret{:k} $__secret{ns:} $secret{a:b} $__secret{a:b",
			want:    []string{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := secrets.References(tc.content)
			require.Equal(t, tc.want, got)
			require.Equal(t, len(got) > 0, secrets.HasSecrets(tc.content), "References agrees with HasSecrets")
		})
	}
}

// Every reference names a placeholder Expand resolves, in the same order.
func TestReferences_MatchesExpand(t *testing.T) {
	t.Parallel()

	content := "a=$__secret{app:a} b=$__secret{app:b} a2=$__secret{app:a}"
	var resolved []string
	e := secrets.New(secrets.ResolverFunc(func(_ context.Context, ref string) (string, error) {
		resolved = append(resolved, ref)
		return "v", nil
	}))
	_, err := e.Expand(t.Context(), content)
	require.NoError(t, err)
	require.Equal(t, secrets.References(content), resolved)
}
