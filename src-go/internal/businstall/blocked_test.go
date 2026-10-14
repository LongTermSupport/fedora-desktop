package businstall

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// Every directory the installer works in, replaced by a regular file after a good install:
// each command must say what it could not open and exit non-zero. The commands that never
// touch a path are not in its row.

var blockedPaths = []string{
	"var/lib/agent-bus-install",
	"var/lib/agent-bus-install/alpha",
	"var/lib/agent-bus-install/alpha/backups",
	"var/lib/agent-bus-install/alpha/firewalld.rules",
	"var/lib/agent-bus-applied",
	"var/lib/agent-bus",
	"var/lib/agent-bus/alpha",
	"var/lib/agent-bus/alpha/team.json",
	"var/lib/agent-bus/alpha/secrets",
	"etc/systemd/system",
	"usr/local/lib/agent-bus",
	"usr/local/share/agent-bus",
	"usr/local/share/agent-bus/kit",
	"usr/local/bin",
}

type blockedCommand struct {
	name string
	args func(h *harness, teamFile string) []string
}

var blockedCommands = []blockedCommand{
	{"check", func(*harness, string) []string { return []string{"check", "--team", "alpha"} }},
	{"backups", func(*harness, string) []string { return []string{"backups", "--team", "alpha"} }},
	{"backup-now", func(*harness, string) []string { return []string{"backup-now", "--team", "alpha"} }},
	{"restore", func(*harness, string) []string { return []string{"restore", "--team", "alpha", "--backup", "1"} }},
	{"remove", func(*harness, string) []string { return []string{"remove", "--team", "alpha"} }},
	{"remove --purge", func(*harness, string) []string { return []string{"remove", "--team", "alpha", "--purge"} }},
	{"team", func(_ *harness, file string) []string { return []string{"team", "--team-file", file} }},
	{"software", func(h *harness, _ string) []string { return []string{"software", "--source", h.source} }},
}

// blockedOutcome is the exit status of each command when the path is a file in the way;
// a command or path not listed succeeds, because it never needs that directory (or a missing
// one is the state it expects).
var blockedOutcome = map[string]map[string]term.ExitCode{
	"check": {
		"var/lib/agent-bus-install":                       term.ExitFailure,
		"var/lib/agent-bus-install/alpha":                 term.ExitFailure,
		"var/lib/agent-bus-install/alpha/firewalld.rules": term.ExitFailure,
		"var/lib/agent-bus":                               term.ExitFailure,
		"var/lib/agent-bus/alpha":                         term.ExitFailure,
		"var/lib/agent-bus/alpha/team.json":               term.ExitConfig,
		"etc/systemd/system":                              term.ExitFailure,
		"usr/local/lib/agent-bus":                         term.ExitFailure,
	},
	"backups": {
		"var/lib/agent-bus-install":               term.ExitFailure,
		"var/lib/agent-bus-install/alpha":         term.ExitFailure,
		"var/lib/agent-bus-install/alpha/backups": term.ExitFailure,
		"var/lib/agent-bus":                       term.ExitFailure,
		"var/lib/agent-bus/alpha":                 term.ExitFailure,
	},
	"backup-now": {
		"var/lib/agent-bus-install":               term.ExitFailure,
		"var/lib/agent-bus-install/alpha":         term.ExitFailure,
		"var/lib/agent-bus-install/alpha/backups": term.ExitFailure,
		"var/lib/agent-bus":                       term.ExitFailure,
		"var/lib/agent-bus/alpha":                 term.ExitFailure,
		"var/lib/agent-bus/alpha/team.json":       term.ExitConfig,
		"var/lib/agent-bus/alpha/secrets":         term.ExitFailure,
	},
	"restore": {
		"var/lib/agent-bus-install":               term.ExitFailure,
		"var/lib/agent-bus-install/alpha":         term.ExitFailure,
		"var/lib/agent-bus-install/alpha/backups": term.ExitFailure,
		"var/lib/agent-bus-applied":               term.ExitFailure,
		"var/lib/agent-bus":                       term.ExitFailure,
		"var/lib/agent-bus/alpha":                 term.ExitFailure,
		"var/lib/agent-bus/alpha/team.json":       term.ExitConfig,
		"etc/systemd/system":                      term.ExitFailure,
		"usr/local/lib/agent-bus":                 term.ExitFailure,
		"usr/local/bin":                           term.ExitFailure,
	},
	"remove": {
		"var/lib/agent-bus-install":                       term.ExitFailure,
		"var/lib/agent-bus-install/alpha":                 term.ExitFailure,
		"var/lib/agent-bus-install/alpha/firewalld.rules": term.ExitFailure,
		"var/lib/agent-bus-applied":                       term.ExitFailure,
		"etc/systemd/system":                              term.ExitFailure,
	},
	"remove --purge": {
		"var/lib/agent-bus-install":                       term.ExitFailure,
		"var/lib/agent-bus-install/alpha":                 term.ExitFailure,
		"var/lib/agent-bus-install/alpha/firewalld.rules": term.ExitFailure,
		"var/lib/agent-bus-applied":                       term.ExitFailure,
		"var/lib/agent-bus":                               term.ExitFailure,
		"etc/systemd/system":                              term.ExitFailure,
	},
	"team": {
		"var/lib/agent-bus-install":                       term.ExitFailure,
		"var/lib/agent-bus-install/alpha":                 term.ExitFailure,
		"var/lib/agent-bus-install/alpha/firewalld.rules": term.ExitFailure,
		"var/lib/agent-bus-applied":                       term.ExitFailure,
		"var/lib/agent-bus":                               term.ExitFailure,
		"var/lib/agent-bus/alpha":                         term.ExitFailure,
		"var/lib/agent-bus/alpha/team.json":               term.ExitConfig,
		"var/lib/agent-bus/alpha/secrets":                 term.ExitFailure,
		"etc/systemd/system":                              term.ExitFailure,
		"usr/local/lib/agent-bus":                         term.ExitFailure,
		"usr/local/bin":                                   term.ExitFailure,
	},
	"software": {
		"var/lib/agent-bus-install":     term.ExitFailure,
		"var/lib/agent-bus":             term.ExitFailure,
		"etc/systemd/system":            term.ExitFailure,
		"usr/local/lib/agent-bus":       term.ExitFailure,
		"usr/local/share/agent-bus":     term.ExitFailure,
		"usr/local/share/agent-bus/kit": term.ExitFailure,
		"usr/local/bin":                 term.ExitFailure,
	},
}

func TestAPathThatIsAFileFailsTheCommandThatNeedsItAndNoOtherOne(t *testing.T) {
	for _, command := range blockedCommands {
		for _, blocked := range blockedPaths {
			t.Run(command.name+" with "+blocked+" a file", func(t *testing.T) {
				h := newHarness(t)
				file := h.installTeam(nil)
				if code := h.run("backup-now", "--team", "alpha"); code != 0 {
					t.Fatalf("backup-now exited %d:\n%s", code, h.stderr())
				}
				if err := removeAll(h.rel(blocked)); err != nil {
					t.Fatal(err)
				}
				h.write(h.rel(blocked), "in the way\n", 0o644)
				want := blockedOutcome[command.name][blocked]
				code := h.run(command.args(h, file)...)
				if code != want {
					t.Fatalf("exit %d, want %d\nstderr: %s", code, want, h.stderr())
				}
				if want != term.ExitOK && h.stderr() == "" {
					t.Fatal("it failed without saying why")
				}
			})
		}
	}
}
