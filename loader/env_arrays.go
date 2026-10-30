// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package loader

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"

	coreslices "github.com/altessa-s/go-atlas-core/collections/slices"
)

// loadArrayEnvs handles array element environment variables using configured
// delimiter, among the variables keys names.
func (cf *loadState) loadArrayEnvs(ctx context.Context, envs map[string]string, keys []string) error {
	// Group environment variables by array pattern
	arrayEnvs := cf.parseArrayEnvs(envs, keys)

	// Sort array paths by depth (number of delimiters) to process parent arrays first
	arrayPaths := make([]string, 0, len(arrayEnvs))
	for fieldPath := range arrayEnvs {
		arrayPaths = append(arrayPaths, fieldPath)
	}

	// Sort by depth (fewer delimiters = shallower = process first), then by
	// name, so the order does not depend on map iteration.
	delimiter := cf.options.envSectionDelimiter
	slices.SortFunc(arrayPaths, func(a, b string) int {
		return cmp.Or(cmp.Compare(strings.Count(a, delimiter), strings.Count(b, delimiter)), strings.Compare(a, b))
	})

	// Process each array pattern in order of depth
	for _, fieldPath := range arrayPaths {
		if err := cf.setArrayElements(ctx, fieldPath, arrayEnvs[fieldPath]); err != nil {
			return err
		}
	}

	return nil
}

// parseArrayEnvs parses environment variables to find array patterns.
// Returns a map where key is the field path and value is a map
// of element paths to their values using configured section delimiter. It
// reads the variables keys names.
func (cf *loadState) parseArrayEnvs(envs map[string]string, keys []string) map[string]map[string]string {
	arrayEnvs := make(map[string]map[string]string)

	for _, envKey := range keys {
		cf.processArrayEnvKey(envKey, envs[envKey], arrayEnvs)
	}

	return arrayEnvs
}

// processArrayEnvKey processes a single environment key for array patterns.
func (cf *loadState) processArrayEnvKey(envKey, envValue string, result map[string]map[string]string) {
	parts := cf.parseArrayEnvKey(envKey)
	if len(parts) < minEnvKeyParts {
		return
	}

	arrayPositions := findArrayIndices(parts)
	if len(arrayPositions) == 0 {
		return
	}

	// A name that addresses nothing settable is left out, so its candidate
	// slices allocate nothing along its path.
	if !cf.indexedKeyResolves(envKey, parts) {
		return
	}

	cf.extractArrayFields(parts, arrayPositions, envValue, result)
}

// findArrayIndices locates all array index positions in parts.
func findArrayIndices(parts []string) []int {
	positions := make([]int, 0)

	for i := 1; i < len(parts); i++ {
		positions = coreslices.AppendIf(positions, isArrayIndex(parts[i]), i)
	}

	return positions
}

// isArrayIndex checks if a part represents an array index: a number. An
// index suffix (NAME__0, NAME[0]) is a level of its own once the name is
// normalized (see normalizeEnvName).
func isArrayIndex(part string) bool {
	_, err := strconv.Atoi(part)
	return err == nil
}

// extractArrayFields extracts field paths from array patterns.
func (cf *loadState) extractArrayFields(parts []string, positions []int, envValue string, result map[string]map[string]string) {
	delimiter := cf.options.envSectionDelimiter

	// Pre-compute joined paths to avoid repeated string allocations
	pathPrefixCache := make(map[int]string, len(parts))
	pathSuffixCache := make(map[int]string, len(parts))

	for _, position := range positions {
		if position == 0 {
			continue
		}

		arrayField, elementPath := buildArrayPaths(parts, position, delimiter, pathPrefixCache, pathSuffixCache)

		if result[arrayField] == nil {
			result[arrayField] = make(map[string]string)
		}
		result[arrayField][elementPath] = envValue
	}
}

