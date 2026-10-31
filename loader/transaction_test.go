// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package loader_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/loader"
	"github.com/altessa-s/go-nomos/loader/backend"
	"github.com/altessa-s/go-nomos/loader/backend/toml"
	"github.com/altessa-s/go-nomos/loader/backend/yaml3"
)

var errRejectedLoad = errors.New("reject configuration")

type transactionalConfig struct {
	Name   string              `yaml:"name"`
	Labels map[string]string   `yaml:"labels"`
	Ports  []int               `yaml:"ports"`
	Child  *transactionalChild `yaml:"child"`
}
type transactionalChild struct {
	Value int `yaml:"value"`
}

func (c *transactionalConfig) Validate() error {
	if c.Name == "reject" {
		return errRejectedLoad
	}
	return nil
}
func TestFailedReloadPreservesPublishedGraph(t *testing.T) {
	t.Parallel()
	for _, reuse := range []bool{false, true} {
		name := "new destination"
		if reuse {
			name = "same destination"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(path, []byte("name: good\nlabels: {x: old}\nports: [1]\nchild: {value: 1}\n"), 0o600))
			l := loader.New(nil, loader.WithPath(path), loader.WithSkipEnv())
			first := &transactionalConfig{}
			_, err := l.LoadContext(t.Context(), first)
			require.NoError(t, err)
			labels, ports, child := first.Labels, first.Ports, first.Child
			next := &transactionalConfig{Labels: labels, Ports: ports, Child: child}
			if reuse {
				next = first
			}
			require.NoError(t, os.WriteFile(path, []byte("name: reject\nlabels: {x: changed, y: added}\nports: [2]\nchild: {value: 2}\n"), 0o600))
			_, err = l.LoadContext(t.Context(), next)
			require.ErrorIs(t, err, errRejectedLoad)
			require.Same(t, first, l.Config())
			require.Equal(t, "good", first.Name)
			require.Equal(t, map[string]string{"x": "old"}, labels)
			require.Equal(t, []int{1}, ports)
			require.Same(t, child, first.Child)
			require.Equal(t, 1, child.Value)
		})
	}
}
func TestFailedFirstLoadPublishesNothing(t *testing.T) {
	t.Parallel()
	l := loader.New(nil, loader.WithSkipEnv())
	_, err := l.LoadContext(t.Context(), &transactionalConfig{Name: "reject"})
	require.ErrorIs(t, err, loader.ErrValidation)
	require.Nil(t, l.Config())
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = l.LoadContext(ctx, &transactionalConfig{})
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, l.Config())
}
func TestEmptyDocumentsUseBackendSemantics(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		b    backend.Backend
		ext  string
	}{
		{"yaml", &yaml3.Backend{}, "yaml"}, {"toml", &toml.Backend{}, "toml"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, content := range []string{"", "   \n", "# comment\n"} {
				path := filepath.Join(t.TempDir(), "config."+tc.ext)
				require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
				var cfg struct {
					Port int `default:"42"`
				}
				_, err := loader.New(tc.b, loader.WithPath(path), loader.WithSkipEnv()).LoadContext(t.Context(), &cfg)
				require.NoError(t, err)
				require.Equal(t, 42, cfg.Port)
			}
		})
	}
}

type renamedTOML struct{ toml.Backend }

func (renamedTOML) StructTagName() string { return "custom" }
func TestSliceReuseIsACapability(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.toml"), []byte("items = [{enabled = false, name = 'a'}]\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.toml"), []byte("items = [{name = 'b'}]\n"), 0o600))
	var cfg struct {
		Items []struct {
			Enabled bool `default:"true"`
			Name    string
		}
	}
	_, err := loader.New(&renamedTOML{}, loader.WithPath(dir), loader.WithSkipEnv()).LoadContext(t.Context(), &cfg)
	require.NoError(t, err)
	require.Len(t, cfg.Items, 1)
	require.False(t, cfg.Items[0].Enabled)
	require.Equal(t, "b", cfg.Items[0].Name)
}

// aliasedTransactionalConfig has two pointers of different types to one
// address: an array and its first element.
type aliasedTransactionalConfig struct {
	Name  string  `yaml:"name"`
	First *int    `yaml:"first"`
	Pair  *[2]int `yaml:"pair"`
}

func (c *aliasedTransactionalConfig) Validate() error {
	if c.Name == "reject" {
		return errRejectedLoad
	}
	return nil
}

func TestFailedReloadRestoresPointersSharingAnAddress(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("name: good\n"), 0o600))
	l := loader.New(nil, loader.WithPath(path), loader.WithSkipEnv())
	pair := [2]int{1, 2}
	cfg := &aliasedTransactionalConfig{First: &pair[0], Pair: &pair}
	_, err := l.LoadContext(t.Context(), cfg)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(path, []byte("name: reject\npair: [7, 8]\n"), 0o600))
	_, err = l.LoadContext(t.Context(), cfg)
	require.ErrorIs(t, err, errRejectedLoad)
	require.Equal(t, [2]int{1, 2}, pair)
	require.Equal(t, "good", cfg.Name)
}
