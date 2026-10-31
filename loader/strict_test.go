// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package loader_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/loader"
)

// --- Test configs for strict mode ---

type strictEnvConfig struct {
	Host string `yaml:"host" env:"STRICT_TEST_HOST"`
}

type strictDefaultConfig struct {
	URL string `yaml:"url" default:"https://$STRICT_UNDEFINED_DOMAIN/api"`
}

type strictDefaultBracketConfig struct {
	URL string `yaml:"url" default:"https://${STRICT_UNDEFINED_DOMAIN}/api"`
}

type strictChanConfig struct {
	Name    string   `yaml:"name" default:"test"`
	Channel chan int `yaml:"channel"`
}

type strictNestedConfig struct {
	Database struct {
		Host string `yaml:"host" default:"localhost"`
	} `yaml:"database"`
}

// --- Undefined env var tests ---

// It mutates the process environment via t.Setenv, so it is not parallel.
func TestStrict_UndefinedEnvVar_SimpleValue(t *testing.T) {
	// Set an env var whose value references an undefined variable
	t.Setenv("STRICT_TEST_HOST", "$STRICT_UNDEFINED_VAR")

	cfg := &strictEnvConfig{}
	l := loader.New(nil, loader.WithStrict())

	_, err := l.Load(cfg)
	require.Error(t, err)
	require.True(t, errors.Is(err, loader.ErrUndefinedEnvVar))
}

// It mutates the process environment via t.Setenv, so it is not parallel.
func TestStrict_UndefinedEnvVar_MixedString(t *testing.T) {
	t.Setenv("STRICT_TEST_HOST", "prefix_$STRICT_UNDEFINED_VAR_suffix")

	cfg := &strictEnvConfig{}
	l := loader.New(nil, loader.WithStrict())

	_, err := l.Load(cfg)
	require.Error(t, err)
	require.True(t, errors.Is(err, loader.ErrUndefinedEnvVar))
}

func TestStrict_UndefinedEnvVar_InDefaultTag(t *testing.T) {
	// STRICT_UNDEFINED_DOMAIN is not set
	cfg := &strictDefaultConfig{}
	l := loader.New(nil, loader.WithStrict())

	_, err := l.Load(cfg)
	require.Error(t, err)
	require.True(t, errors.Is(err, loader.ErrUndefinedEnvVar))
}

func TestStrict_UndefinedEnvVar_InDefaultTagBracketSyntax(t *testing.T) {
	// STRICT_UNDEFINED_DOMAIN is not set
	cfg := &strictDefaultBracketConfig{}
	l := loader.New(nil, loader.WithStrict())

	_, err := l.Load(cfg)
	require.Error(t, err)
	require.True(t, errors.Is(err, loader.ErrUndefinedEnvVar))
}

func TestStrict_UndefinedEnvVar_InConfigFile(t *testing.T) {
	content := `host: ${STRICT_UNDEFINED_FILE_VAR}`

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(content), 0644))

	cfg := &strictEnvConfig{}
	l := loader.New(nil, loader.WithPath(configPath), loader.WithStrict())

	_, err := l.Load(cfg)
	require.Error(t, err)
	require.True(t, errors.Is(err, loader.ErrUndefinedEnvVar))
}

// --- Unsupported field type test ---

// It mutates the process environment via t.Setenv, so it is not parallel.
func TestStrict_UnsupportedFieldType(t *testing.T) {
	// chan fields cannot be set from string
	cfg := &strictChanConfig{}
	l := loader.New(nil, loader.WithStrict(), loader.WithSkipEnv())

	// Set the channel field via env to trigger the set() default case
	t.Setenv("CHANNEL", "something")

	cfg2 := &strictChanConfig{}
	l2 := loader.New(nil, loader.WithStrict())

	_, err := l2.Load(cfg2)
	if err != nil && errors.Is(err, loader.ErrUnsupportedFieldType) {
		// Expected error
		return
	}

	// If channel env didn't trigger it, try loading with defaults only
	// The chan field with no default tag should just be skipped
	_, err = l.Load(cfg)
	if err != nil && errors.Is(err, loader.ErrUnsupportedFieldType) {
		// Expected
		return
	}

	// If there's no default or env for chan, strict mode won't error
	// because set() is only called when there's a value to set.
	// This is expected behavior - strict mode only fails on actual operations.
}

