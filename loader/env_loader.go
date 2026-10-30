// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package loader

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"

	coreslices "github.com/altessa-s/go-atlas-core/collections/slices"
	loadersecrets "github.com/altessa-s/go-nomos/loader/secrets"
)

// loadEnvs loads configuration values from environment variables.
// With a prefix configured, only the variables carrying it are read, and the
// prefix is stripped before their names are matched against the fields.
// It maps struct field names to environment variable names using the configured delimiter.
// Supports array element access using the configured delimiter.
// If a secrets manager is configured, the $__secret{namespace:key} placeholders
// of the values it assigns are resolved (see setEnvValue).
func (cf *loadState) loadEnvs(ctx context.Context) error {
	if cf.options.skipEnv {
		return nil
	}

	var err error

	// The fields an env tag names, scanned for every variable name below.
	cf.envTagged = cf.collectEnvTagged()
	defer func() { cf.envTagged = nil }()

	// $VAR references and secret placeholders are resolved per assigned
	// value (setEnvValue), after precedence picked the variables, not here.
	envs := cf.normalizedEnvs(envValues(cf.options.envPrefix))

	// First, handle regular field mappings. A field under a nil pointer
	// struct is reached by allocating the structs along its path, only when
	// the environment sets it. When several names of a field are set, the
	// struct tag spelling wins (see preferredEnvName); every name of the field
	// is consumed, so the other passes do not set it again.
	root := reflect.ValueOf(cf.conf).Elem()
	var consumed map[string]struct{}
	cf.fields.each(func(fld *field) bool {
		names := cf.envFieldNames(fld)
		envName, ok := cf.preferredEnvName(names, envs)
		if !ok {
			return true
		}
		value := fld.value
		if !value.IsValid() {
			value = fieldByIndexAlloc(root, fld.index)
		}
		err = cf.envVarError(envName, cf.setFieldValue(ctx, value, envs[envName], fld.pathSteps()))
		if consumed == nil {
			consumed = make(map[string]struct{})
		}
		for _, name := range names {
			consumed[name] = struct{}{}
		}

		return err == nil
	})

	if err != nil {
		return err
	}

	// The other passes read only the variables left whose names contain the
	// section delimiter, in name order. Of those that set the same value in
	// different spellings, only the winner is applied.
	keys := cf.dropShadowedEnvs(cf.nestedEnvKeys(envs, consumed))

	// Then, handle array element mappings using configured delimiter
	if err = cf.loadArrayEnvs(ctx, envs, keys); err != nil {
		return err
	}

	// Finally, handle nested structs using configured delimiter
	if err = cf.loadNestedEnvs(ctx, envs, keys); err != nil {
		return err
	}

	return nil
}

// normalizedEnvs returns the variables of raw keyed by their normalized names
// (see normalizeEnvName). Of several variables with one normalized name, such
// as HOSTS[0] and HOSTS__0, the first in name order wins, so the outcome does
// not depend on the order of the process environment.
func (cf *loadState) normalizedEnvs(raw map[string]string) map[string]string {
	envs := make(map[string]string, len(raw))
	for _, name := range slices.Sorted(maps.Keys(raw)) {
		key := cf.normalizeEnvName(name)
		if _, taken := envs[key]; !taken {
			envs[key] = raw[name]
		}
	}
	return envs
}

