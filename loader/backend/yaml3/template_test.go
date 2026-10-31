// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package yaml3

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQuoteYAMLSpecialValues(t *testing.T) {
	t.Parallel()
	conv := &Backend{}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "asterisk value",
			input:    `  maskString: ****`,
			expected: `  maskString: "****"`,
		},
		{
			name:     "ampersand anchor",
			input:    `  anchor: &name value`,
			expected: `  anchor: "&name value"`,
		},
		{
			name:     "exclamation tag",
			input:    `  typed: !custom data`,
			expected: `  typed: "!custom data"`,
		},
		{
			name:     "at sign reserved",
			input:    `  reserved: @value`,
			expected: `  reserved: "@value"`,
		},
		{
			name:     "backtick reserved",
			input:    "  code: `value`",
			expected: "  code: \"`value`\"",
		},
		{
			name:     "normal value unchanged",
			input:    `  key: normal-value`,
			expected: `  key: normal-value`,
		},
		{
			name:     "already double quoted",
			input:    `  key: "****"`,
			expected: `  key: "****"`,
		},
		{
			name:     "already single quoted",
			input:    `  key: '****'`,
			expected: `  key: '****'`,
		},
		{
			name:     "empty value unchanged",
			input:    `  key:`,
			expected: `  key:`,
		},
		{
			name:     "no colon unchanged",
			input:    `  - list item`,
			expected: `  - list item`,
		},
		{
			name:     "value with embedded quotes",
			input:    `  key: *"quoted"`,
			expected: `  key: "*\"quoted\""`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := conv.quoteYAMLSpecialValues(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestProcessLineQuotesSpecialValues(t *testing.T) {
	t.Parallel()
	conv := &Backend{}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "commented maskString with asterisks",
			input:    `#  maskString: ****`,
			expected: `  maskString: "****"`,
		},
		{
			name:     "commented normal value",
			input:    `#  level: error`,
			expected: `  level: error`,
		},
		{
			name:     "non-YAML comment unchanged",
			input:    `# This is a description`,
			expected: `# This is a description`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := conv.processLine(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}
