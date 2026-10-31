// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package validationconfig

import (
	"time"

	ozzo_rules "github.com/altessa-s/ozzo-rules"
	validation "github.com/go-ozzo/ozzo-validation/v4"
)

// ErrNegativeDuration is the validation error for a negative duration.
var ErrNegativeDuration = validation.NewError("validation_duration_negative", "must not be negative")

// PositiveDuration returns a rule for a duration that must be greater than
// zero. ozzo_rules.Duration rejects zero but accepts a negative duration;
// PositiveDuration rejects both.
func PositiveDuration() validation.Rule {
	return durationRule{base: ozzo_rules.Duration()}
}

// NonNegativeDuration returns a rule for a duration where zero means unset or
// no limit. ozzo_rules.DurationOrZero accepts a negative duration;
// NonNegativeDuration rejects it.
func NonNegativeDuration() validation.Rule {
	return durationRule{base: ozzo_rules.DurationOrZero()}
}

// durationRule runs an ozzo-rules duration rule, then rejects a negative
// time.Duration (or *time.Duration).
type durationRule struct {
	base ozzo_rules.DurationRule
}

// Validate implements [validation.Rule].
func (r durationRule) Validate(value any) error {
	if err := r.base.Validate(value); err != nil {
		return err
	}
	v, isNil := validation.Indirect(value)
	if d, ok := v.(time.Duration); !isNil && ok && d < 0 {
		return ErrNegativeDuration
	}
	return nil
}
