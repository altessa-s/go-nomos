// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package yaml3

import (
	"strconv"
	"strings"
)

// Uncomment removes leading # from YAML lines to parse commented configurations.
func (b *Backend) Uncomment(data []byte) []byte {
	lines := strings.Split(string(data), "\n")
	uncommented := make([]string, 0, len(lines))

	for _, line := range lines {
		uncommented = append(uncommented, b.processLine(line))
	}

	return []byte(strings.Join(uncommented, "\n"))
}

// processLine processes a single line, uncommenting if it's YAML syntax.
func (b *Backend) processLine(line string) string {
	trimmed := strings.TrimLeft(line, " \t")
	leadingSpace := line[:len(line)-len(trimmed)]

	afterHash, ok := strings.CutPrefix(trimmed, "#")
	if !ok {
		return line
	}
	afterHashTrimmed := strings.TrimLeft(afterHash, " \t")

	if b.isYAMLSyntax(afterHashTrimmed) || afterHash == "" {
		uncommented := leadingSpace + afterHash
		return b.quoteYAMLSpecialValues(uncommented)
	}

	return line
}

// yamlSpecialValuePrefixes are characters that have special meaning at the start
// of a YAML scalar value and cause parse errors when left unquoted.
const yamlSpecialValuePrefixes = "*&!@`"

// quoteYAMLSpecialValues wraps values starting with YAML-special characters
// in double quotes to prevent parse errors after uncommenting.
func (b *Backend) quoteYAMLSpecialValues(line string) string {
	trimmed := strings.TrimLeft(line, " \t")

	colonIdx := strings.Index(trimmed, ":")
	if colonIdx <= 0 {
		return line
	}

	afterColon := trimmed[colonIdx+1:]
	value := strings.TrimLeft(afterColon, " \t")
	if len(value) == 0 {
		return line
	}

	// Already quoted
	if value[0] == '"' || value[0] == '\'' {
		return line
	}

	// Check if value starts with a YAML-special character
	if !strings.ContainsRune(yamlSpecialValuePrefixes, rune(value[0])) {
		return line
	}

	leadingSpace := line[:len(line)-len(trimmed)]
	key := trimmed[:colonIdx]
	quotedValue := strconv.Quote(value)

	return leadingSpace + key + ": " + quotedValue
}

// isYAMLSyntax checks if the content matches YAML syntax patterns.
func (b *Backend) isYAMLSyntax(content string) bool {
	if content == "" {
		return false
	}
	return b.isYAMLListItem(content) || b.isYAMLKeyValue(content)
}

// isYAMLListItem checks if content is a YAML list item (starts with -).
func (b *Backend) isYAMLListItem(content string) bool {
	return strings.HasPrefix(content, "-")
}

// isYAMLKeyValue checks if content matches key:value pattern with valid YAML key.
func (b *Backend) isYAMLKeyValue(content string) bool {
	colonIdx := strings.Index(content, ":")
	if colonIdx <= 0 {
		return false
	}
	return b.isValidYAMLKey(content[:colonIdx])
}

// isValidYAMLKey checks if a string is a valid YAML key (alphanumeric, _, -).
func (b *Backend) isValidYAMLKey(key string) bool {
	for _, r := range key {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') &&
			(r < '0' || r > '9') && r != '_' && r != '-' {
			return false
		}
	}
	return true
}

// IsFullyCommented checks if the YAML file is fully commented out.
func (b *Backend) IsFullyCommented(data []byte) bool {
	lines := strings.Split(string(data), "\n")
	hasContent := false
	hasCommentedYAML := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "# ") {
			continue // Empty line or regular comment
		}
		if strings.HasPrefix(trimmed, "#") && strings.Contains(trimmed, ":") {
			hasCommentedYAML = true
		} else if strings.Contains(trimmed, ":") {
			hasContent = true
		}
	}

	return hasCommentedYAML && !hasContent
}
