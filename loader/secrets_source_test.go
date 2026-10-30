// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package loader_test

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-atlas-core/types/redacted"

	"github.com/altessa-s/go-nomos/loader"
	"github.com/altessa-s/go-nomos/loader/backend"
	"github.com/altessa-s/go-nomos/loader/backend/toml"
	"github.com/altessa-s/go-nomos/loader/backend/yaml3"

	coreerrs "github.com/altessa-s/go-atlas-core/errors"
	validationconfig "github.com/altessa-s/go-nomos/validation"
	validation "github.com/go-ozzo/ozzo-validation/v4"
)

// countingManager serves fixed secrets and counts every lookup by key.
type countingManager struct {
	mu      sync.Mutex
	values  map[string]string
	lookups map[string]int
}

func newCountingManager(values map[string]string) *countingManager {
	return &countingManager{values: values, lookups: map[string]int{}}
}

func (m *countingManager) Resolve(_ context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lookups[key]++
	v, ok := m.values[key]
	if !ok {
		return "", coreerrs.ErrNotFound
	}
	return strings.Clone(v), nil
}

func (m *countingManager) count(key string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lookups[key]
}

// collectionConfig takes whole collections from one value.
type collectionConfig struct {
	Headers map[string]string `yaml:"headers"`
	Hosts   []string          `yaml:"hosts"`
	Ports   []int             `yaml:"ports"`
}

// A whole map or slice may come from one secret: the secret supplies the
// serialized collection, which is then parsed; a missing one fails the load
// instead of vanishing when the placeholder is split. It mutates the
// environment, so it is not parallel.
func TestLoad_WholeCollectionSecrets(t *testing.T) {
	manager := newCountingManager(map[string]string{
		"app:headers": "authorization:Bearer t0k3n,x-tenant:$__secret{app:other}",
		"app:hosts":   "a.example,b.example",
		"app:ports":   "80,443",
	})
	t.Setenv("SECRETWHOLETEST_HEADERS", "$__secret{app:headers}")
	t.Setenv("SECRETWHOLETEST_HOSTS", "$__secret{app:hosts}")
	t.Setenv("SECRETWHOLETEST_PORTS", "$__secret{app:ports}")

	cfg := &collectionConfig{}
	_, err := loader.New(&yaml3.Backend{}, loader.WithEnvPrefix("SECRETWHOLETEST"), loader.WithSecretResolver(manager)).Load(cfg)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"authorization": "Bearer t0k3n", "x-tenant": "$__secret{app:other}"}, cfg.Headers)
	require.Equal(t, []string{"a.example", "b.example"}, cfg.Hosts)
	require.Equal(t, []int{80, 443}, cfg.Ports)
	require.Zero(t, manager.count("app:other"), "elements parsed from a secret are literal")

	for _, name := range []string{"HEADERS", "HOSTS", "PORTS"} {
		t.Run(name, func(t *testing.T) {
			prefix := "SECRETWHOLEMISSING" + name + "TEST"
			t.Setenv(prefix+"_"+name, "$__secret{app:missing}")
			_, err := loader.New(&yaml3.Backend{}, loader.WithEnvPrefix(prefix), loader.WithSecretResolver(manager)).Load(&collectionConfig{})
			require.ErrorIs(t, err, coreerrs.ErrNotFound)
		})
	}
}

// typedConfig takes typed values from file secrets.
type typedConfig struct {
	Port    int               `yaml:"port"`
	Enabled bool              `yaml:"enabled"`
	Timeout time.Duration     `yaml:"timeout"`
	Ratio   float64           `yaml:"ratio"`
	Name    string            `yaml:"name"`
	Quoted  string            `yaml:"quoted"`
	Any     any               `yaml:"any"`
	Params  map[string]any    `yaml:"params"`
	Labels  map[string]string `yaml:"labels"`
}

// File scalars resolve before decoding into their type: numbers, bools and
// durations parse from the secret, strings and interfaces keep it literal,
// even when it reads as YAML (null, a number, a placeholder, control
// characters). Map keys are not expanded.
func TestLoad_FileSecretsBeforeDecoding(t *testing.T) {
	t.Parallel()

	manager := newCountingManager(map[string]string{
		"app:port": "8080", "app:on": "true", "app:timeout": "3s", "app:ratio": "0.5",
		"app:null": "null", "app:num": "123", "app:fake": "$__secret{app:never}", "app:ctl": "a\x00\"b\n$c: d",
	})
	content := `port: $__secret{app:port}
enabled: $__secret{app:on}
timeout: $__secret{app:timeout}
ratio: $__secret{app:ratio}
name: $__secret{app:null}
quoted: "$__secret{app:num}"
any: $__secret{app:fake}
params:
  nested: ["$__secret{app:ctl}", "$__secret{app:num}"]
labels:
  $__secret{app:port}: keyed
`
	cfg := &typedConfig{}
	require.NoError(t, loadFiles(t, nil, cfg, map[string]string{"a.yaml": content}, loader.WithSecretResolver(manager)))
	require.Equal(t, 8080, cfg.Port)
	require.True(t, cfg.Enabled)
	require.Equal(t, 3*time.Second, cfg.Timeout)
	require.InDelta(t, 0.5, cfg.Ratio, 0)
	require.Equal(t, "null", cfg.Name)
	require.Equal(t, "123", cfg.Quoted)
	require.Equal(t, "$__secret{app:never}", cfg.Any)
	require.Equal(t, []any{"a\x00\"b\n$c: d", "123"}, cfg.Params["nested"])
	require.Equal(t, map[string]string{"$__secret{app:port}": "keyed"}, cfg.Labels)
	require.Zero(t, manager.count("app:never"))
}

