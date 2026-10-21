package seat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// v1Harness runs v1's own `agent-bus seat list` (cli.main, its exit mapping and its stderr
// prefix) for each fixture checkout, as a working directory with a user id, and reports the
// exit code, stdout and stderr, or the class of the exception it did not catch (a crash). The
// stderr is encoded as python's sys.stderr encodes it (backslashreplace).
const v1Harness = `
import dataclasses, io, json, pathlib, sys
from helpers.agent_bus import checkout, cli

def run(case):
    system = dataclasses.replace(checkout.real_system(pathlib.Path(case["top"])), uid=case["uid"])
    out, err = io.BytesIO(), io.StringIO()
    try:
        code = cli.main(["seat", "list"], stdout=out, stderr=err, checkout_system=system)
    except Exception as exc:
        return {"crash": type(exc).__name__}
    return {"code": code, "stdout": out.getvalue().decode(),
            "stderr": err.getvalue().encode("utf-8", "backslashreplace").decode()}

print(json.dumps([run(case) for case in json.load(sys.stdin)]))
`

type cliResult struct {
	Code   int    `json:"code"`
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
	Crash  string `json:"crash"`
}

type parityCase struct {
	Top string `json:"top"`
	UID int    `json:"uid"`
}

type parityFixture struct {
	parityCase

	name string
}

// goSeatList is what agent-bus seat list would print given List's answer.
func goSeatList(top string, uid int) cliResult {
	listing, err := List(top, uid)
	if refusal, ok := errors.AsType[*RefusalError](err); ok {
		return cliResult{Code: 78, Stderr: "agent-bus: " + refusal.Msg + "\n"}
	}
	if err != nil {
		return cliResult{Crash: err.Error()}
	}
	var res cliResult
	for _, s := range listing.Seats {
		state := "free"
		if s.Held {
			state = "held"
		}
		res.Stdout += fmt.Sprintf("SEAT\t%s\t%s\t%s\t-\t%s\n", s.Team, s.Seat, state, s.Handle)
	}
	for _, f := range listing.Failures {
		res.Stderr += "agent-bus: " + f + "\n"
	}
	if len(listing.Failures) > 0 {
		res.Code = 78
	}
	return res
}

// lineDiff is each line only Go printed (+Go) or only v1 printed (-v1), in order.
func lineDiff(goText, v1Text string) string {
	goLines, v1Lines := strings.Split(goText, "\n"), strings.Split(v1Text, "\n")
	var out strings.Builder
	for _, line := range goLines {
		if !slices.Contains(v1Lines, line) {
			out.WriteString("\n  +Go " + line)
		}
	}
	for _, line := range v1Lines {
		if !slices.Contains(goLines, line) {
			out.WriteString("\n  -v1 " + line)
		}
	}
	return out.String()
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "helpers", "agent_bus", "cli.py")); err != nil {
		t.Fatalf("v1 not found at the repository root: %v", err)
	}
	return root
}

// checkoutFixture is a fresh checkout, a git work tree (v1 asks git for the top level),
// built by build.
func checkoutFixture(t *testing.T, name string, build func(f fx, top string)) parityFixture {
	t.Helper()
	top := newCheckout(t)
	if _, err := (execx.OS{}).Run(context.Background(), execx.Cmd{Name: "git", Args: []string{"init", "-q", top}}); err != nil {
		t.Fatal(err)
	}
	build(fx{t}, top)
	return parityFixture{parityCase{Top: top, UID: uid()}, name}
}

// buildMembers puts every member case v1 3.11 answers as the host does into top, as seats of
// alpha, and the lock cases as seats of beta; it returns the count of seat cases.
func buildMembers(f fx, top string) int {
	count := 0
	for i, c := range memberCases() {
		if !c.notOn311 {
			c.build(f, f.addSeat(top, testTeam, fmt.Sprintf("m%03d", i)))
			count++
		}
	}
	lock := func(seat string) string { return filepath.Join(f.addMember(top, "beta", seat), lockFile) }
	lock("l1")
	f.writeFile(lock("l2"), "seat 1")
	held := lock("l3")
	f.writeFile(held, "seat 3")
	holdLock(f.t, held, "-x")
	shared := lock("l4")
	f.writeFile(shared, "")
	holdLock(f.t, shared, "-s")
	f.symlink("elsewhere", lock("l5"))
	f.mkdirs(lock("l6"))
	f.mkfifo(lock("l7"))
	f.symlink("elsewhere", filepath.Join(f.addSeat(top, "beta", "l8"), lockFile))
	return count + 8
}

