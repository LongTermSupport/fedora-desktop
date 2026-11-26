package privatenames

import (
	"maps"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// Every name below is invented: this file is committed to a public repository.

// tcStatus is `teamchat status` (the v2 bus) for a session on one team: JSON Lines records, the
// team record naming the team and this session's handle, the member records each member's handle.
const tcStatus = `{"kind":"team","v":1,"team":"quillworks","name":"quillworks","handle":"inkwell-press.1+local.podman","role":"worker","about":"","trust":"trusted","delivery":"push","pending":0,"down":false,"counts":{}}
{"kind":"member","v":1,"team":"quillworks","handle":"inkwell-press.1+local.podman","role":"worker","present":true,"presence":"online"}
{"kind":"member","v":1,"team":"quillworks","handle":"marblehead-yard.2+local.host","role":"coordinator","present":false,"presence":"offline"}
{"kind":"room","v":1,"team":"quillworks","purpose":"main","state":"joined","check":null,"uncounted":null}
{"kind":"runtime","v":1,"keeper":{},"link":{},"unusable":{}}
`

// tcNoTeam is a teamchat session that reports no team record at all.
const tcNoTeam = `{"kind":"runtime","v":1,"keeper":{},"link":{},"unusable":{}}
`

const tcKey = "teamchat status"

const pinnedFile = "/repo/.git/" + PinnedName

func addedLine(line string) string {
	return "diff --git a/docs/a.md b/docs/a.md\n--- a/docs/a.md\n+++ b/docs/a.md\n@@ -1,0 +2 @@\n+" + line + "\n"
}

// R79: a session on teamchat, with no pingbus (it cannot start) or pingbus answering "no team",
// learned nothing from the bus, so its own team's name passed every gate. The team and its
// members' names, as teamchat reports them, must block a commit, and never be printed.
func TestATeamchatSessionsTeamAndMembersBlockACommit(t *testing.T) {
	pingbusAnswers := map[string]map[string]proc.Result{
		"no pingbus":      {},
		"pingbus no team": {"pingbus status": {Code: noTeam}},
	}
	for name, pb := range pingbusAnswers {
		for _, line := range []string{"the quillworks team", "seat marblehead-yard.2 said", "see inkwell-press here"} {
			answers := map[string]proc.Result{tcKey: {Stdout: tcStatus}, stagedKey: {Stdout: addedLine(line)}}
			maps.Copy(answers, pb)
			r := newRig(true, answers, files{}, "")
			if code := r.run("staged"); code != ExitFindings {
				t.Errorf("%s: %q passed: exit %d: %s", name, line, code, r.text())
				continue
			}
			lower := strings.ToLower(r.text())
			for _, word := range []string{"quillworks", "marblehead", "inkwell"} {
				if strings.Contains(lower, word) {
					t.Errorf("%s: a term was printed: %s", name, r.text())
				}
			}
		}
	}
}

// teamchat's own "no runtime" answer (exit 69: not a member session) is an empty answer, as
// pingbus's "no team" is; any other failure is a bus that failed, and a half-known list is no list.
func TestTeamchatOutsideASessionIsEmptyAndAnyOtherFailureIsFatal(t *testing.T) {
	clean := newRig(true, map[string]proc.Result{tcKey: {Code: teamchatNoRuntimeExit}, stagedKey: {Stdout: "+++ b/x\n"}}, files{}, "")
	if code := clean.run("staged"); code != ExitOK {
		t.Fatalf("no runtime: exit %d: %s", code, clean.text())
	}
	for _, c := range []int{1, 2, 10, 78} {
		r := newRig(true, map[string]proc.Result{tcKey: {Code: c}}, files{}, "")
		if code := r.run("staged"); code != ExitCannotRun || !strings.Contains(r.text(), "teamchat status exited") {
			t.Errorf("status %d: exit %d: %s", c, code, r.text())
		}
	}
	bad := newRig(true, map[string]proc.Result{tcKey: {Stdout: "not json\n"}}, files{}, "")
	if code := bad.run("staged"); code != ExitCannotRun || !strings.Contains(bad.text(), "teamchat status") {
		t.Fatalf("unreadable output: exit %d: %s", code, bad.text())
	}
}

// A bus that answered for a session yet taught no team or member name is refused in a container:
// the session is on a team whose name the gate does not know, so it has not judged. A name the
// owner pins fills the gap.
func TestAContainerSessionThatTeachesNoBusNameIsRefusedUnlessPinned(t *testing.T) {
	r := newRig(true, map[string]proc.Result{tcKey: {Stdout: tcNoTeam}, stagedKey: {Stdout: "+++ b/x\n"}}, files{}, "")
	if code := r.run("staged"); code != ExitCannotRun || !strings.Contains(r.text(), PinnedName) {
		t.Fatalf("exit %d: %s", code, r.text())
	}
	filled := newRig(true, map[string]proc.Result{tcKey: {Stdout: tcNoTeam}, stagedKey: {Stdout: "+++ b/x\n"}}, files{pinnedFile: "# the owner's names\nquillworks\n"}, "")
	if code := filled.run("staged"); code != ExitOK {
		t.Fatalf("a pinned name did not fill the list: exit %d: %s", code, filled.text())
	}
}

// A neighbouring team on the same bus host cannot be learned from inside a container (teamchat
// reports this session's teams only), so the owner pins its name. A pinned name always blocks,
// even one the specific-term rule would drop, is never printed, and is never copied into the
// learned list, so removing the pin removes the block.
func TestAPinnedNameAlwaysBlocksAndIsNeverPrintedOrCopied(t *testing.T) {
	pins := "\n# neighbours\n  otterbank  \nkiwi\n"
	for _, line := range []string{"the otterbank client", "a kiwi note"} {
		r := newRig(true, map[string]proc.Result{tcKey: {Stdout: tcStatus}, stagedKey: {Stdout: addedLine(line)}}, files{pinnedFile: pins}, "")
		if code := r.run("staged"); code != ExitFindings {
			t.Fatalf("%q passed: exit %d: %s", line, code, r.text())
		}
		lower := strings.ToLower(r.text())
		if strings.Contains(lower, "otterbank") || strings.Contains(lower, "kiwi") {
			t.Fatalf("a pinned term was printed: %s", r.text())
		}
		if !strings.Contains(r.text(), "("+SourcePinned+" #") {
			t.Fatalf("the finding does not name its source: %s", r.text())
		}
		if strings.Contains(r.files[listFile], "otterbank") || strings.Contains(r.files[listFile], "kiwi") {
			t.Fatal("a pinned term was copied into the learned list")
		}
	}
}

func TestAnUnreadablePinnedListIsFatal(t *testing.T) {
	r := newRig(true, map[string]proc.Result{tcKey: {Stdout: tcStatus}}, files{pinnedFile: "UNREADABLE"}, "")
	if code := r.run("staged"); code != ExitCannotRun || !strings.Contains(r.text(), PinnedName) {
		t.Fatalf("exit %d: %s", code, r.text())
	}
}

// A pinned name is a blocking term, so the published denylist carries it too.
func TestThePublishedDenylistCarriesAPinnedName(t *testing.T) {
	fl := files{pinnedFile: "otterbank\n"}
	r := denyRig(fl)
	if code := r.run("denylist", denyOut); code != ExitOK {
		t.Fatalf("exit %d: %s", code, r.text())
	}
	found := false
	for _, p := range patterns(t, fl[denyOut]) {
		found = found || p == DenylistPattern("otterbank")
	}
	if !found {
		t.Fatal("the pinned name is missing from the denylist")
	}
}

// Outside a container a pinned list alone is a list: the scan runs against it.
func TestAPinnedListAloneIsJudgedAgainstOutsideAContainer(t *testing.T) {
	r := newRig(false, map[string]proc.Result{stagedKey: {Stdout: addedLine("otterbank")}}, files{pinnedFile: "otterbank\n"}, "")
	r.env.Hostnames = nil
	if code := r.run("staged"); code != ExitFindings {
		t.Fatalf("exit %d: %s", code, r.text())
	}
}
