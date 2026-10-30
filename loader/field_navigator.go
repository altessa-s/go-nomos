// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package loader

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// findFieldByPath dynamically locates a field within the configuration structure
// using the configured delimiter. This is used for nested array elements that are
// populated at runtime and not present in the static field list.
func (cf *loadState) findFieldByPath(fieldPath string) *field {
	if cf.conf == nil {
		return nil
	}

	// Split the path into components using configured section delimiter
	parts := strings.Split(fieldPath, cf.options.envSectionDelimiter)
	if len(parts) == 0 {
		return nil
	}

	// Start with the root configuration value
	w := &pathWalk{cf: cf, fieldPath: fieldPath, current: reflect.ValueOf(cf.conf), fixed: true}
	if w.current.Kind() == reflect.Pointer {
		w.current = w.current.Elem()
	}

	// A slice or map field with an env tag is addressed through the tag:
	// start from that field, then traverse its elements or entries.
	tagged, rest := cf.findTaggedCollection(fieldPath, true)
	// Resolve the path to a slice on the type first, so a path to anything
	// else allocates nothing along it.
	if !cf.slicePathResolves(tagged, rest, parts) {
		return nil
	}
	if tagged != nil {
		taggedValue := fieldByIndexAlloc(w.current, tagged.index)
		if rest == nil {
			fld := *tagged
			fld.value = taggedValue
			return &fld
		}
		w.current = taggedValue
		w.steps = tagged.pathSteps()
		w.fixed = false
		parts = rest
	}

	for i, part := range parts {
		if !w.current.IsValid() {
			return nil
		}
		fld, ok := w.step(part, i == len(parts)-1)
		if fld != nil || !ok {
			return fld
		}
	}

	return nil
}

// pathWalk is the state of findFieldByPath while it walks a path from the
// configuration root.
type pathWalk struct {
	cf        *loadState
	fieldPath string
	current   reflect.Value
	// steps records the steps taken from the configuration root.
	steps []pathStep
	// entries tracks the map entries held by value: they are navigated as
	// addressable copies that must be stored back.
	entries []mapEntry
	// fixed reports whether the path so far went through struct fields only
	// (see fieldEnvNames.matches).
	fixed bool
}

// step moves the walk through one path part: a slice index, a map key or a
// struct field. It returns the field descriptor when part, the last part when
// last is true, addresses the result; ok is false when the path does not
// resolve.
func (w *pathWalk) step(part string, last bool) (fld *field, ok bool) {
	// A number is a key of a map and an index elsewhere.
	if w.current.Kind() == reflect.Map {
		if !w.mapEntry(part) {
			return nil, false
		}
		// The last part addresses the map value itself: return a field
		// descriptor whose updates are stored back into the map.
		if last {
			return w.field(part, reflect.StructField{}), true
		}
		return nil, true
	}
	if arrayIndex, err := strconv.Atoi(part); err == nil {
		return nil, w.elem(arrayIndex)
	}
	return w.structField(part, last)
}

// elem moves to element arrayIndex of the current slice, allocating a nil
// pointer element.
func (w *pathWalk) elem(arrayIndex int) bool {
	// This is an array index - the current value should already be a slice from previous iteration
	if w.current.Kind() != reflect.Slice {
		return false
	}
	if arrayIndex < 0 || arrayIndex >= w.current.Len() {
		return false
	}

	w.fixed = false
	w.steps = withStep(w.steps, pathStep{kind: elemStep, index: arrayIndex})
	w.current = w.current.Index(arrayIndex)
	return w.derefAlloc()
}

