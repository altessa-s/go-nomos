// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package secrets

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strings"

	coreerrs "github.com/altessa-s/go-atlas-core/errors"
)

// secretPattern matches the $__secret{namespace:key} syntax.
// Capture groups:
//  1. namespace - the logical grouping for secrets
//  2. key - the specific secret identifier
var secretPattern = regexp.MustCompile(`\$__secret\{([^:}]+):([^}]+)\}`)

// ErrNoResolver indicates content carries secret placeholders but no
// [Resolver] was configured to resolve them. Expansion fails closed rather
// than turning each secret into an empty string.
var ErrNoResolver = errors.New("secret placeholders present but no secret resolver is configured")

// Expander handles expansion of secret placeholders in strings.
// It uses a [Resolver] to retrieve secret values and replaces
// placeholders with the actual secret data.
//
// Thread Safety:
// The Expander is thread-safe and can be used concurrently from multiple goroutines.
type Expander struct {
	options
	resolver Resolver
}

// Resolver resolves a "namespace:key" secret reference to its value.
//
// Any error fails the expansion (fail-closed) unless WithFailOnError(false)
// is set; implementations should wrap core/errors.ErrNotFound for an unknown
// reference so callers can tell it apart. security/secrets.StringResolver
// adapts a *secrets.Manager[string]; neither package imports the other.
type Resolver interface {
	Resolve(ctx context.Context, ref string) (string, error)
}

// ResolverFunc adapts a function to [Resolver], for example an environment-
// or file-based source in a CLI, or a test double.
type ResolverFunc func(ctx context.Context, ref string) (string, error)

// Resolve calls f(ctx, ref).
func (f ResolverFunc) Resolve(ctx context.Context, ref string) (string, error) {
	return f(ctx, ref)
}

// New creates a new Expander with the specified resolver.
// If resolver is nil, Expand fails with [ErrNoResolver] on content that
// carries placeholders, unless WithFailOnError(false) blanks them.
//
// Parameters:
//   - resolver: The resolver that retrieves secret values
//   - opts: Optional configuration options
//
// Returns a configured Expander instance.
func New(resolver Resolver, opts ...Option) *Expander {
	e := &Expander{options: *newOptions(opts...), resolver: resolver}

	if !e.failOnError {
		e.logger.Warn("secret expander configured in fail-open mode (WithFailOnError(false)): " +
			"unresolved secret placeholders will be replaced with empty strings instead of failing. " +
			"This can silently produce empty credentials and allow unauthenticated access — do not use in production")
	}

	return e
}

// Expand processes a string and replaces all $__secret{namespace:key} placeholders
// with actual secret values retrieved from the resolver.
//
// Parameters:
//   - ctx: Context for cancellation, deadlines, and tracing
//   - content: The string containing secret placeholders to expand
//
// Returns the string with all placeholders replaced, in one pass: a secret's
// value is inserted literally and never scanned for placeholders, even when
// it, alone or with the text around it, looks like one. If a secret cannot
// be resolved, or no resolver is configured ([ErrNoResolver]), Expand fails
// (fail-closed), unless WithFailOnError(false) replaces the placeholder with
// an empty string.
//
// Example:
//
//	expander := secrets.New(resolver)
//	result, err := expander.Expand(ctx, "password: $__secret{myapp:db_password}")
//	// result: "password: actual_secret_value"
func (e *Expander) Expand(ctx context.Context, content string) (string, error) {
	if e.resolver == nil {
		if !HasSecrets(content) {
			return content, nil
		}
		if e.failOnError {
			return "", ErrNoResolver
		}
		e.logger.WarnContext(ctx, "no secret resolver configured; replacing secret placeholders with empty strings")
		return secretPattern.ReplaceAllString(content, ""), nil
	}

	// Find all matches first to avoid issues with replacement affecting indices
	matches := secretPattern.FindAllStringSubmatchIndex(content, -1)
	if len(matches) == 0 {
		return content, nil
	}

	// Build result using strings.Builder for efficiency
	var result strings.Builder
	result.Grow(len(content))

	lastIndex := 0
	for _, match := range matches {
		// match[0:2] - full match
		// match[2:4] - namespace group
		// match[4:6] - key group
		fullMatchStart, fullMatchEnd := match[0], match[1]
		namespaceStart, namespaceEnd := match[2], match[3]
		keyStart, keyEnd := match[4], match[5]

		// Write content before this match
		result.WriteString(content[lastIndex:fullMatchStart])

		// Extract namespace and key
		namespace := content[namespaceStart:namespaceEnd]
		key := content[keyStart:keyEnd]

		// Build the full secret key as "namespace:key"
		secretKey := namespace + ":" + key

		// Retrieve the secret value
		secretValue, err := e.getSecretValue(ctx, secretKey)
		if err != nil {
			// Log the namespace/key (operator-private context) ONCE at
			// debug level. The returned error is intentionally redacted
			// — failOnError propagates this to operator-visible log
			// streams (CI, ops dashboards), and a literal secret name
			// like "vault_unseal_key" or "mongo_root_password" in those
			// streams is information leak even without the secret value.
			e.logger.DebugContext(ctx, "secret expansion target",
				slog.String("namespace", namespace),
				slog.String("key", key))
			if e.failOnError {
				return "", coreerrs.Wrap(err, "secret expansion failed; see debug log for the offending key")
			}
			// Do not include namespace/key here: WARN ships to operator-visible
			// log streams, and a literal secret name is an information leak. The
			// debug log above already captured them for local diagnosis.
			e.logger.WarnContext(ctx, "failed to retrieve secret; see debug log for the offending key",
				slog.Any("error", err))
			secretValue = ""
		}

		// Write the secret value (or empty string)
		result.WriteString(secretValue)

		lastIndex = fullMatchEnd
	}

	// Write remaining content after last match
	result.WriteString(content[lastIndex:])

	return result.String(), nil
}

// getSecretValue retrieves a secret value from the resolver.
// Returns the secret value or an error if retrieval fails.
func (e *Expander) getSecretValue(ctx context.Context, key string) (string, error) {
	return e.resolver.Resolve(ctx, key)
}

// HasSecrets checks if the content contains any secret placeholders.
// This can be used to optimize processing by skipping expansion
// when no placeholders are present.
//
// Parameters:
//   - content: The string to check for secret placeholders
//
// Returns true if the content contains at least one $__secret{...} placeholder.
func HasSecrets(content string) bool {
	return secretPattern.MatchString(content)
}

// References returns the "namespace:key" reference of every placeholder in
// content, in order. A reference names a secret, never its value, so it is
// safe in an error message.
func References(content string) []string {
	matches := secretPattern.FindAllStringSubmatch(content, -1)
	refs := make([]string, 0, len(matches))
	for _, m := range matches {
		refs = append(refs, m[1]+":"+m[2])
	}
	return refs
}

// ExpandString is a convenience function that expands secrets in a string
// using the provided resolver. It creates a temporary Expander instance.
//
// Parameters:
//   - ctx: Context for cancellation, deadlines, and tracing
//   - content: The string containing secret placeholders to expand
//   - resolver: The resolver that retrieves secret values
//
// Returns the string with all placeholders replaced, or [ErrNoResolver] when
// content carries placeholders and resolver is nil.
func ExpandString(ctx context.Context, content string, resolver Resolver) (string, error) {
	if !HasSecrets(content) {
		return content, nil
	}

	expander := New(resolver)
	return expander.Expand(ctx, content)
}
