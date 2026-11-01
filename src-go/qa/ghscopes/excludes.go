package ghscopes

import (
	"fmt"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/semgrepreport"
)

// readRule checks the rule and fixture are regular files and reads the rule's own paths.exclude
// globs: files semgrep drops without saying so.
func (g *gate) readRule() (semgrepreport.Excludes, error) {
	for _, rel := range []string{ruleFile, fixtureFile} {
		info, err := os.Stat(g.path(rel))
		if err != nil {
			return nil, fmt.Errorf("cannot read %s: %w", rel, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%s is not a regular file", rel)
		}
	}
	data, err := os.ReadFile(g.path(ruleFile))
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", ruleFile, err)
	}
	return semgrepreport.ParseExcludes(ruleFile, string(data))
}
