// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package loader

import (
	"context"
	"errors"
	"maps"
	"reflect"
	"slices"
	"strings"

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
