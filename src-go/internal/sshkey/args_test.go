package sshkey

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestE1HelpPrintsTheUsageOnStdout(t *testing.T) {
	for _, tool := range []Tool{SSH, SCP, CopyID} {
		for _, flag := range []string{"-h", "--help"} {
			w := newWorld(t)
			wantCode(t, w.run(tool, flag), term.ExitOK)
			out := w.stdout.String()
			if !strings.Contains(out, "Usage: "+tool.Name) || !strings.Contains(out, tool.KeyFlag+" ") {
				t.Errorf("%s %s: usage is\n%s", tool.Name, flag, out)
			}
			if w.stderr.Len() != 0 || len(w.ran()) != 0 {
				t.Errorf("%s %s: stderr %q, ran %v", tool.Name, flag, w.stderr.String(), w.ran())
			}
		}
	}
}

func TestE1ForwardingIsInTheUsageOfSSHAndSCPOnly(t *testing.T) {
	for _, tool := range []Tool{SSH, SCP, CopyID} {
		w := newWorld(t)
		w.run(tool, "--help")
		if got := strings.Contains(w.stdout.String(), "-A"); got != tool.forwards() {
			t.Errorf("%s: usage mentions -A %v", tool.Name, got)
		}
	}
}

func TestE2NoArgumentsIsAUsageError(t *testing.T) {
	for _, tool := range []Tool{SSH, SCP, CopyID} {
		w := newWorld(t)
		wantCode(t, w.run(tool), term.ExitUsage)
		w.stderrHas(tool.Name + ": no arguments — see '" + tool.Name + " --help'.")
		if w.stdout.Len() != 0 {
			t.Errorf("stdout %q", w.stdout.String())
		}
	}
}

func TestE3TheKeyFlagNeedsAValue(t *testing.T) {
	for _, tool := range []Tool{SSH, SCP, CopyID} {
		for _, args := range [][]string{{tool.KeyFlag}, {tool.KeyFlag, ""}} {
			w := newWorld(t)
			wantCode(t, w.run(tool, args...), term.ExitUsage)
			w.stderrHas(tool.KeyFlag + " needs a key path")
			if len(w.ran()) != 0 {
				t.Errorf("ran %v", w.ran())
			}
		}
	}
}

func TestE4ASecondIdentityIsRefused(t *testing.T) {
	cases := []struct {
		tool Tool
		args []string
	}{
		{SSH, []string{"-i", "k", "-i", "other", "host.example.com"}},
		{SSH, []string{"-i", "k", "-vi", "other", "host.example.com"}},
		{SSH, []string{"-i", "k", "-o", "IdentityFile=other", "host.example.com"}},
		{SSH, []string{"-i", "k", "-oidentityfile other", "host.example.com"}},
		{SSH, []string{"-i", "k", "host.example.com", "-o", "IdentityFile=other"}},
		{SCP, []string{"-i", "k", "-i", "other", "f", "host.example.com:/tmp/"}},
		{SCP, []string{"-i", "k", "-o", "IdentityFile=other", "f", "host.example.com:/tmp/"}},
		{CopyID, []string{"--auth-key", "k", "-o", "IdentityFile=other", "host.example.com"}},
	}
	for _, c := range cases {
		_, err := c.tool.parse(c.args)
		if !errors.Is(err, errSecondKey) {
			t.Errorf("%s %v: err %v, want the second key refused", c.tool.Name, c.args, err)
		}
	}
	// ssh-copy-id's own -i is the key to install, not a second login key.
	if _, err := CopyID.parse([]string{"--auth-key", "k", "-i", "new.pub", "host.example.com"}); err != nil {
		t.Errorf("ssh-copy-id -i: %v", err)
	}
	// A remote command's words are not options.
	if _, err := SSH.parse([]string{"-i", "k", "host.example.com", "grep", "-i", "x"}); err != nil {
		t.Errorf("remote command with -i: %v", err)
	}
}

func TestE4RefusalRunsNothing(t *testing.T) {
	w := newWorld(t)
	key := w.key("id_a")
	wantCode(t, w.run(SSH, "-i", key, "-i", key, "host.example.com"), term.ExitUsage)
	w.stderrHas("give the one key as the first argument")
	if len(w.ran()) != 0 {
		t.Errorf("ran %v", w.ran())
	}
}

