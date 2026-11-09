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
