// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package validationconfig_test

import (
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
