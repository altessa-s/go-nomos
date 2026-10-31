// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package loader

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/altessa-s/go-atlas-core/runtime/appinfo"
)

const (
	// Environment variable unwrapping constants
	maxDepth           = 10
	envVarDollarPrefix = "$"

	// dollarSentinel temporarily replaces escaped `$$` sequences during
	// expansion so the resulting literal `$` is not re-interpreted as an
	// env-var prefix by subsequent scan iterations or recursive calls.
	// The control characters at both ends ensure it cannot collide with any
	// realistic env-var value or user-supplied string.
	dollarSentinel = "\x00ATL_LITERAL_DOLLAR\x00"

	// secretPlaceholderPrefix is how a $__secret{namespace:key} placeholder
	// starts. Environment-variable substitution keeps it literal, so the
	// placeholder reaches the secrets expander intact instead of being read
	// as the (unset) variable __secret. Any other reference to a variable
	// named __secret ($__secret, ${__secret}) expands as usual.
	secretPlaceholderPrefix = envVarDollarPrefix + "__secret{"

	// secretSentinel stands in for secretPlaceholderPrefix while os.Expand
	// runs; like dollarSentinel, its control characters cannot collide with
	// configuration text.
	// #nosec G101 -- Internal substitution marker, not a credential.
	secretSentinel = "\x00ATL_SECRET_PLACEHOLDER\x00{"
)

// ErrInvalidConfig is returned when the config parameter is invalid.
var ErrInvalidConfig = errors.New("config must be a struct pointer")

// lookupEnv, getenv and envValues are the loader's only reads of the process
// environment.
func lookupEnv(key string) (string, bool) {
	return os.LookupEnv(key)
}

// getenv returns the value of the variable key, empty when it is unset.
func getenv(key string) string {
	value, _ := lookupEnv(key)
	return value
}

// envValues returns the environment variables carrying prefix, keyed by the
// rest of the name as it is spelled, with their values verbatim. Their $VAR
// references are expanded (see expandEnvValue) and their secret placeholders
// resolved only when the loader assigns them, so only the variables that name
// a field and win precedence are expanded.
func envValues(prefix string) map[string]string {
	envs := make(map[string]string)
	for _, s := range os.Environ() {
		key, value, ok := strings.Cut(s, "=")
		if !ok {
			continue
		}
		if name, ok := strings.CutPrefix(key, prefix); ok && name != "" {
			envs[name] = value
		}
	}
	return envs
}

// expandEnvValue unwraps the $VAR references of an environment value (see
// unwrapEnvValue); with strict set, a reference to an undefined variable is an
// error that names the reference, never the value.
func expandEnvValue(value string, strict bool) (string, error) {
	if strict {
		return unwrapEnvValueStrict(value)
	}
	return unwrapEnvValue(value), nil
}

// normalizeEnvKey converts square bracket syntax to the section delimiter.
// Examples with the default delimiter "__":
//   - "NATS__CONSUMERS[keycloak_sync]__DESCRIPTION" → "NATS__CONSUMERS__keycloak_sync__DESCRIPTION"
//   - "BACK_OFF[0]" → "BACK_OFF__0"
//   - "NORMAL_KEY" → "NORMAL_KEY" (unchanged)
func normalizeEnvKey(key, delimiter string) string {
	// Quick check if normalization is needed
	if !strings.Contains(key, "[") {
		return key
	}

	// Replace [ with the delimiter and remove ]
	normalized := strings.ReplaceAll(key, "[", delimiter)
	normalized = strings.ReplaceAll(normalized, "]", "")
	return normalized
}

// unwrapEnvValue unwraps the environment variable value.
// Prevents infinite recursion by limiting depth and tracking visited variables.
//
// Supported syntax in values:
//   - "$VAR"          — expanded to the value of VAR (or empty if undefined).
//   - "$$"            — literal "$" (escape; the second "$" is consumed).
//   - "$$VAR"         — literal "$VAR" (no expansion of VAR).
//   - bare "$" with no following [A-Za-z0-9_] — preserved as a literal "$".
//
// The "${VAR}" form is not handled here; it is processed separately by
// substituteEnvVariables when applied to default values and templates.
func unwrapEnvValue(value string) string {
	return decodeDollarSentinel(unwrapEnvValueWithDepthAndOriginal(value, value, 0, make(map[string]bool)))
}

// decodeDollarSentinel converts the dollar sentinel back to a literal `$`.
// Called by the public entry points after expansion completes — never inside
// the recursive workhorse, so escaped `$` survives every recursive pass.
func decodeDollarSentinel(s string) string {
	if !strings.Contains(s, dollarSentinel) {
		return s
	}
	return strings.ReplaceAll(s, dollarSentinel, envVarDollarPrefix)
}