// mapEntry moves to the entry of the current map for part, creating the map
// and the entry when missing.
func (w *pathWalk) mapEntry(part string) bool {
	m := w.current
	// The key for this part, converted to the key type
	mapKey, ok := envMapKey(m.Type(), part)
	if !ok {
		return false
	}

	// Initialize map if nil
	if m.IsNil() && m.CanSet() {
		w.cf.noteEnvGrowth(m, 0, w.steps)
		m.Set(reflect.MakeMap(m.Type()))
	}

	// Get the map value for this key
	mapValue := m.MapIndex(mapKey)
	if !mapValue.IsValid() {
		// Create new value for this key
		mapValueType := m.Type().Elem()
		newMapValue := reflect.New(mapValueType).Elem()
		// Initialize if it's a pointer
		if mapValueType.Kind() == reflect.Pointer {
			newMapValue.Set(reflect.New(mapValueType.Elem()))
		}
		m.SetMapIndex(mapKey, newMapValue)
		mapValue = m.MapIndex(mapKey)
	}
	w.fixed = false
	w.steps = withStep(w.steps, pathStep{kind: keyStep, key: mapKey.Interface()})

	// Navigate into the map value. A nil pointer entry is not
	// settable through MapIndex: allocate it and store it back.
	if mapValue.Kind() == reflect.Pointer && mapValue.IsNil() {
		newValue := reflect.New(mapValue.Type().Elem())
		m.SetMapIndex(mapKey, newValue)
		mapValue = newValue
	}
	// A value held by value is not addressable: navigate a copy and
	// store it back later.
	if mapValue.Kind() != reflect.Pointer {
		entry := reflect.New(mapValue.Type()).Elem()
		entry.Set(mapValue)
		w.entries = append(w.entries, mapEntry{m: m, key: mapKey, value: entry})
		mapValue = entry
	}
	w.current = mapValue
	if w.current.Kind() == reflect.Pointer {
		w.current = w.current.Elem()
	}
	return true
}

// structField moves to the field of the current struct named by part. When
// part is the last one and names a slice, it returns that slice's field
// descriptor.
func (w *pathWalk) structField(part string, last bool) (*field, bool) {
	// This must be a struct field access
	if w.current.Kind() != reflect.Struct {
		return nil, false
	}

	// Use findFieldIndex for proper field matching
	fieldValue, index, found := w.cf.findFieldIndex(w.current, part, w.fixed)
	if !found {
		return nil, false
	}
	w.steps = indexSteps(w.steps, index)

	// If this is the last part and it's a slice, or a pointer to one, create
	// a field descriptor for it (setArrayElements allocates a nil pointer).
	// Resolve struct field metadata via recursive walk so slices declared inside
	// `,inline` embedded structs (e.g. grpcconfig.BaseInterceptor.IgnorePatterns)
	// also work — direct iteration over NumField() would miss them.
	if last && derefType(fieldValue.Type()).Kind() == reflect.Slice {
		structField, foundField := w.cf.findStructFieldRecursive(w.current.Type(), part, w.fixed)
		if !foundField {
			// Fall back to a synthetic StructField — setSliceElements only
			// reads .value, so this is sufficient to set the slice.
			structField = reflect.StructField{Name: part, Type: fieldValue.Type()}
		}
		w.current = fieldValue
		// ensureSliceSize stores the updated entries back into their maps.
		return w.field(structField.Name, structField), true
	}

	w.current = fieldValue
	// Initialize nil pointers so we can navigate into them
	return nil, w.derefAlloc()
}

// derefAlloc dereferences a pointer current value, allocating it when nil.
// It reports false for a nil pointer that cannot be set.
func (w *pathWalk) derefAlloc() bool {
	if w.current.Kind() != reflect.Pointer {
		return true
	}
	if w.current.IsNil() {
		if !w.current.CanSet() {
			return false
		}
		w.current.Set(reflect.New(w.current.Type().Elem()))
	}
	w.current = w.current.Elem()
	return true
}

// field returns the descriptor of the current value, named name, carrying
// the steps taken and the map entries to store back.
func (w *pathWalk) field(name string, sf reflect.StructField) *field {
	return &field{
		name:     name,
		fullName: w.fieldPath,
		value:    w.current,
		field:    sf,
		tags:     make(map[string]string),
		entries:  w.entries,
		steps:    w.steps,
	}
}

