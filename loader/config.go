// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package loader

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/altessa-s/go-atlas-core/text/strings/intern"
	"github.com/altessa-s/go-atlas-core/types/nilcheck"

	"github.com/altessa-s/go-nomos/loader/backend"

	corefiles "github.com/altessa-s/go-atlas-core/io/files"
)

// DefaultEnvSectionDelimiter is the default delimiter for separating nested struct levels.
const DefaultEnvSectionDelimiter = "__"

const (
	minEnvKeyParts = 2 // Minimum parts for nested structures

	// appEnvTagPrefix is the placeholder prefix an 'env' tag may carry; with
	// a prefix configured it is replaced by that prefix.
	appEnvTagPrefix = "APP_"
)

var (
	// ErrBindDefaults is the error that returns when a default value cannot be bind.
	ErrBindDefaults = errors.New("bind defaults")

	// ErrValidation is wrapped by the error Load returns when the
	// configuration's Validate rejects it.
	ErrValidation = errors.New("config validation failed")

	// ErrBindEnv is the error that returns when an environment value cannot be bind.
	ErrBindEnv = errors.New("bind env")

	// ErrEnvIndexOutOfRange is wrapped by the [ErrBindEnv] error returned when
	// an environment variable names a negative slice index, or one that would
	// grow the slice past the limit set by [WithMaxEnvSliceLen].
	ErrEnvIndexOutOfRange = errors.New("env slice index out of range")

	// ErrDecode is the error that returns when decode failed.
	ErrDecode = errors.New("decode error")

	// ErrUnknownField is wrapped by the [ErrDecode] error returned when a
	// configuration file sets a key that binds to no field of the
	// configuration struct, typically a misspelled key such as "enable" for
	// "enabled". See [WithAllowUnknownFields].
	ErrUnknownField = backend.ErrUnknownField

	// ErrPathOutsideRoot is returned when an entry inside the configured
	// config directory resolves (via symlink) to a target outside that
	// directory. Without confinement a writer with access to the config
	// directory could drop a `something.yaml -> /etc/shadow` symlink and
	// trick the loader into reading it; the loader rejects such paths.
	ErrPathOutsideRoot = errors.New("config path resolves outside the configured root")

	// ErrNotRegularFile is wrapped by the [ErrDecode] error returned for a
	// configuration file or an !include target that is not a regular file
	// (a FIFO, a device, a socket): opening or reading one may block forever
	// or never end. It is core/io/files.ErrNotRegular.
	ErrNotRegularFile = corefiles.ErrNotRegular
)

// Validator defines the interface for configuration validation.
// Implementations should validate configuration values and return an error
// if the configuration is invalid.
type Validator interface {
	Validate() error
}

// Defaulter defines the interface for setting default configuration values.
// Implementations should populate fields with appropriate default values.
type Defaulter interface {
	Default()
}

// Normalizer defines the interface for normalizing configuration values.
// Implementations should transform configuration values to their canonical form.
type Normalizer interface {
	Normalize()
}

// Config represents a configuration loader that can read from files and environment variables.
// It supports multiple backends (YAML, TOML) and provides validation, normalization,
// and default value handling.
//
// Example:
//
//	cfg := loader.New(nil, loader.WithPath("config.yaml"))
//	result, _ := cfg.Load(&MyConfig{})
type Config struct {
	options   *options
	backend   backend.Backend
	envNames  map[reflect.Type][]fieldEnvNames
	smx       sync.RWMutex
	loadSlot  chan struct{}
	published any
	// secretsResolved reports that a load of this loader resolved a secret,
	// whether it succeeded or failed: a later load may keep that value in its
	// destination (a reload from a document that omits it), so its errors are
	// withheld too.
	secretsResolved bool
}

