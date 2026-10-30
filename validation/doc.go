// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package validationconfig defines shared validation helpers for configuration
// schemas: struct validation with readable errors and storage-selector checks.
//
// Schemas carry yaml and default tags read by
// github.com/altessa-s/go-nomos/loader, expose Default* constructors
// where defaults exist, and implement Validate; component factories map them
// to generated options.
//
// # Nested schemas
//
// validation.Field skips a nested schema whose Validate has a pointer
// receiver when the schema is held by value, embedded, or an element of a
// slice, and it panics on a nil entry of a map or slice of pointers. Use
// [NestedField], [NestedEach] and [NotNilEntries] for those fields.
//
// Key types: [StorageCase].
//
// # Usage
//
//	err := validationconfig.ValidateStruct(cfg, validation.Field(&cfg.Name, validation.Required))
package validationconfig
