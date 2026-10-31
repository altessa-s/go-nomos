// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package conversion

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	coreerrs "github.com/altessa-s/go-atlas-core/errors"
	corestrings "github.com/altessa-s/go-atlas-core/text/strings"
)

// ErrConflictingKey indicates incompatible assignments to one path.
var ErrConflictingKey = errors.New("conflicting configuration key")

// ErrInvalidInput indicates a malformed environment line or array index.
var ErrInvalidInput = errors.New("invalid environment input")

// DefaultMaxArrayLen bounds allocations caused by untrusted array indices.
const DefaultMaxArrayLen = 1024
const envKeyValueParts = 2
const quotedValueMinLen = 2
const maxArrayDepth = 64

// Parser reconstructs configuration values from environment documents.
type Parser struct{}

// Parse reads KEY=VALUE assignments and builds their nested configuration tree.
func (conv *Parser) Parse(reader io.Reader, parseComments bool) (map[string]any, error) {
	return conv.ParseContext(context.Background(), reader, parseComments)
}

// ParseContext checks cancellation before reading and between assignments.
func (conv *Parser) ParseContext(ctx context.Context, reader io.Reader, parseComments bool) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Parse .env file
	envVars := make(map[string]string)
	scanner := bufio.NewScanner(reader)
	lineNum := 0

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines
		if line == "" {
			continue
		}

		// Handle commented lines
		if comment, ok := strings.CutPrefix(line, "#"); ok {
			if !parseComments {
				continue
			}
			line = strings.TrimSpace(comment)
			key, _, assignment := strings.Cut(line, "=")
			if !assignment || strings.TrimSpace(key) == "" {
				continue
			}
		}

		// Parse KEY=VALUE
		parts := strings.SplitN(line, "=", envKeyValueParts)
		if len(parts) != envKeyValueParts {
			return nil, fmt.Errorf("%w: line %d", ErrInvalidInput, lineNum)
		}

		key := strings.TrimSpace(parts[0])
		if !utf8.ValidString(key) {
			return nil, fmt.Errorf("%w: invalid variable name on line %d", ErrInvalidInput, lineNum)
		}
		value := strings.TrimSpace(parts[1])

		envVars[key] = value
	}

	if scanErr := scanner.Err(); scanErr != nil {
		return nil, coreerrs.WrapOperation(scanErr, "scan .env file")
	}

	// Build config structure
	configData, err := conv.buildConfigStructure(envVars)
	if err != nil {
		return nil, err
	}

	return configData, nil
}