// slicePathResolves reports whether findFieldByPath resolves to a slice for
// the path parts, or for the entry path rest of the tagged field when it is
// not nil, on the types alone (see indexedPathType).
func (cf *loadState) slicePathResolves(tagged *field, rest, parts []string) bool {
	t, fixed := reflect.TypeOf(cf.conf), true
	if tagged != nil {
		if rest == nil {
			return derefType(tagged.field.Type).Kind() == reflect.Slice
		}
		t, fixed, parts = tagged.field.Type, false, rest
	}
	t, _, ok := cf.indexedPathType(t, parts, fixed)
	return ok && derefType(t).Kind() == reflect.Slice
}

// fieldNavigator encapsulates the logic for navigating nested struct fields.
type fieldNavigator struct {
	cf          *loadState
	structValue reflect.Value
	pathParts   []string
	value       string
	// steps locates structValue from the configuration root; the navigator
	// extends it to record which value the environment set.
	steps []pathStep
}

// setNestedStructField sets a nested field value using path parts. steps
// locates structValue from the configuration root.
func (cf *loadState) setNestedStructField(ctx context.Context, structValue reflect.Value, pathParts []string, value string, steps []pathStep) error {
	if len(pathParts) == 0 {
		return nil
	}
	navigator := &fieldNavigator{
		cf:          cf,
		structValue: structValue,
		pathParts:   pathParts,
		value:       value,
		steps:       steps,
	}
	return navigator.navigate(ctx)
}

// navigate performs the main navigation through nested structures.
func (fn *fieldNavigator) navigate(ctx context.Context) error {
	currentValue := fn.structValue
	steps := fn.steps

	for i, part := range fn.pathParts {
		currentValue = fn.dereferencePointer(currentValue)

		if currentValue.Kind() != reflect.Struct {
			// A level below a value addresses nothing.
			if fn.cf.options.strict && isLeafType(currentValue.Type()) {
				return fmt.Errorf("%w: %s in path %s", ErrFieldNotFound, part, strings.Join(fn.pathParts, fn.cf.options.envSectionDelimiter))
			}
			return nil
		}

		// The navigator walks slice elements and map values, where env tags
		// do not apply.
		fieldValue, index, found := fn.cf.findFieldIndex(currentValue, part, false)
		if !found || !fieldValue.CanSet() {
			if fn.cf.options.strict {
				return fmt.Errorf("%w: %s in path %s", ErrFieldNotFound, part, strings.Join(fn.pathParts, fn.cf.options.envSectionDelimiter))
			}
			return nil
		}
		steps = indexSteps(steps, index)

		done, err := fn.processField(ctx, fieldValue, i, &currentValue, &steps)
		if err != nil || done {
			return err
		}
	}

	return nil
}

// dereferencePointer dereferences pointer values and initializes if nil.
func (fn *fieldNavigator) dereferencePointer(value reflect.Value) reflect.Value {
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			value.Set(reflect.New(value.Type().Elem()))
		}
		return value.Elem()
	}
	return value
}

// processField processes a field based on its type and position in the path.
// It reports done when it handled the rest of the path itself; otherwise
// navigation continues from *currentValue, located by *steps.
func (fn *fieldNavigator) processField(
	ctx context.Context,
	fieldValue reflect.Value,
	partIndex int,
	currentValue *reflect.Value,
	steps *[]pathStep,
) (bool, error) {
	// Handle map navigation
	if fieldValue.Kind() == reflect.Map && partIndex+1 < len(fn.pathParts) {
		return true, fn.handleMapField(ctx, fieldValue, partIndex, *steps)
	}

	// An element of a slice is set by the indexed pass at its own index
	// (see extractArrayFields), which grows the slice and reports a level
	// below a value element: each variable is assigned on one path, so its
	// secrets are resolved once.
	if fieldValue.Kind() == reflect.Slice && partIndex+1 < len(fn.pathParts) {
		if _, err := strconv.Atoi(fn.pathParts[partIndex+1]); err == nil {
			return true, nil
		}
	}

	// Handle last part or continue navigation
	if partIndex == len(fn.pathParts)-1 {
		return true, fn.cf.setFieldValue(ctx, fieldValue, fn.value, *steps)
	}

	*currentValue = fieldValue
	return false, nil
}

