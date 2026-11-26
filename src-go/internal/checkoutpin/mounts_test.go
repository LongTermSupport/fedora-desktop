package checkoutpin

import (
	"slices"
	"strings"
	"testing"
)

const (
	testHome    = "/home/u"
	testProject = "/home/u/Projects/app"
)

// parse validates content as a mounts file with no extra denied paths.
func parse(t *testing.T, content string) ([]Mount, []Problem) {
	t.Helper()
	return ParseMounts([]byte(content), testHome, testProject, nil)
}

// M1, M2: a line is <host-src>:<container-dst>[:ro|rw], `#` comments and blank lines are
// skipped, `~/` and `$HOME/` expand to the home directory, a trailing slash is dropped, rw is
// the default.
func TestParseMountsReadsTheLines(t *testing.T) {
	mounts, problems := parse(t, "# a comment\n\n~/Drive:/mnt/drive:ro\n  $HOME/data/:/data # trailing\n/srv/x:/x:rw\n")
	if len(problems) != 0 {
		t.Fatalf("problems: %v", problems)
	}
	want := []Mount{
		{Line: 3, Src: "/home/u/Drive", Dst: "/mnt/drive", Opt: "ro"},
		{Line: 4, Src: "/home/u/data", Dst: "/data", Opt: "rw"},
		{Line: 5, Src: "/srv/x", Dst: "/x", Opt: "rw"},
	}
	if !slices.Equal(mounts, want) {
		t.Fatalf("mounts = %+v, want %+v", mounts, want)
	}
}

// M3: every rule a line breaks is a problem naming its line; nothing of a broken line is kept.
func TestParseMountsRefusesEachBrokenRule(t *testing.T) {
	cases := map[string]string{
		"no colon":              "/srv",
		"three colons":          "/a:/b:ro:x",
		"relative source":       "srv:/x",
		"relative target":       "/srv:x",
		"dot dot":               "/srv/../etc:/x",
		"bad option":            "/srv:/x:z",
		"root":                  "/:/x",
		"home":                  "~/:/x",
		"ssh":                   "~/.ssh:/x:ro",
		"under ssh":             "~/.ssh/id:/x:ro",
		"an ancestor of gh":     "~/.config:/x:ro",
		"an ancestor of keys":   "~/.local:/x:ro",
		"host bin":              "~/.local/bin:/x",
		"launcher state":        "~/.local/state/ccy:/x",
		"systemd user units":    "~/.config/systemd/user:/x",
		"shell rc":              "~/.bashrc:/x",
		"podman config":         "~/.config/containers:/x",
		"the project":           "/home/u/Projects/app:/x",
		"above the project":     "/home/u/Projects:/x",
		"etc":                   "/etc/x:/x",
		"workspace target":      "/srv:/workspace",
		"under usr target":      "/srv:/usr/local",
		"a comma":               "/srv/a,b:/x",
		"twice the same target": "/srv/a:/x\n/srv/b:/x",
	}
	for name, content := range cases {
		mounts, problems := parse(t, content)
		if len(problems) == 0 {
			t.Errorf("%s: %q accepted as %+v", name, content, mounts)
			continue
		}
		if problems[0].Line < 1 || problems[0].Why == "" {
			t.Errorf("%s: problem %+v names no line or reason", name, problems[0])
		}
	}
}

// M4: a directory under the workspace that is not its state, and a path beside a denied one, are
// fine: the deny list is exact on whole path elements.
func TestParseMountsAllowsNeighbours(t *testing.T) {
	_, problems := parse(t, "~/.sshkeys-public:/keys:ro\n/srv:/workspace/data\n~/.config/rclone:/rc:ro\n")
	if len(problems) != 0 {
		t.Fatalf("problems: %v", problems)
	}
}

// M5: extra denied paths (the launcher's own state when relocated) are refused, with their
// ancestors.
func TestParseMountsRefusesExtraDenied(t *testing.T) {
	_, problems := ParseMounts([]byte("/var/state:/x\n"), testHome, testProject, []string{"/var/state/ccy"})
	if len(problems) != 1 || !strings.Contains(problems[0].Why, "/var/state/ccy") {
		t.Fatalf("problems = %v", problems)
	}
}
