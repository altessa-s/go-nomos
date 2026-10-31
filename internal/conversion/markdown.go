// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package conversion

import (
	"context"
	"fmt"
	"html"
	"maps"
	"reflect"
	"slices"
	"strings"
	"time"

	coreerrs "github.com/altessa-s/go-atlas-core/errors"
	corestrings "github.com/altessa-s/go-atlas-core/text/strings"
)

// Describer supplies optional descriptions without coupling rendering to a provider.
type Describer interface {
	Describe(context.Context, map[string]string, string) (map[string]string, error)
}

// Markdown renders configuration reference documents.
type Markdown struct{}

// Render creates a reference document, optionally enriching it through provider.
func (conv *Markdown) Render(ctx context.Context, configData map[string]any, provider Describer) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Flatten to env vars with type information
	envVars := conv.flattenConfigWithTypes(configData, "")

	// Sort keys for consistent output
	keys := make([]string, 0, len(envVars))
	for key := range envVars {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	// Group keys by top-level section
	sections := conv.groupBySection(keys)

	// Generate Markdown documentation
	var builder strings.Builder

	// Add header
	builder.WriteString("# Environment Variables Reference\n\n")
	builder.WriteString("This document describes all available environment variables for configuration.\n\n")

	// Add table of contents
	builder.WriteString("## Table of Contents\n\n")
	for _, sectionName := range getSortedSectionNames(sections) {
		anchor := strings.ToLower(strings.ReplaceAll(sectionName, "_", "-"))
		fmt.Fprintf(&builder, "- [%s Configuration](#%s-configuration)\n", formatSectionName(sectionName), anchor)
	}
	builder.WriteString("\n")

	// Enrich the document when a description provider is supplied.
	allDescriptions, err := conv.generateDescriptions(ctx, provider, sections, envVars)
	if err != nil {
		return nil, err
	}

	// Add sections with tables
	for _, sectionName := range getSortedSectionNames(sections) {
		conv.writeSection(&builder, sectionName, sections[sectionName], envVars, allDescriptions[sectionName], provider != nil)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return []byte(builder.String()), nil
}

// generateDescriptions enriches each section through the injected provider.
// A provider failure aborts the conversion before an output document is returned.
func (conv *Markdown) generateDescriptions(
	ctx context.Context, provider Describer,
	sections map[string][]string,
	envVars map[string]variableInfo,
) (map[string]map[string]string, error) {
	allDescriptions := make(map[string]map[string]string)
	if provider == nil {
		return allDescriptions, nil
	}
	for _, sectionName := range getSortedSectionNames(sections) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		sectionVars := make(map[string]string)
		for _, key := range sections[sectionName] {
			v := envVars[key]
			sectionVars[key] = "type: " + v.Type
		}
		descriptions, err := provider.Describe(ctx, sectionVars, sectionName)
		if err != nil {
			return nil, coreerrs.Wrapf(err, "failed to generate descriptions for %s", sectionName)
		}
		allDescriptions[sectionName] = descriptions
	}
	return allDescriptions, nil
}

// writeSection renders one section's header and variable table; the
// description column is present only when an API key is set.
func (conv *Markdown) writeSection(
	builder *strings.Builder,
	sectionName string,
	sectionKeys []string,
	envVars map[string]variableInfo,
	descriptions map[string]string,
	withDescriptions bool,
) {
	// Add section header
	fmt.Fprintf(builder, "## %s Configuration\n\n", formatSectionName(sectionName))

	// Add table header
	if withDescriptions {
		builder.WriteString("| Environment Variable | Type | Default Value | Description |\n")
		builder.WriteString("|---------------------|------|---------------|-------------|\n")
	} else {
		builder.WriteString("| Environment Variable | Type | Default Value |\n")
		builder.WriteString("|---------------------|------|---------------|\n")
	}

	// Add variables in this section
	for _, key := range sectionKeys {
		vi := envVars[key]
		// Format value - use empty cell for empty strings, otherwise wrap in backticks
		valueCell := " "
		if vi.Value != "" {
			valueCell = markdownCodeCell(vi.Value)
		}

		if !withDescriptions {
			fmt.Fprintf(builder, "| %s | %s | %s |\n", markdownCodeCell(key), vi.Type, valueCell)
			continue
		}
		description := descriptions[key]
		if description == "" {
			description = " "
		}
		fmt.Fprintf(builder, "| %s | %s | %s | %s |\n", markdownCodeCell(key), vi.Type, valueCell, markdownTextCell(description))
	}

	builder.WriteString("\n")
}

// variableInfo holds the inferred Go type and default value for a single
// environment variable, used when rendering Markdown tables.
type variableInfo struct {
	Value string
	Type  string
}

// flattenConfigWithTypes recursively flattens config structure into env variables with type information.
func (conv *Markdown) flattenConfigWithTypes(data map[string]any, prefix string) map[string]variableInfo {
	result := make(map[string]variableInfo)

	for key, value := range data {
		// Convert key to SCREAMING_SNAKE_CASE
		envKey := corestrings.ToScreamingSnakeCase(key)

		// Build full key with prefix
		fullKey := envKey
		if prefix != "" {
			fullKey = prefix + "__" + envKey
		}

		// Process value and detect type
		conv.processValueWithType(result, fullKey, value)
	}

	return result
}

