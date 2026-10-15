// Command driver is the go vet tool that runs this module's project rules:
//
//	go vet -vettool=<built driver> ./...
//
// go-qa (the go-qa-ci toolchain, configured by src-go/qa/go-qa.toml) builds it for the run and
// uses it as go vet's tool over src-go and this module; go-qa-ci's driver registers its bundled
// analyzers beside the rules here.
package main

import (
	"golang.org/x/tools/go/analysis"

	"github.com/LongTermSupport/go-qa-ci/driver"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/rules"
)

func analyzers() []*analysis.Analyzer {
	return rules.All()
}

func main() {
	driver.Main(analyzers()...)
}