func TestE13ForwardingIsAskedByTheOptionsAlone(t *testing.T) {
	cases := []struct {
		tool Tool
		args []string
		want bool
	}{
		{SSH, []string{"host.example.com"}, false},
		{SSH, []string{"-A", "host.example.com"}, true},
		{SSH, []string{"-tA", "host.example.com"}, true},
		{SSH, []string{"host.example.com", "-A"}, true},
		{SSH, []string{"-A", "-a", "host.example.com"}, false},
		{SSH, []string{"-a", "-A", "host.example.com"}, true},
		{SSH, []string{"host.example.com", "ls", "-A"}, false},
		{SSH, []string{"-lAdmin", "host.example.com"}, false},
		{SSH, []string{"-l", "Admin", "host.example.com"}, false},
		{SSH, []string{"-oForwardAgent=no", "host.example.com"}, false},
		{SSH, []string{"-o", "ForwardAgent=yes", "host.example.com"}, true},
		{SSH, []string{"-o", "forwardagent YES", "host.example.com"}, true},
		{SSH, []string{"-o", "ForwardAgent=no", "-o", "ForwardAgent=yes", "host.example.com"}, false},
		{SSH, []string{"-o", "ForwardAgent=no", "-A", "host.example.com"}, true},
		{SSH, []string{"-P", "tag", "host.example.com", "ls", "-A"}, false},
		{SSH, []string{"--", "host.example.com", "-A"}, false},
		{SCP, []string{"-A", "f", "host.example.com:/tmp/"}, true},
		{SCP, []string{"f", "-A"}, false},
		{SCP, []string{"-o", "ForwardAgent=yes", "f", "host.example.com:/tmp/"}, true},
		{SCP, []string{"-lA", "f", "host.example.com:/tmp/"}, false},
		{CopyID, []string{"-A", "host.example.com"}, false},
	}
	for _, c := range cases {
		inv, err := c.tool.parse(append([]string{c.tool.KeyFlag, "k"}, c.args...))
		if err != nil {
			t.Errorf("%s %v: %v", c.tool.Name, c.args, err)
			continue
		}
		if inv.forward != c.want {
			t.Errorf("%s %v: forward %v, want %v", c.tool.Name, c.args, inv.forward, c.want)
		}
	}
}

func TestE13AnotherAgentIsRefused(t *testing.T) {
	for _, value := range []string{"ForwardAgent=/run/other.sock", "ForwardAgent $SOME_SOCK"} {
		_, err := SSH.parse([]string{"-i", "k", "-o", value, "host.example.com"})
		if !errors.Is(err, errOtherAgent) {
			t.Errorf("%s: err %v", value, err)
		}
	}
}

func TestE13TheRemoteCommandAndTheModesWithout(t *testing.T) {
	cases := []struct {
		args          []string
		command, none bool
	}{
		{[]string{"host.example.com"}, false, false},
		{[]string{"host.example.com", "uptime"}, true, false},
		{[]string{"-p", "22", "host.example.com", "-v"}, false, false},
		{[]string{"-N", "host.example.com"}, false, true},
		{[]string{"-W", "h:22", "host.example.com"}, false, true},
		{[]string{"-s", "host.example.com", "sftp"}, true, true},
		{[]string{"host.example.com", "--", "uptime"}, true, false},
	}
	for _, c := range cases {
		inv, err := SSH.parse(append([]string{"-i", "k"}, c.args...))
		if err != nil {
			t.Fatalf("%v: %v", c.args, err)
		}
		if inv.command != c.command || inv.noCommand != c.none {
			t.Errorf("%v: command %v none %v, want %v %v", c.args, inv.command, inv.noCommand, c.command, c.none)
		}
	}
}

func TestE11TheDestinationsProbed(t *testing.T) {
	cases := []struct {
		tool Tool
		args []string
		want [][]string
	}{
		{
			SSH,
			[]string{"-p", "2222", "admin@host.example.com", "uptime"},
			[][]string{{"-p", "2222", "admin@host.example.com", "uptime"}},
		},
		{SSH, []string{"-v"}, nil},
		{
			SCP,
			[]string{"-P", "2222", "-F", "cfg", "-r", "dir", "admin@host.example.com:/tmp/"},
			[][]string{{"-p", "2222", "-F", "cfg", "-l", "admin", "host.example.com"}},
		},
		{SCP, []string{"a", "b"}, nil},
		{SCP, []string{"./x:y", "/abs/p:q", "b"}, nil},
		{
			SCP,
			[]string{"scp://bob@host.example.com:2200/x", "[2001:db8::1]:/y", "host.example.com:z", "."},
			[][]string{{"-l", "bob", "-p", "2200", "host.example.com"}, {"2001:db8::1"}, {"host.example.com"}},
		},
		{SCP, []string{"host.example.com:a", "host.example.com:b", "."}, [][]string{{"host.example.com"}}},
		{
			CopyID,
			[]string{"-i", "new.pub", "-p", "2222", "-o", "Port=1", "-f", "admin@host.example.com"},
			[][]string{{"-p", "2222", "-o", "Port=1", "admin@host.example.com"}},
		},
		{CopyID, []string{"-i", "admin@host.example.com"}, [][]string{{"admin@host.example.com"}}},
		{CopyID, []string{"-n"}, nil},
	}
	for _, c := range cases {
		inv, err := c.tool.parse(append([]string{c.tool.KeyFlag, "k"}, c.args...))
		if err != nil {
			t.Errorf("%s %v: %v", c.tool.Name, c.args, err)
			continue
		}
		var got [][]string
		for _, d := range inv.dests {
			got = append(got, d.args)
		}
		if !slices.EqualFunc(got, c.want, slices.Equal) {
			t.Errorf("%s %v: probes %q, want %q", c.tool.Name, c.args, got, c.want)
		}
	}
}