// handleMapField handles map field navigation. steps locates the map.
func (fn *fieldNavigator) handleMapField(ctx context.Context, fieldValue reflect.Value, partIndex int, steps []pathStep) error {
	mapKey := fn.pathParts[partIndex+1]
	mapKeyValue, ok := envMapKey(fieldValue.Type(), mapKey)
	if !ok {
		return nil
	}
	mapValueType := fieldValue.Type().Elem()

	// Entries of a map of maps are set as on the nested-section path, once
	// the rest of the path resolves on the type.
	if mapValueType.Kind() == reflect.Map {
		if missing, ok := fn.cf.settablePath(fieldValue.Type(), fn.pathParts[partIndex+1:], false); !ok {
			if missing != "" && fn.cf.options.strict {
				return fmt.Errorf("%w: %s in path %s", ErrFieldNotFound, missing, strings.Join(fn.pathParts, fn.cf.options.envSectionDelimiter))
			}
			return nil
		}
		return fn.cf.setNestedFieldValue(ctx, &field{value: fieldValue}, fn.pathParts[partIndex+1:], fn.value, steps, false)
	}

	if fn.isMapValuePrimitive(mapValueType) {
		return fn.handleMapWithPrimitiveValue(ctx, fieldValue, mapKeyValue, mapValueType, partIndex, steps)
	}
	if fieldValue.IsNil() {
		fn.cf.noteEnvGrowth(fieldValue, 0, steps)
		fieldValue.Set(reflect.MakeMap(fieldValue.Type()))
	}
	entrySteps := withStep(steps, pathStep{kind: keyStep, key: mapKeyValue.Interface()})
	return fn.handleMapWithStructValue(ctx, fieldValue, mapKeyValue, mapValueType, partIndex, entrySteps)
}

// isMapValuePrimitive checks if map value type is primitive.
func (fn *fieldNavigator) isMapValuePrimitive(valueType reflect.Type) bool {
	return valueType.Kind() != reflect.Struct &&
		(valueType.Kind() != reflect.Pointer || valueType.Elem().Kind() != reflect.Struct)
}

// handleMapWithPrimitiveValue handles maps with primitive values. steps
// locates the map.
func (fn *fieldNavigator) handleMapWithPrimitiveValue(ctx context.Context,
	fieldValue reflect.Value,
	mapKeyValue reflect.Value,
	mapValueType reflect.Type,
	partIndex int,
	steps []pathStep,
) error {
	if partIndex+2 != len(fn.pathParts) {
		// A collection entry is set by the indexed pass at its own index.
		if fn.cf.options.strict && isLeafType(mapValueType) {
			return fmt.Errorf("%w: %s in path %s", ErrFieldNotFound, fn.pathParts[partIndex+2], strings.Join(fn.pathParts, fn.cf.options.envSectionDelimiter))
		}
		return nil
	}

	return fn.cf.setMapEntryValue(ctx, fieldValue, mapKeyValue, fn.value, steps)
}

// handleMapWithStructValue handles maps with struct values. Map values are not
// addressable, so an existing entry is copied, updated and stored back. steps
// locates the entry.
func (fn *fieldNavigator) handleMapWithStructValue(
	ctx context.Context,
	fieldValue reflect.Value,
	mapKeyValue reflect.Value,
	mapValueType reflect.Type,
	partIndex int,
	steps []pathStep,
) error {
	structValue := reflect.New(mapValueType).Elem()
	if existingValue := fieldValue.MapIndex(mapKeyValue); existingValue.IsValid() {
		structValue.Set(existingValue)
	}

	remainingParts := fn.pathParts[partIndex+2:]
	if err := fn.cf.setNestedStructField(ctx, structValue, remainingParts, fn.value, steps); err != nil {
		return err
	}

	fieldValue.SetMapIndex(mapKeyValue, structValue)
	return nil
}

