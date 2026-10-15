package vmtest

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func sum(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "")))
	return hex.EncodeToString(h[:])
}

// E49: the recipe digest: version, cleanup; a full base its kickstart; a desktop the runner.
func TestE49RecipeDigest(t *testing.T) {
	k := newKit(t)
	l := &lab{paths: newPaths(k.environment())}
	for _, tc := range []struct{ kind, profile, want string }{
		{"fast", "server", sum("build-recipe-version=1\n", "cleanup\n")},
		{"full", "server", sum("build-recipe-version=1\n", "cleanup\n", "ks server\n")},
		{"full", "desktop", sum("build-recipe-version=1\n", "cleanup\n", "ks desktop\n", "runner\n")},
	} {
		got, err := recipeDigest(l, tc.kind, tc.profile)
		if err != nil || got != tc.want {
			t.Fatalf("%s %s: got %s %v", tc.kind, tc.profile, got, err)
		}
	}
	if err := os.Remove(k.path(sessionRunner)); err != nil {
		t.Fatal(err)
	}
	if _, err := recipeDigest(l, "full", "desktop"); err == nil || !strings.Contains(err.Error(), sessionRunner) {
		t.Fatalf("got %v", err)
	}
	if _, err := recipeDigest(l, "full", "appliance"); err == nil || !strings.Contains(err.Error(), "no VM kickstart for profile appliance") {
		t.Fatalf("got %v", err)
	}
}

// E48: the record must match the disk; a quick pass hands the record's hash, a deep one hashes.
func TestE48FreshnessVerdict(t *testing.T) {
	k := newKit(t)
	k.builtBase("server-fast-44")
	k.gate(0, "current")
	k.wantCode(k.run("ensure-base", "server-fast"), term.ExitOK)
	verdict := k.lines("freshness_gate")
	if len(verdict) != 2 || !strings.Contains(verdict[0], "--record-only") ||
		!strings.Contains(verdict[1], "--actual-base-sha256 "+strings.Repeat("b", 64)) ||
		!strings.Contains(verdict[1], "--recipe-digest "+sum("build-recipe-version=1\n", "cleanup\n")) {
		t.Fatalf("got %v", verdict)
	}
	if !strings.Contains(verdict[0], "--actual-base-sha256 "+strings.Repeat("0", 64)) {
		t.Fatalf("the record-only pass got %s", verdict[0])
	}

	k = newKit(t)
	k.builtBase("server-fast-44")
	k.gate(0, "current")
	k.wantCode(k.run("freshness-status", "--deep"), term.ExitOK)
	k.wantErr("==> hashing " + k.path("bases/server-fast-44/base.qcow2") + " (deep check; this reads the whole base)")
	if !strings.Contains(k.lines("freshness_gate")[1], "--actual-base-sha256 "+sum("disk")) {
		t.Fatalf("got %v", k.lines("freshness_gate"))
	}

	k = newKit(t)
	k.builtBase("server-fast-44")
	k.gate(0, "current")
	k.write("bases/server-fast-44/base.qcow2", "a bigger disk")
	k.fake.OnFunc(helperArgv("freshness_gate"), func(execx.Cmd) (execx.Result, error) {
		return execx.Result{Stdout: []byte("VMTEST-BASE-RECORD name=server-fast-44 kind=fast profile=server base_sha256=x base_size=4 base_mtime=1\n")}, nil
	})
	k.wantCode(k.run("ensure-base", "server-fast"), term.ExitFailure)
	k.wantErr("ERROR: base.qcow2 for server-fast-44 differs from its record (size/mtime); refusing a base of unknown provenance")

	for name, reply := range map[string]execx.Result{
		"an error exit":         {Stdout: []byte("VMTEST-FRESHNESS-VERDICT decision=current\tdegraded=false\tdivergences=\n"), ExitCode: 2},
		"no verdict":            {ExitCode: 0},
		"no decision":           {Stdout: []byte("VMTEST-FRESHNESS-VERDICT degraded=false\tdivergences=\n")},
		"two verdicts":          {Stdout: []byte("VMTEST-FRESHNESS-VERDICT decision=current\tdegraded=false\tdivergences=\nVMTEST-FRESHNESS-VERDICT decision=current\tdegraded=false\tdivergences=\n")},
		"no record":             {Stdout: []byte("nothing\n")},
		"a record without size": {Stdout: []byte("VMTEST-BASE-RECORD name=n kind=fast profile=server base_sha256=x base_mtime=1\n")},
	} {
		k = newKit(t)
		k.builtBase("server-fast-44")
		k.gate(0, "current")
		if name == "no record" || name == "a record without size" {
			k.fake.On(helperArgv("freshness_gate"), reply)
		} else {
			k.fake.OnFunc(append(helperArgv("freshness_gate"), "--fedora-version", testFedora), verdictReply(k, reply))
		}
		k.stubBuild()
		k.wantCode(k.run("ensure-base", "server-fast"), term.ExitFailure)
		if len(k.built) != 0 {
			t.Fatalf("%s: a broken gate led to a rebuild", name)
		}
	}
}