// buildConfigStructure builds nested config structure from flat env variables.
func (conv *Parser) buildConfigStructure(envVars map[string]string) (map[string]any, error) {
	result := make(map[string]any)

	for key, value := range envVars {
		if err := conv.setNestedValue(result, key, value); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// setNestedValue sets a value in nested map structure based on key path.
// A purely numeric part following a field is an index into that field's
// array: SERVERS__0__HOST sets servers[0].host, and PORTS__0 sets ports[0].
func (conv *Parser) setNestedValue(data map[string]any, key, value string) error {
	// Split by __ (section delimiter) and . (array index)
	parts := conv.splitKey(key)
	if len(parts) == 0 {
		return nil
	}

	current := data
	for i := 0; i < len(parts); {
		// Convert SCREAMING_SNAKE_CASE to camelCase
		fieldName := corestrings.ScreamingSnakeToCamelCase(parts[i])

		if i == len(parts)-1 {
			if _, exists := current[fieldName]; exists {
				return fmt.Errorf("%w: %s", ErrConflictingKey, key)
			}
			current[fieldName] = conv.parseValue(value)
			return nil
		}

		idx, parseErr := strconv.Atoi(parts[i+1])
		if parseErr != nil {
			// Regular nested field: move to (or create) the nested map.
			nestedMap, err := nestedSection(current, fieldName, key)
			if err != nil {
				return err
			}
			current = nestedMap
			i++
			continue
		}

		if idx < 0 || idx >= DefaultMaxArrayLen {
			return fmt.Errorf("%w: array index %q in key %q", ErrInvalidInput, parts[i+1], key)
		}

		// The next part is an array index: extend the slice to cover it.
		slice, previouslyAllocated, err := growArray(current, fieldName, idx, key)
		if err != nil {
			return err
		}

		// A trailing index sets the array element itself.
		if i+1 == len(parts)-1 {
			if previouslyAllocated && !isEmptyPlaceholder(slice[idx]) {
				return fmt.Errorf("%w: %s", ErrConflictingKey, key)
			}
			slice[idx] = conv.parseValue(value)
			return nil
		}

		// Move to the array element, consuming both the field and the index.
		element, ok := slice[idx].(map[string]any)
		if !ok {
			return fmt.Errorf("%w: %s", ErrConflictingKey, key)
		}
		current = element
		i += 2
	}

	return nil
}

// nestedSection returns the nested map at fieldName in current, creating it
// when absent; a non-map value there conflicts with key.
func nestedSection(current map[string]any, fieldName, key string) (map[string]any, error) {
	existing, exists := current[fieldName]
	nestedMap, ok := existing.(map[string]any)
	if exists && !ok {
		return nil, fmt.Errorf("%w: %s", ErrConflictingKey, key)
	}
	if !ok {
		nestedMap = make(map[string]any)
		current[fieldName] = nestedMap
	}
	return nestedMap, nil
}

// growArray returns the array at fieldName in current, extended with empty
// placeholder maps to cover idx, and whether idx was already allocated; a
// non-array value there conflicts with key.
func growArray(current map[string]any, fieldName string, idx int, key string) ([]any, bool, error) {
	existing, exists := current[fieldName]
	slice, ok := existing.([]any)
	if exists && !ok {
		return nil, false, fmt.Errorf("%w: %s", ErrConflictingKey, key)
	}
	previouslyAllocated := idx < len(slice)
	for len(slice) <= idx {
		slice = append(slice, make(map[string]any))
	}
	current[fieldName] = slice
	return slice, previouslyAllocated, nil
}

// isEmptyPlaceholder reports whether v is an element growArray allocated and
// nothing has set since.
func isEmptyPlaceholder(v any) bool {
	placeholder, isPlaceholder := v.(map[string]any)
	return isPlaceholder && len(placeholder) == 0
}

// splitKey splits environment variable key into parts.
// Handles both __ (section delimiter) and . (array index).
func (conv *Parser) splitKey(key string) []string {
	// First split by __
	sections := strings.Split(key, "__")

	// Then split each section by . (for array indices)
	var parts []string
	for _, section := range sections {
		if strings.Contains(section, ".") {
			dotParts := strings.Split(section, ".")
			parts = append(parts, dotParts...)
		} else {
			parts = append(parts, section)
		}
	}

	return parts
}

// SCREAMING_SNAKE_CASE → camelCase conversion is implemented in core/text/strings.

// parseValue attempts to parse string value to appropriate type.
func (conv *Parser) parseValue(value string) any {
	if len(value) >= quotedValueMinLen {
		if value[0] == '"' && value[len(value)-1] == '"' {
			if decoded, err := strconv.Unquote(value); err == nil {
				return decoded
			}
			// Preserve legacy quoted paths whose backslashes are not escape sequences.
			return value[1 : len(value)-1]
		}
		if value[0] == '\'' && value[len(value)-1] == '\'' {
			return value[1 : len(value)-1]
		}
	}
	// Try array format [element1,element2,element3]
	if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
		return conv.parseArray(value)
	}

	return parseScalar(value)
}

// parseScalar infers scalar types for both standalone values and array elements.
func parseScalar(value string) any {
	// Try boolean
	if value == "true" {
		return true
	}
	if value == "false" {
		return false
	}

	// Try integer
	if intVal, err := strconv.ParseInt(value, 10, 64); err == nil {
		return intVal
	}

	// Try float
	if floatVal, err := strconv.ParseFloat(value, 64); err == nil {
		return floatVal
	}

	// Return as string
	return value
}

// parseArray parses array format [element1,element2,element3] into a slice.
func (conv *Parser) parseArray(value string) any {
	// Remove brackets
	content := value[1 : len(value)-1]

	// Handle empty array
	if content == "" {
		return []any{}
	}

	// Split by comma
	elements, ok := splitArrayElements(content)
	if !ok {
		return value
	}
	result := make([]any, len(elements))

	for i, element := range elements {
		result[i] = conv.parseValue(strings.TrimSpace(element))
	}

	return result
}

// splitArrayElements ignores commas inside quoted strings and nested arrays.
func splitArrayElements(content string) ([]string, bool) {
	var elements []string
	var quote rune
	escaped, depth, start := false, 0, 0
	for i, char := range content {
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if quote == '"' && char == '\\' {
				escaped = true
				continue
			}
			if char == quote {
				quote = 0
			}
			continue
		}
		switch char {
		case '\'', '"':
			quote = char
		case '[':
			depth++
			if depth >= maxArrayDepth {
				return nil, false
			}
		case ']':
			depth--
			if depth < 0 {
				return nil, false
			}
		case ',':
			if depth == 0 {
				elements = append(elements, content[start:i])
				start = i + 1
			}
		}
	}
	if quote != 0 || depth != 0 {
		return nil, false
	}
	return append(elements, content[start:]), true
}