// buildArrayPaths builds the array field path and element path for the
// numeric index at position.
func buildArrayPaths(
	parts []string,
	position int,
	delimiter string,
	prefixCache, suffixCache map[int]string,
) (arrayField, elementPath string) {
	prefix := getCachedPrefix(parts, position, delimiter, prefixCache)
	suffix := getCachedSuffix(parts, position, delimiter, suffixCache)

	return prefix, suffix
}

// getCachedPrefix returns the cached prefix or computes and caches it.
func getCachedPrefix(parts []string, position int, delimiter string, cache map[int]string) string {
	if prefix, ok := cache[position]; ok {
		return prefix
	}

	prefix := strings.Join(parts[0:position], delimiter)
	cache[position] = prefix
	return prefix
}

// getCachedSuffix returns the cached suffix or computes and caches it.
func getCachedSuffix(parts []string, position int, delimiter string, cache map[int]string) string {
	if suffix, ok := cache[position]; ok {
		return suffix
	}

	suffix := strings.Join(parts[position:], delimiter)
	cache[position] = suffix
	return suffix
}

// parseArrayEnvKey parses environment variable key to check for array pattern.
// Returns parts if it matches array pattern using configured section delimiter.
func (cf *loadState) parseArrayEnvKey(envKey string) []string {
	parts := strings.Split(envKey, cf.options.envSectionDelimiter)
	if len(parts) < minEnvKeyParts {
		return nil
	}

	// Check if any part is a number (array index)
	for _, part := range parts[1:] {
		if _, err := strconv.Atoi(part); err == nil {
			return parts
		}
	}

	return nil
}

// setArrayElements sets values for array elements based on parsed environment variables.
func (cf *loadState) setArrayElements(ctx context.Context, fieldPath string, elements map[string]string) error {
	// Try to find it dynamically in the structure (this will ensure parent structs are initialized)
	arrayField := cf.findFieldByPath(fieldPath)

	// If not found dynamically, try to find in existing static fields
	if arrayField == nil {
		cf.fields.each(func(fld *field) bool {
			if cf.matchesArrayField(fld, fieldPath) {
				arrayField = fld
				return false // Stop searching
			}
			return true
		})
	}

	if arrayField == nil {
		// Do not error here even in strict mode: parseArrayEnvs processes ALL env vars
		// that match array patterns, including system env vars that are not targeting
		// the config struct.
		return nil
	}

	// A field of the field list under a nil pointer section has no value
	// yet: allocate the sections along its index path, as the plain-field
	// pass does.
	if !arrayField.value.IsValid() && arrayField.index != nil {
		if derefType(arrayField.field.Type).Kind() != reflect.Slice {
			return nil
		}
		allocated := *arrayField
		allocated.value = fieldByIndexAlloc(reflect.ValueOf(cf.conf).Elem(), arrayField.index)
		arrayField = &allocated
	}

	// Check if the field value is valid and settable: an unexported field
	// is not.
	if !arrayField.value.IsValid() || !arrayField.value.CanSet() {
		return nil
	}

	// A pointer to a slice is allocated when nil, then its slice is set.
	if v := arrayField.value; v.Kind() == reflect.Pointer && v.Type().Elem().Kind() == reflect.Slice {
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		deref := *arrayField
		deref.value = v.Elem()
		arrayField = &deref
	}

	// Check if this is actually a slice/array field
	fieldType := arrayField.value.Type()
	if fieldType.Kind() != reflect.Slice {
		return nil
	}

	return cf.setSliceElements(ctx, arrayField, fieldPath, elements)
}

// matchesArrayField checks if a field matches the array field path from environment variable.
func (cf *loadState) matchesArrayField(fld *field, fieldPath string) bool {
	return slices.Contains(cf.envFieldNames(fld), fieldPath)
}