// normalizeEnvName returns the variable name key, without the prefix, in the
// one spelling every pass of the environment loader reads: the bracket syntax
// (NAME[0], NAME[key]) becomes a level of its own (see normalizeEnvKey), and so
// does the legacy index suffix (NAME__0) of a struct level that names a slice
// field, while every level after it is kept (see normalizeLevels). With the
// default delimiter "__" the legacy suffix already is a level of its own.
//
// A key an env tag names whole is kept as it is. Otherwise the levels after
// the tag of each slice or map field whose tag leads key are normalized from
// that field's type, and the longest tag whose normalized entry path
// addresses something in the type claims key, as findTaggedCollection then
// resolves it; a key no tag claims is normalized from the configuration root.
func (cf *loadState) normalizeEnvName(name string) string {
	delimiter := cf.options.envSectionDelimiter
	key := normalizeEnvKey(name, delimiter)
	if !strings.Contains(key, legacyIndexSeparator) || delimiter == legacyIndexSeparator {
		return key
	}
	claimed, longest := "", -1
	for _, tf := range cf.envTagged {
		rest, ok := strings.CutPrefix(key, tf.name)
		if !ok {
			continue
		}
		if rest == "" {
			return key
		}
		t := tf.fld.field.Type
		if len(tf.name) <= longest || !isCollectionType(t) {
			continue
		}
		if rest, ok = strings.CutPrefix(rest, delimiter); !ok {
			continue
		}
		path := strings.Split(rest, delimiter)
		if cf.normalizeLevels(t, path, false) {
			// A rewritten level spans two levels now.
			path = strings.Split(strings.Join(path, delimiter), delimiter)
		}
		if _, ok := cf.entryPathType(t, path); ok {
			claimed, longest = tf.name+delimiter+strings.Join(path, delimiter), len(tf.name)
		}
	}
	if longest >= 0 {
		return claimed
	}
	parts := strings.Split(key, delimiter)
	if !cf.normalizeLevels(reflect.TypeOf(cf.conf), parts, true) {
		return key
	}
	return strings.Join(parts, delimiter)
}

// normalizeLevels rewrites, in place, the variable name levels parts walked
// from type t: a level with a legacy index suffix (NAME__0) on a struct level
// that names a slice field becomes NAME<delimiter>0, a map key is taken
// verbatim, and the levels from the first one that resolves to nothing are
// kept as they are. fixed is passed to the struct level lookup until the first
// index or map key. It reports whether it rewrote a level.
func (cf *loadState) normalizeLevels(t reflect.Type, parts []string, fixed bool) bool {
	delimiter := cf.options.envSectionDelimiter
	changed := false
	for i, part := range parts {
		t = derefType(t)
		switch t.Kind() {
		case reflect.Slice, reflect.Map:
			t, fixed = t.Elem(), false
		case reflect.Struct:
			if sf, found := cf.findStructFieldRecursive(t, part, fixed); found {
				t = sf.Type
				continue
			}
			base, index, ok := cutIndexSuffix(part)
			if !ok {
				return changed
			}
			sf, found := cf.findStructFieldRecursive(t, base, fixed)
			if !found || derefType(sf.Type).Kind() != reflect.Slice {
				return changed
			}
			parts[i], changed = base+delimiter+index, true
			t, fixed = derefType(sf.Type).Elem(), false
		default:
			return changed
		}
	}
	return changed
}

// nestedEnvKeys returns, sorted, the names in envs that contain the section
// delimiter, except the ones in consumed.
func (cf *loadState) nestedEnvKeys(envs map[string]string, consumed map[string]struct{}) []string {
	var keys []string
	for envKey := range envs {
		if _, done := consumed[envKey]; !done && strings.Contains(envKey, cf.options.envSectionDelimiter) {
			keys = append(keys, envKey)
		}
	}
	slices.Sort(keys)
	return keys
}

// minShadowingKeys is the number of variables one can shadow another from.
const minShadowingKeys = 2