// setNestedFieldValue navigates through nested fields and sets the final
// value. steps locates fld from the configuration root; fixed reports whether
// fld is reached through struct fields only (see fieldEnvNames.matches).
func (cf *loadState) setNestedFieldValue(ctx context.Context, fld *field, pathParts []string, value string, steps []pathStep, fixed bool) error {
	if len(pathParts) == 0 {
		return nil
	}

	currentValue := fld.value

	// Navigate through the path
	for i, part := range pathParts {
		currentValue = cf.dereferencePointer(currentValue)

		if currentValue.Kind() == reflect.Map {
			return cf.setNestedMapEntry(ctx, currentValue, pathParts[i:], value, steps)
		}

		if currentValue.Kind() != reflect.Struct {
			return nil // Can't navigate further
		}

		// Find the field in the current struct (including inline structs)
		fieldValue, index, found := cf.findFieldIndex(currentValue, part, fixed)
		if !found {
			if cf.options.strict {
				return fmt.Errorf("%w: %s", ErrFieldNotFound, part)
			}
			return nil
		}
		steps = indexSteps(steps, index)

		if i == len(pathParts)-1 {
			// This is the final field, set its value
			if fieldValue.CanSet() {
				return cf.setFieldValue(ctx, fieldValue, value, steps)
			}
			return nil
		}
		// Continue navigation
		currentValue = fieldValue
	}

	return nil
}

// setNestedMapEntry sets the entry of map m that pathParts names: its first
// part is the map key, the rest a path into the entry. steps locates m.
func (cf *loadState) setNestedMapEntry(ctx context.Context, m reflect.Value, pathParts []string, value string, steps []pathStep) error {
	// Get map key, converted to the key type; a map whose keys a
	// variable name cannot spell is left alone.
	mapKeyValue, ok := envMapKey(m.Type(), pathParts[0])
	if !ok {
		return nil
	}

	// Check if this is the last part or if we need to navigate deeper
	if len(pathParts) == 1 {
		return cf.setMapEntryValue(ctx, m, mapKeyValue, value, steps)
	}

	// Initialize map if nil
	if m.IsNil() {
		cf.noteEnvGrowth(m, 0, steps)
		m.Set(reflect.MakeMap(m.Type()))
	}

	mapValueType := m.Type().Elem()
	entrySteps := withStep(steps, pathStep{kind: keyStep, key: mapKeyValue.Interface()})

	// Not the last part - need to navigate into map value
	// Check if map value type is a struct or a map
	if !isNestedEntryType(mapValueType) {
		// Map value is not a struct or a map, cannot navigate further
		return nil
	}

	// Map values are not addressable: update a copy of the
	// existing entry, or a new one, and store it back.
	structValue := reflect.New(mapValueType).Elem()
	if existingValue := m.MapIndex(mapKeyValue); existingValue.IsValid() {
		structValue.Set(existingValue)
	}

	// Recursively set nested field in the struct, or the entry
	// of the inner map, through a temporary field descriptor
	tempField := &field{
		value: structValue,
	}
	if err := cf.setNestedFieldValue(ctx, tempField, pathParts[1:], value, entrySteps, false); err != nil {
		return err
	}

	// Set the updated struct back to the map
	m.SetMapIndex(mapKeyValue, tempField.value)
	return nil
}

// setMapEntryValue sets the entry of map m at key to value. A value that
// assigns nothing (an empty one for a number, a bool, a slice or a map) leaves
// an existing entry, and a nil map, untouched, and is not recorded as set by
// the environment. The value is parsed into the type below the pointers of a
// pointer-valued map, so allocating them alone does not count as assigning;
// they are allocated only for a value that assigns. steps locates m.
func (cf *loadState) setMapEntryValue(ctx context.Context, m, key reflect.Value, value string, steps []pathStep) error {
	leaf := reflect.New(derefType(m.Type().Elem())).Elem()
	assigned, err := cf.setEnvValue(ctx, leaf, value)
	if err != nil || !assigned {
		return err
	}
	newValue := leaf
	for newValue.Type() != m.Type().Elem() {
		ptr := reflect.New(newValue.Type())
		ptr.Elem().Set(newValue)
		newValue = ptr
	}
	if m.IsNil() {
		cf.noteEnvGrowth(m, 0, steps)
		m.Set(reflect.MakeMap(m.Type()))
	}
	m.SetMapIndex(key, newValue)
	cf.markEnv(withStep(steps, pathStep{kind: keyStep, key: key.Interface()}))
	return nil
}