// A placeholder anywhere inside an anchored node (scalar, mapping, sequence,
// at any depth) or a "<<" merge is rejected with ErrSecretInAnchor naming the
// line and the reference, never a value, and nothing is looked up; plain
// anchors and placeholders outside anchors keep working.
func TestLoad_FileSecretAnchors(t *testing.T) {
	t.Parallel()

	type intValue struct {
		Value int `yaml:"value"`
	}
	type strValue struct {
		Value string `yaml:"value"`
	}
	type cfgT struct {
		Base  string            `yaml:"base"`
		A     string            `yaml:"a"`
		Num   intValue          `yaml:"num"`
		Str   strValue          `yaml:"str"`
		Any   map[string]any    `yaml:"any"`
		List  []string          `yaml:"list"`
		Other []string          `yaml:"other"`
		M     map[string]string `yaml:"m"`
		Plain string            `yaml:"plain"`
	}
	manager := newCountingManager(map[string]string{"app:pw": "8080"})

	for name, content := range map[string]string{
		"scalar":         "base: &pw $__secret{app:pw}\na: *pw\n",
		"mapping":        "num: &shared\n  value: $__secret{app:pw}\nstr: *shared\nany: *shared\n",
		"deep mapping":   "any: &deep\n  inner:\n    value: $__secret{app:pw}\nm: {}\n",
		"sequence":       "list: &seq\n  - $__secret{app:pw}\nother: *seq\n",
		"merge key":      "base: x\nm:\n  <<: {k: \"$__secret{app:pw}\"}\n",
		"merged mapping": "any: &tpl {k: v}\nm:\n  <<: *tpl\n  k2: v2\nnum:\n  <<: {value: \"$__secret{app:pw}\"}\n",
	} {
		t.Run(name, func(t *testing.T) {
			err := loadFiles(t, nil, &cfgT{}, map[string]string{"a.yaml": content}, loader.WithSecretResolver(manager))
			require.ErrorIs(t, err, loader.ErrSecretInAnchor)
			require.Contains(t, err.Error(), "app:pw")
			require.NotContains(t, err.Error(), "8080")
		})
	}
	require.Zero(t, manager.count("app:pw"))

	cfg := &cfgT{}
	require.NoError(t, loadFiles(t, nil, cfg, map[string]string{
		"a.yaml": "base: &plain value\na: *plain\nplain: $__secret{app:pw}\nnum:\n  value: $__secret{app:pw}\n",
	}, loader.WithSecretResolver(manager)))
	require.Equal(t, "value", cfg.A)
	require.Equal(t, "8080", cfg.Plain)
	require.Equal(t, 8080, cfg.Num.Value)
}

// Every placeholder of every file resolves, also one a later file replaces:
// a missing one fails the load; a resolvable one is resolved, then replaced
// (a map merges).
func TestLoad_LaterFileOverridesFileSecret(t *testing.T) {
	t.Parallel()

	type cfgT struct {
		Password string            `yaml:"password"`
		Hosts    []string          `yaml:"hosts"`
		M        map[string]string `yaml:"m"`
	}
	manager := newCountingManager(map[string]string{"app:x": "resolved", "app:old": "old"})
	err := loadFiles(t, nil, &cfgT{}, map[string]string{
		"a.yaml": "password: $__secret{app:missing}\n",
		"b.yaml": "password: literal\n",
	}, loader.WithSecretResolver(manager))
	require.ErrorIs(t, err, coreerrs.ErrNotFound)

	cfg := &cfgT{}
	require.NoError(t, loadFiles(t, nil, cfg, map[string]string{
		"a.yaml": "password: $__secret{app:old}\nhosts: [\"$__secret{app:old}\"]\nm:\n  x: $__secret{app:x}\n  y: $__secret{app:old}\n",
		"b.yaml": "password: literal\nhosts: [h]\nm: {y: lit}\n",
	}, loader.WithSecretResolver(manager)))
	require.Equal(t, "literal", cfg.Password)
	require.Equal(t, []string{"h"}, cfg.Hosts)
	require.Equal(t, map[string]string{"x": "resolved", "y": "lit"}, cfg.M)
}

// A backend that cannot resolve placeholders at the source fails a file
// holding one instead of loading it unresolved.
func TestLoad_FileSecretsUnsupportedBackend(t *testing.T) {
	t.Parallel()

	type cfgT struct {
		Password string `toml:"password"`
	}
	manager := newCountingManager(map[string]string{"app:x": "v"})
	err := loadFiles(t, &toml.Backend{}, &cfgT{}, map[string]string{"a.toml": "password = \"$__secret{app:x}\"\n"},
		loader.WithSecretResolver(manager))
	require.ErrorIs(t, err, loader.ErrSecretsUnsupported)
	require.NoError(t, loadFiles(t, &toml.Backend{}, &cfgT{}, map[string]string{"a.toml": "password = \"plain\"\n"},
		loader.WithSecretResolver(manager)))
}

// defaultedCollections take collections from secrets in default tags.
type defaultedCollections struct {
	Headers map[string]string `yaml:"headers" default:"$__secret{app:headers}"`
	Marker  string            `yaml:"marker" default:"$__secret{app:marker}"`
}

// A default tag resolves once, before parsing: a secret supplies a whole map,
// and payloads that look like placeholders or like any in-band marker stay
// literal, from a default tag or the environment, through aliases of one
// storage. It mutates the environment, so it is not parallel.
func TestLoad_DefaultAndEnvPayloadsStayLiteral(t *testing.T) {
	manager := newCountingManager(map[string]string{
		"app:headers": "k:$__secret{app:other}",
		"app:marker":  "\x00ATL_RESOLVED\x00$__secret{app:other}",
	})
	cfg := &defaultedCollections{}
	_, err := loader.New(&yaml3.Backend{}, loader.WithEnvPrefix("SECRETDEFAULTCOLLTEST"), loader.WithSecretResolver(manager)).Load(cfg)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"k": "$__secret{app:other}"}, cfg.Headers)
	require.Equal(t, "\x00ATL_RESOLVED\x00$__secret{app:other}", cfg.Marker)

	t.Setenv("SECRETENVMARKERTEST_MARKER", "$__secret{app:marker}")
	s := "aliased"
	aliased := &struct {
		Marker *string `yaml:"marker"`
		Alias  *string `yaml:"alias"`
	}{Marker: &s, Alias: &s}
	_, err = loader.New(&yaml3.Backend{}, loader.WithEnvPrefix("SECRETENVMARKERTEST"), loader.WithSecretResolver(manager)).Load(aliased)
	require.NoError(t, err)
	require.Equal(t, "\x00ATL_RESOLVED\x00$__secret{app:other}", *aliased.Alias)
	require.Zero(t, manager.count("app:other"))
}