// dropShadowedEnvs removes from the sorted keys the variables that set the
// same value as another one (see envTarget) and lose to it: the winner has the
// fewest Go field name levels, then comes first in name order. A single
// variable shadows none, so its target is not resolved.
func (cf *loadState) dropShadowedEnvs(keys []string) []string {
	if len(keys) < minShadowingKeys {
		return keys
	}
	type candidate struct {
		key  string
		rank int
	}
	var (
		winners map[string]candidate
		losers  map[string]struct{}
	)
	for _, envKey := range keys {
		target, rank, ok := cf.envTarget(envKey)
		if !ok {
			continue
		}
		if winners == nil {
			winners = make(map[string]candidate)
		}
		current, seen := winners[target]
		if !seen {
			winners[target] = candidate{key: envKey, rank: rank}
			continue
		}
		loser := envKey
		if rank < current.rank || rank == current.rank && envKey < current.key {
			loser = current.key
			winners[target] = candidate{key: envKey, rank: rank}
		}
		if losers == nil {
			losers = make(map[string]struct{})
		}
		losers[loser] = struct{}{}
	}
	if losers == nil {
		return keys
	}
	return slices.DeleteFunc(keys, func(key string) bool {
		_, lost := losers[key]
		return lost
	})
}

// preferredEnvName returns the name of names set in envs that wins, as on
// the other paths of the environment loader: the one with the fewest Go field
// name levels (see aliasLevels), then the first in name order.
func (cf *loadState) preferredEnvName(names []string, envs map[string]string) (string, bool) {
	first := -1
	for i, name := range names {
		if _, ok := envs[name]; !ok {
			continue
		}
		if first >= 0 {
			return cf.preferredOf(names[first:], envs), true
		}
		first = i
	}
	if first < 0 {
		return "", false
	}
	return names[first], true
}

// preferredOf returns the name of names set in envs that wins, when several
// are set (see preferredEnvName).
func (cf *loadState) preferredOf(names []string, envs map[string]string) string {
	var (
		best     string
		bestRank int
	)
	for _, name := range names {
		if _, ok := envs[name]; !ok {
			continue
		}
		_, rank, _ := cf.envTarget(name)
		if best == "" || rank < bestRank || rank == bestRank && name < best {
			best, bestRank = name, rank
		}
	}
	return best
}

// envVarError names the environment variable name, without the prefix, in
// err. The value is never quoted: it may be an expanded secret.
func (cf *loadState) envVarError(name string, err error) error {
	if err == nil {
		return nil
	}
	return &envVarErr{prefix: cf.options.envPrefix, key: name, err: err}
}

// fieldByIndexAlloc returns the field at the struct index path from v,
// allocating nil pointer structs along the way.
func fieldByIndexAlloc(v reflect.Value, index []int) reflect.Value {
	for _, i := range index {
		for v.Kind() == reflect.Pointer {
			if v.IsNil() {
				v.Set(reflect.New(v.Type().Elem()))
			}
			v = v.Elem()
		}
		v = v.Field(i)
	}
	return v
}

// noteEnvGrowth records, before the environment grows the slice v at steps
// to n elements or creates the nil map v there, how many entries it held, so
// default tags fill only the entries the environment added.
func (cf *loadState) noteEnvGrowth(v reflect.Value, n int, steps []pathStep) {
	switch {
	case v.Kind() == reflect.Slice && v.Len() < n,
		v.Kind() == reflect.Map && v.IsNil():
		if cf.present == nil {
			cf.present = &presence{}
		}
		cf.present.markGrown(steps, v.Len())
	default:
	}
}

// markEnv records that the environment set the value at steps.
func (cf *loadState) markEnv(steps []pathStep) {
	if cf.present == nil {
		cf.present = &presence{}
	}
	cf.present.mark(steps)
}

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

	arrayPositions := cf.findArrayIndices(parts)
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
func (cf *loadState) findArrayIndices(parts []string) []int {
	positions := make([]int, 0)

	for i := 1; i < len(parts); i++ {
		positions = coreslices.AppendIf(positions, cf.isArrayIndex(parts[i]), i)
	}

	return positions
}

