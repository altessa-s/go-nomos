// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package validationconfig_test

import (
	"errors"
	"testing"
	"time"

	validationconfig "github.com/altessa-s/go-nomos/validation"
	validation "github.com/go-ozzo/ozzo-validation/v4"
)

type benchConfig struct {
	Name    string
	Timeout time.Duration
}

func BenchmarkValidateStruct(b *testing.B) {
	c := &benchConfig{Name: "svc", Timeout: time.Second}
	for b.Loop() {
		if err := validationconfig.ValidateStruct(c,
			validation.Field(&c.Name, validation.Required),
			validation.Field(&c.Timeout, validationconfig.PositiveDuration()),
		); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkValidateStruct_Invalid(b *testing.B) {
	c := &benchConfig{}
	for b.Loop() {
		if err := validationconfig.ValidateStruct(c,
			validation.Field(&c.Name, validation.Required),
			validation.Field(&c.Timeout, validationconfig.PositiveDuration()),
		); err == nil {
			b.Fatal("expected a validation error")
		}
	}
}

func BenchmarkPositiveDuration(b *testing.B) {
	rule := validationconfig.PositiveDuration()
	for b.Loop() {
		if err := rule.Validate(time.Second); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkNonNegativeDuration(b *testing.B) {
	rule := validationconfig.NonNegativeDuration()
	for b.Loop() {
		if err := rule.Validate(time.Duration(0)); err != nil {
			b.Fatal(err)
		}
	}
}

type benchLeaf struct{ Name string }

func (l *benchLeaf) Validate() error {
	return validation.ValidateStruct(l, validation.Field(&l.Name, validation.Required))
}

type benchParent struct {
	Leaf   benchLeaf
	Leaves []benchLeaf
}

func BenchmarkNestedField(b *testing.B) {
	p := &benchParent{Leaf: benchLeaf{Name: "x"}}
	b.ReportAllocs()
	for b.Loop() {
		if err := validationconfig.ValidateStruct(p, validationconfig.NestedField(&p.Leaf)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkNestedEach(b *testing.B) {
	p := &benchParent{Leaves: make([]benchLeaf, 100)}
	for i := range p.Leaves {
		p.Leaves[i].Name = "x"
	}
	b.ReportAllocs()
	for b.Loop() {
		if err := validationconfig.ValidateStruct(p, validationconfig.NestedEach(&p.Leaves)); err != nil {
			b.Fatal(err)
		}
	}
}

type benchStorageSub struct{ Addr string }

type benchStorage struct {
	Type   string
	Memory *benchStorageSub
	Redis  *benchStorageSub
}

func BenchmarkValidateStorage(b *testing.B) {
	c := &benchStorage{Type: "redis", Redis: &benchStorageSub{Addr: "localhost:6379"}}
	allowed := []string{"memory", "redis"}
	cases := []validationconfig.StorageCase[string]{
		{When: "memory", Field: &c.Memory},
		{When: "redis", Field: &c.Redis},
	}
	b.ReportAllocs()
	for b.Loop() {
		if err := validationconfig.ValidateStorage(c, &c.Type, allowed, cases); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPrettyError(b *testing.B) {
	err := validation.Errors{
		"server": validation.Errors{
			"port": errors.New("invalid port"),
			"tls":  validation.Errors{"cert": errors.New("cannot be blank")},
		},
		"database": validation.Errors{"host": errors.New("cannot be blank")},
		"name":     errors.New("cannot be blank"),
	}
	b.ReportAllocs()
	for b.Loop() {
		if validationconfig.PrettyError(err) == "" {
			b.Fatal("empty message")
		}
	}
}
