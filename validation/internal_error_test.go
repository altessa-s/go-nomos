// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package validationconfig_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	validationconfig "github.com/altessa-s/go-nomos/validation"
	validation "github.com/go-ozzo/ozzo-validation/v4"
)

// An internal error other than a misreferenced field rule is returned as
// ozzo-validation reports it.
func TestValidateStruct_OtherInternalErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		structPtr any
	}{
		{name: "untyped nil", structPtr: nil},
		{name: "struct value, not a pointer", structPtr: struct{ A int }{}},
		{name: "pointer to a non-struct", structPtr: new(int)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validationconfig.ValidateStruct(tc.structPtr)
			ie, ok := errors.AsType[validation.InternalError](err)
			require.True(t, ok, "got %v", err)
			require.ErrorIs(t, ie.InternalError(), validation.ErrStructPointer)
		})
	}
}