// A typed file secret resolves and parses before the environment overrides
// it. It mutates the environment, so it is not parallel.
func TestLoad_EnvOverridesTypedFileSecret(t *testing.T) {
	type cfgT struct {
		Port    int           `yaml:"port"`
		Timeout time.Duration `yaml:"timeout"`
	}
	manager := newCountingManager(map[string]string{"app:port": "8080", "app:t": "2s"})
	t.Setenv("SECRETTYPEDOVERTEST_PORT", "9090")
	cfg := &cfgT{}
	require.NoError(t, loadFiles(t, nil, cfg, map[string]string{"a.yaml": "port: $__secret{app:port}\ntimeout: $__secret{app:t}\n"},
		loader.WithEnvPrefix("SECRETTYPEDOVERTEST"), loader.WithSecretResolver(manager)))
	require.Equal(t, 9090, cfg.Port)
	require.Equal(t, 2*time.Second, cfg.Timeout)
	require.Equal(t, 1, manager.count("app:port"))
}

// leakySecret is a secret value that parses as no number, bool or duration;
// its characters appear in no loader message.
const leakySecret = "Ωζ§¤µ‰"

// requireNoSecret fails when err quotes the secret or any prefix of it.
func requireNoSecret(t testing.TB, err error) {
	t.Helper()
	requireNoneOf(t, err, leakySecret)
}

// requireNoneOf fails when err, through Error() or %+v, shows any of the
// secrets in any form: raw, %q-escaped or hex, whole or by a prefix of at
// least four bytes, or, for a secret with one, the part after its first ':'
// (a map entry parsed from it).
func requireNoneOf(t testing.TB, err error, secretValues ...string) {
	t.Helper()
	require.Error(t, err)
	shown := err.Error() + "\n" + fmt.Sprintf("%+v", err) + "\n" + fmt.Sprintf("%v", err) + "\n" + fmt.Sprintf("%s", err)
	for _, secret := range secretValues {
		forms := []string{secret, strconv.Quote(secret), fmt.Sprintf("%q", secret)[1:], hex.EncodeToString([]byte(secret))}
		if _, after, ok := strings.Cut(secret, ":"); ok {
			forms = append(forms, after)
		}
		for _, form := range forms {
			for i := min(4, len(form)); i <= len(form); i++ {
				require.NotContains(t, shown, form[:i], "the error shows a form of a secret")
			}
		}
	}
}

// quotingType quotes the value it rejects, as many unmarshalers do.
type quotingType struct{}

// UnmarshalYAML rejects every value, quoting it.
func (*quotingType) UnmarshalYAML(decode func(any) error) error {
	var value string
	if err := decode(&value); err != nil {
		return err
	}
	return fmt.Errorf("quotingType: bad value %q", value)
}

// A value resolved from a secret that does not decode or parse is reported
// with the placeholder and its position, never the value or a prefix of it:
// file numbers, bools, durations and custom unmarshalers, environment
// numbers, durations and slices, and default tags. It mutates the
// environment, so it is not parallel.
func TestLoad_SecretValuesNeverInErrors(t *testing.T) {
	manager := newCountingManager(map[string]string{"app:bad": leakySecret})
	opts := func(prefix string) []loader.Option {
		return []loader.Option{loader.WithEnvPrefix(prefix), loader.WithSecretResolver(manager)}
	}

	type fileCfg struct {
		Port    int           `yaml:"port"`
		On      bool          `yaml:"on"`
		Timeout time.Duration `yaml:"timeout"`
		Custom  quotingType   `yaml:"custom"`
	}
	for _, field := range []string{"port", "on", "timeout", "custom"} {
		t.Run("file "+field, func(t *testing.T) {
			err := loadFiles(t, nil, &fileCfg{}, map[string]string{"a.yaml": field + ": $__secret{app:bad}\n"}, opts("SECRETLEAKFILETEST")...)
			require.ErrorIs(t, err, loader.ErrSecretValueInvalid)
			require.ErrorIs(t, err, loader.ErrDecode)
			requireNoSecret(t, err)
		})
	}

	type envCfg struct {
		Port    int            `yaml:"port"`
		Timeout time.Duration  `yaml:"timeout"`
		Ports   []int          `yaml:"ports"`
		Hosts   map[string]int `yaml:"hosts"`
	}
	for _, name := range []string{"PORT", "TIMEOUT", "PORTS", "HOSTS"} {
		t.Run("env "+name, func(t *testing.T) {
			prefix := "SECRETLEAKENV" + name + "TEST"
			t.Setenv(prefix+"_"+name, "$__secret{app:bad}")
			_, err := loader.New(&yaml3.Backend{}, opts(prefix)...).Load(&envCfg{})
			require.ErrorIs(t, err, loader.ErrSecretValueInvalid)
			requireNoSecret(t, err)
		})
	}

	t.Run("default tag", func(t *testing.T) {
		type defaultCfg struct {
			Port  int   `yaml:"port" default:"$__secret{app:bad}"`
			Ports []int `yaml:"ports" default:"$__secret{app:bad}"`
		}
		_, err := loader.New(&yaml3.Backend{}, opts("SECRETLEAKDEFAULTTEST")...).Load(&defaultCfg{})
		require.ErrorIs(t, err, loader.ErrSecretValueInvalid)
		requireNoSecret(t, err)
	})
}

// errRejected is the sentinel quotingValidator wraps.
var errRejected = errors.New("rejected")

// quotingValidator quotes its field's value when Validate rejects it, as many
// validators do; Mode selects what it quotes.
type quotingValidator struct {
	Token string `yaml:"token"`
	Mode  string `yaml:"mode"`
}

// Validate quotes the token, a prefix of it, or nothing.
func (v *quotingValidator) Validate() error {
	switch v.Mode {
	case "prefix":
		return fmt.Errorf("token starting %q: %w", string([]rune(v.Token)[:2]), errRejected)
	case "safe":
		return fmt.Errorf("token: %w", errRejected)
	default:
		return fmt.Errorf("token %q: %w", v.Token, errRejected)
	}
}

