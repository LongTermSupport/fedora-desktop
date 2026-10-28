package vmtest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	passedRun = "20261010T080000Z-server-fast-provision"
	failedRun = "20261012T080000Z-server-full-provision"
	liveRun   = "20261015T110000Z-server-fast-provision"
	bareRun   = "20261009T080000Z-server-fast-provision"
)

// pruneKit is a lab with four runs: one passed, one failed and young, one whose guest is up,
// and one that kept no overlay; a base; and a prune plan answering for each.
func pruneKit(t *testing.T) *kit {
	t.Helper()
	k := newKit(t)
	k.withLibvirt()
	k.builtBase("server-fast-44")
	for _, id := range []string{passedRun, failedRun, liveRun} {
		k.write("runs/"+id+"/overlay.qcow2", strings.Repeat("o", 4096))
		k.write("runs/"+id+"/transcript.log", "log\n")
	}
	k.write("runs/"+bareRun+"/transcript.log", "log\n")
	k.lv.domains = []string{"vmtest-run-" + liveRun, "vmtest-member-m0"}
	k.response(passedRun, "pass")
	k.response(failedRun, "fail")
	k.response(liveRun, "fail")
	k.response(bareRun, "pass")
	return k
}

// response is the judged response of a finished run.
func (k *kit) response(id, verdict string) {
	k.write("runs/"+id+"/response.json", `{"state": "finished", "verdict": "`+verdict+`"}`)
}

// E61: the dry run lists what it would remove and why, removes nothing, and names the bases.
func TestE61PruneRunsDryRun(t *testing.T) {
	k := pruneKit(t)
	k.wantCode(k.run("prune-runs"), term.ExitOK)
	out := k.out.String()
	for _, want := range []string{
		"VMTEST-PRUNE-RUN " + passedRun + " would-remove overlay.qcow2 " + overlaySize(t, k, passedRun) + ": passed\n",
		"VMTEST-PRUNE-RUN " + failedRun + " keep overlay.qcow2 " + overlaySize(t, k, failedRun) + ": did not pass; kept for diagnosis until",
		"VMTEST-PRUNE-RUN " + liveRun + " live overlay.qcow2 " + overlaySize(t, k, liveRun) + ": libvirt has the domain vmtest-run-" + liveRun + "\n",
		"VMTEST-PRUNE-BASE server-fast-44 ",
		": bases are never touched\n",
		"VMTEST-PRUNE-DONE would free " + overlaySize(t, k, passedRun) + " from 1 run; dry run, vmtest prune-runs --yes removes it\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, bareRun) {
		t.Fatal("a run with no overlay was listed")
	}
	for _, id := range []string{passedRun, failedRun, liveRun} {
		if !k.exists("runs/" + id + "/overlay.qcow2") {
			t.Fatalf("the dry run removed %s's overlay", id)
		}
	}
	if k.exists(retentionLog) {
		t.Fatal("the dry run wrote to the retention log")
	}
}

// E62: --yes removes only the prunable overlays, keeps every other file, and logs each removal.
func TestE62PruneRunsRemoves(t *testing.T) {
	k := pruneKit(t)
	k.write("runs/"+passedRun+"/disk2.qcow2", "x")
	k.wantCode(k.run("prune-runs", "--yes", "--older-than", "4"), term.ExitOK)
	if k.exists("runs/"+passedRun+"/overlay.qcow2") || k.exists("runs/"+passedRun+"/disk2.qcow2") {
		t.Fatal("the passed run's overlays are still there")
	}
	if !k.exists("runs/"+passedRun+"/transcript.log") || !k.exists("runs/"+failedRun+"/overlay.qcow2") || !k.exists("runs/"+liveRun+"/overlay.qcow2") {
		t.Fatal("prune-runs removed more than the passed run's overlays")
	}
	if !strings.Contains(k.out.String(), "VMTEST-PRUNE-RUN "+passedRun+" removed disk2.qcow2,overlay.qcow2 ") ||
		!strings.Contains(k.out.String(), "VMTEST-PRUNE-DONE freed ") {
		t.Fatalf("stdout %s", k.out.String())
	}
	log := k.read(retentionLog)
	if !strings.Contains(log, "1792065600 pruned overlay "+passedRun+" passed\n") {
		t.Fatalf("log %q", log)
	}

	k = pruneKit(t)
	k.wantCode(k.run("prune-runs", "--yes", "--older-than", "3"), term.ExitOK)
	if k.exists("runs/"+failedRun+"/overlay.qcow2") || !strings.Contains(k.read(retentionLog), " pruned overlay "+failedRun+" did not pass, and is older than 3 days\n") {
		t.Fatal("--older-than 3 kept a failed run three days and four hours old")
	}
}

