// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package loader_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/loader"
)

// The skip_zero option is removed from a default tag without touching the
// commas of its value.
func TestDefaultTagSkipZeroKeepsCommas(t *testing.T) {
	t.Parallel()
	var cfg struct {
		Hosts []string `default:"a,b,skip_zero"`
	}
	_, err := loader.New(nil, loader.WithSkipEnv()).LoadContext(t.Context(), &cfg)
	require.NoError(t, err)
	require.Equal(t, []string{"a", "b"}, cfg.Hosts)
}

// collectionEnvConfig takes collections from the environment.
type collectionEnvConfig struct {
	Ports  []int
	Hosts  []string
	Groups [][]string
}

// A bracketed list is parsed with the grammar the env converter emits: []
// is an empty list, and a double-quoted element is unquoted whole, commas,
// brackets and escapes included. An unbracketed list is split on commas. It
// mutates the environment, so it is not parallel.
func TestEnvBracketedLists(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
		want collectionEnvConfig
	}{
		{
			name: "empty lists",
			env:  map[string]string{"PORTS": "[]", "HOSTS": "[ ]"},
			want: collectionEnvConfig{Ports: []int{}, Hosts: []string{}},
		},
		{
			name: "quoted elements",
			env:  map[string]string{"HOSTS": `["a,b", "c\"]d", "e\\f", plain]`},
			want: collectionEnvConfig{Hosts: []string{"a,b", `c"]d`, `e\f`, "plain"}},
		},
		{
			name: "nested lists",
			env:  map[string]string{"GROUPS": `[[a,"b,c"],[],[d]]`},
			want: collectionEnvConfig{Groups: [][]string{{"a", "b,c"}, {}, {"d"}}},
		},
		{
			name: "unbracketed list",
			env:  map[string]string{"HOSTS": "a, b", "PORTS": "1,2"},
			want: collectionEnvConfig{Hosts: []string{"a", "b"}, Ports: []int{1, 2}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv("COREBRACKETTEST_"+k, v)
			}
			var cfg collectionEnvConfig
			_, err := loader.New(nil, loader.WithEnvPrefix("COREBRACKETTEST")).LoadContext(t.Context(), &cfg)
			require.NoError(t, err)
			require.Equal(t, tc.want, cfg)
		})
	}
}

// A malformed bracketed list fails the load without quoting the value. It
// mutates the environment, so it is not parallel.
func TestEnvBracketedListMalformed(t *testing.T) {
	for _, tc := range []struct{ name, env, value string }{
		{"invalid escape", "HOSTS", `["a\qsecretvalue"]`},
		{"unterminated quote", "HOSTS", `["secretvalue]`},
		{"dangling escape", "HOSTS", `["secretvalue\]`},
		{"text after a quote", "HOSTS", `["a"secretvalue]`},
		{"unclosed nested bracket", "GROUPS", `[[secretvalue]`},
		{"extra closing bracket", "GROUPS", `[secretvalue]]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("COREBRACKETBADTEST_"+tc.env, tc.value)
			var cfg collectionEnvConfig
			_, err := loader.New(nil, loader.WithEnvPrefix("COREBRACKETBADTEST")).LoadContext(t.Context(), &cfg)
			require.ErrorIs(t, err, loader.ErrBindEnv)
			require.ErrorIs(t, err, loader.ErrSliceItemInvalid)
			require.NotContains(t, err.Error(), "secretvalue")
		})
	}
}

// An empty variable assigns nothing to a pointer field, so it allocates
// nothing. It mutates the environment, so it is not parallel.
func TestEnvEmptyValueLeavesPointerNil(t *testing.T) {
	t.Setenv("COREEMPTYPTRTEST_COUNT", "")
	var cfg struct {
		Count *int
	}
	_, err := loader.New(nil, loader.WithEnvPrefix("COREEMPTYPTRTEST")).LoadContext(t.Context(), &cfg)
	require.NoError(t, err)
	require.Nil(t, cfg.Count)
}