// Once a secret was resolved, every error Load returns has its message
// withheld, whatever the validator quotes (the value, a prefix, nothing),
// and errors.Is still matches the sentinel; the secret comes from a file,
// the environment or a default tag. It mutates the environment, so it is
// not parallel.
func TestLoad_ErrorsWithheldAfterSecrets(t *testing.T) {
	manager := newCountingManager(map[string]string{"app:token": leakySecret})

	for _, mode := range []string{"quote", "prefix", "safe"} {
		t.Run(mode, func(t *testing.T) {
			err := loadFiles(t, nil, &quotingValidator{}, map[string]string{"a.yaml": "token: $__secret{app:token}\nmode: " + mode + "\n"},
				loader.WithSecretResolver(manager))
			require.ErrorIs(t, err, errRejected)
			require.ErrorIs(t, err, loader.ErrValidation)
			require.Equal(t, "validation failed for configuration (secrets were resolved; details withheld)", err.Error())
			requireNoSecret(t, err)
		})
	}
	t.Run("environment", func(t *testing.T) {
		t.Setenv("SECRETSCRUBENVTEST_TOKEN", "$__secret{app:token}")
		_, err := loader.New(&yaml3.Backend{}, loader.WithEnvPrefix("SECRETSCRUBENVTEST"), loader.WithSecretResolver(manager)).Load(&quotingValidator{})
		require.ErrorIs(t, err, errRejected)
		requireNoSecret(t, err)
	})
	t.Run("default tag", func(t *testing.T) {
		_, err := loader.New(&yaml3.Backend{}, loader.WithEnvPrefix("SECRETSCRUBDEFAULTTEST"), loader.WithSecretResolver(manager)).Load(&tagValidator{})
		require.ErrorIs(t, err, errRejected)
		requireNoSecret(t, err)
	})
	t.Run("no secret resolved", func(t *testing.T) {
		err := loadFiles(t, nil, &quotingValidator{}, map[string]string{"a.yaml": "token: plain\n"}, loader.WithSecretResolver(manager))
		require.ErrorIs(t, err, errRejected)
		require.Contains(t, err.Error(), `token "plain"`, "without a resolved secret the error is returned as it is")
	})
}

// escapingSecrets hold quotes, backslashes, newlines and control bytes in
// their first bytes, which %q escapes.
var escapingSecrets = []string{`ab"cdefgh`, "a\\b\nc\x01defg", "\x00\x7f\tz+xyzw"}

// Secrets that %q, %v, %x or %s would render escaped never show, in any
// form, in an error a validator builds from them.
func TestLoad_EscapedSecretFormsWithheld(t *testing.T) {
	t.Parallel()

	values := map[string]string{}
	for i, v := range escapingSecrets {
		values[fmt.Sprintf("app:s%d", i)] = v
	}
	manager := newCountingManager(values)
	for i := range escapingSecrets {
		for _, verb := range []string{"%q", "%v", "%x", "%s", "%+q"} {
			t.Run(fmt.Sprintf("%d %s", i, verb), func(t *testing.T) {
				t.Parallel()
				cfg := &formattingValidator{verb: verb}
				err := loadFiles(t, nil, cfg, map[string]string{"a.yaml": fmt.Sprintf("token: $__secret{app:s%d}\n", i)},
					loader.WithSecretResolver(manager))
				require.ErrorIs(t, err, errRejected)
				requireNoneOf(t, err, escapingSecrets...)
			})
		}
	}
}

// formattingValidator formats its token with verb in the error it returns.
type formattingValidator struct {
	Token string `yaml:"token"`
	verb  string
}

// Validate rejects the token, formatting it with verb.
func (v *formattingValidator) Validate() error {
	return fmt.Errorf("token "+v.verb+": %w", v.Token, errRejected)
}

// mapValidator validates every value of a map parsed from a secret, quoting
// the rejected one.
type mapValidator struct {
	Users map[string]string `yaml:"users"`
}

// Validate rejects every user, quoting its password.
func (v *mapValidator) Validate() error {
	var errs []error
	for name, password := range v.Users {
		errs = append(errs, fmt.Errorf("user %s: password %q: %w", name, password, errRejected))
	}
	return errors.Join(errs...)
}

// Parts of a collection parsed from a secret never show in an error a
// validator builds from them. It mutates the environment, so it is not
// parallel.
func TestLoad_SecretCollectionPartsWithheld(t *testing.T) {
	const secret = "admin:supersecret,guest:hunter2pw"
	manager := newCountingManager(map[string]string{"app:users": secret})
	t.Setenv("SECRETCOLLPARTSTEST_USERS", "$__secret{app:users}")
	_, err := loader.New(&yaml3.Backend{}, loader.WithEnvPrefix("SECRETCOLLPARTSTEST"), loader.WithSecretResolver(manager)).Load(&mapValidator{})
	require.ErrorIs(t, err, errRejected)
	requireNoneOf(t, err, secret, "supersecret", "hunter2pw")
}

// tagValidator takes its token from a default tag and quotes it in Validate.
type tagValidator struct {
	Token string `yaml:"token" default:"$__secret{app:token}"`
}

// Validate quotes the token.
func (v *tagValidator) Validate() error {
	return fmt.Errorf("token %q: %w", v.Token, errRejected)
}

// ozzoValidated validates with ozzo-validation, whose error map does not
// unwrap; its field rule quotes the value and wraps errRejected.
type ozzoValidated struct {
	Token string `yaml:"token"`
}

// Validate rejects the token, quoting it.
func (v *ozzoValidated) Validate() error {
	return validationconfig.ValidateStruct(v, validation.Field(&v.Token, validation.By(func(any) error {
		return fmt.Errorf("token %q: %w", v.Token, errRejected)
	})))
}

// A field error inside a validation error map, nested in wrapped errors, is
// withheld: the message names the field path only, and the sentinel still
// matches.
func TestLoad_ValidationMapErrorsWithheld(t *testing.T) {
	t.Parallel()

	manager := newCountingManager(map[string]string{"app:token": leakySecret})
	err := loadFiles(t, nil, &ozzoValidated{}, map[string]string{"a.yaml": "token: $__secret{app:token}\n"},
		loader.WithSecretResolver(manager))
	require.ErrorIs(t, err, errRejected)
	require.Equal(t, "validation failed for Token (secrets were resolved; details withheld)", err.Error())
	requireNoSecret(t, err)
}

