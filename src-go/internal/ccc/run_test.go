package ccc

import (
	"slices"
	"testing"
)

func TestSessionArgsMountOnlyTheFolderAndTheState(t *testing.T) {
	got := sessionArgs(session{
		Agent: Claude, Folder: "/srv/u/CCC", State: "/srv/u/.local/share/ccc/claude",
		Name: "ccc-claude-42", TimeZone: "Europe/London", Term: "xterm-256color",
	})
	want := []string{
		"run", "--rm", "--interactive", "--tty",
		"--name", "ccc-claude-42",
		"--label", "ccc=true", "--label", "ccc-agent=claude",
		"--network", "podman",
		"--entrypoint", "/usr/bin/tini",
		"--volume", "/srv/u/CCC:/workspace:z",
		"--volume", "/srv/u/.local/share/ccc/claude:/var/lib/ccc:z",
		"--workdir", "/workspace",
		"--env", "CLAUDE_CODE_OAUTH_TOKEN",
		"--env", "TZ=Europe/London",
		"--env", "TERM=xterm-256color",
		"--env", "COLORTERM=truecolor",
		"--env", "CLAUDE_CONFIG_DIR=/var/lib/ccc",
		"--env", "IS_SANDBOX=1",
		"--env", "CLAUDE_CODE_AUTO_COMPACT_WINDOW=600000",
		"claude-yolo:latest",
		"--", "claude", "--dangerously-skip-permissions",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestSessionArgsLeaveOutAnUnknownZone(t *testing.T) {
	got := sessionArgs(session{Agent: Claude, Folder: "/f", State: "/s", Name: "n", Continue: true})
	if slices.Contains(got, "TZ=") || slices.ContainsFunc(got, func(a string) bool { return len(a) > 3 && a[:3] == "TZ=" }) {
		t.Fatalf("got %v", got)
	}
	if !slices.Contains(got, "TERM=xterm") || got[len(got)-1] != "--continue" {
		t.Fatalf("got %v", got)
	}
}

func TestSignInArgsMountNothing(t *testing.T) {
	got := signInArgs(Claude)
	want := []string{"run", "--rm", "--interactive", "--tty", "--network", "podman", "--entrypoint", "claude", "claude-yolo:latest", "setup-token"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q", got)
	}
}

func TestVersionAtLeast(t *testing.T) {
	for _, c := range []struct {
		have, want string
		ok         bool
	}{
		{"2.79.15", "2.79.15", true},
		{"2.80.0", "2.79.15", true},
		{"3.0.0", "2.79.15", true},
		{"2.79.14", "2.79.15", false},
		{"2.9.99", "2.10.0", false},
		{"2.79", "2.79.15", false},
		{"junk", "2.79.15", false},
		{"", "2.79.15", false},
		{"2.79.15", "junk", false},
	} {
		if got := versionAtLeast(c.have, c.want); got != c.ok {
			t.Fatalf("%s >= %s: got %v", c.have, c.want, got)
		}
	}
}
