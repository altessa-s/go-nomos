// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package validationconfig_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	validationconfig "github.com/altessa-s/go-nomos/validation"
	validation "github.com/go-ozzo/ozzo-validation/v4"
)

func TestDurationRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		value       any
		positive    bool // PositiveDuration accepts
		nonNegative bool // NonNegativeDuration accepts
	}{
		{"positive", time.Second, true, true},
		{"zero", time.Duration(0), false, true},
		{"negative", -time.Second, false, false},
		{"positive pointer", new(time.Second), true, true},
		{"negative pointer", new(-time.Second), false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			check := func(accept bool, rule validation.Rule) {
				t.Helper()
				err := validation.Validate(tc.value, rule)
				if accept {
					require.NoError(t, err)
					return
				}
				require.Error(t, err)
			}
			check(tc.positive, validationconfig.PositiveDuration())
			check(tc.nonNegative, validationconfig.NonNegativeDuration())
		})
	}

	err := validation.Validate(-time.Second, validationconfig.NonNegativeDuration())
	verr, ok := errors.AsType[validation.Error](err)
	require.True(t, ok, "got %v", err)
	require.Equal(t, validationconfig.ErrNegativeDuration.Code(), verr.Code())
}
