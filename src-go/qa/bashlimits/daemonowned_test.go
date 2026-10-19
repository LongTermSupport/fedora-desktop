package bashlimits

import (
	"maps"
	"slices"
	"strings"
	"testing"
)

const mkplan = "CLAUDE/Plan/mkplan.bash"

// upgradeMessage is a hooks-daemon upgrade commit message whose metadata names files.
func upgradeMessage(files ...string) string {
	return "hooks daemon upgrade: v1 -> v2\n\n<<<UPGRADE_METADATA\nfrom_version=v1\nto_version=v2\nmodified_files=" +
		strings.Join(files, ",") + "\nhandler_changes=\nUPGRADE_METADATA>>>\n"
}

// daemonRepo is repo with an over-limit file at p on the allowlist, committed with message;
// the allowlist in the work tree then drops p's row, as the fix for a daemon-owned file does.
func daemonRepo(t *testing.T, p, message string) string {
	t.Helper()
	root := repo(t)
	put(t, root, p, script(500))
	keep := Allowlist{"files/bin/long": {Code: 80, Stmts: 80}, "run.bash": {Code: 500, Stmts: 500}}
	withP := Allowlist{p: {Code: 500, Stmts: 500}}
	maps.Copy(withP, keep)
	put(t, root, allowlistRel, withP.Format())
	gitIn(t, root, "add", "-A")
	gitIn(t, root, "commit", "-q", "-m", message)
	put(t, root, allowlistRel, keep.Format())
	return root
}

func TestMkplanIsExemptAsDaemonOwned(t *testing.T) {
	reason, ok, err := ExemptionFor(mkplan)
	if err != nil || !ok || !strings.Contains(reason, "daemon") {
		t.Fatalf("got %q, %v, %v; want exempt as daemon-owned", reason, ok, err)
	}
	if !IsDaemonOwnedFile(mkplan) {
		t.Fatal("mkplan.bash is not on the daemon-owned list")
	}
}

func TestDaemonOwnedFilesAreExactPathsWithReasons(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range DaemonOwnedFiles {
		if strings.ContainsAny(e.Pattern, "*?[\\") || strings.HasSuffix(e.Pattern, "/") || e.Pattern == "" {
			t.Errorf("%q is not one exact file path: a daemon-owned entry names a file, never a pattern", e.Pattern)
		}
		if len(e.Reason) < 20 {
			t.Errorf("%q has no real reason", e.Pattern)
		}
		if seen[e.Pattern] {
			t.Errorf("%q is listed twice", e.Pattern)
		}
		seen[e.Pattern] = true
	}
}

func TestPathsThatOnlyLookDaemonOwnedAreNotExempt(t *testing.T) {
	for _, p := range []string{
		"CLAUDE/Plan/mkplan2.bash",
		"CLAUDE/Plan/sub/mkplan.bash",
		"x/CLAUDE/Plan/mkplan.bash",
		"CLAUDE/Plan/mkplan.bash.j2",
		"CLAUDE/Plan/_planlib.inc.bash",
		"CLAUDE/Plan/meta-deploy.bash",
		"scripts/hooks-daemon-mkplan.bash",
	} {
		if IsDaemonOwnedFile(p) {
			t.Errorf("%s: claims daemon ownership by its name alone", p)
		}
		if _, ok, err := ExemptionFor(p); err != nil || ok {
			t.Errorf("%s: want checked, got exempt=%v, %v", p, ok, err)
		}
	}
}

func TestParseUpgradeMetadata(t *testing.T) {
	log := "unrelated\nmodified_files=not/in/a/block.bash\n\x00" +
		upgradeMessage("a.bash", mkplan) + "\x00" +
		upgradeMessage() + "\x00" +
		upgradeMessage(".claude/init.sh") + "\x00"
	got := slices.Sorted(maps.Keys(ParseUpgradeMetadata(log)))
	want := []string{".claude/init.sh", mkplan, "a.bash"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestDaemonOwnedFileShippedByAnUpgradeLeavesTheRatchet(t *testing.T) {
	root := daemonRepo(t, mkplan, upgradeMessage(".claude/init.sh", mkplan))
	r := run(t, root, "--base", "HEAD")
	if r.code != ExitOK {
		t.Fatalf("exit %d: %s", r.code, r.err)
	}
	if !strings.Contains(r.out, "2 exempt") {
		t.Errorf("mkplan.bash was not counted exempt: %s", r.out)
	}
}

func TestDaemonOwnedFileStillListedMustBeRemoved(t *testing.T) {
	root := daemonRepo(t, mkplan, upgradeMessage(mkplan))
	gitIn(t, root, "checkout", "--", allowlistRel)
	wantFail(t, "listed", run(t, root, "--base", "HEAD"), mkplan, "daemon-owned")
	if r := run(t, root, "--base", "HEAD", "--update"); r.code != ExitOK || strings.Contains(readAllowlist(t, root), mkplan) {
		t.Fatalf("--update kept the daemon-owned entry: exit %d: %s", r.code, r.err)
	}
}

func TestDaemonOwnedFileNoUpgradeShipsIsRefused(t *testing.T) {
	root := daemonRepo(t, mkplan, "an ordinary commit\n\nmodified_files="+mkplan+"\n")
	wantFail(t, "unshipped", run(t, root, "--base", "HEAD"), mkplan, "upgrade commit", "allowlist entry removed")
}

func TestUpgradeMetadataAloneDoesNotExemptARatchetedFile(t *testing.T) {
	// The guard holds: a ratcheted file the daemon names but that is not on the list.
	for _, p := range []string{"CLAUDE/Plan/sub/mkplan.bash", "CLAUDE/Plan/_planlib.inc.bash"} {
		root := daemonRepo(t, p, upgradeMessage(p))
		wantFail(t, p, run(t, root, "--base", "HEAD"), p, "code lines")
	}
}

func TestExemptRatchetedFileThatIsNotDaemonOwnedCannotLeave(t *testing.T) {
	rec := Measurement{Code: 100}
	scan := scanOf(map[string]Measurement{})
	const planScript = "CLAUDE/Plan/001-x/a.bash"
	for _, p := range []string{planScript, mkplan} {
		scan.Exempt[p] = true
		scan.Present[p] = true
	}
	scan.DaemonOwned = map[string]bool{mkplan: true}
	base := fileOf(MeasurerVersion, Allowlist{planScript: rec, mkplan: rec})
	wantKinds(t, "drop", CompareBase(base, fileOf(MeasurerVersion, Allowlist{}), scan), planScript+":"+KindDropped)
	_, refused := Tighten(base.Records, scan)
	wantKinds(t, "tighten", refused, planScript+":"+KindOrphan)
}
