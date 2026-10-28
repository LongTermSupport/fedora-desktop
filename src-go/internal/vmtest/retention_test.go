package vmtest

import (
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const day = 24 * time.Hour

// spoolDone is the spool helper's answer when it evicted nothing.
var spoolDone = execx.Result{Stdout: []byte("VMTEST-SPOOL-SWEEP-DONE evicted=0\n")}

// stampAgo is the run id of a run of scenario started ago before the kit's clock.
func stampAgo(k *kit, ago time.Duration, scenario string) string {
	return k.now.Add(-ago).UTC().Format(runIDLayout) + "-" + scenario
}

func planOf(runs []runFacts, now time.Time, days int) map[string]pruneDecision {
	got := map[string]pruneDecision{}
	for _, d := range planPrune(runs, now, days) {
		got[d.id] = d
	}
	return got
}

// E61: the prune rule. A run passed, a later run of its scenario passed, or it is old.
func TestE61PrunePlanRule(t *testing.T) {
	k := newKit(t)
	passed := factsOf(stampAgo(k, time.Minute, "server-fast-provision"), true)
	if got := planOf([]runFacts{passed}, k.now, 7)[passed.name]; got.action != actionPrune || got.reason != "passed" {
		t.Fatalf("a passed run: %+v", got)
	}

	failed := factsOf(stampAgo(k, 3*day, "server-fast-provision"), false)
	later := factsOf(stampAgo(k, day, "server-fast-provision"), true)
	other := factsOf(stampAgo(k, 2*day, "desktop-fresh-install"), false)
	earlier := factsOf(stampAgo(k, 4*day, "desktop-fresh-install"), true)
	got := planOf([]runFacts{failed, later, other, earlier}, k.now, 7)
	if got[failed.name].action != actionPrune || got[failed.name].reason != "a later run of server-fast-provision passed: "+later.name {
		t.Fatalf("a failure with a later pass: %+v", got[failed.name])
	}
	if got[other.name].action != actionKeep || got[other.name].reason !=
		"did not pass; kept for diagnosis until a later run of desktop-fresh-install passes or it is older than 7 days" {
		t.Fatalf("an EARLIER pass superseded a failure: %+v", got[other.name])
	}
}

// E61: a run that did not pass goes once it is older than DAYS; a name that is not a run id stays.
func TestE61PrunePlanByAge(t *testing.T) {
	k := newKit(t)
	old := factsOf(stampAgo(k, 8*day, "server-fast-provision"), false)
	young := factsOf(stampAgo(k, 6*day, "server-full-provision"), false)
	got := planOf([]runFacts{old, young}, k.now, 7)
	if got[old.name].action != actionPrune || got[old.name].reason != "did not pass, and is older than 7 days" || got[young.name].action != actionKeep {
		t.Fatalf("by age: %+v", got)
	}
	if d := planOf([]runFacts{young}, k.now, 5)[young.name]; d.action != actionPrune || d.reason != "did not pass, and is older than 5 days" {
		t.Fatalf("--older-than 5: %+v", d)
	}

	for _, name := range []string{"scratch", "20261399T000000Z-server-fast-provision"} {
		d := planOf([]runFacts{factsOf(name, true)}, k.now, 7)[name]
		if d.action != actionKeep || d.reason != "not a run directory name (<UTC stamp>-<scenario>)" {
			t.Fatalf("%s: %+v", name, d)
		}
	}
}

func TestE61PrunePlanIsInNameOrder(t *testing.T) {
	k := newKit(t)
	failed := factsOf(stampAgo(k, 3*day, "a"), false)
	later := factsOf(stampAgo(k, day, "a"), true)
	other := factsOf(stampAgo(k, 2*day, "b"), false)
	names := make([]string, 0, 3)
	for _, d := range planPrune([]runFacts{later, failed, other}, k.now, 7) {
		names = append(names, d.id)
	}
	if !slices.IsSorted(names) {
		t.Fatalf("the plan is not in name order: %v", names)
	}
}

// E61: a run passed only when its response says finished and pass; anything else is kept.
func TestE61RunPassed(t *testing.T) {
	k := newKit(t)
	for body, want := range map[string]bool{
		`{"state": "finished", "verdict": "pass"}`:  true,
		`{"state": "finished", "verdict": "fail"}`:  false,
		`{"state": "running", "verdict": "pass"}`:   false,
		`{"state": "finished", "verdict": "pass"`:   false,
		`["finished", "pass"]`:                      false,
		`{"state": "finished", "verdict": "error"}`: false,
	} {
		k.write("runs/r/response.json", body)
		got, err := runPassed(k.path("runs/r"))
		if err != nil || got != want {
			t.Fatalf("%s: %v %v, want %v", body, got, err, want)
		}
	}
	if got, err := runPassed(k.path("runs/none")); err != nil || got {
		t.Fatalf("no response: %v %v", got, err)
	}
	k.write("runs/odd/response.json/x", "a directory where the response goes")
	if _, err := runPassed(k.path("runs/odd")); err == nil {
		t.Fatal("an unreadable response was not an error")
	}
}

// E61: prune-runs reads the run directories itself; a stray file is not a run.
func TestE61PrunePlanReadsTheRuns(t *testing.T) {
	k := newKit(t)
	k.withLibvirt()
	passed := stampAgo(k, day, "server-fast-provision")
	failed := stampAgo(k, 2*day, "server-full-provision")
	for id, verdict := range map[string]string{passed: "pass", failed: "fail"} {
		k.write("runs/"+id+"/overlay.qcow2", "o")
		k.write("runs/"+id+"/response.json", `{"state": "finished", "verdict": "`+verdict+`"}`)
	}
	k.write("runs/stray-file", "x")
	k.wantCode(k.run("prune-runs"), term.ExitOK)
	out := k.out.String()
	if !strings.Contains(out, "VMTEST-PRUNE-RUN "+failed+" keep overlay.qcow2 ") || !strings.Contains(out, "VMTEST-PRUNE-RUN "+passed+" would-remove overlay.qcow2 ") {
		t.Fatalf("stdout %s", out)
	}
	if strings.Index(out, failed) > strings.Index(out, passed) || strings.Contains(out, "stray-file") {
		t.Fatalf("stdout %s", out)
	}
	if k.count("retention") != 0 {
		t.Fatal("the prune plan asked a helper")
	}

	k = newKit(t)
	k.withLibvirt()
	k.wantCode(k.run("prune-runs"), term.ExitOK)
	if !strings.Contains(k.out.String(), "VMTEST-PRUNE-DONE would free 0 B from 0 runs") {
		t.Fatalf("no runs directory: %s", k.out.String())
	}
}

// E33, E56: a run and a member are refused when the guest's RAM is over the host's ceiling,
// before anything is cloned.
func TestE33RAMCeiling(t *testing.T) {
	k := runKit(t, "pass")
	k.hostMiB = 5000
	k.wantCode(k.run("run", "server-fast-provision"), term.ExitFailure)
	k.wantErr("ERROR: the host cannot carry a run on server-fast-44 (stage: clone): guest 4096 MiB exceeds the 3750 MiB ceiling of a 5000 MiB host")
	if k.count("qemu-img") != 0 || k.exists("runs/"+runID) {
		t.Fatal("cloned past the RAM ceiling")
	}
	k.hostMiB = 5462
	k.wantCode(k.run("run", "server-fast-provision"), term.ExitOK)

	k = memberKit(t)
	k.hostMiB = 5000
	k.wantCode(k.run("member-up", "m1", "server-fast"), term.ExitFailure)
	k.wantErr("ERROR: the host cannot carry a member on server-fast-44: guest 4096 MiB exceeds the 3750 MiB ceiling of a 5000 MiB host")
	if k.exists("members/m1") {
		t.Fatal("a refused member made its directory")
	}

	k = runKit(t, "pass")
	k.hostErr = os.ErrPermission
	k.wantCode(k.run("run", "server-fast-provision"), term.ExitFailure)
	k.wantErr("ERROR: the host cannot carry a run on server-fast-44 (stage: clone): cannot read the host's memory: permission denied")
}

// E16: a rebuild needs room for two bases and the margin, and the RAM, before anything is built.
func TestE16RebuildFloor(t *testing.T) {
	k := buildKit(t)
	k.builtBase("server-fast-44")
	if err := os.Truncate(k.path("bases/server-fast-44/base.qcow2"), 13*gib); err != nil {
		t.Fatal(err)
	}
	k.free = buildDiskNeed
	k.wantCode(k.run("build-base", "server-fast"), term.ExitFailure)
	k.wantErr("ERROR: the host cannot carry a rebuild of server-fast-44 (stage: base): only 26.0 GiB free under " + k.path("bases") +
		"; a rebuild needs 28.0 GiB (the old and the new base, 13.0 GiB each, and a 2 GiB margin)")
	if k.count("virt-install") != 0 || k.count("cp --reflink") != 0 {
		t.Fatal("built past a refused floor")
	}

	k = buildKit(t)
	k.hostMiB = 4096
	k.wantCode(k.run("build-base", "server-fast"), term.ExitFailure)
	k.wantErr("ERROR: the host cannot carry a rebuild of server-fast-44 (stage: base): guest 4096 MiB exceeds the 3072 MiB ceiling of a 4096 MiB host")

	k = buildKit(t)
	k.freeErrAt = map[string]error{k.path("bases"): os.ErrPermission}
	k.wantCode(k.run("build-base", "server-fast"), term.ExitFailure)
	k.wantErr("ERROR: the host cannot carry a rebuild of server-fast-44 (stage: base): cannot read the free space under " + k.path("bases") + ": permission denied")
}

func TestHostMiB(t *testing.T) {
	if n, err := memTotalMiB("MemFree:  10 kB\nMemTotal:       16384000 kB\n"); err != nil || n != 16000 {
		t.Fatalf("got %d %v", n, err)
	}
	for _, bad := range []string{"", "MemFree: 1 kB\n", "MemTotal: lots kB\n", "MemTotal:\n"} {
		if _, err := memTotalMiB(bad); err == nil {
			t.Fatalf("%q read as a memory size", bad)
		}
	}
	if n, err := hostMiB(); err != nil || n <= 0 {
		t.Fatalf("hostMiB = %d, %v", n, err)
	}
}

// sweepKit is a lab with one run per verdict ("" is no response), oldest first, an hour apart.
func sweepKit(t *testing.T, verdicts ...string) (*kit, []string) {
	t.Helper()
	k := newKit(t)
	k.fake.On(append(helperArgv("retention"), "sweep-spool"), spoolDone)
	ids := make([]string, 0, len(verdicts))
	for i, v := range verdicts {
		ago := time.Duration(len(verdicts)-i) * time.Hour
		id := stampAgo(k, ago, "s")
		k.write("runs/"+id+"/transcript.log", "x")
		if v != "" {
			k.write("runs/"+id+"/response.json", `{"state": "finished", "verdict": "`+v+`"}`)
		}
		if err := os.Chtimes(k.path("runs/"+id), k.now.Add(-ago), k.now.Add(-ago)); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return k, ids
}

// E51: the sweep keeps the newest passing runs and every run that did not pass, drops a failed
// fast-base build directory but keeps a desktop one, logs each eviction first, then bounds the
// spool through the helper.
func TestE51Sweep(t *testing.T) {
	verdicts := make([]string, 0, 5+defaultKeepRuns)
	verdicts = append(verdicts, "pass", "fail", "", "pass", "error")
	for range defaultKeepRuns {
		verdicts = append(verdicts, "pass")
	}
	k, ids := sweepKit(t, verdicts...)
	k.write("bases/server-fast-44.build/disk.qcow2", "x")
	k.write("bases/desktop-44.build/disk.qcow2", "x")
	k.builtBase("server-fast-44")
	k.wantCode(k.run("sweep"), term.ExitOK)
	for i, id := range ids {
		evicted := i == 0 || i == 3
		if k.exists("runs/"+id) == evicted {
			t.Fatalf("run %d (%s, %q): exists=%v", i, id, verdicts[i], !evicted)
		}
	}
	if k.exists("bases/server-fast-44.build") || !k.exists("bases/desktop-44.build") || !k.exists("bases/server-fast-44/base.qcow2") {
		t.Fatal("the build directories were swept wrongly")
	}
	want := "VMTEST-EVICTED run " + ids[3] + "\nVMTEST-EVICTED run " + ids[0] + "\nVMTEST-EVICTED build server-fast-44.build\n" + string(spoolDone.Stdout)
	if k.out.String() != want {
		t.Fatalf("stdout %q, want %q", k.out.String(), want)
	}
	log := k.read(retentionLog)
	for _, line := range []string{
		"1792065600 evicted run " + ids[0] + " passing run beyond the retention cap\n",
		"1792065600 kept build desktop-44.build failed desktop build kept for diagnosis\n",
		"1792065600 evicted build server-fast-44.build failed fast-base build directory\n",
	} {
		if !strings.Contains(log, line) {
			t.Fatalf("log lacks %q:\n%s", line, log)
		}
	}
	checkout := strings.TrimSuffix(k.home, "/lab")
	if c := k.mustCall("retention sweep-spool"); !strings.HasSuffix(c.Line(), "sweep-spool --checkout "+checkout+" --log "+k.path(retentionLog)) {
		t.Fatalf("got %s", c.Line())
	}
}

// E51: a sweep under the cap, or of an empty lab, evicts nothing and still bounds the spool.
func TestE51SweepUnderTheCap(t *testing.T) {
	k, ids := sweepKit(t, "pass", "pass")
	k.wantCode(k.run("sweep"), term.ExitOK)
	if !k.exists("runs/"+ids[0]) || k.out.String() != string(spoolDone.Stdout) || k.exists(retentionLog) {
		t.Fatal("a sweep under the cap evicted")
	}

	k, ids = sweepKit(t)
	k.wantCode(k.run("sweep"), term.ExitOK)
	if len(ids) != 0 || k.count("sweep-spool") != 1 {
		t.Fatal("an empty lab was not swept")
	}
}

// E51: a sweep that cannot read a run's response, or log what it evicts, stops.
func TestE51SweepStops(t *testing.T) {
	k, ids := sweepKit(t, "")
	k.write("runs/"+ids[0]+"/response.json/x", "a directory where the response goes")
	k.wantCode(k.run("sweep"), term.ExitFailure)
	k.wantErr("ERROR: cannot read " + k.path("runs/"+ids[0]+"/response.json"))
	if k.count("sweep-spool") != 0 {
		t.Fatal("the spool was swept after the runs failed")
	}

	k, ids = sweepKit(t)
	k.write(retentionLog+"/x", "a directory where the log goes")
	k.write("bases/server-fast-44.build/disk.qcow2", "x")
	k.wantCode(k.run("sweep"), term.ExitFailure)
	k.wantErr("ERROR: cannot open " + k.path(retentionLog))
	if len(ids) != 0 || !k.exists("bases/server-fast-44.build") {
		t.Fatal("a build directory went unlogged")
	}
}
