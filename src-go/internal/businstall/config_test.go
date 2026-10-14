package businstall

import "testing"

// The names below are what the units, the sudo argv of ccy-teams and the sudoers rule spell
// out; the Production and Test configurations are covered in app_test.go.
func TestTheProgramAndAccountNamesAreTheDeployedOnes(t *testing.T) {
	if prog != "agent-bus-install" || svcName != "agent-bus" {
		t.Fatalf("prog %q, svcName %q", prog, svcName)
	}
	if productionTool != "/usr/local/bin/agent-bus" {
		t.Fatalf("tool %q", productionTool)
	}
}
