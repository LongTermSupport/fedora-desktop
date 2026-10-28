package nord

import (
	"errors"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// E1: --help and --version print on stdout and exit 0, running nothing.
func TestHelpAndVersion(t *testing.T) {
	for _, arg := range []string{"-h", "--help"} {
		w := newWorld(t)
		wantCode(t, w.run(arg), term.ExitOK)
		w.wantOut("connect <name>", "nord list", "--debug", "~/.config/nordvpn/configs/",
			"play-nordvpn-openvpn.yml")
		if w.stderr.Len() != 0 || len(w.fake.Calls()) != 0 {
			t.Errorf("%s: stderr %q, calls %d", arg, w.stderr.String(), len(w.fake.Calls()))
		}
	}
	for _, args := range [][]string{{"-v"}, {"--version"}, {"--debug", "--version"}} {
		w := newWorld(t)
		wantCode(t, w.run(args...), term.ExitOK)
		if got := w.stdout.String(); got != "nord "+Version+"\n" {
			t.Errorf("%q: stdout %q", args, got)
		}
	}
	if Version != "2.0.0" {
		t.Errorf("Version %q", Version)
	}
}

// E2: usage errors exit 64 with one line naming --help, and run nothing.
func TestUsageErrors(t *testing.T) {
	cases := map[string][]string{
		"unknown command: frob":             {"frob"},
		"unknown option: -x":                {"-x"},
		"unknown option: --nope":            {"--nope"},
		"list takes no argument":            {"list", "extra"},
		"status takes no argument":          {"status", "x"},
		"disconnect takes no argument":      {"disconnect", "x"},
		"cleanup takes no argument":         {"cleanup", "x"},
		"--yes is only for cleanup":         {"disconnect", "--yes"},
		"list-active takes no argument":     {"list-active", "x"},
		"connect takes one name":            {"connect", "a", "b"},
		"switch takes one name":             {"switch", "a", "b"},
		"unknown option after the command":  {"connect", "--debug"},
		"--help is only before the command": {"list", "--help"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			wantCode(t, w.run(args...), term.ExitUsage)
			w.wantErr("nord: ", " — see 'nord --help'.\n")
			if w.stdout.Len() != 0 || len(w.fake.Calls()) != 0 {
				t.Errorf("stdout %q, calls %d", w.stdout.String(), len(w.fake.Calls()))
			}
		})
	}
}

// E2: connect or switch without a name is a usage error followed by the config names.
func TestMissingNameListsTheConfigs(t *testing.T) {
	for _, cmd := range []string{"connect", "switch"} {
		w := newWorld(t)
		w.config("uk1.nordvpn.com.udp.ovpn", "de7.nordvpn.com.tcp.ovpn")
		wantCode(t, w.run(cmd), term.ExitUsage)
		w.wantErr("nord: "+cmd+" needs a config name — see 'nord --help'.\n", "Available configs:\n", "  de7\n", "  uk1\n")
		if w.stdout.Len() != 0 || len(w.fake.Calls()) != 0 {
			t.Errorf("stdout %q, calls %d", w.stdout.String(), len(w.fake.Calls()))
		}
	}
}

// E3: --debug names each command before it runs.
func TestDebugNamesEachCommand(t *testing.T) {
	w := newWorld(t)
	wantCode(t, w.run("--debug", "list-active"), term.ExitOK)
	w.wantErr("nord: run: systemctl is-active --quiet NetworkManager\n",
		"nord: run: nmcli -t -f NAME,UUID,TYPE connection show\n")
	w2 := newWorld(t)
	wantCode(t, w2.run("list-active"), term.ExitOK)
	if strings.Contains(w2.stderr.String(), "run:") {
		t.Errorf("without --debug: %q", w2.stderr.String())
	}
}

// E4: an unwritable stdout fails a run that would succeed.
func TestUnwritableStdoutFails(t *testing.T) {
	w := newWorld(t)
	env := w.envFor()
	env.Stdout = failWriter{}
	wantCode(t, App{Env: env}.Run(w.runCtx(), []string{"--version"}), term.ExitFailure)
	w.wantErr("nord: could not write the output")
}

var errWrite = errors.New("write refused")

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errWrite }