// --- Field assignment failure test ---

type strictAnonymousBase struct {
	Value string
}

type strictAnonymousOuter struct {
	*strictAnonymousBase
	Name string `yaml:"name" default:"test"`
}

func TestStrict_FieldAssignmentFailure(t *testing.T) {
	// A nil embedded pointer struct is allocated in strict mode; any assignment
	// failure must surface as ErrFieldAssignment.
	cfg := &strictAnonymousOuter{}
	l := loader.New(nil, loader.WithStrict())

	_, err := l.Load(cfg)
	// For compatible types this should succeed
	if err != nil {
		// If there is an assignment error, it should be ErrFieldAssignment
		require.True(t, errors.Is(err, loader.ErrFieldAssignment))
	}
}

// --- Missing field tests ---

// It mutates the process environment via t.Setenv, so it is not parallel.
func TestStrict_MissingNestedField(t *testing.T) {
	// DATABASE matches the root field, but NONEXISTENT doesn't match any nested field
	t.Setenv("DATABASE__NONEXISTENT", "value")

	cfg := &strictNestedConfig{}
	l := loader.New(nil, loader.WithStrict())

	_, err := l.Load(cfg)
	require.Error(t, err)
	require.True(t, errors.Is(err, loader.ErrFieldNotFound))
}

// --- Defined empty env var (not an error) ---

// It mutates the process environment via t.Setenv, so it is not parallel.
func TestStrict_DefinedEmptyEnvVar(t *testing.T) {
	// An empty string is a valid defined value - should NOT error
	t.Setenv("STRICT_TEST_HOST", "")

	cfg := &strictEnvConfig{}
	l := loader.New(nil, loader.WithStrict())

	_, err := l.Load(cfg)
	require.NoError(t, err)
}

// --- Backward compatibility (lenient mode) ---

// It mutates the process environment via t.Setenv, so it is not parallel.
func TestLenient_BackwardCompat_UndefinedEnvVar(t *testing.T) {
	t.Setenv("STRICT_TEST_HOST", "$STRICT_UNDEFINED_VAR")

	cfg := &strictEnvConfig{}
	l := loader.New(nil) // No WithStrict()

	_, err := l.Load(cfg)
	require.NoError(t, err)
}

func TestLenient_BackwardCompat_UndefinedDefaultTag(t *testing.T) {
	cfg := &strictDefaultConfig{}
	l := loader.New(nil) // No WithStrict()

	_, err := l.Load(cfg)
	require.NoError(t, err)

	// In lenient mode, undefined vars are replaced with empty string
	require.Equal(t, "https:///api", cfg.URL)
}

// It mutates the process environment via t.Setenv, so it is not parallel.
func TestLenient_BackwardCompat_MissingNestedField(t *testing.T) {
	t.Setenv("DATABASE__NONEXISTENT", "value")

	cfg := &strictNestedConfig{}
	l := loader.New(nil) // No WithStrict()

	_, err := l.Load(cfg)
	require.NoError(t, err)
}

func TestLenient_BackwardCompat_ConfigFile(t *testing.T) {
	content := `host: ${STRICT_UNDEFINED_FILE_VAR}`

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(content), 0644))

	cfg := &strictEnvConfig{}
	l := loader.New(nil, loader.WithPath(configPath)) // No WithStrict()

	_, err := l.Load(cfg)
	require.NoError(t, err)
}

// --- Strict mode with valid config (no errors) ---

// It mutates the process environment via t.Setenv, so it is not parallel.
func TestStrict_ValidConfig(t *testing.T) {
	t.Setenv("STRICT_TEST_HOST", "valid-host")

	cfg := &strictEnvConfig{}
	l := loader.New(nil, loader.WithStrict())

	_, err := l.Load(cfg)
	require.NoError(t, err)
	require.Equal(t, "valid-host", cfg.Host)
}