// E63: a run holding its lock is live, whatever libvirt lists; the lock is held for a whole run.
func TestE63PruneRunsSkipsALockedRun(t *testing.T) {
	k := pruneKit(t)
	release, err := holdRun(k.path("runs/" + passedRun))
	if err != nil {
		t.Fatal(err)
	}
	k.wantCode(k.run("prune-runs", "--yes"), term.ExitOK)
	if err = release(); err != nil {
		t.Fatal(err)
	}
	if !k.exists("runs/" + passedRun + "/overlay.qcow2") {
		t.Fatal("prune-runs removed the overlay of a run that holds its lock")
	}
	if !strings.Contains(k.out.String(), "VMTEST-PRUNE-RUN "+passedRun+" live overlay.qcow2 ") || !strings.Contains(k.out.String(), ": run.lock is held, the run is in progress\n") {
		t.Fatalf("stdout %s", k.out.String())
	}

	k = runKit(t, "pass")
	held := false
	k.fake.OnFunc([]string{"virt-install"}, func(c execx.Cmd) (execx.Result, error) {
		free, locked, lockErr := tryRun(k.path("runs/" + runID))
		if lockErr != nil {
			return execx.Result{}, lockErr
		}
		held = !locked
		k.lv.domains = append(k.lv.domains, argAfter(c.Args, "--name"))
		return execx.Result{}, free()
	})
	k.wantCode(k.run("run", "server-fast-provision"), term.ExitOK)
	if !held {
		t.Fatal("a booting run did not hold its run.lock")
	}
	release, locked, err := tryRun(k.path("runs/" + runID))
	if err != nil || !locked {
		t.Fatalf("a finished run still holds its lock (locked=%v, err=%v)", locked, err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
}

// E61: what stops a prune before it removes anything.
func TestE61PruneRunsRefusals(t *testing.T) {
	k := pruneKit(t)
	k.fake.OnFunc([]string{"virsh"}, func(execx.Cmd) (execx.Result, error) { return execx.Result{ExitCode: 1}, nil })
	k.wantCode(k.run("prune-runs", "--yes"), term.ExitFailure)
	k.wantErr("ERROR: virsh -c qemu:///session list failed")
	if !k.exists("runs/"+passedRun+"/overlay.qcow2") || k.out.Len() != 0 {
		t.Fatal("a failed domain listing still pruned")
	}

	k = pruneKit(t)
	if err := os.Remove(k.path("runs/" + failedRun + "/response.json")); err != nil {
		t.Fatal(err)
	}
	k.write("runs/"+failedRun+"/response.json/x", "a directory where the response goes")
	k.wantCode(k.run("prune-runs", "--yes"), term.ExitFailure)
	k.wantErr("ERROR: cannot read " + k.path("runs/"+failedRun+"/response.json"))
	if !k.exists("runs/"+passedRun+"/overlay.qcow2") || k.out.Len() != 0 {
		t.Fatal("a run whose response cannot be read still pruned")
	}

	for _, args := range [][]string{{"--older-than"}, {"--older-than", "x"}, {"--older-than", "-1"}, {"--older-than", "36501"}, {"--older-than", "999999"}, {"--now"}, {"--yes", "--yes"}} {
		k = pruneKit(t)
		k.wantCode(k.run(append([]string{"prune-runs"}, args...)...), term.ExitUsage)
		if len(k.fake.Calls()) != 0 {
			t.Fatalf("%v reached the plan", args)
		}
	}
}

// E64: a run and a build are refused before anything is done when the disk cannot carry them,
// naming both figures and what prune-runs would free.
func TestE64DiskGuard(t *testing.T) {
	k := runKit(t, "pass")
	k.withLibvirt()
	k.write("runs/"+passedRun+"/overlay.qcow2", strings.Repeat("o", 4096))
	k.response(passedRun, "pass")
	k.free = runDiskNeed - 1
	k.wantCode(k.run("run", "server-fast-provision"), term.ExitFailure)
	k.wantErr("ERROR: only 21.9 GiB free under " + k.home + "; a run needs 22.0 GiB (a guest disk of up to 20 GiB and a 2 GiB margin). " +
		"vmtest prune-runs would free " + overlaySize(t, k, passedRun) + " of run overlays; vmtest prune-runs lists them and vmtest prune-runs --yes removes them")
	for _, never := range []string{"validate_manifest", "probe_upstream", "git ls-remote", "qemu-img", "virt-install"} {
		if k.count(never) != 0 {
			t.Fatalf("a refused run ran %s", never)
		}
	}
	k.free = runDiskNeed
	k.wantCode(k.run("run", "server-fast-provision"), term.ExitOK)

	k = buildKit(t)
	k.free = buildDiskNeed - 1
	k.wantCode(k.run("build-base", "server-fast"), term.ExitFailure)
	k.wantErr("a base build needs 26.0 GiB (a guest disk of up to 20 GiB, a 2 GiB margin and 4 GiB of install media)")
	k.wantErr("vmtest prune-runs would free 0 B of run overlays")
	if k.count("validate_manifest") != 0 || k.count("probe_upstream") != 0 {
		t.Fatal("a refused build ran its steps")
	}

	k = buildKit(t)
	k.free = 0
	k.fake.OnFunc([]string{"virsh"}, func(execx.Cmd) (execx.Result, error) { return execx.Result{ExitCode: 1}, nil })
	k.wantCode(k.run("build-base", "server-fast"), term.ExitFailure)
	k.wantErr("vmtest prune-runs lists the run overlays that can go, but working out what it would free failed: virsh -c qemu:///session list failed")

	k = buildKit(t)
	k.freeErr = os.ErrPermission
	k.wantCode(k.run("build-base", "server-fast"), term.ExitFailure)
	k.wantErr("ERROR: cannot read the free space under " + k.home + ": permission denied")
}

// E64: member-up boots an overlay too, so it is refused the same way, after its name is checked.
func TestE64DiskGuardMemberUp(t *testing.T) {
	k := memberKit(t)
	k.free = runDiskNeed - 1
	k.wantCode(k.run("member-up", "m1", "server-fast"), term.ExitFailure)
	k.wantErr("ERROR: only 21.9 GiB free under " + k.home + "; a member needs 22.0 GiB (a guest disk of up to 20 GiB and a 2 GiB margin). vmtest prune-runs would free 0 B of run overlays")
	for _, never := range []string{"validate_manifest", "probe_upstream", "qemu-img", "virt-install"} {
		if k.count(never) != 0 {
			t.Fatalf("a refused member-up ran %s", never)
		}
	}
	if k.exists("members/m1") {
		t.Fatal("a refused member-up made its directory")
	}

	k = memberKit(t)
	k.free = 0
	k.wantCode(k.run("member-up", "Bad", "server-fast"), term.ExitFailure)
	k.wantErr("a member name is 1 to 40")
	if len(k.fake.Calls()) != 0 {
		t.Fatal("the guard ran before the name was checked")
	}

	k = memberKit(t)
	k.free = runDiskNeed
	k.wantCode(k.run("member-up", "m1", "server-fast"), term.ExitOK)
}

// E65: a failed run that keeps its overlay says how much it kept and how it goes.
func TestE65FailedRunSaysWhatItKept(t *testing.T) {
	k := runKit(t, "fail")
	k.fake.OnFunc([]string{"qemu-img"}, func(c execx.Cmd) (execx.Result, error) {
		k.writeAbs(createdImage(c.Args), strings.Repeat("o", 3*1024*1024))
		return execx.Result{}, nil
	})
	k.wantCode(k.run("run", "server-fast-provision"), term.ExitFailure)
	k.wantErr("==> overlay (" + overlaySize(t, k, runID) + ") and console kept for diagnosis: " + k.path("runs/"+runID) +
		"; vmtest prune-runs --yes removes the overlay once a later run of server-fast-provision passes or it is older than 7 days")
}

// overlaySize is what prune-runs reports for a run's overlay.
func overlaySize(t *testing.T, k *kit, id string) string {
	t.Helper()
	n, err := allocated(filepath.Join(k.path("runs/"+id), "overlay.qcow2"))
	if err != nil {
		t.Fatal(err)
	}
	return humanBytes(n)
}

func TestHumanBytes(t *testing.T) {
	for n, want := range map[int64]string{0: "0 B", 512: "512 B", 4096: "4.0 KiB", 3 << 20: "3.0 MiB", 22 << 30: "22.0 GiB", 1536 << 30: "1.5 TiB"} {
		if got := humanBytes(n); got != want {
			t.Fatalf("humanBytes(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestFreeBytesReadsTheFilesystem(t *testing.T) {
	n, err := freeBytes(t.TempDir())
	if err != nil || n <= 0 {
		t.Fatalf("freeBytes = %d, %v", n, err)
	}
	if _, err := freeBytes(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("an absent path had free space")
	}
}
