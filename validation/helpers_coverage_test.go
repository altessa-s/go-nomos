// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package validationconfig_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	validationconfig "github.com/altessa-s/go-nomos/validation"
	validation "github.com/go-ozzo/ozzo-validation/v4"
)

func TestErrorsToFlatMap(t *testing.T) {
	t.Parallel()

	portErr := errors.New("invalid port")
	hostErr := errors.New("cannot be blank")
	in := validation.Errors{
		"server": validation.Errors{
			"port": portErr,
			"tls":  validation.Errors{"cert": hostErr},
		},
		"name": hostErr,
	}

	out := validationconfig.ErrorsToFlatMap(in)

	require.Len(t, out, 3)
	require.ErrorIs(t, out["server.port"], portErr)
	require.ErrorIs(t, out["server.tls.cert"], hostErr)
	require.ErrorIs(t, out["name"], hostErr)
}

func TestErrorsToFlatMap_Empty(t *testing.T) {
	t.Parallel()

	require.Empty(t, validationconfig.ErrorsToFlatMap(nil))
}

func TestPrettyError(t *testing.T) {
	t.Parallel()

	t.Run("nil", func(t *testing.T) {
		t.Parallel()
		require.Empty(t, validationconfig.PrettyError(nil))
	})

	t.Run("plain error", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, "boom", validationconfig.PrettyError(errors.New("boom")))
	})

	t.Run("nested validation errors", func(t *testing.T) {
		t.Parallel()
		err := validation.Errors{
			"server": validation.Errors{"port": errors.New("invalid port")},
			"host":   errors.New("cannot be blank"),
		}

		got := validationconfig.PrettyError(err)

		lines := strings.Split(got, "\n")
		require.Len(t, lines, 3)
		require.Equal(t, "Configuration is not valid:", lines[0])
		require.Contains(t, got, "  - server.port: invalid port")
		require.Contains(t, got, "  - host: cannot be blank")
	})
}

type storageSub struct{ Addr string }

type storageCfg struct {
	Type   string
	Memory *storageSub
	Redis  *storageSub
}

func validateStorageCfg(c *storageCfg) error {
	return validationconfig.ValidateStorage(c, &c.Type, []string{"memory", "redis"},
		[]validationconfig.StorageCase[string]{
			{When: "memory", Field: &c.Memory},
			{When: "redis", Field: &c.Redis},
		})
}

func TestValidateStorage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     storageCfg
		wantErr string
	}{
		{name: "memory without sub-config is allowed", cfg: storageCfg{Type: "memory"}},
		{name: "redis with sub-config", cfg: storageCfg{Type: "redis", Redis: &storageSub{Addr: "x"}}},
		{name: "missing type", cfg: storageCfg{}, wantErr: "Type"},
		{name: "unknown type", cfg: storageCfg{Type: "mongo"}, wantErr: "Type"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateStorageCfg(&tc.cfg)
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			verrs, ok := errors.AsType[validation.Errors](err)
			require.True(t, ok, "want validation.Errors, got %v", err)
			require.Contains(t, verrs, tc.wantErr)
		})
	}
}

func TestValidateStructIfEnabled(t *testing.T) {
	t.Parallel()

	s := storageSub{}
	require.NoError(t, validationconfig.ValidateStructIfEnabled(false, &s,
		validation.Field(&s.Addr, validation.Required)))
	require.Error(t, validationconfig.ValidateStructIfEnabled(true, &s,
		validation.Field(&s.Addr, validation.Required)))
}

func TestValidateStorageNilSelector(t *testing.T) {
	t.Parallel()
	for _, cases := range [][]validationconfig.StorageCase[string]{nil, {{When: "redis"}}} {
		t.Run(fmt.Sprint(len(cases)), func(t *testing.T) {
			t.Parallel()
			err := validationconfig.ValidateStorage(&storageCfg{}, nil, []string{"redis"}, cases)
			require.ErrorIs(t, err, validation.ErrFieldPointer(0))
		})
	}
}