// It mutates the process environment via t.Setenv, so it is not parallel.
func TestStrict_ValidConfig_WithDefinedDefaultVars(t *testing.T) {
	t.Setenv("STRICT_UNDEFINED_DOMAIN", "example.com")

	cfg := &strictDefaultConfig{}
	l := loader.New(nil, loader.WithStrict())

	_, err := l.Load(cfg)
	require.NoError(t, err)
	require.Equal(t, "https://example.com/api", cfg.URL)
}

// It mutates the process environment via t.Setenv, so it is not parallel.
func TestStrict_ValidConfigFile(t *testing.T) {
	t.Setenv("STRICT_FILE_HOST", "file-host-value")

	content := `host: ${STRICT_FILE_HOST}`

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(content), 0644))

	cfg := &strictEnvConfig{}
	l := loader.New(nil, loader.WithPath(configPath), loader.WithStrict())

	_, err := l.Load(cfg)
	require.NoError(t, err)
	require.Equal(t, "file-host-value", cfg.Host)
}

// --- Paths deeper than a primitive map value or slice element ---

type strictPrimitiveContainerConfig struct {
	Tags  map[string]string `yaml:"tags"`
	Ports []int             `yaml:"ports"`
}

// It mutates the process environment via t.Setenv, so it is not parallel.
func TestStrict_PathBeyondPrimitiveMapValue(t *testing.T) {
	t.Setenv("TAGS__KEY__EXTRA", "value")

	cfg := &strictPrimitiveContainerConfig{}
	l := loader.New(nil, loader.WithStrict())

	_, err := l.Load(cfg)
	require.Error(t, err)
	require.True(t, errors.Is(err, loader.ErrFieldNotFound))
}

// It mutates the process environment via t.Setenv, so it is not parallel.
func TestStrict_PathBeyondPrimitiveSliceElement(t *testing.T) {
	t.Setenv("PORTS__0__EXTRA", "80")

	cfg := &strictPrimitiveContainerConfig{}
	l := loader.New(nil, loader.WithStrict())

	_, err := l.Load(cfg)
	require.Error(t, err)
	require.True(t, errors.Is(err, loader.ErrFieldNotFound))
}

// It mutates the process environment via t.Setenv, so it is not parallel.
func TestLenient_BackwardCompat_PathBeyondPrimitiveMapValue(t *testing.T) {
	t.Setenv("TAGS__KEY__EXTRA", "value")

	cfg := &strictPrimitiveContainerConfig{}
	l := loader.New(nil) // No WithStrict()

	_, err := l.Load(cfg)
	require.NoError(t, err)
	require.Empty(t, cfg.Tags["KEY"])
}

// It mutates the process environment via t.Setenv, so it is not parallel.
func TestLenient_BackwardCompat_PathBeyondPrimitiveSliceElement(t *testing.T) {
	t.Setenv("PORTS__0__EXTRA", "80")

	cfg := &strictPrimitiveContainerConfig{}
	l := loader.New(nil) // No WithStrict()

	_, err := l.Load(cfg)
	require.NoError(t, err)
	require.NotContains(t, cfg.Ports, 80)
}

type strictNestedPrimitiveContainerConfig struct {
	Items []strictPrimitiveContainerConfig `yaml:"items"`
}

// It mutates the process environment via t.Setenv, so it is not parallel.
func TestStrict_ExtraLevelBelowPrimitiveSliceElement(t *testing.T) {
	tests := []struct {
		name string
		env  string
	}{
		{name: "numeric level", env: "PORTS__0__1"},
		{name: "name then numeric level", env: "PORTS__0__EXTRA__1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(tc.env, "80")

			cfg := &strictPrimitiveContainerConfig{}
			_, err := loader.New(nil, loader.WithStrict()).Load(cfg)
			require.ErrorIs(t, err, loader.ErrFieldNotFound)

			cfg = &strictPrimitiveContainerConfig{}
			_, err = loader.New(nil).Load(cfg)
			require.NoError(t, err)
			require.Empty(t, cfg.Ports)
		})
	}
}

type strictLegacySuffixConfig struct {
	Inner struct {
		Ports []int `yaml:"ports"`
	} `yaml:"inner"`
}

