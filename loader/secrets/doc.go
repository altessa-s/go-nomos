// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package secrets provides secret expansion functionality for configuration loading.
// It enables automatic substitution of secret placeholders in configuration structs
// and environment variables using the $__secret{namespace:key} syntax.
//
// Values come from a [Resolver]. The package does not depend on any secret
// store: security/secrets.NewStringResolver adapts a *secrets.Manager[string]
// (HashiCorp Vault, Google Cloud Secret Manager, Yandex Cloud Lockbox or
// in-memory storage), and [ResolverFunc] adapts any other source.
//
// # Syntax
//
// Secret placeholders use the following syntax:
//
//	$__secret{namespace:key}
//
// Where:
//   - namespace: A logical grouping for secrets (e.g., "myapp", "database", "api")
//   - key: The specific secret identifier within the namespace
//
// The namespace and key are combined into a single lookup key "namespace:key"
// that is passed to the [Resolver].
//
// # Configuration File Example
//
//	database:
//	  host: ${DB_HOST}
//	  password: $__secret{myapp:db_password}
//	  api_key: $__secret{external:stripe_api_key}
//
// # Environment Variable Example
//
//	export API_KEY="$__secret{myapp:api_key}"
//	export DB_PASSWORD="prefix_$__secret{database:password}_suffix"
//
// # Expansion at the Source
//
// The configuration loader resolves placeholders where a value is taken from
// its source, before it is parsed: a file scalar in the parsed document, an
// environment value when it is assigned, a default tag when it is applied.
// Each source value is expanded once with [Expander.Expand], which inserts a
// secret's value literally and never scans it again.
//
// # Error Handling
//
// When a secret is not found, an error occurs during retrieval, or no
// resolver is configured ([ErrNoResolver]), Expand fails (fail-closed). With
// WithFailOnError(false) the placeholder is replaced with an empty string, a
// warning is logged and processing continues with the remaining placeholders;
// the application then decides how to handle the empty values.
//
// # Thread Safety
//
// The Expander is thread-safe and can be used concurrently from multiple goroutines.
// It requires the [Resolver] to be safe for concurrent use as well;
// security/secrets.StringResolver is.
//
// # Performance
//
// Caching belongs to the resolver: security/secrets.StringResolver serves
// from the manager's cache and falls back to the storage provider on a miss.
package secrets
