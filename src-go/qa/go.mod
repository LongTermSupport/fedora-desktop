module github.com/LongTermSupport/fedora-desktop/src-go/qa

go 1.26.9

require (
	github.com/LongTermSupport/go-qa-ci v0.0.0-20261019182226-8e17b432bfd3
	github.com/yuin/goldmark v1.8.6
	go.yaml.in/yaml/v3 v3.0.5
	golang.org/x/mod v0.42.0
	golang.org/x/tools v0.52.0
	mvdan.cc/sh/v3 v3.14.1
)

require (
	github.com/pelletier/go-toml/v2 v2.4.3 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/telemetry v0.0.0-20261010193011-f71dc92da45e // indirect
	golang.org/x/vuln v1.9.0 // indirect
)

tool (
	github.com/LongTermSupport/go-qa-ci/cmd/go-qa
	golang.org/x/vuln/cmd/govulncheck
)
