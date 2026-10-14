// Command driver is the go vet tool that runs this module's project rules:
//
//	go vet -vettool=<built driver> ./...
//
// scripts/qa-go.bash builds it once and runs it over src-go.
package main

import (
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/unitchecker"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/rules"
)

func analyzers() []*analysis.Analyzer {
	return rules.All()
}

func main() {
	unitchecker.Main(analyzers()...)
}