// legacyIndexSeparator separates a field name from its legacy index suffix
// (NAME__0).
const legacyIndexSeparator = "__"

// cutIndexSuffix splits a variable name level with a legacy index suffix,
// "BACK_OFF__0", into the field name "BACK_OFF" and the index "0". ok is false
// when the level carries no numeric suffix.
func cutIndexSuffix(level string) (base, index string, ok bool) {
	base, index, found := strings.CutLast(level, legacyIndexSeparator)
	if !found || index == "" {
		return level, "", false
	}
	if _, err := strconv.Atoi(index); err != nil {
		return level, "", false
	}
	return base, index, true
}

// findFieldIndex searches for a field by name in a struct, including
// inline/anonymous fields, and returns it with its struct index path. fixed
// is passed to fieldEnvNames.matches.
func (cf *loadState) findFieldIndex(structValue reflect.Value, fieldName string, fixed bool) (reflect.Value, []int, bool) {
	// Resolve the index path on the type first, so a lookup that fails
	// allocates nothing and recursive embeddings, even mutual ones, are
	// searched once; then allocate the embedded pointers along that path only.
	sf, _, ok := cf.findStructFieldIn(structValue.Type(), fieldName, fixed, nil)
	if !ok {
		return reflect.Value{}, nil, false
	}
	v := structValue
	for _, i := range sf.Index {
		v = cf.dereferencePointer(v).Field(i)
	}
	return v, sf.Index, true
}

// findStructFieldRecursive searches a struct type for a field matching name:
// direct fields first, then `,inline` embedded structs, depth first. Returns
// the matching reflect.StructField (preserving its tags) so callers can use
// it as field metadata even when the field is declared inside an embedded
// struct; its Index is the index path from structType.
func (cf *loadState) findStructFieldRecursive(structType reflect.Type, fieldName string, fixed bool) (reflect.StructField, bool) {
	sf, _, ok := cf.findStructFieldIn(structType, fieldName, fixed, nil)
	return sf, ok
}

// findStructFieldIn is findStructFieldRecursive below the embedding path of
// struct types path: an embedded type already on it is not searched again,
// so a recursive embedding, even a mutual one, ends the search. It also
// returns the environment naming of the field found. Unexported fields, which
// cannot be set, are never found, nor are the fields of an unexported embedded
// pointer, which cannot be allocated.
func (cf *loadState) findStructFieldIn(
	structType reflect.Type,
	fieldName string,
	fixed bool,
	path []reflect.Type,
) (reflect.StructField, *fieldEnvNames, bool) {
	names := cf.structEnvNames(structType)
	for i := range names {
		if !names[i].anonymous && names[i].matches(fieldName, fixed) && structType.Field(i).IsExported() {
			return structType.Field(i), &names[i], true
		}
	}
	extended := false
	for i := range names {
		if !names[i].anonymous {
			continue
		}
		f := structType.Field(i)
		ft := derefType(f.Type)
		if ft.Kind() != reflect.Struct || ft == structType || slices.Contains(path, ft) ||
			!f.IsExported() && f.Type.Kind() == reflect.Pointer {
			continue
		}
		if !extended {
			path, extended = append(slices.Clip(path), structType), true
		}
		if sf, sfNames, ok := cf.findStructFieldIn(ft, fieldName, fixed, path); ok {
			sf.Index = append(slices.Clip(f.Index), sf.Index...)
			return sf, sfNames, true
		}
	}
	return reflect.StructField{}, nil, false
}

// dereferencePointer dereferences a pointer value, initializing it if nil.
func (cf *loadState) dereferencePointer(value reflect.Value) reflect.Value {
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			value.Set(reflect.New(value.Type().Elem()))
		}
		return value.Elem()
	}
	return value
}