// eachValidated validates map and slice entries with validation.Each, whose
// error map is keyed by the entry key or index: data, not schema.
type eachValidated struct {
	Levels map[string]string    `yaml:"levels"`
	Groups []eachGroup          `yaml:"groups"`
	ByName map[string]eachGroup `yaml:"byName"`
}

// eachGroup is a struct element validated field by field.
type eachGroup struct {
	Name string `yaml:"name"`
}

// Validate rejects every level, group name and entry.
func (v *eachValidated) Validate() error {
	reject := validation.By(func(any) error { return errRejected })
	return validationconfig.ValidateStruct(v,
		validation.Field(&v.Levels, validation.Each(reject)),
		validation.Field(&v.Groups),
		validation.Field(&v.ByName),
	)
}

// Validate rejects the name.
func (g eachGroup) Validate() error {
	return validationconfig.ValidateStruct(&g, validation.Field(&g.Name, validation.By(func(any) error { return errRejected })))
}

// The keys of collection entries, which come from data (here a secret, or an
// environment variable name), never reach a withheld message: the location
// keeps only the schema's field names and "*" for an entry. errors.Is still
// matches. It mutates the environment, so it is not parallel.
func TestLoad_WithheldPathsAreSchemaStatic(t *testing.T) {
	const secret = "confidential-token:invalid"
	manager := newCountingManager(map[string]string{"app:levels": secret, "app:name": "group-secret-name"})

	t.Setenv("SECRETSTATICPATHTEST_LEVELS", "$__secret{app:levels}")
	t.Setenv("SECRETSTATICPATHTEST_BY_NAME__privatekey__NAME", "$__secret{app:name}")
	cfg := &eachValidated{Groups: []eachGroup{{Name: "g"}}}
	_, err := loader.New(&yaml3.Backend{}, loader.WithEnvPrefix("SECRETSTATICPATHTEST"), loader.WithSecretResolver(manager)).Load(cfg)
	require.ErrorIs(t, err, errRejected)
	require.ErrorIs(t, err, loader.ErrValidation)
	require.Equal(t, "validation failed for ByName.*.Name, Groups.*.Name, Levels.* (secrets were resolved; details withheld)", err.Error())
	requireNoneOf(t, err, secret, "confidential", "privatekey", "group-secret-name")

	// An environment variable named after a map entry keeps "*" for the key.
	type counted struct {
		N int `yaml:"n"`
	}
	type countsCfg struct {
		Counts map[string]counted `yaml:"counts"`
	}
	t.Setenv("SECRETSTATICENVTEST_COUNTS__privatekey__N", "$__secret{app:name}")
	_, err = loader.New(&yaml3.Backend{}, loader.WithEnvPrefix("SECRETSTATICENVTEST"), loader.WithSecretResolver(manager)).Load(&countsCfg{})
	require.ErrorIs(t, err, loader.ErrBindEnv)
	require.Equal(t, "environment failed for SECRETSTATICENVTEST_COUNTS__*__N (secrets were resolved; details withheld)", err.Error())
	requireNoneOf(t, err, "privatekey", "group-secret-name")
}

// Whether a file value holds a placeholder is decided on the parsed value:
// escaped and quoted forms (\x24, \u0024, single and double quotes) resolve,
// a missing secret fails, and an unrelated literal placeholder elsewhere in
// the file changes nothing. A TOML file holding an escaped placeholder is
// rejected like an unescaped one.
func TestLoad_EscapedFilePlaceholders(t *testing.T) {
	t.Parallel()

	type cfgT struct {
		A    string `yaml:"a"`
		B    string `yaml:"b"`
		C    string `yaml:"c"`
		Port int    `yaml:"port"`
	}
	manager := newCountingManager(map[string]string{"app:pw": "s3cret", "app:port": "8080"})
	escaped := `a: "\x24__secret{app:pw}"` + "\n" + `b: "\u0024__secret{app:pw}"` + "\n" + `c: '$__secret{app:pw}'` + "\n" +
		`port: "\x24__secret{app:port}"` + "\n"

	for name, extra := range map[string]string{"alone": "", "with an unrelated placeholder": "# $__secret{app:unrelated}\n"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg := &cfgT{}
			require.NoError(t, loadFiles(t, nil, cfg, map[string]string{"a.yaml": extra + escaped}, loader.WithSecretResolver(manager)))
			require.Equal(t, cfgT{A: "s3cret", B: "s3cret", C: "s3cret", Port: 8080}, *cfg)

			err := loadFiles(t, nil, &cfgT{}, map[string]string{"a.yaml": extra + `a: "\x24__secret{app:missing}"` + "\n"},
				loader.WithSecretResolver(manager))
			require.ErrorIs(t, err, coreerrs.ErrNotFound)
		})
	}

	type tomlCfg struct {
		Password string `toml:"password"`
	}
	err := loadFiles(t, &toml.Backend{}, &tomlCfg{}, map[string]string{"a.toml": "password = \"\\u0024__secret{app:pw}\"\n"},
		loader.WithSecretResolver(manager))
	require.ErrorIs(t, err, loader.ErrSecretsUnsupported)
	require.NoError(t, loadFiles(t, &toml.Backend{}, &tomlCfg{}, map[string]string{"a.toml": "# $__secret{app:pw}\npassword = \"plain\"\n"},
		loader.WithSecretResolver(manager)))
}

// A default tag whose ${VAR} reference expands to a placeholder resolves it,
// and a parse failure of the resolved value shows no form of the secret. It
// mutates the environment, so it is not parallel.
func TestLoad_DefaultTagPlaceholderFromVariable(t *testing.T) {
	manager := newCountingManager(map[string]string{"app:port": "8080", "app:bad": leakySecret})
	type cfgT struct {
		Port int `yaml:"port" default:"${SECRETDEFAULTVARREF}"`
	}
	t.Setenv("SECRETDEFAULTVARREF", "$__secret{app:port}")
	cfg := &cfgT{}
	_, err := loader.New(&yaml3.Backend{}, loader.WithEnvPrefix("SECRETDEFAULTVARTEST"), loader.WithSecretResolver(manager)).Load(cfg)
	require.NoError(t, err)
	require.Equal(t, 8080, cfg.Port)

	t.Setenv("SECRETDEFAULTVARREF", "$__secret{app:bad}")
	_, err = loader.New(&yaml3.Backend{}, loader.WithEnvPrefix("SECRETDEFAULTVARTEST"), loader.WithSecretResolver(manager)).Load(&cfgT{})
	require.ErrorIs(t, err, loader.ErrSecretValueInvalid)
	requireNoSecret(t, err)
}