// isArrayIndex checks if a part represents an array index: a number. An
// index suffix (NAME__0, NAME[0]) is a level of its own once the name is
// normalized (see normalizeEnvName).
func (cf *loadState) isArrayIndex(part string) bool {
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

		arrayField, elementPath := cf.buildArrayPaths(parts, position, delimiter, pathPrefixCache, pathSuffixCache)

		if result[arrayField] == nil {
			result[arrayField] = make(map[string]string)
		}
		result[arrayField][elementPath] = envValue
	}
}

// buildArrayPaths builds the array field path and element path for the
// numeric index at position.
func (cf *loadState) buildArrayPaths(
	parts []string,
	position int,
	delimiter string,
	prefixCache, suffixCache map[int]string,
) (arrayField, elementPath string) {
	prefix := cf.getCachedPrefix(parts, position, delimiter, prefixCache)
	suffix := cf.getCachedSuffix(parts, position, delimiter, suffixCache)

	return prefix, suffix
}

// getCachedPrefix returns the cached prefix or computes and caches it.
func (cf *loadState) getCachedPrefix(parts []string, position int, delimiter string, cache map[int]string) string {
	if prefix, ok := cache[position]; ok {
		return prefix
	}

	prefix := strings.Join(parts[0:position], delimiter)
	cache[position] = prefix
	return prefix
}

