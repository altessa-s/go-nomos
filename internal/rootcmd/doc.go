// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package rootcmd assembles Cobra command trees with generated functional options.
//
// # Execution
//
// Run preserves cancellation and recovers command panics:
//
//	err := rootcmd.Run(ctx, "tool", args, rootcmd.WithShort("Configuration tool"))
package rootcmd