// A placeholder under an explicit tag other than !!str is rejected with
// ErrSecretTagged, without a lookup: !!binary would decode base64 into the
// placeholder after the check, and !!int, !!timestamp or a custom tag would
// decode a resolved value differently (yaml.v3 already rejects a placeholder
// under !!int or !!timestamp while parsing). Explicit !!str resolves.
func TestLoad_TaggedFileSecrets(t *testing.T) {
	type cfgT struct {
		A   string `yaml:"a"`
		B   string `yaml:"b"`
		Raw []byte `yaml:"raw"`
	}
	manager := newCountingManager(map[string]string{"app:pw": "s3cret"})
	b64 := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

	for name, content := range map[string]string{
		"binary known":    "a: !!binary " + b64("$__secret{app:pw}") + "\n",
		"binary missing":  "a: !!binary " + b64("$__secret{app:missing}") + "\n",
		"binary bytes":    "raw: !!binary " + b64("x$__secret{app:pw}") + "\n",
		"binary anchored": "a: &bin !!binary " + b64("$__secret{app:pw}") + "\nb: *bin\n",
		"int":             "a: !!int $__secret{app:pw}\n",
		"timestamp":       "a: !!timestamp $__secret{app:pw}\n",
		"custom":          "a: !secret $__secret{app:pw}\n",
	} {
		t.Run(name, func(t *testing.T) {
			err := loadFiles(t, nil, &cfgT{}, map[string]string{"a.yaml": content}, loader.WithSecretResolver(manager))
			require.Error(t, err)
			// !!int and !!timestamp fail parsing already, before any lookup.
			require.True(t, errors.Is(err, loader.ErrSecretTagged) || errors.Is(err, loader.ErrSecretInAnchor) ||
				errors.Is(err, loader.ErrDecode), "%v", err)
			require.NotContains(t, err.Error(), "s3cret")
		})
	}
	require.Zero(t, manager.count("app:pw"))
	require.Zero(t, manager.count("app:missing"))

	cfg := &cfgT{}
	require.NoError(t, loadFiles(t, nil, cfg, map[string]string{"a.yaml": "a: !!str $__secret{app:pw}\nb: !!binary " + b64("plain") + "\n"},
		loader.WithSecretResolver(manager)))
	require.Equal(t, "s3cret", cfg.A)
	require.Equal(t, "plain", cfg.B)
}

// errCustom is the sentinel customRejecting wraps.
var errCustom = errors.New("custom rejected")

// customRejecting rejects the value "bad" with errCustom.
type customRejecting struct{ v string }

// UnmarshalYAML rejects "bad", quoting the value.
func (c *customRejecting) UnmarshalYAML(decode func(any) error) error {
	var value string
	if err := decode(&value); err != nil {
		return err
	}
	if value == "bad" || strings.Contains(value, "Ω") {
		return fmt.Errorf("value %q: %w", value, errCustom)
	}
	c.v = value
	return nil
}

// A decode error in a file holding resolved secrets is withheld, but
// errors.Is still reaches the decoder's own errors (an unknown field, an
// unmarshaler's sentinel); ErrSecretValueInvalid is added only when the
// failure concerns a secret value.
func TestLoad_DecodeErrorsKeepIdentityAfterSecrets(t *testing.T) {
	t.Parallel()

	type cfgT struct {
		Password string          `yaml:"password"`
		Custom   customRejecting `yaml:"custom"`
	}
	manager := newCountingManager(map[string]string{"app:pw": "s3cret", "app:bad": leakySecret})

	t.Run("unknown field", func(t *testing.T) {
		t.Parallel()
		err := loadFiles(t, nil, &cfgT{}, map[string]string{"a.yaml": "password: $__secret{app:pw}\nunknwon: 1\n"},
			loader.WithSecretResolver(manager))
		require.ErrorIs(t, err, backend.ErrUnknownField)
		require.ErrorIs(t, err, loader.ErrDecode)
		require.NotErrorIs(t, err, loader.ErrSecretValueInvalid)
		require.NotContains(t, err.Error(), "s3cret")
	})
	t.Run("unrelated unmarshaler error", func(t *testing.T) {
		t.Parallel()
		err := loadFiles(t, nil, &cfgT{}, map[string]string{"a.yaml": "password: $__secret{app:pw}\ncustom: bad\n"},
			loader.WithSecretResolver(manager))
		require.ErrorIs(t, err, errCustom)
		require.NotErrorIs(t, err, loader.ErrSecretValueInvalid)
		require.NotContains(t, err.Error(), "s3cret")
	})
	t.Run("unmarshaler rejecting a secret", func(t *testing.T) {
		t.Parallel()
		err := loadFiles(t, nil, &cfgT{}, map[string]string{"a.yaml": "custom: $__secret{app:bad}\n"},
			loader.WithSecretResolver(manager))
		require.ErrorIs(t, err, errCustom)
		require.ErrorIs(t, err, loader.ErrSecretValueInvalid)
		requireNoSecret(t, err)
	})
}

// A quoted or !!str "<<" key is an ordinary key, as yaml.v3 decodes it: its
// placeholder resolves; a real merge still rejects one.
func TestLoad_MergeKeyFollowsYAMLRules(t *testing.T) {
	t.Parallel()

	type cfgT struct {
		Labels map[string]string `yaml:"labels"`
	}
	manager := newCountingManager(map[string]string{"app:pw": "s3cret"})
	for name, content := range map[string]string{
		"double quoted": "labels:\n  \"<<\": $__secret{app:pw}\n",
		"single quoted": "labels:\n  '<<': $__secret{app:pw}\n",
		"explicit str":  "labels:\n  !!str <<: $__secret{app:pw}\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg := &cfgT{}
			require.NoError(t, loadFiles(t, nil, cfg, map[string]string{"a.yaml": content}, loader.WithSecretResolver(manager)))
			require.Equal(t, map[string]string{"<<": "s3cret"}, cfg.Labels)
		})
	}
	err := loadFiles(t, nil, &cfgT{}, map[string]string{"a.yaml": "labels:\n  <<: {k: \"$__secret{app:pw}\"}\n"},
		loader.WithSecretResolver(manager))
	require.ErrorIs(t, err, loader.ErrSecretInAnchor)
}

