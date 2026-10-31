// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package loader_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/loader"

	loadersecrets "github.com/altessa-s/go-nomos/loader/secrets"
)

var (
	errScrubTarget = errors.New("scrub target")
	errScrubAlias  = errors.New("scrub alias")
	errScrubOther  = errors.New("scrub other")
)

// fieldErrors is a map of errors keyed by location that does not unwrap, like
// ozzo-validation's Errors.
type fieldErrors map[string]error

func (e fieldErrors) Error() string {
	parts := make([]string, 0, len(e))
	for k, v := range e {
		parts = append(parts, k+": "+v.Error())
	}
	return strings.Join(parts, "; ")
}

// aliasError matches errScrubAlias through its Is method only.
type aliasError struct{}

func (aliasError) Error() string        { return "alias" }
func (aliasError) Is(target error) bool { return target == errScrubAlias }

// emptyWrapper wraps nothing.
type emptyWrapper struct{}

func (emptyWrapper) Error() string { return "empty wrapper" }
func (emptyWrapper) Unwrap() error { return nil }

type scrubConfig struct {
	Token  string   `yaml:"token"`
	Items  []string `yaml:"items"`
	Leaf   int      `yaml:"leaf"`
	Skip   string   `yaml:"-" json:"-"`
	hidden string
}

// Validate rejects the configuration with a joined tree of location maps.
func (c *scrubConfig) Validate() error {
	_ = c.hidden
	return errors.Join(emptyWrapper{}, fieldErrors{
		"token":  aliasError{},
		"items":  fieldErrors{"3": errScrubTarget},
		"leaf":   fieldErrors{"below-a-value": errScrubTarget},
		"Skip":   errScrubTarget,
		"hidden": fieldErrors{"deeper": errScrubTarget},
	})
}

// A validation error withheld because secrets were resolved names only the
// locations of the configuration's schema: a key that names no exported
// field, or that lies inside a collection or below a value, becomes "*".
// errors.Is still reaches every error of the tree, through Unwrap, maps of
// errors and Is methods.
func TestLoad_WithheldValidationTree(t *testing.T) {
	t.Parallel()

	resolver := loadersecrets.ResolverFunc(func(context.Context, string) (string, error) { return "s3cr3t-value", nil })
	err := loadFiles(t, nil, &scrubConfig{}, map[string]string{"a.yaml": "token: $__secret{app:token}\n"},
		loader.WithSecretResolver(resolver))

	require.ErrorIs(t, err, loader.ErrValidation)
	require.ErrorIs(t, err, errScrubTarget)
	require.ErrorIs(t, err, errScrubAlias)
	require.NotErrorIs(t, err, errScrubOther)
	require.Equal(t, "validation failed for *.*, Skip, items.*, leaf.*, token (secrets were resolved; details withheld)", err.Error())
	require.NotContains(t, err.Error(), "s3cr3t-value")
}