// setSliceElements sets individual elements in a slice based on environment
// variables. slicePath names the slice in their names.
func (cf *loadState) setSliceElements(ctx context.Context, arrayField *field, slicePath string, elements map[string]string) error {
	sliceType := arrayField.value.Type()
	elementType := sliceType.Elem()

	// Check if this is a slice of primitives or a slice of structs
	isPrimitiveSlice := elementType.Kind() != reflect.Struct &&
		(elementType.Kind() != reflect.Pointer || elementType.Elem().Kind() != reflect.Struct)

	if isPrimitiveSlice {
		// Handle slice of primitives (e.g., []string, []int)
		return cf.setPrimitiveSliceElements(ctx, arrayField, slicePath, elements)
	}

	// Handle slice of structs
	return cf.setStructSliceElements(ctx, arrayField, slicePath, elements)
}

// setPrimitiveSliceElements sets elements in a slice of primitive types.
func (cf *loadState) setPrimitiveSliceElements(ctx context.Context, arrayField *field, slicePath string, elements map[string]string) error {
	// Group elements by index (should be just index -> value for primitives)
	indexedValues := make(map[int]string)
	maxIndex := -1

	for elementPath, value := range elements {
		index, err := strconv.Atoi(elementPath)
		if err != nil {
			// An element path with more levels (SLICE__0__NAME) sets
			// nothing here: a collection element is set at its own index, a
			// level below a value element addresses nothing.
			if rest, deeper := cf.levelBelowLeafElement(arrayField, elementPath); deeper && cf.options.strict {
				return fmt.Errorf("%w: %s in path %s", ErrFieldNotFound, rest, slicePath+cf.options.envSectionDelimiter+elementPath)
			}
			continue
		}

		if err := cf.checkEnvIndex(arrayField.fullName, index, arrayField.value.Len()); err != nil {
			return err
		}
		if index > maxIndex {
			maxIndex = index
		}

		indexedValues[index] = value
	}

	if maxIndex < 0 {
		return nil
	}

	// Ensure slice is large enough
	if err := cf.ensureSliceSize(arrayField, maxIndex+1); err != nil {
		return err
	}

	// Set values for each element
	steps := arrayField.pathSteps()
	for index, value := range indexedValues {
		elementValue := arrayField.value.Index(index)
		if err := cf.setFieldValue(ctx, elementValue, value, withStep(steps, pathStep{kind: elemStep, index: index})); err != nil {
			return cf.envVarError(slicePath+cf.options.envSectionDelimiter+strconv.Itoa(index), err)
		}
	}

	return nil
}

// levelBelowLeafElement reports whether elementPath names a level below an
// element of the slice arrayField whose elements are set whole (see
// isLeafType), and returns that level.
func (cf *loadState) levelBelowLeafElement(arrayField *field, elementPath string) (string, bool) {
	index, rest, found := strings.Cut(elementPath, cf.options.envSectionDelimiter)
	if !found || rest == "" || !isLeafType(arrayField.value.Type().Elem()) {
		return "", false
	}
	if _, err := strconv.Atoi(index); err != nil {
		return "", false
	}
	level, _, _ := strings.Cut(rest, cf.options.envSectionDelimiter)
	return level, true
}

// setStructSliceElements sets elements in a slice of struct types.
func (cf *loadState) setStructSliceElements(ctx context.Context, arrayField *field, slicePath string, elements map[string]string) error {
	// Group elements by index
	indexedElements := make(map[int]map[string]string)
	maxIndex := -1
	delimiter := cf.options.envSectionDelimiter

	for elementPath, value := range elements {
		parts := strings.Split(elementPath, delimiter)
		if len(parts) < minEnvKeyParts {
			continue
		}

		index, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}
		if err := cf.checkEnvIndex(arrayField.fullName, index, arrayField.value.Len()); err != nil {
			return err
		}

		if index > maxIndex {
			maxIndex = index
		}

		fieldPath := strings.Join(parts[1:], delimiter)

		if indexedElements[index] == nil {
			indexedElements[index] = make(map[string]string)
		}
		indexedElements[index][fieldPath] = value
	}

	if maxIndex < 0 {
		return nil
	}

	// Ensure slice is large enough
	if err := cf.ensureSliceSize(arrayField, maxIndex+1); err != nil {
		return err
	}

	// Set values for each element
	steps := arrayField.pathSteps()
	for index, fieldValues := range indexedElements {
		elementValue := arrayField.value.Index(index)
		elementSteps := withStep(steps, pathStep{kind: elemStep, index: index})
		elementName := slicePath + delimiter + strconv.Itoa(index)
		if err := cf.setStructFields(ctx, elementValue, elementName, fieldValues, elementSteps); err != nil {
			return err
		}
	}

	return nil
}

