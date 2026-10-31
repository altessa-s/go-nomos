// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package conversion transforms configuration data independently of CLI and I/O.
//
// # Rendering
//
// Renderers accept data and optional ports rather than concrete file or HTTP clients:
//
//	text := (&conversion.Env{}).Render(map[string]any{"port": 8080}, nil, false)
//
// # Parsing
//
// Parser reads environment assignments from an io.Reader. Merge obtains source
// documents through a caller-supplied reader and applies lexical precedence.
package conversion
