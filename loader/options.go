// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package loader

//go:generate optgen generate --type=options

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/altessa-s/go-atlas-core/runtime/appinfo"

	loadersecrets "github.com/altessa-s/go-nomos/loader/secrets"
)

// defaultSecretsTimeout is the default timeout for secrets expansion operations.
const defaultSecretsTimeout = 5 * time.Second

// DefaultStructTagName is the default struct tag name for configuration field mapping.
const DefaultStructTagName = "yaml"

// DefaultMaxConfigBytes caps how many bytes the loader will read from a
// single configuration file. The same cap bounds every !include target and
// the document after include expansion. Without a cap, a symlink pointing to
// `/dev/zero`, a multi-GB tmpfs file, or a malicious include target
// could OOM the loader before the YAML parser ever rejected the input.
//
// 16 MiB is generous compared to realistic config files (typical
// service configs are a few KB to a few hundred KB) but tight enough
// that the worst-case read is bounded to a memory size the loader
// process can safely handle even on small instances.
const DefaultMaxConfigBytes int64 = 16 * 1024 * 1024

// DefaultMaxEnvSliceLen caps the length an environment variable can grow a
// slice to by its index: APP_HOSTS__1023 is accepted, APP_HOSTS__1024 fails
// the load with [ErrEnvIndexOutOfRange] unless the slice already holds that
// element. Without a cap, APP_HOSTS__1000000000 would allocate a billion
// elements before any value is assigned.
const DefaultMaxEnvSliceLen = 1024

const (
	defaultValueTagName = "default"
	envTagName          = "env"
)

type options struct {
	path string
	// envPrefix selects the environment variables the loader reads: when it
	// is set, a variable without it is ignored, and it is stripped before
	// the rest of the name is matched. It is normalized by
	// appinfo.NormalizeEnvKeyPrefix (uppercased, "-" and "." replaced by
	// "_", exactly one trailing "_"), so "app", "APP" and "APP_" all select
	// APP_*.
	envPrefix           string
	envSectionDelimiter string `optgen:"default=DefaultEnvSectionDelimiter"`
	structTag           string `optgen:"default=DefaultStructTagName"`
	maxConfigBytes      int64  `optgen:"default=DefaultMaxConfigBytes"`
	skipEnv             bool
	skipDefaults        bool
	strict              bool
	secretResolver      loadersecrets.Resolver
	// allowUnknownFields disables the default rejection of file keys that
	// bind to no field of the configuration struct.
	allowUnknownFields bool
	// maxEnvSliceLen caps the length an indexed environment variable can grow
	// a slice to; an index inside the slice's current length is always
	// accepted.
	maxEnvSliceLen int `optgen:"default=DefaultMaxEnvSliceLen"`
}

// WithPathOnEnvKey sets the config path from an environment variable.
// If the environment variable exists, its value is used as the path.
// Otherwise, the defaultPath is used.
func WithPathOnEnvKey(envKey, defaultPath string) Option {
	return func(opt *options) {
		if cfgFile, ok := os.LookupEnv(envKey); ok {
			WithPath(cfgFile)(opt)
			return
		}

		WithPath(defaultPath)(opt)
	}
}

func (o *options) normalization() {
	// Keep previous behavior: empty path means "unset". If set, clean it.
	if o.path != "" {
		o.path = filepath.Clean(o.path)
	}
	o.envPrefix = appinfo.NormalizeEnvKeyPrefix(o.envPrefix)
}

// getSecretsContext returns the context of one secrets operation: parent
// (the load's context) capped at defaultSecretsTimeout (5 seconds), so the
// caller's cancellation and an earlier deadline still apply. The returned
// cancel function should be called when the operation completes.
func (o *options) getSecretsContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, defaultSecretsTimeout)
}
