package admin

import (
	"bytes"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/unitfiles"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// knownFiles is how a units usage error names the files.
const knownFiles = "the files are teamchat-server@.service, teamchat-tls@.service, teamchat-health.service, " +
	"teamchat-health.timer, teamchat-backup.service, teamchat-backup.timer, stub-resolv.conf, resolv.conf, teamchat.conf"

// `units list` is one line per installed file, its name and its install path, in the play's
// order, for every caller class; it reaches no host seam.
func TestUnitsList(t *testing.T) {
	want := "teamchat-server@.service\t/etc/systemd/system/teamchat-server@.service\n" +
		"teamchat-tls@.service\t/etc/systemd/system/teamchat-tls@.service\n" +
		"teamchat-health.service\t/etc/systemd/system/teamchat-health.service\n" +
		"teamchat-health.timer\t/etc/systemd/system/teamchat-health.timer\n" +
		"teamchat-backup.service\t/etc/systemd/system/teamchat-backup.service\n" +
		"teamchat-backup.timer\t/etc/systemd/system/teamchat-backup.timer\n" +
		"stub-resolv.conf\t/usr/local/share/teamchat/resolve/stub-resolv.conf\n" +
		"resolv.conf\t/usr/local/share/teamchat/resolve/resolv.conf\n" +
		"teamchat.conf\t/etc/tmpfiles.d/teamchat.conf\n"
	for _, c := range []Class{ClassRoot, ClassCtl, ClassOther} {
		if got := run(t, procFor(c), "", "units", "list"); got.code != term.ExitOK || got.stdout != want || got.stderr != "" {
			t.Errorf("units list by %v = %+v", c, got)
		}
	}
	if got := run(t, rootProc(), "", "units", "list", "--all"); got.code != term.ExitUsage || got.stdout != "" ||
		got.stderr != "teamchat-admin: units list takes no arguments\n" {
		t.Errorf("units list --all = %+v", got)
	}
}

// `units print <name>` writes exactly the file's text, for every caller class.
func TestUnitsPrint(t *testing.T) {
	for _, f := range unitfiles.Files() {
		for _, c := range []Class{ClassRoot, ClassCtl, ClassOther} {
			if got := run(t, procFor(c), "", "units", "print", f.Name); got.code != term.ExitOK || got.stdout != f.Text || got.stderr != "" {
				t.Errorf("units print %s by %v = %d %q, stdout differs: %v", f.Name, c, got.code, got.stderr, got.stdout != f.Text)
			}
		}
	}
}

// A name that is not one file's, no name or more than one is a usage error naming the files.
func TestUnitsPrintRefusesWhatIsNotOneFile(t *testing.T) {
	cases := map[string]struct {
		args []string
		want string
	}{
		"no name":      {nil, "units print takes one file name: " + knownFiles},
		"two names":    {[]string{"resolv.conf", "teamchat-health.timer"}, "units print takes one file name: " + knownFiles},
		"unknown":      {[]string{"teamchat-seats.service"}, `units print: no file "teamchat-seats.service": ` + knownFiles},
		"an instance":  {[]string{"teamchat-server@ops.service"}, `units print: no file "teamchat-server@ops.service": ` + knownFiles},
		"a path":       {[]string{"/etc/systemd/system/teamchat-health.service"}, `units print: no file "/etc/systemd/system/teamchat-health.service": ` + knownFiles},
		"a flag":       {[]string{"--name", "resolv.conf"}, "units print takes one file name: " + knownFiles},
		"a line break": {[]string{"a\nb"}, `units print: no file "a\nb": ` + knownFiles},
	}
	for name, c := range cases {
		got := run(t, rootProc(), "", append([]string{"units", "print"}, c.args...)...)
		if got.code != term.ExitUsage || got.stdout != "" || got.stderr != "teamchat-admin: "+c.want+"\n" {
			t.Errorf("%s: %+v, want 64 %q", name, got, c.want)
		}
	}
}

// A stdout that cannot be written is a failure (CA7).
func TestUnitsUnwritableStdoutFails(t *testing.T) {
	for _, args := range [][]string{{"units", "list"}, {"units", "print", "resolv.conf"}} {
		var stderr bytes.Buffer
		code := Run(t.Context(), Env{Args: args, Stdout: failWriter{}, Stderr: &stderr, Proc: ownerProc(), Digest: digest})
		if code != term.ExitFailure || stderr.String() != "teamchat-admin: cannot write the answer: closed\n" {
			t.Errorf("%s: %d %q", strings.Join(args, " "), code, stderr.String())
		}
	}
}
