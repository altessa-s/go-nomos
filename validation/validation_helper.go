// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package validationconfig

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

// ValidateStruct validates a struct and enhances ozzo-validation's internal errors
// (ErrFieldNotFound, ErrFieldPointer) with the struct type name and actionable hints.
//
// Use this instead of validation.ValidateStruct to get clearer error messages when
// a validation.Field pointer does not reference a field in the target struct.
//
// Example:
//
//	func (c *Config) Validate() error {
//	    return validationconfig.ValidateStruct(c,
//	        validation.Field(&c.Field1, validation.Required),
//	    )
//	}
func ValidateStruct(structPtr any, fields ...*validation.FieldRules) error {
	err := validation.ValidateStruct(structPtr, fields...)
	if err == nil {
		return nil
	}

	return enhanceInternalError(err, structPtr)
}

// ValidateStructIfEnabled validates a struct only if the enabled flag is true.
// This helper reduces boilerplate in configuration structs that have an "Enabled" field.
//
// Example:
//
//	func (c *Config) Validate() error {
//	    return ValidateStructIfEnabled(c.Enabled, c,
//	        validation.Field(&c.Field1, validation.Required),
//	    )
//	}
func ValidateStructIfEnabled(enabled bool, structPtr any, fields ...*validation.FieldRules) error {
	if !enabled {
		return nil
	}

	return ValidateStruct(structPtr, fields...)
}

// NestedField returns the field rules for a struct field held by value whose
// Validate method has a pointer receiver.
//
// validation.Field(&c.Sub) does not validate such a field: ozzo-validation
// checks a copy of the value for [validation.Validatable], and the copy does
// not carry the pointer method set, so Sub.Validate is silently skipped.
// NestedField calls it on the field itself. Fields held by pointer, or whose
// Validate has a value receiver, need no helper.
//
// Example:
//
//	func (c *Config) Validate() error {
//	    return validationconfig.ValidateStruct(c,
//	        validationconfig.NestedField(&c.Storage),
//	    )
//	}
func NestedField[T any, P interface {
	*T
	validation.Validatable
}](field P) *validation.FieldRules {
	return validation.Field(field, validation.By(func(any) error { return field.Validate() }))
}

// NestedEach returns the field rules for a slice whose elements are held by
// value and whose Validate method has a pointer receiver.
//
// validation.Field(&c.Items) does not validate such elements: ozzo-validation
// descends into a slice only when the element type itself implements
// [validation.Validatable], which a value type with a pointer-receiver
// Validate does not, so every element is silently skipped. NestedEach calls
// Validate on each element in place and keys the errors by index.
//
// Example:
//
//	func (c *Config) Validate() error {
//	    return validationconfig.ValidateStruct(c,
//	        validationconfig.NestedEach(&c.Rules),
//	    )
//	}
func NestedEach[S ~[]T, T any, P interface {
	*T
	validation.Validatable
}](field *S) *validation.FieldRules {
	return validation.Field(field, validation.By(func(any) error {
		errs := validation.Errors{}
		for i := range *field {
			if err := P(&(*field)[i]).Validate(); err != nil {
				errs[strconv.Itoa(i)] = err
			}
		}
		return errs.Filter()
	}))
}

// NotNilEntries returns a rule rejecting nil entries of a map or slice of
// pointers, such as an empty YAML entry (filters: {x: }).
//
// validation.Field(&c.Items) descends into a collection whose element type
// implements [validation.Validatable] and calls Validate on every entry,
// typed nil pointers included, so a Validate that reads its receiver panics.
// Listing NotNilEntries first reports such entries as errors and stops the
// descent.
//
// Example:
//
//	func (c *Config) Validate() error {
//	    return validationconfig.ValidateStruct(c,
//	        validation.Field(&c.Items, validationconfig.NotNilEntries()),
//	    )
//	}
func NotNilEntries() validation.Rule {
	return validation.Each(validation.NotNil)
}

// enhanceInternalError checks if the error is an ozzo-validation InternalError wrapping
// ErrFieldNotFound or ErrFieldPointer, and replaces it with a more descriptive message.
func enhanceInternalError(err error, structPtr any) error {
	ie, ok := err.(validation.InternalError) //nolint:errorlint // ozzo-validation uses type assertion
	if !ok || ie.InternalError() == nil {
		return err
	}

	inner := ie.InternalError()
	typeName := structTypeName(structPtr)

	if fieldNotFound, ok := errors.AsType[validation.ErrFieldNotFound](inner); ok {
		return fmt.Errorf("validation of %s: field rule #%d: "+
			"pointer does not reference a field in the struct (use &s.FieldName, not &s)",
			typeName, int(fieldNotFound))
	}

	if fieldPointer, ok := errors.AsType[validation.ErrFieldPointer](inner); ok {
		return fmt.Errorf("validation of %s: field rule #%d: "+
			"field must be specified as a pointer (use &s.FieldName)",
			typeName, int(fieldPointer))
	}

	return err
}

// structTypeName returns a human-readable type name for the struct pointer.
func structTypeName(structPtr any) string {
	t := reflect.TypeOf(structPtr)
	if t == nil {
		return "<nil>"
	}
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	return t.Name()
}