func unwrapEnvValueWithDepthAndOriginal(value, original string, depth int, visited map[string]bool) string {
	if depth >= maxDepth {
		return original // Stop at max depth, return original value
	}

	// Handle simple case where entire value is a single environment variable.
	// Skip when value is a bare "$" — it has no variable name and must be
	// preserved as a literal by the mixed-string scan below.
	if isWholeEnvRef(value) {
		key := value[1:]

		if visited[key] {
			// Circular reference detected - return current value if env var not set
			envValue := getenv(key)
			if envValue == "" {
				return value
			}
			// Return the environment value (next in chain) if it's a simple variable
			if isWholeEnvRef(envValue) {
				return envValue
			}
			return value // Return current value for complex expressions
		}

		envValue := getenv(key)
		if envValue == "" {
			return "" // Return empty string for undefined variables
		}

		// Mark this key as visited before recursing
		visited[key] = true
		result := unwrapEnvValueWithDepthAndOriginal(envValue, original, depth+1, visited)
		// Don't unmark - leave it visited to detect future circular references

		return result
	}

	result, changed, _ := scanDollarTokens(value, func(key string) (string, tokenAction, error) {
		if visited[key] {
			return "", tokenKeepLiteral, nil
		}
		// Non-strict: undefined and defined-but-empty are indistinguishable
		// and both result in $VAR being dropped (substituted with "").
		return getenv(key), tokenSubstitute, nil
	})

	// Recursively process the result if it changed and we haven't hit max depth
	if changed && depth < maxDepth-1 {
		return unwrapEnvValueWithDepthAndOriginal(result, original, depth+1, visited)
	}

	return result
}

// tokenAction describes how scanDollarTokens should handle a `$VAR` token.
type tokenAction int

const (
	// tokenSubstitute replaces the `$VAR` (including the `$`) with the
	// callback's returned string. An empty string drops the token entirely.
	tokenSubstitute tokenAction = iota
	// tokenKeepLiteral leaves the `$` in place and advances past it,
	// treating it as a literal — used for already-visited (circular) keys.
	tokenKeepLiteral
)

// scanDollarTokens runs one expansion pass over value:
//
//   - `$$` is replaced by a sentinel that survives recursive passes and is
//     decoded back to a literal `$` by decodeDollarSentinel at the top level.
//   - `$NAME` (where NAME matches [A-Za-z0-9_]+) calls onVar(NAME). The
//     callback returns either a substitution string or tokenKeepLiteral to
//     preserve the `$` as-is. Any non-nil error is propagated immediately.
//   - A bare `$` (no following [A-Za-z0-9_]) is preserved as a literal.
//   - `$__secret{` (the start of a secret placeholder) is preserved as a
//     literal; `$__secret` without a brace is an ordinary variable.
//
// Returns the rewritten string, whether any token was rewritten, and the
// callback's first error if it returned one.
func scanDollarTokens(
	value string,
	onVar func(key string) (string, tokenAction, error),
) (string, bool, error) {
	result := value
	changed := false
	offset := 0

	for {
		dollarIndex := strings.Index(result[offset:], envVarDollarPrefix)
		if dollarIndex == -1 {
			break
		}
		dollarIndex += offset

		// Escape: `$$` collapses to a literal `$` (encoded as a sentinel so it
		// is not re-interpreted on subsequent passes). The second `$` is
		// consumed so a following name is NOT expanded (e.g. `$$VAR` → `$VAR`).
		if dollarIndex+1 < len(result) && result[dollarIndex+1] == '$' {
			result = result[:dollarIndex] + dollarSentinel + result[dollarIndex+2:]
			offset = dollarIndex + len(dollarSentinel)
			changed = true
			continue
		}

		// Scan the variable name: contiguous [A-Za-z0-9_].
		start := dollarIndex + 1
		end := start
		for end < len(result) && isEnvNameByte(result[end]) {
			end++
		}

		// A secret placeholder is not a variable: keep it for the secrets
		// expander.
		if end > start && !strings.HasPrefix(result[dollarIndex:], secretPlaceholderPrefix) {
			replacement, action, err := onVar(result[start:end])
			if err != nil {
				return "", false, err
			}
			if action == tokenSubstitute {
				result = result[:dollarIndex] + replacement + result[end:]
				offset = dollarIndex + len(replacement)
				changed = true
				continue
			}
			// tokenKeepLiteral falls through to the literal-preserve branch.
		}

		// No variable name follows this `$` (or the callback opted to keep it
		// literal): preserve it and advance past it.
		offset = dollarIndex + 1
	}

	return result, changed, nil
}

// isEnvNameByte reports whether c may appear in a variable name of a $VAR
// reference: [A-Za-z0-9_].
func isEnvNameByte(c byte) bool {
	return c == '_' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9'
}

// isWholeEnvRef reports whether value is exactly one $VAR reference: a "$"
// followed by a name of [A-Za-z0-9_] only, as scanDollarTokens reads it. A
// secret placeholder is not one.
func isWholeEnvRef(value string) bool {
	if len(value) < 2 || value[0] != '$' {
		return false
	}
	for i := 1; i < len(value); i++ {
		if !isEnvNameByte(value[i]) {
			return false
		}
	}
	return true
}

// userHomeDir returns the home directory of the user.
// It handles both Windows and Unix-like systems.
func userHomeDir() string {
	return appinfo.HomeDir()
}