// processValueWithType processes a single value, detects its type, and adds it to the result map.
func (conv *Markdown) processValueWithType(result map[string]variableInfo, key string, value any) {
	if value == nil {
		result[key] = variableInfo{Value: "", Type: stringType}
		return
	}

	v := reflect.ValueOf(value)

	switch v.Kind() {
	case reflect.Map:
		// Nested map - recurse
		if mapValue, ok := value.(map[string]any); ok {
			nested := conv.flattenConfigWithTypes(mapValue, key)
			maps.Copy(result, nested)
		}

	case reflect.Slice, reflect.Array:
		// Array - format as [element1,element2,element3]
		arrayType := conv.detectArrayType(v)
		var arrayValue string
		if v.Len() == 0 {
			arrayValue = "[]"
		} else {
			var builder strings.Builder
			builder.WriteByte('[')
			for i := range v.Len() {
				if i > 0 {
					builder.WriteByte(',')
				}
				// A null element is empty, as a null value is.
				if elem := v.Index(i).Interface(); elem != nil {
					fmt.Fprintf(&builder, "%v", elem)
				}
			}
			builder.WriteByte(']')
			arrayValue = builder.String()
		}
		result[key] = variableInfo{Value: arrayValue, Type: arrayType}

	case reflect.String:
		strValue := fmt.Sprintf("%v", value)
		if conv.isDuration(strValue) {
			result[key] = variableInfo{Value: strValue, Type: "duration"}
		} else {
			result[key] = variableInfo{Value: strValue, Type: stringType}
		}

	case reflect.Bool:
		result[key] = variableInfo{Value: fmt.Sprintf("%v", value), Type: "bool"}

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		result[key] = variableInfo{Value: fmt.Sprintf("%v", value), Type: "int"}

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		result[key] = variableInfo{Value: fmt.Sprintf("%v", value), Type: "uint"}

	case reflect.Float32, reflect.Float64:
		result[key] = variableInfo{Value: fmt.Sprintf("%v", value), Type: "float"}

	default:
		// For other types, use string representation
		result[key] = variableInfo{Value: fmt.Sprintf("%v", value), Type: stringType}
	}
}

// detectArrayType detects the type of array elements. YAML and TOML decode
// arrays as []any, so each element is unwrapped from its interface before
// its kind is inspected; nested arrays are typed recursively ([][]int).
// Empty arrays and arrays whose elements differ in type report "array".
func (conv *Markdown) detectArrayType(v reflect.Value) string {
	const untyped = "array"
	elemType := ""
	for i := range v.Len() {
		t := conv.detectElementType(v.Index(i))
		if t == "" || (elemType != "" && t != elemType) {
			return untyped
		}
		elemType = t
	}
	if elemType == "" {
		return untyped
	}
	return "[]" + elemType
}

// detectElementType returns the type name of a single array element, or ""
// when it has no specific type.
func (conv *Markdown) detectElementType(elem reflect.Value) string {
	for elem.Kind() == reflect.Interface && !elem.IsNil() {
		elem = elem.Elem()
	}
	switch elem.Kind() {
	case reflect.String:
		return stringType
	case reflect.Bool:
		return "bool"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return "int"
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "uint"
	case reflect.Float32, reflect.Float64:
		return "float"
	case reflect.Map:
		return "map"
	case reflect.Slice, reflect.Array:
		if nested := conv.detectArrayType(elem); nested != "array" {
			return nested
		}
		return ""
	default:
		return ""
	}
}

// isDuration reports whether value parses as a Go duration with a unit suffix
// (e.g., "30s", "1h30m"). A bare "0", which time.ParseDuration accepts, is not
// treated as a duration.
func (conv *Markdown) isDuration(value string) bool {
	if value == "" || value[len(value)-1] >= '0' && value[len(value)-1] <= '9' {
		return false
	}
	_, err := time.ParseDuration(value)
	return err == nil
}

// groupBySection groups environment variable keys by their top-level section.
func (conv *Markdown) groupBySection(keys []string) map[string][]string {
	sections := make(map[string][]string)

	for _, key := range keys {
		// Extract top-level section (everything before first __)
		parts := strings.Split(key, "__")
		sectionName := parts[0]

		sections[sectionName] = append(sections[sectionName], key)
	}

	return sections
}

const stringType = "string"

// HTML code spans preserve literal punctuation without opening Markdown syntax.
func markdownCodeCell(value string) string {
	if !strings.ContainsAny(value, "`|\r\n<>&") {
		return "`" + value + "`"
	}
	return "<code>" + markdownTextCell(value) + "</code>"
}

func markdownTextCell(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	value = html.EscapeString(value)
	return strings.NewReplacer("|", "&#124;", "`", "&#96;", "\n", "<br>", "\\", "&#92;", "*", "&#42;", "_", "&#95;", "[", "&#91;", "]", "&#93;").Replace(value)
}
