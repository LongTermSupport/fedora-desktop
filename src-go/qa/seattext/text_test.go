package seattext

import (
	"slices"
	"testing"
)

// S3: the docs but the changelog, the embedded help, and the launchers' files.
func TestScopeIsTheDocsTheHelpAndTheLaunchers(t *testing.T) {
	for p, want := range map[string]Kind{
		"docs/ccy.md":                               KindText,
		"docs/sub/agent-bus.md":                     KindText,
		"docs/ccy-changelog.md":                     KindNone,
		"docs/image.png":                            KindNone,
		"src-go/internal/teams/usage.txt":           KindText,
		"src-go/internal/ccylaunch/help.txt":        KindText,
		"src-go/internal/teams/other.txt":           KindNone,
		"files/var/local/claude-yolo/tool":          KindLauncher,
		"files/var/local/claude-yolo/lib/x.inc":     KindLauncher,
		"files/var/local/claude-code/cc-teams.bash": KindLauncher,
		"files/var/local/other/x.bash":              KindNone,
		"README.md":                                 KindNone,
	} {
		if got := InScope(p); got != want {
			t.Errorf("InScope(%q) = %v, want %v", p, got, want)
		}
	}
}

func findings(kind Kind, text string) []string {
	var got []string
	for _, l := range Lines(kind, text) {
		if f := Finding(l.Template, l.Operands, false); f != "" {
			got = append(got, f)
		}
	}
	return got
}

// S4: a doc line is judged whole; a handle's format and a path's directory are not a finding.
func TestADocLineIsJudgedWholeButAHandleOrPathIsNot(t *testing.T) {
	text := "run `ccy --teams <seat>@<team>`\n" +
		"the handle is `<repo>.<seat>+<host>.lxc`\n" +
		"in `.claude/ccy/pingbus/seats/<team>/<seat>/`\n" +
		"launch with `ccy --teams <team>`\n"
	if got := findings(KindText, text); !slices.Equal(got, []string{FindingLaunch}) {
		t.Fatalf("got %q", got)
	}
}

// S5: a launcher line is judged only as a message; comments and code are not; a variable is a
// filled-in value spelt as its name.
func TestALauncherIsJudgedOnItsMessagesOnly(t *testing.T) {
	text := "#!/bin/bash\n" +
		"# --teams <seat>@<team> in a comment\n" +
		"args+=(--teams \"$n@$team\")\n" +
		"    print_error \"run --teams <seat>@<team>\"\n" +
		"echo \"ccy --teams ${canonical}\" >&2\n" +
		"printf '%s\\n' \"ccy --teams $team\"\n" +
		"cat <<EOF\n" +
		"  --teams [TEAM | SEAT@TEAM]\n" +
		"EOF\n" +
		"run --teams 1@x\n"
	got := findings(KindLauncher, text)
	want := []string{FindingLaunch, FindingList, FindingPlaceholder}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
	lines := Lines(KindLauncher, text)
	if len(lines) != 4 || lines[0].No != 4 || lines[3].No != 8 {
		t.Fatalf("lines %+v", lines)
	}
}

// S5: an indented here-document (<<-) ends at its tabbed delimiter.
func TestAnIndentedHereDocumentEnds(t *testing.T) {
	text := "\tcat <<-'END'\n\t--teams SEAT@TEAM\n\tEND\nrun --teams 1@x\n"
	if got := findings(KindLauncher, text); len(got) != 1 {
		t.Fatalf("got %q", got)
	}
}