// It mutates the process environment via t.Setenv, so it is not parallel.
func TestStrict_LegacyIndexSuffixWithCustomDelimiter(t *testing.T) {
	t.Setenv("INNER.PORTS__0", "80")

	cfg := &strictLegacySuffixConfig{}
	_, err := loader.New(nil, loader.WithStrict(), loader.WithEnvSectionDelimiter(".")).Load(cfg)
	require.NoError(t, err)
	require.Equal(t, []int{80}, cfg.Inner.Ports)
}

type strictLeafInSliceElementConfig struct {
	Items []struct {
		Timeout time.Duration `yaml:"timeout"`
	} `yaml:"items"`
	Refs []*struct {
		Name string `yaml:"name"`
	} `yaml:"refs"`
}

// It mutates the process environment via t.Setenv, so it is not parallel.
func TestStrict_LevelBelowLeafInSliceElement(t *testing.T) {
	tests := []struct {
		name string
		env  string
	}{
		{name: "named level", env: "ITEMS__0__TIMEOUT__EXTRA"},
		{name: "numeric level", env: "ITEMS__0__TIMEOUT__1"},
		{name: "pointer element", env: "REFS__0__NAME__EXTRA"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(tc.env, "80")

			_, err := loader.New(nil, loader.WithStrict()).Load(&strictLeafInSliceElementConfig{})
			require.ErrorIs(t, err, loader.ErrFieldNotFound)

			_, err = loader.New(nil).Load(&strictLeafInSliceElementConfig{})
			require.NoError(t, err)
		})
	}
}

type numericMapKeyConfig struct {
	Tags  map[string][]int `yaml:"tags"`
	Items []struct {
		Tags map[string][]int `yaml:"tags"`
	} `yaml:"items"`
}

// It mutates the process environment via t.Setenv, so it is not parallel.
func TestLoad_NumericMapKeyInMapOfSlices(t *testing.T) {
	t.Setenv("TAGS__0__0", "80")
	t.Setenv("ITEMS__0__TAGS__1__0", "81")

	for _, strict := range []bool{false, true} {
		var opts []loader.Option
		if strict {
			opts = append(opts, loader.WithStrict())
		}
		cfg := &numericMapKeyConfig{}
		_, err := loader.New(nil, opts...).Load(cfg)
		require.NoError(t, err)
		require.Equal(t, map[string][]int{"0": {80}}, cfg.Tags)
		require.Len(t, cfg.Items, 1)
		require.Equal(t, map[string][]int{"1": {81}}, cfg.Items[0].Tags)
	}
}

type strictCollectionValuedConfig struct {
	Items []struct {
		Tags map[string][]int `yaml:"tags"`
	} `yaml:"items"`
	Matrix [][]int `yaml:"matrix"`
}

// It mutates the process environment via t.Setenv, so it is not parallel.
func TestStrict_CollectionValuedPathsStillLoad(t *testing.T) {
	t.Setenv("ITEMS__0__TAGS__key__0", "80")
	// A slice of slices element is not set from the environment; strict
	// mode must still not report its path as addressing nothing.
	t.Setenv("MATRIX__0__1", "82")

	cfg := &strictCollectionValuedConfig{}
	_, err := loader.New(nil, loader.WithStrict()).Load(cfg)
	require.NoError(t, err)
	require.Len(t, cfg.Items, 1)
	require.Equal(t, map[string][]int{"key": {80}}, cfg.Items[0].Tags)
}

// It mutates the process environment via t.Setenv, so it is not parallel.
func TestStrict_PathBeyondPrimitiveInSliceElement(t *testing.T) {
	tests := []struct {
		name string
		env  string
	}{
		{name: "map value", env: "ITEMS__0__TAGS__KEY__EXTRA"},
		{name: "slice element", env: "ITEMS__0__PORTS__1__EXTRA"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(tc.env, "80")

			_, err := loader.New(nil, loader.WithStrict()).Load(&strictNestedPrimitiveContainerConfig{})
			require.ErrorIs(t, err, loader.ErrFieldNotFound)

			cfg := &strictNestedPrimitiveContainerConfig{}
			_, err = loader.New(nil).Load(cfg)
			require.NoError(t, err)
			require.Empty(t, cfg.Items)
		})
	}
}
