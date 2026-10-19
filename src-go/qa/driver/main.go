// Command driver is the go vet tool that runs this module's project rules:
//
//	go vet -vettool=<built driver> ./...
//
// go-qa (go-qa-ci, configured by src-go/qa/go-qa.toml) builds it for each run and uses it as go
// vet's tool over src-go and this module; go-qa-ci's driver registers its bundled analyzers
// beside the rules here.
package main

import (
	"github.com/LongTermSupport/go-qa-ci/driver"
	"golang.org/x/tools/go/analysis"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/rules"
)

func analyzers() []*analysis.Analyzer {
	return rules.All()
}

func main() {
	driver.Main(analyzers()...)
}
