# validationconfig

```go
import validationconfig "github.com/altessa-s/go-nomos/validation"
```

Package `validationconfig` defines shared validation helpers for configuration schemas: struct validation with readable errors and storage-selector
checks. Schemas are populated by [`loader`](../loader) and implement `Validate` with these helpers; the helpers depend on no schema.

## Key types

| Type          | Description                                                                                                        |
|---------------|--------------------------------------------------------------------------------------------------------------------|
| `StorageCase` | Pairs a storage type with its sub-config field pointer; nil sub-configs are allowed. |

## Functions

| Function                  | Description                                                                                                     |
|---------------------------|-----------------------------------------------------------------------------------------------------------------|
| `ValidateStruct`          | `validation.ValidateStruct` with actionable messages for a field rule that does not point into the struct.      |
| `ValidateStructIfEnabled` | `ValidateStruct` gated on an `Enabled` flag.                                                                    |
| `NestedField`             | Field rule for a struct held by value whose `Validate` has a pointer receiver — ozzo skips it otherwise.         |
| `NestedEach`              | Field rule for a slice of values whose `Validate` has a pointer receiver; validates every element in place.     |
| `NotNilEntries`           | Rule rejecting nil entries of a map or slice of pointers before ozzo calls `Validate` on them and panics.       |
| `PositiveDuration`        | Rule for a duration that must be greater than zero (`ozzo_rules.Duration` accepts a negative one).              |
| `NonNegativeDuration`     | Rule for a duration where zero means unset or no limit; rejects a negative one, unlike `DurationOrZero`.        |
| `ValidateStorage`         | Checks a storage selector and applies `NilOrNotEmpty` to its selected sub-config.                                 |
| `ErrorsToFlatMap`         | Flattens nested `validation.Errors` into dotted keys (`server.port`).                                           |
| `PrettyError`             | Renders a validation error as readable text.                                                                    |

Hold nested schemas by value and validate them with `NestedField(&c.Sub)`: `validation.Field(&c.Sub)` passes ozzo a copy of the value, which lacks the
pointer method set, so `Sub.Validate` is silently never called. The same holds for a slice of such values: use `NestedEach(&c.Items)`, since ozzo
descends into a slice only when the element type itself implements `Validatable`. An embedded struct needs `NestedField` too; its errors are inlined
into the parent's, matching its inline YAML keys.

Guard a map or slice of pointers with `validation.Field(&c.Items, NotNilEntries())`: ozzo calls `Validate` on every entry, a typed nil one included, so
an empty YAML entry (`items: {x: }`) would otherwise panic.

See the [module index](../README.md) for the other packages.

`ValidateStorage` preserves optional sub-configs: a selected nil value is valid. Add `validation.NotNil` in the schema when presence is required.
A nil selector pointer returns a `validation.ErrFieldPointer` error instead of panicking.
