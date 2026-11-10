package vmtest

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// E74: lab-network prints the subnet the lab play wrote for a name into lab.env, so a scenario
// reads the owner's input without reading the lab's layout.
func TestE74LabNetwork(t *testing.T) {
	k := newKit(t)
	k.write("lab.env", "VMTEST_FEDORA_VERSION="+testFedora+"\nVMTEST_REPO_URL="+testRepoURL+"\nVMTEST_REPO_BRANCH="+testBranch+"\nVMTEST_NET_N1=192.0.2.0/24\n\n")
	k.wantCode(k.run("lab-network", "n1"), term.ExitOK)
	if got := k.out.String(); got != "192.0.2.0/24\n" {
		t.Fatalf("stdout %q", got)
	}
}

// E74: a name lab.env does not set is a failure naming the host_vars variable and the play.
func TestE74LabNetworkAbsent(t *testing.T) {
	k := newKit(t)
	k.wantCode(k.run("lab-network", "n2"), term.ExitFailure)
	k.wantErr("lab.env sets no VMTEST_NET_N2; set vm_test_lab_networks.n2 in host_vars and re-run " + playName)
	if k.out.Len() != 0 {
		t.Fatalf("stdout %q", k.out.String())
	}
}

// E74: a value that is not a canonical IPv4 CIDR is refused; so are a bad name and bad usage.
func TestE74LabNetworkRefusals(t *testing.T) {
	for _, v := range []string{"192.0.2.1/24", "192.0.2.0", "2001:db8::/64", "x"} {
		k := newKit(t)
		k.write("lab.env", "VMTEST_FEDORA_VERSION="+testFedora+"\nVMTEST_REPO_URL="+testRepoURL+"\nVMTEST_REPO_BRANCH="+testBranch+"\nVMTEST_NET_N1="+v+"\n")
		k.wantCode(k.run("lab-network", "n1"), term.ExitFailure)
		k.wantErr("VMTEST_NET_N1 is '" + v + "', not a canonical IPv4 CIDR")
	}
	for _, bad := range []string{"N1", "1n", "n-1", "abcdefghijklmnopq"} {
		k := newKit(t)
		k.wantCode(k.run("lab-network", bad), term.ExitUsage)
		k.wantErr("a lab network name is 1 to 16 of a-z and 0-9, starting with a letter; got '" + bad + "'")
	}
	for _, args := range [][]string{{"lab-network"}, {"lab-network", "a", "b"}} {
		k := newKit(t)
		k.wantCode(k.run(args...), term.ExitUsage)
		k.wantErr("usage: vmtest lab-network <name>")
	}
}
