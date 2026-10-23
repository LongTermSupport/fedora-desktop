package rules

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// The keeper's tests read keeper.log only through the harness's turn-aware reader: the
// originating barrier that polled the file itself, a direct read, a method value, a method
// expression and a goroutine are flagged; the allowed readers, the keeper's own code, another
// file of the run and another package's tests are not.
func TestKeeperLogBarrier(t *testing.T) {
	allow := []allowance{
		{pkg: "internal/teamchat/member/keeper", fn: "world.keeperLog", reason: "the one reader"},
		{pkg: "internal/teamchat/member/keeper", fn: "world.turned", reason: "the turn-end copy"},
	}
	analysistest.Run(t, analysistest.TestData(), newKeeperLogBarrier(allow),
		"fix/internal/teamchat/member/keeper", "fix/internal/teamchat/member/elsewhere")
}

func TestEveryKeeperLogReaderHasAReason(t *testing.T) {
	for _, a := range keeperLogReaders {
		if a.reason == "" || a.pkg == "" || a.fn == "" {
			t.Errorf("allowance %+v needs a package, a function and a reason", a)
		}
	}
}
