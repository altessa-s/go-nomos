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

	root := pathNode{fields: make(map[string]*pathNode)}
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

		path, err := conv.parsePath(key)
		if err != nil {
			return nil, err
		}

		if err := root.assign(path, key, value); err != nil {
			return nil, err
		}
	}

	if scanErr := scanner.Err(); scanErr != nil {
		return nil, coreerrs.WrapOperation(scanErr, "scan .env file")
	}

	return root.configFields(conv), nil
}

// A pathNode holds either fields, indexed elements, or a complete assignment.
// Keep a leaf's text until the tree is built so an array literal is also a leaf.
type pathNode struct {
	name   string
	value  string
	rawKey string
	fields map[string]*pathNode
	elems  []*pathNode
}

type pathField struct {
	name  string
	index int // -1 for an unindexed field.
}

// parsePath validates the whole assignment before a conflicting prefix can mask an invalid suffix.
func (conv *Parser) parsePath(key string) ([]pathField, error) {
	parts := conv.splitKey(key)

	path := make([]pathField, 0, len(parts))
	for i := 0; i < len(parts); i++ {
		name := corestrings.ScreamingSnakeToCamelCase(parts[i])
		if name == "" {
			return nil, fmt.Errorf("%w: empty field name in key %q", ErrInvalidInput, key)
		}

		field := pathField{name: name, index: -1}

		if i+1 < len(parts) && isDecimalIndex(parts[i+1]) {
			i++

			idx, err := strconv.Atoi(parts[i])
			if err != nil || idx < 0 || idx >= DefaultMaxArrayLen {
				return nil, fmt.Errorf("%w: array index %q in key %q", ErrInvalidInput, parts[i], key)
			}

			field.index = idx
		}

		path = append(path, field)
	}

	return path, nil
}

// Check the spelling separately so integer overflow cannot turn an index into a field.
func isDecimalIndex(part string) bool {
	if strings.HasPrefix(part, "+") || strings.HasPrefix(part, "-") {
		part = part[1:]
	}

	return part != "" && strings.Trim(part, "0123456789") == ""
}

func (node *pathNode) assign(path []pathField, key, value string) error {
	for _, field := range path {
		if node.rawKey != "" || node.elems != nil {
			return fmt.Errorf("%w: %s", ErrConflictingKey, key)
		}

		if node.fields == nil {
			node.fields = make(map[string]*pathNode)
		}

		envName := corestrings.ToScreamingSnakeCase(field.name)

		child := node.fields[envName]
		if child == nil {
			child = &pathNode{name: field.name}
			node.fields[envName] = child
		} else if child.name != field.name {
			return fmt.Errorf("%w: %s", ErrConflictingKey, key)
		}

		node = child

		idx := field.index
		if idx < 0 {
			continue
		}

		if node.rawKey != "" || node.fields != nil {
			return fmt.Errorf("%w: %s", ErrConflictingKey, key)
		}

		for len(node.elems) <= idx {
			node.elems = append(node.elems, nil)
		}

		if node.elems[idx] == nil {
			node.elems[idx] = &pathNode{}
		}

		node = node.elems[idx]
	}

	if node.fields != nil || node.elems != nil || (node.rawKey != "" && node.rawKey != key) {
		return fmt.Errorf("%w: %s", ErrConflictingKey, key)
	}

	node.value, node.rawKey = value, key

	return nil
}

func (node *pathNode) configFields(conv *Parser) map[string]any {
	fields := make(map[string]any, len(node.fields))
	for _, child := range node.fields {
		fields[child.name] = child.configValue(conv)
	}

	return fields
}

func (node *pathNode) configValue(conv *Parser) any {
	switch {
	case node.rawKey != "":
		return conv.parseValue(node.value)
	case node.elems != nil:
		elems := make([]any, len(node.elems))
		for i, child := range node.elems {
			if child == nil {
				elems[i] = map[string]any{}
			} else {
				elems[i] = child.configValue(conv)
			}
		}

		return elems
	default:
		return node.configFields(conv)
	}
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
	var (
		elements []string
		quote    rune
	)

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