// getCachedSuffix returns the cached suffix or computes and caches it.
func (cf *loadState) getCachedSuffix(parts []string, position int, delimiter string, cache map[int]string) string {
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
	if err := cf.ensureSliceSizeForValue(arrayField.value, minSize); err != nil {
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
func (cf *loadState) ensureSliceSizeForValue(sliceValue reflect.Value, minSize int) error {
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

// setFieldValue sets a reflect.Value from a string value.
// It records the value as set by the environment only when it was actually
// assigned: an empty value leaves numbers, bools, slices and maps untouched,
// so their defaults still apply. steps locates fieldValue from the
// configuration root.
func (cf *loadState) setFieldValue(ctx context.Context, fieldValue reflect.Value, value string, steps []pathStep) error {
	assigned, err := cf.setEnvValue(ctx, fieldValue, value)
	if err != nil {
		return err
	}
	if assigned {
		cf.markEnv(steps)
	}
	return nil
}

// setEnvValue assigns an environment value to fieldValue and reports whether
// it assigned anything. Its $VAR references are expanded (see expandEnvValue)
// and its $__secret{namespace:key} placeholders resolved first, once, before
// the value is parsed: a variable that names no field or lost to another
// spelling never reaches here, and a secret may supply a whole collection,
// which is then parsed literally. A resolved value takes the same
// path as a literal one (an empty one assigns nothing to a number, a bool, a
// slice or a map). A parse error of a resolved value is reported without the
// parser's message, which may quote the secret.
func (cf *loadState) setEnvValue(ctx context.Context, fieldValue reflect.Value, value string) (bool, error) {
	value, err := expandEnvValue(value, cf.options.strict)
	if err != nil {
		return false, err
	}
	resolved, err := cf.resolveSecrets(ctx, value)
	if err != nil {
		return false, err
	}
	assigned, err := set(fieldValue, resolved, false, true, cf.options.strict)
	// An unsupported field type is reported as it is: it names the type,
	// never the value.
	if err != nil && loadersecrets.HasSecrets(value) && !errors.Is(err, ErrUnsupportedFieldType) {
		return false, sealParseError(err, "the value", loadersecrets.References(value))
	}
	return assigned, err
}

// loadNestedEnvs handles nested structures using the section delimiter. It
// applies the variables the sorted keys name, in that order.
func (cf *loadState) loadNestedEnvs(ctx context.Context, envs map[string]string, keys []string) error {
	for _, envKey := range keys {
		if err := cf.setNestedFieldFromEnv(ctx, envKey, envs[envKey]); err != nil {
			return cf.envVarError(envKey, err)
		}
	}
	return nil
}

// setNestedFieldFromEnv sets a nested field value using the section delimiter.
func (cf *loadState) setNestedFieldFromEnv(ctx context.Context, envKey, envValue string) error {
	// Use the section delimiter to split nested struct levels
	delimiter := cf.options.envSectionDelimiter
	parts := strings.Split(envKey, delimiter)

	if len(parts) < minEnvKeyParts {
		return nil
	}

	// A slice or map field with an env tag is addressed through the tag:
	// TAG__<index|key>. The tag claims the variable only for a valid entry
	// path, so nothing is allocated for a variable that sets nothing.
	if tagged, entryPath := cf.findTaggedCollection(envKey, false); tagged != nil {
		if entryPath == nil {
			// The tag names the whole variable: the plain-field pass set it.
			return nil
		}
		if _, ok := cf.settablePath(tagged.field.Type, entryPath, false); !ok {
			// A slice entry path, which the indexed pass sets.
			return nil
		}
		if !tagged.value.IsValid() {
			allocated := *tagged
			allocated.value = fieldByIndexAlloc(reflect.ValueOf(cf.conf).Elem(), tagged.index)
			tagged = &allocated
		}
		return cf.setNestedFieldValue(ctx, tagged, entryPath, envValue, tagged.pathSteps(), false)
	}

	// Find the root field
	rootFieldName := parts[0]
	var rootField *field

	cf.fields.each(func(fld *field) bool {
		// Check if this field matches the root field name
		// Only match root-level fields (no parent)
		if fld.parent == nil && cf.fieldNames(fld).matches(rootFieldName, true) {
			rootField = fld
			return false // Stop searching
		}
		return true
	})

	// If not found in fields list, try to find it directly in the config struct
	// This handles fields with default:"-" tag that haven't been initialized yet
	if rootField == nil {
		rootField = cf.findRootFieldByName(rootFieldName)
	}

	if rootField == nil {
		// Do not error here even in strict mode: loadNestedEnvs processes ALL env vars
		// containing the section delimiter (e.g. "__"), including system env vars
		// (like __CFBundleIdentifier on macOS) that are not targeting the config struct.
		return nil
	}

	// Resolve the rest of the path on the type first, so a variable that sets
	// nothing allocates no nil section or map along it.
	if missing, ok := cf.settablePath(rootField.field.Type, parts[1:], true); !ok {
		if missing != "" && cf.options.strict {
			return fmt.Errorf("%w: %s", ErrFieldNotFound, missing)
		}
		return nil
	}

	// Navigate to the nested field and set its value
	return cf.setNestedFieldValue(ctx, rootField, parts[1:], envValue, rootField.pathSteps(), true)
}

// findRootFieldByName finds a root-level field in the config struct by name.
// This is used to find fields with default:"-" that haven't been initialized
// yet. A nil pointer field is left nil: the navigation allocates it only for a
// path that resolves (see settablePath).
func (cf *loadState) findRootFieldByName(fieldName string) *field {
	if cf.conf == nil {
		return nil
	}

	configValue := reflect.ValueOf(cf.conf)
	if configValue.Kind() == reflect.Pointer {
		configValue = configValue.Elem()
	}

	if configValue.Kind() != reflect.Struct {
		return nil
	}

	configType := configValue.Type()

	// Search for the field in the root struct
	names := cf.structEnvNames(configType)
	for i := range names {
		if names[i].matches(fieldName, true) && configType.Field(i).IsExported() {
			structField := configType.Field(i)
			fieldValue := configValue.Field(i)

			// Create a field descriptor
			tags := make(map[string]string)
			tags[cf.options.structTag] = structField.Tag.Get(cf.options.structTag)
			tags[envTagName] = structField.Tag.Get(envTagName)
			tags[defaultValueTagName] = structField.Tag.Get(defaultValueTagName)

			return &field{
				name:     structField.Name,
				fullName: structField.Name,
				value:    fieldValue,
				field:    structField,
				tags:     tags,
				index:    []int{i},
			}
		}
	}

	return nil
}