// E50: one probe, a line per base in name order, the file renamed in when done, the worst exit.
func TestE50FreshnessStatus(t *testing.T) {
	k := newKit(t)
	k.builtBase("server-fast-44")
	k.builtBase("desktop-44")
	k.write("bases/notes.txt", "not a base")
	k.fake.OnFunc(append(helperArgv("freshness_gate"), "--fedora-version"), func(c execx.Cmd) (execx.Result, error) {
		base := argAfter(c.Args, "--base-json")
		info, err := os.Stat(strings.TrimSuffix(base, "base.json") + "base.qcow2")
		if err != nil {
			return execx.Result{}, err
		}
		name := "server-fast-44"
		decision, rc := "current", 0
		if strings.Contains(base, "desktop") {
			name, decision, rc = "desktop-44", "reinstall", 1
		}
		out := "VMTEST-BASE-RECORD name=" + name + " kind=fast profile=server base_sha256=s base_size=" + itoa(info.Size()) + " base_mtime=" + itoa(info.ModTime().Unix()) + "\n"
		if slices.Contains(c.Args, "--record-only") {
			return execx.Result{Stdout: []byte(out)}, nil
		}
		return execx.Result{Stdout: []byte(out + "VMTEST-FRESHNESS-VERDICT decision=" + decision + "\tdegraded=false\tdivergences=media\treason=r\n"), ExitCode: rc}, nil
	})
	k.wantCode(k.run("freshness-status"), 1)
	if got := k.out.String(); got != "VMTEST-FRESHNESS-STATUS desktop-44 decision=reinstall degraded=false integrity=quick\nVMTEST-FRESHNESS-STATUS server-fast-44 decision=current degraded=false integrity=quick\n" {
		t.Fatalf("stdout %q", got)
	}
	want := "written_at=2026-10-15T12:00:00Z\nintegrity=quick\nprobe_revision=" + testProbeRev + "\nprobe_compose_id=Fedora-44-20261001.0\n" +
		"base=desktop-44\tdecision=reinstall\tdegraded=false\tintegrity=quick\tdivergences=media\n" +
		"base=server-fast-44\tdecision=current\tdegraded=false\tintegrity=quick\tdivergences=media\n"
	if got := k.read(statusFile); got != want {
		t.Fatalf("status file:\n%s", got)
	}
	if k.exists(statusFile+".new") || k.count("probe_upstream") != 1 {
		t.Fatal("the status file was not renamed in, or the probe ran more than once")
	}
	k.wantErr("==> freshness status written: " + k.path(statusFile))

	k = newKit(t)
	k.builtBase("server-fast-44")
	k.write("bases/desktop-44/base.json", "{}")
	k.gate(0, "current")
	k.write(statusFile, "old\n")
	k.wantCode(k.run("freshness-status"), term.ExitFailure)
	k.wantErr("ERROR: desktop-44 has a record but no base.qcow2")
	if k.read(statusFile) != "old\n" {
		t.Fatal("a failed status replaced the last good file")
	}
}

// E51, E52: sweep's arguments; nightly always sweeps and reports the worse status (B13).
func TestE51E52SweepAndNightly(t *testing.T) {
	k := newKit(t)
	k.fake.On(helperArgv("retention"), execx.Result{})
	k.wantCode(k.run("sweep"), term.ExitOK)
	c := k.mustCall("retention sweep")
	checkout := strings.TrimSuffix(k.home, "/lab")
	if !strings.HasSuffix(c.Line(), "retention sweep --lab-dir "+k.home+" --checkout "+checkout+" --log "+k.path(retentionLog)) {
		t.Fatalf("got %s", c.Line())
	}
	k.wantErr("==> every eviction is recorded in " + k.path(retentionLog))

	k = newKit(t)
	k.write("lab.env", "VMTEST_FEDORA_VERSION=44\nVMTEST_REPO_URL=https://x\nVMTEST_REPO_BRANCH=b\nVMTEST_CHECKOUT=/nonexistent/dir\n")
	k.wantCode(k.run("sweep"), term.ExitFailure)
	k.wantErr("does not name an existing VMTEST_CHECKOUT")

	k = newKit(t)
	k.builtBase("server-fast-44")
	k.gate(1, "reinstall")
	k.fake.On(helperArgv("retention"), execx.Result{})
	k.wantCode(k.run("nightly"), 1)
	if k.count("retention sweep") != 1 || k.count("freshness_gate") != 2 {
		t.Fatalf("calls %v", k.lines(""))
	}
	if !strings.Contains(k.read(statusFile), "integrity=deep") {
		t.Fatal("nightly's status pass was not deep")
	}

	k = newKit(t)
	k.fake.On(helperArgv("probe_upstream"), execx.Result{ExitCode: 1})
	k.fake.On(helperArgv("retention"), execx.Result{})
	k.wantCode(k.run("nightly"), term.ExitFailure)
	if k.count("retention sweep") != 1 {
		t.Fatal("an unreachable upstream skipped the sweep")
	}

	k = newKit(t)
	k.fake.On(helperArgv("retention"), execx.Result{ExitCode: 1})
	k.wantCode(k.run("nightly"), term.ExitFailure)
}

