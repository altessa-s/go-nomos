// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package loader_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/loader"
)

// Most tests in this file change HOME, the working directory or the process
// environment (t.Setenv, t.Chdir), which is incompatible with t.Parallel —
// those stay serial.

type pathConfig struct {
	Name string `yaml:"name"`
}

// writePathConfig writes a config.yaml naming name into dir.
func writePathConfig(t testing.TB, dir, name string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("name: "+name+"\n"), 0o600))
}

// The configured path is normalized before it is read: a leading "~" is the
// home directory, $VAR and ${VAR} are expanded, and a relative path is
// resolved against the working directory.
func TestLoad_PathNormalization(t *testing.T) {
	home := t.TempDir()
	writePathConfig(t, home, "from-home")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	envDir := t.TempDir()
	writePathConfig(t, envDir, "from-env")
	t.Setenv("NOMOS_FIXPATH_DIR", envDir)

	workDir := t.TempDir()
	writePathConfig(t, workDir, "from-cwd")
	t.Chdir(workDir)

	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "home directory", path: "~/config.yaml", want: "from-home"},
		{name: "bare variable", path: "$NOMOS_FIXPATH_DIR/config.yaml", want: "from-env"},
		{name: "braced variable", path: "${NOMOS_FIXPATH_DIR}/config.yaml", want: "from-env"},
		{name: "relative path", path: "config.yaml", want: "from-cwd"},
		{name: "relative with dot segments", path: "./sub/../config.yaml", want: "from-cwd"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &pathConfig{}
			_, err := loader.New(nil, loader.WithPath(tc.path), loader.WithSkipEnv()).Load(cfg)
			require.NoError(t, err)
			require.Equal(t, tc.want, cfg.Name)
		})
	}
}

// A configured path that does not exist fails the load with fs.ErrNotExist.
func TestLoad_MissingPath(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "absent.yaml")
	_, err := loader.New(nil, loader.WithPath(missing)).Load(&pathConfig{})
	require.ErrorIs(t, err, fs.ErrNotExist)
}

// WithPathOnEnvKey reads the path from the variable when it is set, else
// uses the default path.
func TestWithPathOnEnvKey(t *testing.T) {
	fromEnv := t.TempDir()
	writePathConfig(t, fromEnv, "env-path")
	fallback := t.TempDir()
	writePathConfig(t, fallback, "default-path")

	t.Run("variable set", func(t *testing.T) {
		t.Setenv("NOMOS_PATH_ON_ENV_KEY", filepath.Join(fromEnv, "config.yaml"))
		cfg := &pathConfig{}
		_, err := loader.New(nil, loader.WithPathOnEnvKey("NOMOS_PATH_ON_ENV_KEY", filepath.Join(fallback, "config.yaml")),
			loader.WithSkipEnv()).Load(cfg)
		require.NoError(t, err)
		require.Equal(t, "env-path", cfg.Name)
	})
	t.Run("variable unset", func(t *testing.T) {
		cfg := &pathConfig{}
		_, err := loader.New(nil, loader.WithPathOnEnvKey("NOMOS_PATH_ON_ENV_KEY_UNSET", filepath.Join(fallback, "config.yaml")),
			loader.WithSkipEnv()).Load(cfg)
		require.NoError(t, err)
		require.Equal(t, "default-path", cfg.Name)
	})
}

// The generic string options accept a *string: a nil or blank one leaves the
// option unset, another one sets it.
func TestStringOptions_Pointer(t *testing.T) {
	dir := t.TempDir()
	writePathConfig(t, dir, "from-pointer")
	path := filepath.Join(dir, "config.yaml")
	blank := "   "

	t.Run("path pointer", func(t *testing.T) {
		cfg := &pathConfig{}
		_, err := loader.New(nil, loader.WithPath(&path), loader.WithSkipEnv()).Load(cfg)
		require.NoError(t, err)
		require.Equal(t, "from-pointer", cfg.Name)
	})
	t.Run("nil and blank pointers leave options unset", func(t *testing.T) {
		t.Setenv("NOMOSPTR_NAME", "from-env")
		cfg := &pathConfig{}
		_, err := loader.New(nil,
			loader.WithPath((*string)(nil)), loader.WithPath(&blank),
			loader.WithEnvPrefix("NOMOSPTR"), loader.WithEnvPrefix((*string)(nil)), loader.WithEnvPrefix(&blank),
			loader.WithEnvSectionDelimiter((*string)(nil)), loader.WithEnvSectionDelimiter(&blank),
			loader.WithStructTag((*string)(nil)), loader.WithStructTag(&blank),
		).Load(cfg)
		require.NoError(t, err)
		require.Equal(t, "from-env", cfg.Name)
	})
	t.Run("delimiter and struct tag pointers", func(t *testing.T) {
		type tagged struct {
			Section struct {
				Value string `cfg:"value"`
			} `cfg:"section"`
		}
		delimiter, tag := "--", "cfg"
		t.Setenv("NOMOSPTR_SECTION--VALUE", "nested")
		cfg := &tagged{}
		_, err := loader.New(nil, loader.WithEnvPrefix("NOMOSPTR"),
			loader.WithEnvSectionDelimiter(&delimiter), loader.WithStructTag(&tag)).Load(cfg)
		require.NoError(t, err)
		require.Equal(t, "nested", cfg.Section.Value)
	})
}