// assertStructPointer validates that the provided configuration is a pointer to a struct.
// This is required for the configuration loading to work properly.
func assertStructPointer(conf any) error {
	t := reflect.TypeOf(conf)
	if t == nil || t.Kind() != reflect.Pointer || indirectType(t).Kind() != reflect.Struct {
		return ErrInvalidConfig
	}
	return nil
}

// substituteEnvVariables performs environment variable substitution in a string.
// It replaces all occurrences of ${VAR_NAME} with the corresponding environment variable values.
// This function handles multiple variables within the same string and is used for text-level substitution.
// ${VAR:default} takes default when VAR is unset.
//
// A $__secret{namespace:key} placeholder is kept as it is for the secrets
// expander.
func substituteEnvVariables(input string) string {
	return restoreSecretPlaceholders(os.Expand(protectSecretPlaceholders(input), func(ref string) string {
		value, _ := lookupRef(ref)
		return value
	}))
}

// lookupRef resolves the body of a ${...} reference: VAR, or VAR:default,
// whose default applies when VAR is unset. A variable set to an empty string
// keeps its empty value. It reports false for an unset variable without a
// default.
func lookupRef(ref string) (string, bool) {
	name, fallback, hasFallback := strings.Cut(ref, ":")
	if value, ok := lookupEnv(name); ok {
		return value, true
	}
	return fallback, hasFallback
}

// protectSecretPlaceholders replaces the start of every secret placeholder
// with secretSentinel, so os.Expand does not read it as a variable.
func protectSecretPlaceholders(s string) string {
	return strings.ReplaceAll(s, secretPlaceholderPrefix, secretSentinel)
}

// restoreSecretPlaceholders undoes protectSecretPlaceholders.
func restoreSecretPlaceholders(s string) string {
	return strings.ReplaceAll(s, secretSentinel, secretPlaceholderPrefix)
}

// unwrapEnvValueStrict is the strict variant of unwrapEnvValue.
// It returns an error if any referenced environment variable is not defined.
// A variable that is defined but set to an empty string is not considered an error.
//
// Supported syntax matches unwrapEnvValue: "$VAR" expands; "$$" yields a
// literal "$" (and the next char is not interpreted as a variable name);
// a bare "$" with no following [A-Za-z0-9_] is preserved as a literal.
func unwrapEnvValueStrict(value string) (string, error) {
	result, err := unwrapEnvValueStrictWithDepth(value, value, 0, make(map[string]bool))
	if err != nil {
		return "", err
	}
	return decodeDollarSentinel(result), nil
}

func unwrapEnvValueStrictWithDepth(value, original string, depth int, visited map[string]bool) (string, error) {
	if depth >= maxDepth {
		return original, nil
	}

	// Handle simple case where entire value is a single environment variable.
	// Skip when value is a bare "$" — it has no variable name and must be
	// preserved as a literal by the mixed-string scan below.
	if isWholeEnvRef(value) {
		return unwrapWholeEnvRefStrict(value, original, depth, visited)
	}

	result, changed, err := scanDollarTokens(value, func(key string) (string, tokenAction, error) {
		if visited[key] {
			return "", tokenKeepLiteral, nil
		}
		envValue, ok := lookupEnv(key)
		if !ok {
			return "", tokenSubstitute, fmt.Errorf("%w: $%s", ErrUndefinedEnvVar, key)
		}
		// Strict mode: defined-but-empty substitutes "" (not an error).
		return envValue, tokenSubstitute, nil
	})
	if err != nil {
		return "", err
	}

	if changed && depth < maxDepth-1 {
		return unwrapEnvValueStrictWithDepth(result, original, depth+1, visited)
	}

	return result, nil
}

// unwrapWholeEnvRefStrict expands a value that is exactly one "$VAR"
// reference, recursing into the variable's value. A variable seen before
// (a reference cycle) is not expanded again.
func unwrapWholeEnvRefStrict(value, original string, depth int, visited map[string]bool) (string, error) {
	key := value[1:]
	envValue, ok := lookupEnv(key)
	if !ok {
		return "", fmt.Errorf("%w: $%s", ErrUndefinedEnvVar, key)
	}

	if visited[key] {
		if isWholeEnvRef(envValue) {
			return envValue, nil
		}
		return value, nil
	}

	if envValue == "" {
		return "", nil
	}

	visited[key] = true
	return unwrapEnvValueStrictWithDepth(envValue, original, depth+1, visited)
}

// substituteEnvVariablesStrict performs environment variable substitution in a string.
// Unlike substituteEnvVariables, it returns an error if any referenced variable is undefined and has no
// default. A variable that is defined but set to an empty string is not considered an error.
func substituteEnvVariablesStrict(input string) (string, error) {
	var firstErr error
	result := os.Expand(protectSecretPlaceholders(input), func(ref string) string {
		val, ok := lookupRef(ref)
		if !ok && firstErr == nil {
			firstErr = fmt.Errorf("%w: ${%s}", ErrUndefinedEnvVar, ref)
		}
		return val
	})
	if firstErr != nil {
		return "", firstErr
	}
	return restoreSecretPlaceholders(result), nil
}
