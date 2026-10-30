// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package loader

import (
	"context"
	"fmt"
	"reflect"
	"strings"
)

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
