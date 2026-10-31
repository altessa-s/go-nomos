// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package conversion

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	corestrings "github.com/altessa-s/go-atlas-core/text/strings"
)

// Env transforms configuration trees into environment-variable documents.
type Env struct{}

// Render writes a deterministic environment document from configuration data.
func (conv *Env) Render(configData map[string]any, commentedKeys map[string]bool, parseComments bool) []byte {
	// Flatten to env vars
	envVars := make(map[string]string, estimateSize(configData))
	conv.flattenConfigInto(envVars, configData, "", true)

	// Sort keys for consistent output
	keys := make([]string, 0, len(envVars))
	for key := range envVars {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	// Group keys by top-level section
	sections := conv.groupBySection(keys)

	// Write .env file with sections
	var builder strings.Builder
	isFirst := true
	for _, sectionName := range getSortedSectionNames(sections) {
		sectionKeys := sections[sectionName]

		// Add section separator (except for first section)
		if !isFirst {
			builder.WriteString("\n")
		}
		isFirst = false

		// Add section header
		builder.WriteString(sectionSeparator)
		fmt.Fprintf(&builder, "# %s Configuration\n", formatSectionName(sectionName))
		builder.WriteString(sectionSeparator)

		// Add variables in this section
		for _, key := range sectionKeys {
			value := envVars[key]
			// If parseComments is enabled and key is marked as commented, add # prefix
			if parseComments && commentedKeys != nil && commentedKeys[key] {
				fmt.Fprintf(&builder, "# %s=%s\n", key, value)
			} else {
				fmt.Fprintf(&builder, "%s=%s\n", key, value)
			}
		}
	}

	return []byte(builder.String())
}

// groupBySection groups environment variable keys by their top-level section.
func (conv *Env) groupBySection(keys []string) map[string][]string {
	sections := make(map[string][]string)

	for _, key := range keys {
		// Extract top-level section (everything before first __)
		parts := strings.Split(key, "__")
		sectionName := parts[0]

		sections[sectionName] = append(sections[sectionName], key)
	}

	return sections
}

// getSortedSectionNames returns section names sorted alphabetically.
func getSortedSectionNames(sections map[string][]string) []string {
	names := make([]string, 0, len(sections))
	for name := range sections {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// formatSectionName converts SCREAMING_SNAKE_CASE to Title Case for display.
func formatSectionName(s string) string {
	// Convert SCREAMING_SNAKE_CASE to Title Case
	// E.g., "GRPC_SERVER" -> "Grpc Server"
	parts := strings.Split(s, "_")
	for i, part := range parts {
		if part != "" {
			// Capitalize first letter, lowercase the rest
			parts[i] = strings.ToUpper(string(part[0])) + strings.ToLower(part[1:])
		}
	}
	return strings.Join(parts, " ")
}

// estimateSize provides a rough estimate of the final map size for nested structures.
// This helps reduce map reallocations during recursive flattening.
func estimateSize(data map[string]any) int {
	// Rough estimate: assume 2x the top-level keys for nested structures
	return len(data) * 2 //nolint:mnd
}

// FlattenConfig recursively flattens a nested config map into a flat
// map[envKey]value suitable for .env output. prefix is prepended (with __)
// to all generated keys; pass "" for the top level.
//
// Every key, including keys of nested objects, is converted to
// SCREAMING_SNAKE_CASE the same way the loader names struct levels. Without a
// schema, map entries cannot be told apart from struct sections, so map keys
// are converted too (e.g. "keycloakSync" becomes "KEYCLOAK_SYNC").
func (conv *Env) FlattenConfig(data map[string]any, prefix string) map[string]string {
	// Estimate size and create map with capacity
	result := make(map[string]string, estimateSize(data))
	conv.flattenConfigInto(result, data, prefix, false)
	return result
}

// flattenConfigInto recursively flattens config structure into an existing result map.
// This avoids allocating new maps for each recursive call, improving performance.
func (conv *Env) flattenConfigInto(result map[string]string, data map[string]any, prefix string, encode bool) {
	for key, value := range data {
		envKey := corestrings.ToScreamingSnakeCase(key)

		// Build full key with prefix
		fullKey := envKey
		if prefix != "" {
			fullKey = prefix + "__" + envKey
		}

		// Handle different value types
		conv.processValueInto(result, fullKey, value, encode)
	}
}

// processValueInto processes a single value and adds it to the result map.
// This version reuses the result map instead of allocating new ones.
func (conv *Env) processValueInto(result map[string]string, key string, value any, encode bool) {
	if value == nil {
		return
	}

	v := reflect.ValueOf(value)

	switch v.Kind() {
	case reflect.Map:
		if mapValue, ok := value.(map[string]any); ok {
			// Recursively flatten into the existing result map
			conv.flattenConfigInto(result, mapValue, key, encode)
		}
	case reflect.Slice, reflect.Array:
		result[key] = conv.formatArrayValue(v, encode)
	case reflect.String:
		result[key] = v.String()
		if encode {
			result[key] = encodeEnvString(v.String())
		}
	default:
		result[key] = fmt.Sprintf("%v", value)
	}
}

// formatArrayValue formats a slice or array as [element1,element2,element3].
func (conv *Env) formatArrayValue(v reflect.Value, encode bool) string {
	if v.Len() == 0 {
		return "[]"
	}

	var builder strings.Builder
	builder.WriteByte('[')

	for i := range v.Len() {
		if i > 0 {
			builder.WriteByte(',')
		}
		element := v.Index(i)
		for element.Kind() == reflect.Interface && !element.IsNil() {
			element = element.Elem()
		}
		switch {
		case encode && element.Kind() == reflect.String:
			builder.WriteString(encodeEnvString(element.String()))
		case encode && (element.Kind() == reflect.Slice || element.Kind() == reflect.Array):
			builder.WriteString(conv.formatArrayValue(element, true))
		default:
			fmt.Fprintf(&builder, "%v", element.Interface())
		}
	}

	builder.WriteByte(']')
	return builder.String()
}

const sectionSeparatorLength = 42

var sectionSeparator = "# " + strings.Repeat("=", sectionSeparatorLength) + "\n"

// encodeEnvString quotes ambiguous scalars and characters with document syntax.
func encodeEnvString(value string) string {
	_, plainString := parseScalar(value).(string)
	if value == "" || !plainString || strings.TrimSpace(value) != value ||
		strings.ContainsAny(value, "\\\"'\n\r\t#$`,[]") || strings.ContainsFunc(value, func(r rune) bool { return r < ' ' }) {
		return strconv.Quote(value)
	}
	return value
}