// Typed map keys yaml.v3 decodes but a generic decode cannot represent (a
// sequence key) load identically with and without a secrets manager.
func TestLoad_TypedSequenceKeysWithSecrets(t *testing.T) {
	t.Parallel()

	type cfgT struct {
		Pairs    map[[2]string]string `yaml:"pairs"`
		Password string               `yaml:"password"`
	}
	content := "pairs:\n  ? [a, b]\n  : value\npassword: plain\n"
	without := &cfgT{}
	require.NoError(t, loadFiles(t, nil, without, map[string]string{"a.yaml": content}))
	with := &cfgT{}
	manager := newCountingManager(map[string]string{"app:pw": "s3cret"})
	require.NoError(t, loadFiles(t, nil, with, map[string]string{"a.yaml": content}, loader.WithSecretResolver(manager)))
	require.Equal(t, without, with)
	require.Equal(t, map[[2]string]string{{"a", "b"}: "value"}, with.Pairs)

	resolved := &cfgT{}
	require.NoError(t, loadFiles(t, nil, resolved, map[string]string{"a.yaml": "pairs:\n  ? [a, b]\n  : value\npassword: $__secret{app:pw}\n"},
		loader.WithSecretResolver(manager)))
	require.Equal(t, "s3cret", resolved.Password)
}

// Hostile documents are rejected before they are walked: an anchor holding
// an alias to itself and excessive aliasing.
func TestLoad_HostileDocumentsWithSecrets(t *testing.T) {
	t.Parallel()

	manager := newCountingManager(map[string]string{"app:pw": "s3cret"})
	var laughs strings.Builder
	laughs.WriteString("a: &a [\"lol\",\"lol\",\"lol\",\"lol\",\"lol\",\"lol\",\"lol\",\"lol\",\"lol\"]\n")
	prev := "a"
	for _, name := range []string{"b", "c", "d", "e", "f", "g", "h", "i"} {
		fmt.Fprintf(&laughs, "%s: &%s [*%s,*%s,*%s,*%s,*%s,*%s,*%s,*%s,*%s]\n", name, name, prev, prev, prev, prev, prev, prev, prev, prev, prev)
		prev = name
	}
	for name, content := range map[string]string{
		"cycle":  "a: &x\n  b: *x\n",
		"laughs": laughs.String(),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := loadFiles(t, nil, &struct {
				A any `yaml:"a"`
				B any `yaml:"b"`
				C any `yaml:"c"`
				D any `yaml:"d"`
				E any `yaml:"e"`
				F any `yaml:"f"`
				G any `yaml:"g"`
				H any `yaml:"h"`
				I any `yaml:"i"`
			}{}, map[string]string{"a.yaml": content}, loader.WithSecretResolver(manager))
			require.ErrorIs(t, err, loader.ErrDecode)
		})
	}
}

// parseConfig takes parsed values from the environment or default tags.
type parseConfig struct {
	On    bool              `yaml:"on"`
	Port  int               `yaml:"port"`
	Ports []int             `yaml:"ports"`
	Hosts map[string]string `yaml:"hosts"`
}

// A value resolved from a secret that does not parse keeps the parser's error
// identities for errors.Is (ErrBoolParseFailure, ErrNumericParse,
// ErrSliceItemInvalid, ErrMapItemInvalid) besides ErrSecretValueInvalid, from
// the environment and from default tags, while the message shows no form of
// the secret. It mutates the environment, so it is not parallel.
func TestLoad_SecretParseErrorsKeepIdentity(t *testing.T) {
	manager := newCountingManager(map[string]string{"app:bad": leakySecret})
	cases := []struct {
		field, env string
		sentinel   error
	}{
		{"on", "ON", loader.ErrBoolParseFailure},
		{"port", "PORT", loader.ErrNumericParse},
		{"ports", "PORTS", loader.ErrSliceItemInvalid},
		{"hosts", "HOSTS", loader.ErrMapItemInvalid},
	}
	for _, tc := range cases {
		t.Run("env "+tc.field, func(t *testing.T) {
			prefix := "SECRETPARSEID" + tc.env + "TEST"
			t.Setenv(prefix+"_"+tc.env, "$__secret{app:bad}")
			_, err := loader.New(&yaml3.Backend{}, loader.WithEnvPrefix(prefix), loader.WithSecretResolver(manager)).Load(&parseConfig{})
			require.ErrorIs(t, err, tc.sentinel)
			require.ErrorIs(t, err, loader.ErrSecretValueInvalid)
			require.ErrorIs(t, err, loader.ErrBindEnv)
			requireNoSecret(t, err)
		})
	}

	defaults := []struct {
		name     string
		conf     any
		sentinel error
	}{
		{"bool", &struct {
			On bool `yaml:"on" default:"$__secret{app:bad}"`
		}{}, loader.ErrBoolParseFailure},
		{"int", &struct {
			Port int `yaml:"port" default:"$__secret{app:bad}"`
		}{}, loader.ErrNumericParse},
		{"slice", &struct {
			Ports []int `yaml:"ports" default:"$__secret{app:bad}"`
		}{}, loader.ErrSliceItemInvalid},
		{"map", &struct {
			Hosts map[string]string `yaml:"hosts" default:"$__secret{app:bad}"`
		}{}, loader.ErrMapItemInvalid},
	}
	for _, tc := range defaults {
		t.Run("default "+tc.name, func(t *testing.T) {
			_, err := loader.New(&yaml3.Backend{}, loader.WithEnvPrefix("SECRETPARSEIDDEFAULTTEST"), loader.WithSecretResolver(manager)).Load(tc.conf)
			require.ErrorIs(t, err, tc.sentinel)
			require.ErrorIs(t, err, loader.ErrSecretValueInvalid)
			require.ErrorIs(t, err, loader.ErrBindDefaults)
			requireNoSecret(t, err)
		})
	}
}

