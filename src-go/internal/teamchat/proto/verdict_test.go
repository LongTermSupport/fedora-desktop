package proto

import (
	"strings"
	"testing"
)

func TestReasonSetsAreA76(t *testing.T) {
	drop := "version schema size edit sender role target verb ref allowlist excluded stale rate forge-rate unresolved provenance"
	ignore := "type seen self admin not-addressed empty not-coordinator"
	if got := strings.Join(DropReasons, " "); got != drop {
		t.Fatalf("DropReasons = %s", got)
	}
	if got := strings.Join(IgnoreReasons, " "); got != ignore {
		t.Fatalf("IgnoreReasons = %s", got)
	}
	if got := strings.Join(SendRefusals, " "); got != drop+" secret body duplicate" {
		t.Fatalf("SendRefusals = %s", got)
	}
}
