// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package loader_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/loader"
)

// reloadValidator quotes its token when Fail is set.
type reloadValidator struct {
	Token string `yaml:"token"`
	Fail  bool   `yaml:"fail"`
}

// Validate rejects the configuration, quoting the token, when Fail is set.
func (v *reloadValidator) Validate() error {
	if v.Fail {
		return fmt.Errorf("token %q: %w", v.Token, errRejected)
	}
	return nil
}

// A secret resolved by an earlier load of a loader stays in a destination
// reloaded from a document that omits it, so the errors of every later load
// are withheld, whether the earlier load succeeded or failed.
func TestReloadKeepsSecretErrorsWithheld(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		first string
	}{
		{"after a successful load", "token: $__secret{app:token}\n"},
		{"after a failed load", "token: $__secret{app:token}\nfail: true\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(path, []byte(tc.first), 0o600))
			manager := newCountingManager(map[string]string{"app:token": leakySecret})
			l := loader.New(nil, loader.WithPath(path), loader.WithSkipEnv(), loader.WithSecretResolver(manager))
			cfg := &reloadValidator{}
			_, _ = l.LoadContext(t.Context(), cfg)
			require.Equal(t, 1, manager.count("app:token"))

			require.NoError(t, os.WriteFile(path, []byte("fail: true\n"), 0o600))
			_, err := l.LoadContext(t.Context(), cfg)
			require.ErrorIs(t, err, errRejected)
			require.ErrorIs(t, err, loader.ErrValidation)
			requireNoSecret(t, err)
			require.Equal(t, 1, manager.count("app:token"), "the reload resolves no new secret")
		})
	}
}