type loadState struct {
	options *options
	files   *files
	// root is the config root (the configured directory, or the directory
	// of a single configured file), held open from discovery until the files
	// are read: the files of a configured directory and every include are
	// read through it. rootDir is its path as resolved at discovery.
	root      *os.Root
	rootDir   string
	conf      any
	backend   backend.Backend
	confType  reflect.Type
	fields    fields
	allowExts map[string]struct{}
	// present records the values the configuration files and the environment
	// set during the current load; defaults are not applied to them.
	present *presence
	// envNames caches the environment naming of the fields of each struct
	// type across loads (see structEnvNames); it is used under smx.
	envNames map[reflect.Type][]fieldEnvNames
	// defaulting holds the pointers whose structs applyDefaults is walking,
	// and those of the structs in aliasPresence it has walked.
	defaulting map[ptrKey]struct{}
	// aliasPresence holds, while defaults are applied, the merged presence
	// of each struct reached through more than one path.
	aliasPresence map[ptrKey]*presence
	// envTagged lists the value fields of fields with an `env` tag, with the
	// variable names their tags give, while the environment is loaded.
	envTagged []taggedField
	// secretsResolved reports that the current load, or an earlier load of
	// the loader, resolved a secret, so an error leaving Load is withheld
	// (see withholdError).
	secretsResolved bool
}

// Load loads configuration from files and environment variables into the provided struct.
// The conf parameter must be a pointer to a struct.
// Returns the populated configuration or an error if loading fails.
// It is [Config.LoadContext] with context.Background.
//
// Example:
//
//	cfg := &AppConfig{}
//	result, err := loader.Load(cfg)
func (cf *Config) Load(conf any) (any, error) {
	return cf.LoadContext(context.Background(), conf)
}

