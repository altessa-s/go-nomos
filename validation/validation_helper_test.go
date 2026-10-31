// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package validationconfig

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

type testStruct struct {
	Name  string
	Value int
}

func TestValidateStruct_Valid(t *testing.T) {
	t.Parallel()

	s := testStruct{Name: "test", Value: 42}
	err := ValidateStruct(&s,
		validation.Field(&s.Name, validation.Required),
	)
	require.NoError(t, err)
}

func TestValidateStruct_ValidationError(t *testing.T) {
	t.Parallel()

	s := testStruct{Name: "", Value: 42}
	err := ValidateStruct(&s,
		validation.Field(&s.Name, validation.Required),
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Name")
}

func TestValidateStruct_ErrFieldNotFound(t *testing.T) {
	t.Parallel()

	s := testStruct{Name: "test"}
	other := testStruct{Name: "other"}

	err := ValidateStruct(&s,
		validation.Field(&other.Name, validation.Required),
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "testStruct")
	require.Contains(t, err.Error(), "field rule #0")
	require.Contains(t, err.Error(), "pointer does not reference a field in the struct")
}

func TestValidateStruct_ErrFieldPointer(t *testing.T) {
	t.Parallel()

	s := testStruct{Name: "test"}

	err := ValidateStruct(&s,
		validation.Field(s.Name, validation.Required),
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "testStruct")
	require.Contains(t, err.Error(), "field rule #0")
	require.Contains(t, err.Error(), "field must be specified as a pointer")
}

func TestValidateStruct_NilStructPtr(t *testing.T) {
	t.Parallel()

	s := testStruct{}
	err := ValidateStruct((*testStruct)(nil),
		validation.Field(&s.Name, validation.Required),
	)
	require.NoError(t, err)
}

func TestValidateStructIfEnabled_Disabled(t *testing.T) {
	t.Parallel()

	s := testStruct{Name: ""}
	err := ValidateStructIfEnabled(false, &s,
		validation.Field(&s.Name, validation.Required),
	)
	require.NoError(t, err)
}

func TestValidateStructIfEnabled_Enabled(t *testing.T) {
	t.Parallel()

	s := testStruct{Name: ""}
	err := ValidateStructIfEnabled(true, &s,
		validation.Field(&s.Name, validation.Required),
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Name")
}

func TestValidateStructIfEnabled_ErrFieldNotFound(t *testing.T) {
	t.Parallel()

	s := testStruct{Name: "test"}
	other := testStruct{Name: "other"}

	err := ValidateStructIfEnabled(true, &s,
		validation.Field(&other.Name, validation.Required),
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "testStruct")
	require.Contains(t, err.Error(), "pointer does not reference a field in the struct")
}

type nestedLeaf struct{ Name string }

func (l *nestedLeaf) Validate() error {
	return validation.ValidateStruct(l, validation.Field(&l.Name, validation.Required))
}

type nestedParent struct{ Leaf nestedLeaf }

func TestNestedField(t *testing.T) {
	t.Parallel()

	p := &nestedParent{}

	// The plain rule never reaches the pointer-receiver Validate.
	require.NoError(t, ValidateStruct(p, validation.Field(&p.Leaf)))

	err := ValidateStruct(p, NestedField(&p.Leaf))
	errs, ok := errors.AsType[validation.Errors](err)
	require.True(t, ok, "got %v", err)
	require.Contains(t, errs, "Leaf")

	p.Leaf.Name = "ok"
	require.NoError(t, ValidateStruct(p, NestedField(&p.Leaf)))
}

type entriesParent struct {
	Map   map[string]*nestedLeaf
	Slice []*nestedLeaf
}

func TestNotNilEntries(t *testing.T) {
	t.Parallel()

	p := &entriesParent{
		Map:   map[string]*nestedLeaf{"x": nil},
		Slice: []*nestedLeaf{{Name: "ok"}, nil},
	}

	// The plain rule calls Validate on the typed nil entry.
	require.Panics(t, func() { _ = ValidateStruct(p, validation.Field(&p.Map)) })

	err := ValidateStruct(p,
		validation.Field(&p.Map, NotNilEntries()),
		validation.Field(&p.Slice, NotNilEntries()),
	)
	errs, ok := errors.AsType[validation.Errors](err)
	require.True(t, ok, "got %v", err)
	require.Contains(t, errs, "Map")
	require.Contains(t, errs, "Slice")

	// Non-nil entries are still validated through their own Validate.
	p.Map = map[string]*nestedLeaf{"x": {}}
	p.Slice = nil
	err = ValidateStruct(p, validation.Field(&p.Map, NotNilEntries()))
	errs, ok = errors.AsType[validation.Errors](err)
	require.True(t, ok, "got %v", err)
	require.Contains(t, errs, "Map")

	p.Map["x"].Name = "ok"
	require.NoError(t, ValidateStruct(p, validation.Field(&p.Map, NotNilEntries())))
}

type eachParent struct{ Leaves []nestedLeaf }

func TestNestedEach(t *testing.T) {
	t.Parallel()

	p := &eachParent{Leaves: []nestedLeaf{{Name: "ok"}, {}}}

	// The plain rule never reaches the pointer-receiver Validate of the elements.
	require.NoError(t, ValidateStruct(p, validation.Field(&p.Leaves)))

	err := ValidateStruct(p, NestedEach(&p.Leaves))
	errs, ok := errors.AsType[validation.Errors](err)
	require.True(t, ok, "got %v", err)
	leaves, ok := errors.AsType[validation.Errors](errs["Leaves"])
	require.True(t, ok, "got %v", errs["Leaves"])
	require.Contains(t, leaves, "1")
	require.NotContains(t, leaves, "0")

	p.Leaves[1].Name = "ok"
	require.NoError(t, ValidateStruct(p, NestedEach(&p.Leaves)))

	p.Leaves = nil
	require.NoError(t, ValidateStruct(p, NestedEach(&p.Leaves)))
}
