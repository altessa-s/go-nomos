// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package loader

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// withheldError replaces an error Load returns after it resolved a secret.
// Any message may show a secret in some form — raw, quoted, escaped, hex or
// as part of a collection parsed from it — so none is kept: the message names
// only the phase and, when structured errors provide them, the field paths
// or variables involved. errors.Is still matches every error of the original
// tree (sentinels included); errors.As is not supported, as a typed error
// would carry the original text. The original error is held privately for
// errors.Is only: it is never unwrapped nor formatted.
type withheldError struct {
	msg      string
	original error
}

// Error returns the value-free message.
func (e *withheldError) Error() string { return e.msg }

// Is reports whether the original error tree matches target, descending
// also into validation error maps, which do not unwrap.
func (e *withheldError) Is(target error) bool { return treeIs(e.original, target) }

// treeIs is errors.Is over err's tree, maps of errors included.
func treeIs(err, target error) bool {
	if err == nil || target == nil {
		return err == target
	}
	if reflect.TypeOf(target).Comparable() && reflect.TypeOf(err).Comparable() && err == target {
		return true
	}
	if x, ok := err.(interface{ Is(error) bool }); ok && x.Is(target) {
		return true
	}
	for _, c := range children(err) {
		if treeIs(c, target) {
			return true
		}
	}
	return false
}

// children returns the errors err wraps directly: through Unwrap, or as the
// values of a map of errors (ozzo-validation's Errors).
func children(err error) []error {
	switch u := err.(type) {
	case interface{ Unwrap() []error }:
		return u.Unwrap()
	case interface{ Unwrap() error }:
		if c := u.Unwrap(); c != nil {
			return []error{c}
		}
		return nil
	}
	v := reflect.ValueOf(err)
	if v.Kind() != reflect.Map || !v.Type().Elem().Implements(errorType) {
		return nil
	}
	out := make([]error, 0, v.Len())
	for it := v.MapRange(); it.Next(); {
		if c, ok := reflect.TypeAssert[error](it.Value()); ok && c != nil {
			out = append(out, c)
		}
	}
	return out
}

// withholdError returns err unchanged when no secret was resolved during the
// load or an earlier load of the loader, and a withheldError otherwise. Its message is built from static
// text, the phase, and locations derived from the configuration's type only
// (see errorPaths): never from the text or the keys of the original error.
func (cf *loadState) withholdError(err error) error {
	if err == nil || !cf.secretsResolved {
		return err
	}
	where := strings.Join(cf.errorPaths(err), ", ")
	if where == "" {
		where = "configuration"
	}
	return &withheldError{
		msg:      fmt.Sprintf("%s failed for %s (secrets were resolved; details withheld)", loadPhase(err), where),
		original: err,
	}
}

// loadPhase names the stage of Load that failed.
func loadPhase(err error) string {
	switch {
	case errors.Is(err, ErrDecode):
		return "decode"
	case errors.Is(err, ErrBindEnv):
		return "environment"
	case errors.Is(err, ErrBindDefaults):
		return "defaults"
	case errors.Is(err, ErrValidation):
		return "validation"
	default:
		return "load"
	}
}

// wildcard replaces a location segment that comes from data, not the schema.
const wildcard = "*"

// errorPaths returns, sorted, the locations the structured errors of err's
// tree name, reduced to the configuration's schema: the failing environment
// variables and the field paths of validation error maps (ozzo-validation's
// Errors). A segment is kept only when it names a field of the struct type
// at that position; an element of a map, slice or array, and any key that
// names no field, becomes "*", and nothing below it is named from the error.
func (cf *loadState) errorPaths(err error) []string {
	root := indirectType(cf.confType)
	var paths []string
	var walk func(err error, path []string, t reflect.Type)
	walk = func(err error, path []string, t reflect.Type) {
		if err == nil {
			return
		}
		if ev, ok := err.(*envVarErr); ok {
			paths = append(paths, cf.staticEnvName(ev.key))
		}
		if v := reflect.ValueOf(err); v.Kind() == reflect.Map && v.Type().Key().Kind() == reflect.String &&
			v.Type().Elem().Implements(errorType) {
			for it := v.MapRange(); it.Next(); {
				segment, childType := staticSegment(t, it.Key().String())
				childPath := append(slices.Clip(path), segment)
				child, _ := reflect.TypeAssert[error](it.Value())
				before := len(paths)
				walk(child, childPath, childType)
				if len(paths) == before {
					paths = append(paths, strings.Join(childPath, "."))
				}
			}
			return
		}
		switch u := err.(type) {
		case interface{ Unwrap() []error }:
			for _, c := range u.Unwrap() {
				walk(c, path, t)
			}
		case interface{ Unwrap() error }:
			walk(u.Unwrap(), path, t)
		}
	}
	walk(err, nil, root)
	slices.Sort(paths)
	return slices.Compact(paths)
}

// staticSegment maps one key of a validation error map at type t to a
// location segment and the type below it: the key itself when it names a
// field of struct t (by its Go name or its json or yaml tag), else "*" with
// the element type of a map, slice or array t, or no type at all.
func staticSegment(t reflect.Type, key string) (string, reflect.Type) {
	if t == nil {
		return wildcard, nil
	}
	t = derefType(t)
	switch t.Kind() {
	case reflect.Struct:
		for sf := range t.Fields() {
			if !sf.IsExported() {
				continue
			}
			if key == sf.Name || key == tagName(sf, "json") || key == tagName(sf, "yaml") {
				return key, sf.Type
			}
		}
		return wildcard, nil
	case reflect.Map, reflect.Slice, reflect.Array:
		return wildcard, t.Elem()
	default:
		return wildcard, nil
	}
}

// tagName returns the name a struct tag gives a field, or "".
func tagName(sf reflect.StructField, key string) string {
	name, _, _ := strings.Cut(sf.Tag.Get(key), ",")
	if name == "-" {
		return ""
	}
	return name
}

// staticEnvName returns the environment variable key (without the prefix)
// reduced to the configuration's schema, with the prefix: a level is kept
// while it names a field of the struct type at that position; the key of a
// map entry or the index of an element becomes "*", and so does every level
// after one that names no field.
func (cf *loadState) staticEnvName(key string) string {
	t := indirectType(cf.confType)
	levels := strings.Split(key, cf.options.envSectionDelimiter)
	out := make([]string, 0, len(levels))
	for _, level := range levels {
		if t == nil {
			out = append(out, wildcard)
			continue
		}
		t = derefType(t)
		switch t.Kind() {
		case reflect.Struct:
			next := reflect.Type(nil)
			for i, names := range cf.structEnvNames(t) {
				if slices.Contains(names.levels, level) {
					next = t.Field(i).Type
					break
				}
			}
			if next == nil {
				out = append(out, wildcard)
			} else {
				out = append(out, level)
			}
			t = next
		case reflect.Map, reflect.Slice, reflect.Array:
			out = append(out, wildcard)
			t = t.Elem()
		default:
			out = append(out, wildcard)
			t = nil
		}
	}
	return cf.options.envPrefix + strings.Join(out, cf.options.envSectionDelimiter)
}

// errorType is the error interface type.
var errorType = reflect.TypeFor[error]()

// envVarErr is the failure of one environment variable.
type envVarErr struct {
	// prefix is the configured variable prefix and key the rest of the
	// name.
	prefix, key string
	err         error
}

// Error names the variable and its failure.
func (e *envVarErr) Error() string {
	return "environment variable " + e.prefix + e.key + ": " + e.err.Error()
}

// Unwrap returns the failure.
func (e *envVarErr) Unwrap() error { return e.err }