func parityFixtures(t *testing.T) ([]parityFixture, int) {
	t.Helper()
	seatCases := 0
	ccy := func(top string) string { return filepath.Join(top, ".claude", "ccy") }
	fixtures := []parityFixture{
		checkoutFixture(t, "members and locks", func(f fx, top string) { seatCases = buildMembers(f, top) }),
		checkoutFixture(t, "no ccy directory", func(f fx, top string) { f.remove(filepath.Join(top, ".claude")) }),
		checkoutFixture(t, "ccy a symlink", func(f fx, top string) {
			f.rename(ccy(top), ccy(top)+".real")
			f.symlink("ccy.real", ccy(top))
		}),
		checkoutFixture(t, "ccy a file", func(f fx, top string) {
			f.remove(ccy(top))
			f.writeFile(ccy(top), "")
		}),
		checkoutFixture(t, "no seats directory", func(fx, string) {}),
		checkoutFixture(t, "an empty seats tree", func(f fx, top string) { f.mkdirs(filepath.Join(seatsRoot(top), "alpha")) }),
		checkoutFixture(t, "order", func(f fx, top string) {
			for _, ref := range orderedRefs {
				f.addMember(top, ref[0], ref[1], with("user_id", js("@demo."+ref[1]+"+box.podman:"+testServer)))
			}
		}),
	}
	owned := checkoutFixture(t, "someone else's checkout", func(fx, string) {})
	owned.UID = uid() + 4242
	more := make([]parityFixture, 0, 1+len(treeCases)+len(crashTrees)+len(crashCases))
	more = append(more, owned)
	for _, c := range treeCases {
		more = append(more, checkoutFixture(t, c.about, func(f fx, top string) { c.build(f, seatsRoot(top)) }))
	}
	for _, c := range crashTrees {
		more = append(more, checkoutFixture(t, c.about, c.build))
	}
	for _, raw := range crashCases {
		more = append(more, checkoutFixture(t, "base_url "+raw, func(f fx, top string) {
			f.addMember(top, testTeam, "1", with("base_url", js(raw)))
		}))
	}
	return slices.Concat(fixtures, more), seatCases
}

// TestParityWithV1 runs the real v1 over the same trees as the Go and requires the same seats,
// held states, failure lines, refusals and crashes (V30-V35).
func TestParityWithV1(t *testing.T) {
	fixtures, seatCases := parityFixtures(t)
	cases := make([]parityCase, 0, len(fixtures))
	for _, f := range fixtures {
		cases = append(cases, f.parityCase)
	}
	input, err := json.Marshal(cases)
	if err != nil {
		t.Fatal(err)
	}
	res, err := execx.OS{}.Run(context.Background(), execx.Cmd{
		Name: "python3", Args: []string{"-P", "-c", v1Harness},
		Env: []string{"PYTHONPATH=" + repoRoot(t)}, Stdin: strings.NewReader(string(input)),
	})
	if err != nil {
		t.Fatal(err)
	}
	var v1 []cliResult
	if err := json.Unmarshal(res.Stdout, &v1); err != nil || len(v1) != len(fixtures) {
		t.Fatalf("v1 answered %d of %d: %v\n%s", len(v1), len(fixtures), err, res.Stdout)
	}
	crashes := 0
	for i, f := range fixtures {
		got, want := goSeatList(f.Top, f.UID), v1[i]
		if want.Crash != "" {
			crashes++
			if got.Crash == "" {
				t.Errorf("%s: v1 crashed (%s), Go answered %+v", f.name, want.Crash, got)
			}
			continue
		}
		if got != want {
			t.Errorf("%s: exit Go %d, v1 %d\nstdout:%s\nstderr:%s", f.name, got.Code, want.Code,
				lineDiff(got.Stdout, want.Stdout), lineDiff(got.Stderr, want.Stderr))
		}
	}
	t.Logf("compared %d fixture checkouts (%d crashes) and %d seat cases in one of them", len(fixtures), crashes, seatCases)
}
