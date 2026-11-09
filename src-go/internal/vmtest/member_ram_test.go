package vmtest

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// E69: --ram-mib sets the member's memory, from 768 MiB up to the base's, in the boot and in the
// RAM check; anything else is a usage error.
func TestE69MemberRAM(t *testing.T) {
	k := memberKit(t)
	k.wantCode(k.run("member-up", "m1", "server-fast", "--ram-mib", "1024"), term.ExitOK)
	if got := argAfter(k.mustCall("virt-install").Args, "--memory"); got != "1024" {
		t.Fatalf("--memory %q, want 1024", got)
	}

	for _, edge := range []string{"768", "4096"} {
		k = memberKit(t)
		k.wantCode(k.run("member-up", "m1", "server-fast", "--ram-mib", edge), term.ExitOK)
		if got := argAfter(k.mustCall("virt-install").Args, "--memory"); got != edge {
			t.Fatalf("--memory %q, want %s", got, edge)
		}
	}

	// the check counts what was asked for, not the base's figure: 1024 fits a host whose ceiling
	// is 1050 MiB, the base's 4096 does not.
	k = memberKit(t)
	k.hostMiB = 1400
	k.wantCode(k.run("member-up", "m1", "server-fast", "--ram-mib", "1024"), term.ExitOK)
	k = memberKit(t)
	k.hostMiB = 1400
	k.wantCode(k.run("member-up", "m1", "server-fast"), term.ExitFailure)
	k.wantErr("guest 4096 MiB exceeds the 1050 MiB ceiling of a 1400 MiB host")

	// no option is the base's figure
	k = memberKit(t)
	k.wantCode(k.run("member-up", "m1", "server-fast"), term.ExitOK)
	if got := argAfter(k.mustCall("virt-install").Args, "--memory"); got != "4096" {
		t.Fatalf("--memory %q, want the base's 4096", got)
	}
}

// E69: a figure that is not a whole number of at least 768, a missing figure, a second figure and
// one above the base's are usage errors that boot nothing.
func TestE69MemberRAMUsage(t *testing.T) {
	for _, tc := range []struct {
		args []string
		msg  string
	}{
		{[]string{"--ram-mib", "767"}, "--ram-mib wants a whole number of MiB, 768 or more; got '767'"},
		{[]string{"--ram-mib", "0"}, "--ram-mib wants a whole number of MiB, 768 or more; got '0'"},
		{[]string{"--ram-mib", "-1024"}, "--ram-mib wants a whole number of MiB, 768 or more; got '-1024'"},
		{[]string{"--ram-mib", "1.5"}, "--ram-mib wants a whole number of MiB, 768 or more; got '1.5'"},
		{[]string{"--ram-mib", "01024"}, "--ram-mib wants a whole number of MiB, 768 or more; got '01024'"},
		{[]string{"--ram-mib", "lots"}, "--ram-mib wants a whole number of MiB, 768 or more; got 'lots'"},
		{[]string{"--ram-mib"}, "--ram-mib wants a whole number of MiB, 768 or more; got ''"},
		{[]string{"--ram-mib", "1024", "--ram-mib", "2048"}, "--ram-mib is given twice"},
		{[]string{"--ram-mib", "4097"}, "--ram-mib 4097 is more than the 4096 MiB base server-fast-44 has"},
	} {
		k := memberKit(t)
		k.wantCode(k.run(append([]string{"member-up", "m1", "server-fast"}, tc.args...)...), term.ExitUsage)
		k.wantErr("ERROR: " + tc.msg)
		if k.count("virt-install") != 0 || k.exists("members/m1") {
			t.Fatalf("%v: a usage error booted or made a member", tc.args)
		}
	}
}

// E70: the RAM check of a member and of a run counts every running lab guest beside this one, and
// a guest whose memory cannot be read is a failure, never zero.
func TestE70RAMCountsEveryGuest(t *testing.T) {
	k := memberKit(t)
	k.hostMiB = 10000 // the ceiling is 7500
	k.lv.domains = []string{"vmtest-member-x"}
	k.lv.mem = map[string]int{"vmtest-member-x": 4096}
	k.wantCode(k.run("member-up", "m1", "server-fast"), term.ExitFailure)
	k.wantErr("ERROR: the host cannot carry a member on server-fast-44: guests 4096 MiB and this 4096 MiB exceed the 7500 MiB ceiling of a 10000 MiB host")
	if k.count("virt-install") != 0 || k.exists("members/m1") {
		t.Fatal("a member past the sum was booted")
	}
	if k.count("dominfo vmtest-member-x") != 1 {
		t.Fatalf("calls %v", k.lines("virsh"))
	}

	// the sum may reach the ceiling: 4096 and 3404 are 7500
	k.wantCode(k.run("member-up", "m1", "server-fast", "--ram-mib", "3404"), term.ExitOK)

	// every guest is counted: two of 2048 and this 4096 are 8192
	k = memberKit(t)
	k.hostMiB = 10000
	k.lv.domains = []string{"vmtest-member-x", "vmtest-member-y"}
	k.lv.mem = map[string]int{"vmtest-member-x": 2048, "vmtest-member-y": 2048}
	k.wantCode(k.run("member-up", "m1", "server-fast"), term.ExitFailure)
	k.wantErr("guests 4096 MiB and this 4096 MiB exceed the 7500 MiB ceiling")

	// a domain that is not the lab's is not counted
	k = memberKit(t)
	k.hostMiB = 10000
	k.lv.domains = []string{"other-guest"}
	k.lv.mem = map[string]int{"other-guest": 8000}
	k.wantCode(k.run("member-up", "m1", "server-fast"), term.ExitOK)
	if k.count("dominfo") != 0 {
		t.Fatalf("a domain outside the lab was asked about: %v", k.lines("virsh"))
	}

	k = memberKit(t)
	k.lv.domains = []string{"vmtest-member-x"}
	k.lv.infoFails = true
	k.wantCode(k.run("member-up", "m1", "server-fast"), term.ExitFailure)
	k.wantErr("ERROR: the host cannot carry a member on server-fast-44: cannot read the memory of vmtest-member-x:")
	if k.count("virt-install") != 0 {
		t.Fatal("a member was booted though a guest's memory could not be read")
	}

	k = memberKit(t)
	k.lv.domains = []string{"vmtest-member-x"}
	k.lv.infoText = "Name: vmtest-member-x\n"
	k.wantCode(k.run("member-up", "m1", "server-fast"), term.ExitFailure)
	k.wantErr("cannot read the memory of vmtest-member-x: no 'Max memory' line")

	// a run counts the guests too
	k = runKit(t, "pass")
	k.hostMiB = 10000
	k.lv.domains = []string{"vmtest-member-x"}
	k.lv.mem = map[string]int{"vmtest-member-x": 4096}
	k.wantCode(k.run("run", "server-fast-provision"), term.ExitFailure)
	k.wantErr("ERROR: the host cannot carry a run on server-fast-44 (stage: clone): guests 4096 MiB and this 4096 MiB exceed the 7500 MiB ceiling of a 10000 MiB host")
	if k.exists("runs/"+runID) || k.count("qemu-img") != 0 {
		t.Fatal("cloned past the sum")
	}
}
