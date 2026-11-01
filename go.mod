module github.com/altessa-s/go-nomos

go 1.27.0

toolchain go1.27.3

require (
	github.com/BurntSushi/toml v1.6.0
	github.com/MakeNowJust/heredoc v1.0.0
	github.com/altessa-s/go-atlas-core v0.3.1
	github.com/altessa-s/go-atlas-net v0.2.1
	github.com/altessa-s/ozzo-rules v1.16.0
	github.com/go-ozzo/ozzo-validation/v4 v4.5.0
	github.com/spf13/cobra v1.10.2
	github.com/stretchr/testify v1.12.1
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/adrg/postcode v0.1.0 // indirect
	github.com/asaskevich/govalidator v0.0.0-20230301143203-a9d515a09cc2 // indirect
	github.com/hashicorp/golang-lru/v2 v2.0.7 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/nyaruka/phonenumbers v1.8.1 // indirect
	github.com/sony/gobreaker/v2 v2.4.0 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/net v0.62.0 // indirect
	golang.org/x/text v0.43.0 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
)

// The history was rewritten after these releases; their published checksums no longer match the tags.
retract [v0.1.0, v0.1.3]