// invalidUTF8Secret is a raw 32-byte key that is not valid UTF-8.
var invalidUTF8Secret = strings.Repeat("\xff", 16) + "\x80\xfe" + strings.Repeat("\xc3", 14)

// A file secret that is not valid UTF-8 keeps its exact bytes in string,
// redacted-string and interface targets, also in a nested struct; for
// a number it fails with ErrSecretValueInvalid. No message shows the value.
// It mutates the environment, so it is not parallel.
func TestLoad_InvalidUTF8FileSecrets(t *testing.T) {
	manager := newCountingManager(map[string]string{"app:key": invalidUTF8Secret})
	type cfgT struct {
		Plain    string                  `yaml:"plain"`
		Redacted redacted.RedactedString `yaml:"redacted"`
		Any      any                     `yaml:"any"`
		Nested   struct {
			Key redacted.RedactedString `yaml:"key"`
		} `yaml:"nested"`
	}
	content := "plain: $__secret{app:key}\nredacted: $__secret{app:key}\nany: $__secret{app:key}\n" +
		"nested:\n  key: $__secret{app:key}\n"
	cfg := &cfgT{}
	require.NoError(t, loadFiles(t, nil, cfg, map[string]string{"a.yaml": content}, loader.WithSecretResolver(manager)))
	require.Equal(t, invalidUTF8Secret, cfg.Plain)
	require.Equal(t, invalidUTF8Secret, cfg.Redacted.Expose())
	require.Equal(t, invalidUTF8Secret, cfg.Any)
	require.Equal(t, invalidUTF8Secret, cfg.Nested.Key.Expose())

	// The environment keeps the bytes as well.
	t.Setenv("SECRETUTF8ENVTEST_PLAIN", "$__secret{app:key}")
	fromEnv := &cfgT{}
	_, err := loader.New(&yaml3.Backend{}, loader.WithEnvPrefix("SECRETUTF8ENVTEST"), loader.WithSecretResolver(manager)).Load(fromEnv)
	require.NoError(t, err)
	require.Equal(t, invalidUTF8Secret, fromEnv.Plain)

	type numeric struct {
		Port int `yaml:"port"`
	}
	err = loadFiles(t, nil, &numeric{}, map[string]string{"a.yaml": "port: $__secret{app:key}\n"}, loader.WithSecretResolver(manager))
	require.ErrorIs(t, err, loader.ErrSecretValueInvalid)
	requireNoneOf(t, err, invalidUTF8Secret)
}

// partialBackend checks documents and rewrites scalars, but its key nodes do
// not list their scalars: it cannot resolve placeholders at the source.
type partialBackend struct {
	yaml3.Backend
}

// DecodeKeys returns key nodes without ScalarLister.
func (b *partialBackend) DecodeKeys(r io.Reader) (backend.KeyNode, error) {
	k, err := b.Backend.DecodeKeys(r)
	return plainKeyNode{KeyNode: k}, err
}

// plainKeyNode exposes only the KeyNode methods of a node.
type plainKeyNode struct {
	backend.KeyNode
}

// A backend with only part of the source-rewriting capability fails closed:
// a placeholder is rejected with ErrSecretsUnsupported, never loaded
// literally.
func TestLoad_PartialSecretSupportFailsClosed(t *testing.T) {
	t.Parallel()

	type cfgT struct {
		Password string `yaml:"password"`
	}
	manager := newCountingManager(map[string]string{"app:pw": "s3cret"})
	for _, content := range []string{"password: $__secret{app:missing}\n", "password: \"\\x24__secret{app:pw}\"\n"} {
		cfg := &cfgT{}
		err := loadFiles(t, &partialBackend{}, cfg, map[string]string{"a.yaml": content}, loader.WithSecretResolver(manager))
		require.ErrorIs(t, err, loader.ErrSecretsUnsupported)
		require.Empty(t, cfg.Password)
	}
	require.NoError(t, loadFiles(t, &partialBackend{}, &cfgT{}, map[string]string{"a.yaml": "password: plain\n"},
		loader.WithSecretResolver(manager)))
}

// numericKinds holds every numeric kind and a bool.
type numericKinds struct {
	I    int     `yaml:"i"`
	I8   int8    `yaml:"i8"`
	I16  int16   `yaml:"i16"`
	I32  int32   `yaml:"i32"`
	I64  int64   `yaml:"i64"`
	U    uint    `yaml:"u"`
	U8   uint8   `yaml:"u8"`
	U16  uint16  `yaml:"u16"`
	U32  uint32  `yaml:"u32"`
	U64  uint64  `yaml:"u64"`
	UP   uintptr `yaml:"up"`
	F32  float32 `yaml:"f32"`
	F64  float64 `yaml:"f64"`
	Flag bool    `yaml:"flag"`
}

// A file secret for every numeric kind (uintptr included) and a bool decodes
// exactly like the same literal.
func TestLoad_NumericKindsFromFileSecrets(t *testing.T) {
	t.Parallel()

	fields := map[string]string{
		"i": "-7", "i8": "-8", "i16": "-16", "i32": "-32", "i64": "-64",
		"u": "7", "u8": "8", "u16": "16", "u32": "32", "u64": "64", "up": "123",
		"f32": "1.5", "f64": "2.25", "flag": "true",
	}
	values := map[string]string{}
	var literal, placeholders strings.Builder
	for _, name := range slices.Sorted(maps.Keys(fields)) {
		values["app:"+name] = fields[name]
		fmt.Fprintf(&literal, "%s: %s\n", name, fields[name])
		fmt.Fprintf(&placeholders, "%s: $__secret{app:%s}\n", name, name)
	}
	manager := newCountingManager(values)

	want := &numericKinds{}
	require.NoError(t, loadFiles(t, nil, want, map[string]string{"a.yaml": literal.String()}))
	got := &numericKinds{}
	require.NoError(t, loadFiles(t, nil, got, map[string]string{"a.yaml": placeholders.String()}, loader.WithSecretResolver(manager)))
	require.Equal(t, want, got)
	require.Equal(t, uintptr(123), got.UP)
}