// E53 (member suite, ensure-base): current untouched, refused rebuilt, absent built.
func TestE53EnsureBase(t *testing.T) {
	k := newKit(t)
	k.builtBase("server-fast-44")
	k.gate(0, "current")
	k.stubBuild()
	k.wantCode(k.run("ensure-base", "server-fast"), term.ExitOK)
	if k.out.String() != "VMTEST-BASE-CURRENT server-fast-44 decision=current\n" || len(k.built) != 0 {
		t.Fatalf("stdout %q built %v", k.out.String(), k.built)
	}
	k.wantErr("==> base server-fast-44 is usable (freshness: current)")

	k = newKit(t)
	k.builtBase("server-fast-44")
	k.gate(1, "reinstall")
	k.stubBuild()
	k.wantCode(k.run("ensure-base", "server-fast"), term.ExitOK)
	if !slices.Equal(k.built, []string{"server-fast"}) {
		t.Fatalf("built %v", k.built)
	}
	k.wantErr("==> base server-fast-44 may not be used (freshness: reinstall): rebuilding it")

	k = newKit(t)
	k.base("server-other", "server-other-44", "fast", "server", "-")
	k.stubBuild()
	k.wantCode(k.run("ensure-base", "server-other"), term.ExitOK)
	if !slices.Equal(k.built, []string{"server-other"}) || k.count("probe_upstream") != 0 {
		t.Fatalf("built %v", k.built)
	}
	k.wantErr("==> base server-other-44 is absent: building it")
	assertNoProbeFiles(t, k)
}

// E54: refresh-base by key, by profile and all; nothing matching fails.
func TestE54RefreshBase(t *testing.T) {
	setup := func() *kit {
		k := newKit(t)
		k.base("desktop", "desktop-44", "full", "desktop", "everything")
		k.base("server-full", "server-full-44", "full", "server", "everything")
		for _, n := range []string{"desktop-44", "server-fast-44", "server-full-44"} {
			k.builtBase(n)
		}
		k.stubBuild()
		return k
	}
	k := setup()
	k.write("bases/stray-44/base.json", "{}") // the manifest does not know it
	k.wantCode(k.run("refresh-base", "server-fast"), term.ExitOK)
	if !slices.Equal(k.built, []string{"server-fast"}) || k.out.String() != "VMTEST-REFRESH-DONE refreshed=1\n" {
		t.Fatalf("built %v stdout %q", k.built, k.out.String())
	}
	if k.count("--base stray") != 0 {
		t.Fatal("a named refresh asked the manifest about another directory")
	}
	k.wantErr("==> refreshing server-fast-44: re-importing the published image and upgrading")

	k = setup()
	k.wantCode(k.run("refresh-base", "server"), term.ExitOK)
	if !slices.Equal(k.built, []string{"server-fast", "server-full"}) {
		t.Fatalf("built %v", k.built)
	}
	k.wantErr("==> refreshing server-full-44: reinstalling from the verified media and upgrading (a full base; this takes a while)")

	k = setup()
	k.wantCode(k.run("refresh-base", "all"), term.ExitOK)
	if !slices.Equal(k.built, []string{"desktop", "server-fast", "server-full"}) || k.out.String() != "VMTEST-REFRESH-DONE refreshed=3\n" {
		t.Fatalf("built %v", k.built)
	}

	k = setup()
	k.write("bases/stray-44/base.json", "{}")
	k.wantCode(k.run("refresh-base", "all"), term.ExitFailure)
	if len(k.built) != 3 {
		t.Fatalf("built %v before the stray", k.built)
	}

	k = setup()
	k.wantCode(k.run("refresh-base", "nothing-like-it"), term.ExitFailure)
	k.wantErr("ERROR: no built base matches nothing-like-it; build one with: vmtest build-base <base-key>")
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// verdictReply is a gate that reads the base's record truly and answers its verdict with reply.
func verdictReply(k *kit, reply execx.Result) func(execx.Cmd) (execx.Result, error) {
	return func(c execx.Cmd) (execx.Result, error) {
		if !slices.Contains(c.Args, "--record-only") {
			return reply, nil
		}
		info, err := os.Stat(k.path("bases/server-fast-44/base.qcow2"))
		if err != nil {
			return execx.Result{}, err
		}
		return execx.Result{Stdout: []byte("VMTEST-BASE-RECORD name=server-fast-44 kind=fast profile=server base_sha256=s base_size=" +
			itoa(info.Size()) + " base_mtime=" + itoa(info.ModTime().Unix()) + "\n")}, nil
	}
}