// checkEnvIndex validates a slice index read from an environment variable
// name for the slice name of length n: the index must not be negative, and
// an index past the slice must stay below the maxEnvSliceLen limit, so the
// slice never grows past it (and index+1 cannot overflow).
func (cf *loadState) checkEnvIndex(name string, index, n int) error {
	if index >= 0 && (index < n || index < cf.options.maxEnvSliceLen) {
		return nil
	}
	return fmt.Errorf("%w: index %d of %s (length %d, limit %d)", ErrEnvIndexOutOfRange, index, name, n, cf.options.maxEnvSliceLen)
}

// ensureSliceSize ensures that a field's slice has at least the specified size.
// It delegates to ensureSliceSizeForValue and stores map entries back if needed.
func (cf *loadState) ensureSliceSize(arrayField *field, minSize int) error {
	cf.noteEnvGrowth(arrayField.value, minSize, arrayField.pathSteps())

	// Use the base function to resize the slice
	if err := ensureSliceSizeForValue(arrayField.value, minSize); err != nil {
		return err
	}

	// A slice inside map entries held by value was resized in copies of
	// them: store the copies back. Elements set afterwards share the backing
	// array of the stored slice.
	arrayField.storeEntries()

	return nil
}

// setStructFields sets fields of a struct based on a map of field names to values.
// Supports nested field paths using configured delimiter. name names
// structValue in the variable names; steps locates it from the configuration
// root. The paths are applied in name order.
func (cf *loadState) setStructFields(ctx context.Context, structValue reflect.Value, name string, fieldValues map[string]string, steps []pathStep) error {
	if structValue.Kind() == reflect.Pointer {
		if structValue.IsNil() {
			structValue.Set(reflect.New(structValue.Type().Elem()))
		}
		structValue = structValue.Elem()
	}

	if structValue.Kind() != reflect.Struct {
		return nil
	}

	delimiter := cf.options.envSectionDelimiter
	for _, fieldPath := range slices.Sorted(maps.Keys(fieldValues)) {
		parts := strings.Split(fieldPath, delimiter)
		if err := cf.setNestedStructField(ctx, structValue, parts, fieldValues[fieldPath], steps); err != nil {
			return cf.envVarError(name+delimiter+fieldPath, err)
		}
	}

	return nil
}

// ensureSliceSizeForValue ensures a slice has at least the specified size.
func ensureSliceSizeForValue(sliceValue reflect.Value, minSize int) error {
	currentLen := sliceValue.Len()
	if currentLen >= minSize {
		return nil
	}

	// Create a new slice with the required size
	sliceType := sliceValue.Type()
	elementType := sliceType.Elem()

	newSlice := reflect.MakeSlice(sliceType, minSize, minSize)

	// Copy existing elements
	for i := range currentLen {
		newSlice.Index(i).Set(sliceValue.Index(i))
	}

	// Initialize new elements
	for i := currentLen; i < minSize; i++ {
		newElement := reflect.New(elementType).Elem()
		if elementType.Kind() == reflect.Struct {
			newSlice.Index(i).Set(newElement)
		} else {
			newSlice.Index(i).Set(reflect.Zero(elementType))
		}
	}

	sliceValue.Set(newSlice)
	return nil
}