// LoadContext is [Config.Load] bounded by ctx: an ended ctx fails the load
// before any source is read (also while it waits for another load of this
// loader to finish), and every secret lookup runs under ctx capped at the
// secrets timeout (5 seconds per lookup), so canceling ctx stops a remote
// lookup in progress.
func (cf *Config) LoadContext(ctx context.Context, conf any) (any, error) {
	if err := assertStructPointer(conf); err != nil {
		return nil, err
	}
	select {
	case cf.loadSlot <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-cf.loadSlot }()

	// The context may have ended while this load waited.
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	cf.smx.Lock()
	defer cf.smx.Unlock()
	rollback := captureConfig(cf.published)
	committed := false
	defer func() {
		if !committed {
			rollback()
		}
	}()
	destination := reflect.ValueOf(conf)
	if destination.IsNil() {
		destination = reflect.New(destination.Type().Elem())
	}
	work := &loadState{
		options: cf.options, backend: cf.backend,
		allowExts: make(map[string]struct{}), envNames: cf.envNames,
		confType: destination.Type(), conf: destination.Interface(),
		secretsResolved: cf.secretsResolved,
	}
	err := work.load(ctx)
	cf.secretsResolved = work.secretsResolved
	if err != nil {
		return nil, work.withholdError(err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cf.envNames = work.envNames
	cf.published = destination.Interface()
	committed = true
	return cf.published, nil
}

// Config returns the current loaded configuration.
// This method is thread-safe and returns the configuration that was last successfully loaded.
func (cf *Config) Config() any {
	cf.smx.RLock()
	defer cf.smx.RUnlock()

	return cf.published
}

// load performs the actual configuration loading process.
// It reads from files, applies defaults, loads environment variables, and runs validation.
func (cf *loadState) load(ctx context.Context) error {
	var err error

	cf.present = nil
	defer func() {
		cf.present = nil
	}()

	// load configuration from file(s). Discovery starts from an empty
	// collection on every load so files removed since the last one are gone.
	if cf.options.path != "" {
		cf.files = newFiles()
		err = cf.loadFiles()
		if err == nil {
			err = cf.loadFileContents(ctx)
		}
		if cf.root != nil {
			err = errors.Join(err, cf.root.Close())
			cf.root, cf.rootDir = nil, ""
		}
		if err != nil {
			return err
		}
	}
	if cf.present == nil {
		cf.present = &presence{}
	}

	// Apply the environment, recording what it sets, then the defaults, which
	// skip every value a file or the environment set.
	if err = ctx.Err(); err != nil {
		return err
	}
	cf.fields = structFields(cf.conf)
	if err = cf.loadEnvs(ctx); err != nil {
		return fmt.Errorf("%w: %w", ErrBindEnv, err)
	}
	if err = ctx.Err(); err != nil {
		return err
	}

	if err = cf.applyDefaults(ctx); err != nil {
		return fmt.Errorf("%w: %w", ErrBindDefaults, err)
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	cf.fields = structFields(cf.conf)

	if cv, ok := cf.conf.(Validator); ok {
		if err = cv.Validate(); err != nil {
			return fmt.Errorf("%w for %s: %w", ErrValidation,
				reflect.TypeOf(cf.conf).Elem().Name(), err)
		}
	}

	// Apply normalization to the root configuration object
	if n, ok := cf.conf.(Normalizer); ok {
		n.Normalize()
	}

	for f := range cf.fields.All() {
		if f.isStructPtr() && nilcheck.IsNotNilValue(f.value) {
			if n, ok := reflect.TypeAssert[Normalizer](f.value); ok {
				n.Normalize()
			}
		}
	}

	return nil
}

// reusesSlices reports whether the backend decodes a later file into an
// existing slice element by element when it is long enough, as
// BurntSushi/toml does, instead of replacing it.
func (cf *loadState) reusesSlices() bool {
	policy, ok := cf.backend.(backend.SliceReuseDecoder)
	return ok && policy.ReusesSliceElements()
}

// loadFileContents reads the files, resolves the secret placeholders of their
// scalars (see expandFileSecrets) and decodes them, in order, into the
// configuration, recording what each sets.
func (cf *loadState) loadFileContents(ctx context.Context) error {
	ordered := slices.Collect(cf.files.All())
	for _, f := range ordered {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := cf.readContent(f); err != nil {
			return err
		}
	}

	if cf.options.secretResolver != nil {
		if err := cf.expandFileSecrets(ctx, ordered); err != nil {
			return err
		}
	}

	// The decoder writes a file into the struct, not the pointer, so a
	// null document leaves it as it is.
	rootType := indirectType(cf.confType)
	for _, f := range ordered {
		if err := cf.decodeContent(f, cf.conf); err != nil {
			return err
		}
		cf.present = mergePresence(cf.present, bindPresence(rootType, f.keys), rootType, cf.reusesSlices())
	}
	return nil
}

// loadFiles loads one or multiple files if path is directory.
// It handles both single file paths and directory paths containing multiple config files.
func (cf *loadState) loadFiles() (err error) {
	var basePath string

	basePath, err = fixPath(cf.options.path)
	if err != nil {
		return
	}

	var pathInfo os.FileInfo

	if pathInfo, err = os.Stat(basePath); err != nil {
		if os.IsNotExist(err) {
			// return error for non-existent paths
			return
		}

		return err
	}

	// if a directory path is specified, load all valid configuration files
	// from that directory.
	if pathInfo.IsDir() {
		return cf.loadFilesFromDir(basePath)
	}

	// A single configured file is read by its path; its includes are read
	// through its directory.
	if err = cf.openRoot(filepath.Dir(basePath)); err != nil {
		return err
	}
	return cf.loadFile(basePath, "", pathInfo)
}

// openRoot opens dir as the config root and records its resolved path.
func (cf *loadState) openRoot(dir string) error {
	// A FIFO swapped in for the directory fails at once instead of blocking.
	root, err := corefiles.OpenRootDir(dir)
	if err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		_ = root.Close()
		return err
	}
	cf.root, cf.rootDir = root, resolved
	return nil
}

// loadFilesFromDir loads all valid configuration files from the specified directory.
// It iterates through directory contents and loads each valid configuration file.
//
// Symlink targets are resolved and confined to basePath: an entry whose
// resolved path escapes the configured root is rejected with
// [ErrPathOutsideRoot] rather than silently loaded. Without this check a
// writer with access to the config directory could drop e.g.
// `creds.yaml -> /etc/shadow` and the loader would decode whatever the
// link points at.
func (cf *loadState) loadFilesFromDir(basePath string) error {
	if err := cf.openRoot(basePath); err != nil {
		return err
	}

	for entry, err := range corefiles.Walk(basePath) {
		if err != nil {
			return err
		}

		itemInfo, err := entry.Info()
		if err != nil {
			return err
		}

		entryPath := path.Join(basePath, entry.Name())
		rel, err := confineToRoot(entryPath, basePath)
		if err != nil {
			return err
		}

		if err := cf.loadFile(entryPath, rel, itemInfo); err != nil {
			return err
		}
	}

	return nil
}

// confineToRoot rejects path values that — after resolving symlinks —
// land outside root, and returns the resolved path relative to the resolved
// root. Both arguments are resolved through [filepath.EvalSymlinks] so a
// chain of symlinks pointing out of the config directory is caught
// regardless of how many hops the link makes.
//
// The returned error wraps [ErrPathOutsideRoot]; callers should use
// `errors.Is(err, ErrPathOutsideRoot)` to detect the case.
func confineToRoot(path, root string) (string, error) {
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		// A broken symlink (target deleted) or denied access is itself a
		// reason not to proceed; surface the underlying error wrapped in
		// our sentinel so the caller can react uniformly.
		return "", fmt.Errorf("%w: %s: %w", ErrPathOutsideRoot, path, err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("%w: %s: %w", ErrPathOutsideRoot, root, err)
	}

	rel, err := filepath.Rel(resolvedRoot, resolvedPath)
	if err != nil {
		return "", fmt.Errorf("%w: %s: %w", ErrPathOutsideRoot, path, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %s -> %s", ErrPathOutsideRoot, path, resolvedPath)
	}
	return rel, nil
}

// loadFile loads a single configuration file if it's valid.
// It checks file validity and adds it to the internal files collection.
//
// rel is the resolved path of a file of the configured directory, relative to
// that directory, through which the file is opened; it is empty for a single
// configured file.
func (cf *loadState) loadFile(filePath, rel string, fileInfo os.FileInfo) (err error) {
	if fileInfo.IsDir() || !cf.isValidConfigFile(fileInfo.Name()) {
		return nil
	}

	_, fileName := filepath.Split(filePath)

	cf.files.add(&file{
		name:    fileName,
		path:    filePath,
		rel:     rel,
		decoder: cf.backend,
	})

	return
}

// isValidConfigFile checks if the given filename has a valid configuration file extension.
// It uses the backend's supported file extensions to determine validity.
func (cf *loadState) isValidConfigFile(fileName string) bool {
	// Initialize allowExts if empty
	if len(cf.allowExts) == 0 {
		for _, ext := range cf.backend.FileExtensions() {
			cf.allowExts["."+intern.LowerString(strings.TrimLeft(ext, "."))] = struct{}{}
		}
	}

	_, ok := cf.allowExts[intern.LowerString(filepath.Ext(fileName))]

	return ok
}

// fixPath normalizes path to config file.
// It expands '~' at the beginning of path to the home directory.
// It expands '$NAME' to the corresponding OS environment variable value.
func fixPath(filePath string) (string, error) {
	if strings.HasPrefix(filePath, "~") {
		filePath = filePath[1:]
		filePath = path.Join(userHomeDir() + filePath)

		goto abs
	}

	if strings.Contains(filePath, "$") {
		filePath = substituteEnvVariables(filePath)
	}

abs:
	if !filepath.IsAbs(filePath) {
		var err error
		if filePath, err = filepath.Abs(filePath); err != nil {
			return "", err
		}
	}

	return filepath.Clean(filePath), nil
}
