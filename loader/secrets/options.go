// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package secrets

//go:generate optgen generate --type=options

import "log/slog"

// DefaultFailOnError is the default expansion error mode: fail closed.
const DefaultFailOnError = true

// options configures an [Expander].
type options struct {
	// logger ([WithLogger]) logs secret retrieval failures. A nil logger is
	// ignored; the default is [slog.Default].
	logger *slog.Logger `optgen:"default=slog.Default(),notnil"`
	// failOnError ([WithFailOnError]) controls whether secret expansion errors
	// cause Expand to return an error (fail-closed) or replace the
	// placeholder with an empty string (fail-open). Fail-open logs a warning
	// when the Expander is created; Expand then logs one warning for each
	// placeholder whose secret cannot be retrieved or, without a resolver,
	// one warning per call that blanks its placeholders. Default is true
	// (fail-closed) to prevent authorization bypass when secrets fail to
	// resolve.
	failOnError bool `optgen:"default=DefaultFailOnError" optval:"param"`
}
