package admin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/health"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// doc is a member or role verb's JSON answer, decoded with no unknown key allowed.
type doc struct {
	V       int      `json:"v"`
	Changed []string `json:"changed"`
	Handle  string   `json:"handle"`
	Notes   []string `json:"notes"`
	Bundle  *struct {
		Team   string `json:"team"`
		Handle string `json:"handle"`
		Kind   string `json:"kind"`
		Secret string `json:"secret"`
	} `json:"bundle"`
}

func decodeDoc(t *testing.T, out outcome) doc {
	t.Helper()
	if strings.Count(out.stdout, "\n") != 1 || !strings.HasSuffix(out.stdout, "\n") {
		t.Fatalf("stdout is not one line: %q", out.stdout)
	}
	dec := json.NewDecoder(strings.NewReader(out.stdout))
	dec.DisallowUnknownFields()
	var d doc
	if err := dec.Decode(&d); err != nil || d.V != 1 || d.Changed == nil {
		t.Fatalf("answer %q: %v", out.stdout, err)
	}
	return d
}

// changedLines are a play verb's CHANGED lines.
func changedLines(t *testing.T, out outcome) []string {
	t.Helper()
	var lines []string
	for line := range strings.Lines(out.stdout) {
		what, ok := strings.CutPrefix(strings.TrimSuffix(line, "\n"), "CHANGED\t")
		if !ok {
			t.Fatalf("not a CHANGED line: %q", line)
		}
		lines = append(lines, what)
	}
	return lines
}

func (w *world) ok(t *testing.T, stdin string, args ...string) outcome {
	t.Helper()
	out := w.verb(stdin, args...)
	if out.code != term.ExitOK {
		t.Fatalf("%v = %d\nstdout %s\nstderr %s", args, out.code, out.stdout, out.stderr)
	}
	return out
}

// applied approves and applies alpha.
func (w *world) applied(t *testing.T) {
	t.Helper()
	in := encodeDoc(t, alphaDoc())
	w.ok(t, in, "team", "approve", "--team", "alpha")
	w.ok(t, in, "team", "apply")
}

func addArgs(seat, cred string) []string {
	return []string{"member", "add", "--team", "alpha", "--prefix", prefix, "--seat", seat, "--kind", "podman", "--human-text", "true", "--credential", cred}
}

// on is a member verb on the third seat.
func on(verb string, more ...string) []string {
	return append([]string{"member", verb, "--team", "alpha", "--handle", sampleHandle}, more...)
}

func (w *world) signsIn(t *testing.T, handle, password string) bool {
	t.Helper()
	_, err := w.admin.LoginDevice(t.Context(), handle, password, "TCTEST")
	return err == nil
}

func (w *world) rooms(t *testing.T) proto.RoomIDs {
	t.Helper()
	raw, err := os.ReadFile(w.root + paths.RoomsFile(alpha(t)))
	if err != nil {
		t.Fatal(err)
	}
	ids, err := apply.DecodeRooms(raw, alpha(t))
	if err != nil {
		t.Fatal(err)
	}
	return ids
}

func (w *world) roster(t *testing.T) apply.RosterFile {
	t.Helper()
	raw, err := os.ReadFile(w.root + paths.RosterFile(alpha(t)))
	if err != nil {
		t.Fatal(err)
	}
	f, err := apply.DecodeRosterFile(raw, alpha(t))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// notices are the admin's notices in main, oldest first, as `<event> <subject localpart>`.
func (w *world) notices(t *testing.T) []string {
	t.Helper()
	page, err := w.admin.Messages(t.Context(), w.rooms(t).Main, matrix.MessagesRequest{Dir: matrix.Forward, Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, raw := range page.Chunk {
		v, err := proto.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		content, found := v.Get("content")
		if _, notice := content.Get(proto.KeyNotice); !found || !notice {
			continue
		}
		c, reason := proto.DecodeContent(content, proto.AdminMainKeys)
		if reason != "" {
			t.Fatalf("notice %s: %s", content.Compact(), reason)
		}
		out = append(out, c.Notice.Event+" "+strings.TrimSuffix(strings.TrimPrefix(c.Notice.Subject, "@"), ":"+server))
	}
	return out
}

// TestApproveThenApply is CV4 and CV5: the approval, then team apply's host steps with step 10
// the child's team.apply, then an apply that changes nothing.
func TestApproveThenApply(t *testing.T) {
	w := newWorld(t)
	in := encodeDoc(t, alphaDoc())
	approved := changedLines(t, w.ok(t, in, "team", "approve", "--team", "alpha"))
	if !slices.Equal(approved, []string{"approved team"}) {
		t.Fatalf("approve %v", approved)
	}
	if again := w.ok(t, in, "team", "approve", "--team", "alpha"); again.stdout != "" {
		t.Fatalf("a second approve: %q", again.stdout)
	}
	lines := changedLines(t, w.ok(t, in, "team", "apply"))
	for _, want := range []string{"files created", "human alice created", "room main created", "stamp written"} {
		if !slices.ContainsFunc(lines, func(l string) bool { return strings.HasPrefix(l, want) }) {
			t.Errorf("no %q in %v", want, lines)
		}
	}
	if len(w.regKeys) != 1 || len(w.regKeys[0]) != 128 {
		t.Fatalf("the child was given registration keys %q", w.regKeys)
	}
	if !slices.Equal(w.ports, []int{8448}) {
		t.Fatalf("the child opened ports %v, not the declared 8448 (OP6)", w.ports)
	}
	if w.rooms(t).Main == "" || w.roster(t).CoordinatorWanted != coord {
		t.Fatalf("rooms %+v roster %+v", w.rooms(t), w.roster(t))
	}
	if again := w.ok(t, in, "team", "apply"); again.stdout != "" {
		t.Fatalf("a second apply: %q", again.stdout)
	}
}

func TestApplyOfAnUnapprovedChangeIs70(t *testing.T) {
	w := newWorld(t)
	w.applied(t)
	d := alphaDoc()
	docTeam(d)["humans"] = []any{"alice", "bob", "mallory"}
	out := w.verb(encodeDoc(t, d), "team", "apply")
	if out.code != ExitRefused || out.stdout != "" || !strings.Contains(out.stderr, "step refusals") {
		t.Fatalf("%+v", out)
	}
	if out = w.verb(encodeDoc(t, d), "team", "approve", "--team", "alpha", "--only", "humans", "--add", "eve"); out.code != ExitRefused {
		t.Fatalf("an approve of another change: %+v", out)
	}
	w.ok(t, encodeDoc(t, d), "team", "approve", "--team", "alpha", "--only", "humans", "--add", "mallory")
	if lines := changedLines(t, w.ok(t, encodeDoc(t, d), "team", "apply")); !slices.ContainsFunc(lines, func(l string) bool { return strings.Contains(l, "mallory") }) {
		t.Fatalf("apply %v", lines)
	}
}

func (w *world) lockIsFree(t *testing.T) {
	t.Helper()
	lock, err := teamlock.Take(t.Context(), w.root+paths.AdminRunDir, alpha(t), "test", 0, w)
	if err != nil {
		t.Fatalf("the lock was not released: %v", err)
	}
	if err = lock.Release(); err != nil {
		t.Fatal(err)
	}
}

// TestMemberAddThroughTheVerb is CV3: the lock, team.json, the child, the bundle and the status.
func TestMemberAddThroughTheVerb(t *testing.T) {
	w := newWorld(t)
	w.applied(t)
	refreshed := len(w.status)
	d := decodeDoc(t, w.ok(t, "", addArgs("3", "password")...))
	if d.Handle != sampleHandle || d.Bundle == nil || d.Bundle.Handle != sampleHandle || d.Bundle.Kind != "password" || d.Bundle.Team != "alpha" {
		t.Fatalf("answer %+v", d)
	}
	if !w.signsIn(t, sampleHandle, d.Bundle.Secret) {
		t.Fatal("the bundle's password does not sign in")
	}
	if !slices.Contains(d.Changed, "member "+sampleHandle+" issued") || slices.ContainsFunc(d.Changed, func(l string) bool { return strings.Contains(l, d.Bundle.Secret) }) {
		t.Fatalf("changed %v", d.Changed)
	}
	if len(w.status) != refreshed+1 || w.status[refreshed] {
		t.Fatalf("status refreshed %v", w.status)
	}
	w.lockIsFree(t)
	// The wanted coordinator is in no state yet, so next is its seat, 1.
	token := decodeDoc(t, w.ok(t, "", addArgs("next", "token")...))
	if token.Handle != coord || token.Bundle == nil || token.Bundle.Kind != "token" {
		t.Fatalf("seat next %+v", token)
	}
}

func TestMemberVerbsThroughTheVerbs(t *testing.T) {
	w := newWorld(t)
	w.applied(t)
	w.ok(t, "", addArgs("3", "password")...)
	set := decodeDoc(t, w.ok(t, "", on("set", "--human-text", "false")...))
	if set.Bundle != nil || !slices.Contains(set.Changed, "member "+sampleHandle+" human_text false") {
		t.Fatalf("set %+v", set)
	}
	issued := decodeDoc(t, w.ok(t, "", on("reissue", "--credential", "password")...))
	if issued.Bundle == nil || !w.signsIn(t, sampleHandle, issued.Bundle.Secret) {
		t.Fatalf("reissue %+v", issued)
	}
	if parked := decodeDoc(t, w.ok(t, "", on("park")...)); parked.Bundle != nil || w.signsIn(t, sampleHandle, issued.Bundle.Secret) {
		t.Fatalf("park %+v", parked)
	}
	back := decodeDoc(t, w.ok(t, "", on("return", "--credential", "token")...))
	if back.Bundle == nil || back.Bundle.Kind != "token" {
		t.Fatalf("return %+v", back)
	}
	retired := decodeDoc(t, w.ok(t, "", on("retire")...))
	if !slices.Contains(retired.Changed, "member "+sampleHandle+" deactivated") {
		t.Fatalf("retire %+v", retired)
	}
	want := []string{"added " + sampleHandle, "parked " + sampleHandle, "added " + sampleHandle, "removed " + sampleHandle}
	if got := w.notices(t); !slices.Equal(got, want) {
		t.Fatalf("notices %v", got)
	}
	if w.bundled[len(w.bundled)-1] != sampleHandle || len(w.bundled) != 3 {
		t.Fatalf("bundled %v", w.bundled)
	}
}

// TestACoordinatorChangeThroughTheVerbs: the one-field approve, then team apply's role steps;
// role apply over the applied team then changes nothing.
func TestACoordinatorChangeThroughTheVerbs(t *testing.T) {
	w := newWorld(t)
	w.applied(t)
	added := decodeDoc(t, w.ok(t, "", addArgs("1", "password")...))
	if !slices.Contains(added.Changed, "role "+coord+" promoted") {
		t.Fatalf("add of the wanted coordinator %v", added.Changed)
	}
	w.ok(t, "", addArgs("2", "password")...)
	d := alphaDoc()
	docTeam(d)["coordinator"] = other
	w.ok(t, encodeDoc(t, d), "team", "approve", "--team", "alpha", "--only", "coordinator", "--set", other)
	lines := changedLines(t, w.ok(t, encodeDoc(t, d), "team", "apply"))
	demoted, promoted := slices.Index(lines, "role "+coord+" demoted"), slices.Index(lines, "role "+other+" promoted")
	if demoted < 0 || promoted < demoted {
		t.Fatalf("apply %v", lines)
	}
	if role := decodeDoc(t, w.ok(t, "", "role", "apply", "--team", "alpha")); len(role.Changed) != 0 || role.Handle != "" {
		t.Fatalf("role apply %+v", role)
	}
}

// TestAddCrashedAtEachStepIsRolledForwardByTheSameVerb is CV6: a crash at each step of add,
// then the same verb asked again answering a working credential, with one added notice.
func TestAddCrashedAtEachStepIsRolledForwardByTheSameVerb(t *testing.T) {
	crashes := []struct {
		method string
		n      int
		landed bool
		step   string
	}{
		{"Account", 1, false, apply.StepAccount}, // the refusals' look, before pending
		{"Account", 2, false, apply.StepAccount},
		{"CreateAccount", 1, true, apply.StepAccount},
		{"ResetPassword", 1, true, apply.StepCredential},
		{"LoginDevice", 1, true, apply.StepCredential},
		{"Invite", 1, false, apply.StepRooms},
		{"Send", 1, true, apply.StepNotice},
		{"Send", 2, true, "role promoted " + coord},
	}
	for _, c := range crashes {
		t.Run(fmt.Sprintf("%s-%d", c.method, c.n), func(t *testing.T) {
			w := newWorld(t)
			w.applied(t)
			args := addArgs("1", "token")
			if c.method == "CreateAccount" {
				args = addArgs("3", "token")
			}
			w.crash = &crashing{method: c.method, n: c.n, landed: c.landed}
			out := w.verb("", args...)
			w.crash = nil
			if out.code != term.ExitFailure || !strings.Contains(out.stderr, "step "+c.step+":") || decodeDoc(t, out).Bundle != nil {
				t.Fatalf("crash %+v", out)
			}
			d := decodeDoc(t, w.ok(t, "", args...))
			member, err := w.admin.WithToken(d.Bundle.Secret)
			if err != nil {
				t.Fatal(err)
			}
			if id, err := member.Whoami(t.Context()); err != nil || id.UserID != proto.UserID(d.Handle, server) {
				t.Fatalf("the retry's token: %+v %v", id, err)
			}
			if n := countPrefixed(w.notices(t), "added "); n != 1 {
				t.Fatalf("notices %v", w.notices(t))
			}
		})
	}
}

func countPrefixed(lines []string, prefix string) int {
	n := 0
	for _, l := range lines {
		if strings.HasPrefix(l, prefix) {
			n++
		}
	}
	return n
}

// TestACrashIsRolledForwardByAnotherVerb is CV6's other rule: another verb completes the op,
// a pending add with its credential revoked and no bundle printed.
func TestACrashIsRolledForwardByAnotherVerb(t *testing.T) {
	cases := []struct {
		name   string
		first  []string
		crash  crashing
		state  string
		notice string
	}{
		{"add", addArgs("3", "password"), crashing{method: "Invite", n: 1}, apply.MemberActive, "added"},
		{"park", on("park"), crashing{method: "ResetPassword", n: 1}, apply.MemberParked, "parked"},
		{"retire", on("retire"), crashing{method: "SetDeactivated", n: 1, landed: true}, apply.MemberRetired, "removed"},
		{"retire notice", on("retire"), crashing{method: "Send", n: 1}, apply.MemberRetired, "removed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			w.applied(t)
			if c.name != "add" {
				w.ok(t, "", addArgs("3", "password")...)
			}
			w.crash = &c.crash
			if out := w.verb("", c.first...); out.code != term.ExitFailure {
				t.Fatalf("crash %+v", out)
			}
			w.crash = nil
			if d := decodeDoc(t, w.ok(t, "", "role", "apply", "--team", "alpha")); d.Bundle != nil {
				t.Fatalf("role apply printed a bundle %+v", d)
			}
			f := w.roster(t)
			if m := f.Members[sampleHandle]; f.Pending != nil || m.State != c.state || (c.name == "add") != (m.Needs != "") {
				t.Fatalf("roster %+v", f)
			}
			if n := countPrefixed(w.notices(t), c.notice+" "); n != 1 {
				t.Fatalf("notices %v", w.notices(t))
			}
		})
	}
}

// TestATeamApplyCrashedInBootstrapIsFinished: the child's failure is the bootstrap step's, and
// the next apply finishes it.
func TestATeamApplyCrashedInBootstrapIsFinished(t *testing.T) {
	w := newWorld(t)
	in := encodeDoc(t, alphaDoc())
	w.ok(t, in, "team", "approve", "--team", "alpha")
	w.crash = &crashing{method: "CreateAccount", n: 1, landed: true}
	out := w.verb(in, "team", "apply")
	w.crash = nil
	if out.code != term.ExitFailure || !strings.Contains(out.stderr, "step bootstrap") || len(changedLines(t, out)) == 0 {
		t.Fatalf("crash %+v", out)
	}
	w.ok(t, in, "team", "apply")
	if w.roster(t).Pending != nil || w.rooms(t).Main == "" {
		t.Fatalf("roster %+v", w.roster(t))
	}
	if again := w.ok(t, in, "team", "apply"); again.stdout != "" {
		t.Fatalf("a third apply: %q", again.stdout)
	}
}

func TestTeamRemove(t *testing.T) {
	w := newWorld(t)
	w.applied(t)
	if lines := changedLines(t, w.ok(t, "", "team", "remove", "--team", "alpha")); len(lines) == 0 {
		t.Fatal("remove changed nothing")
	}
	if again := w.ok(t, "", "team", "remove", "--team", "alpha"); again.stdout != "" {
		t.Fatalf("a second remove: %q", again.stdout)
	}
	w.ok(t, "", "team", "remove", "--team", "alpha", "--purge")
	if _, err := os.Stat(w.root + paths.AdminState(alpha(t))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("purge left root/alpha: %v", err)
	}
}

// TestVerbRefusals: a refusal is 70 before any change, with the answer's empty changed.
func TestVerbRefusals(t *testing.T) {
	w := newWorld(t)
	out := w.verb("", addArgs("3", "password")...)
	if out.code != ExitRefused || !strings.Contains(out.stderr, "team apply") {
		t.Fatalf("an unapplied team %+v", out)
	}
	w.applied(t)
	w.ok(t, "", addArgs("3", "password")...)
	out = w.verb("", addArgs("3", "password")...)
	if out.code != ExitRefused || len(decodeDoc(t, out).Changed) != 0 || !strings.Contains(out.stderr, "is taken") {
		t.Fatalf("a seat taken %+v", out)
	}
}

func TestVerbBusy(t *testing.T) {
	w := newWorld(t)
	w.applied(t)
	lock, err := teamlock.Take(t.Context(), w.root+paths.AdminRunDir, alpha(t), "team.apply", 0, w)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := lock.Release(); err != nil {
			t.Error(err)
		}
	})
	for _, args := range [][]string{addArgs("3", "password"), {"role", "apply", "--team", "alpha"}} {
		out := w.verb("", args...)
		if out.code != ExitBusy || out.stdout != "" || !strings.Contains(out.stderr, "team.apply alpha pid=") {
			t.Fatalf("%v: %+v", args, out)
		}
	}
}

// TestVerbFlagsAreStrict is CV3's 64: no lock taken and no child asked.
func TestVerbFlagsAreStrict(t *testing.T) {
	w := newWorld(t)
	bad := map[string][]string{
		"unknown flag":        {"member", "park", "--team", "alpha", "--handle", sampleHandle, "--seat", "1"},
		"repeated flag":       {"member", "park", "--team", "alpha", "--team", "alpha", "--handle", sampleHandle},
		"no value":            {"member", "park", "--team", "alpha", "--handle"},
		"missing flag":        {"member", "park", "--team", "alpha"},
		"positional":          {"member", "park", "--team", "alpha", "--handle", sampleHandle, "extra"},
		"bad team":            {"member", "park", "--team", "Alpha!", "--handle", sampleHandle},
		"seat zero":           addArgs("0", "password"),
		"seat word":           addArgs("three", "password"),
		"credential":          addArgs("3", "key"),
		"human text":          {"member", "set", "--team", "alpha", "--handle", sampleHandle, "--human-text", "yes"},
		"flag value a flag":   {"member", "park", "--team", "--handle", sampleHandle},
		"bool with a value":   {"team", "remove", "--team", "alpha", "--purge", "yes"},
		"only without change": {"team", "approve", "--team", "alpha", "--only", "humans"},
		"change without only": {"team", "approve", "--team", "alpha", "--add", "eve"},
		"two changes":         {"team", "approve", "--team", "alpha", "--only", "humans", "--add", "eve", "--remove", "bob"},
		"apply with a flag":   {"team", "apply", "--team", "alpha"},
		"role apply no team":  {"role", "apply"},
	}
	for name, args := range bad {
		if out := w.verb(encodeDoc(t, alphaDoc()), args...); out.code != term.ExitUsage || out.stdout != "" {
			t.Errorf("%s: %+v", name, out)
		}
	}
	if len(w.regKeys) != 0 {
		t.Fatal("a bad verb reached the child")
	}
}

// TestVerbInputRefusals: input that cannot be read is 1, another team is 64, and a one-field
// approval the approved team cannot take is 70; none prints anything on stdout.
func TestVerbInputRefusals(t *testing.T) {
	w := newWorld(t)
	w.ok(t, encodeDoc(t, alphaDoc()), "team", "approve", "--team", "alpha")
	other := alphaDoc()
	docTeam(other)["team"] = "beta"
	for name, c := range map[string]struct {
		stdin string
		args  []string
		code  term.ExitCode
		text  string
	}{
		"apply of no json":         {"nope", []string{"team", "apply"}, term.ExitFailure, "strict JSON"},
		"approve of another team":  {encodeDoc(t, other), []string{"team", "approve", "--team", "alpha"}, term.ExitUsage, "team beta"},
		"approve of no json":       {"nope", []string{"team", "approve", "--team", "alpha"}, term.ExitFailure, "strict JSON"},
		"apply of a large input":   {strings.Repeat(" ", maxRequest+1), []string{"team", "apply"}, term.ExitFailure, "over"},
		"approve of a bad --only":  {encodeDoc(t, alphaDoc()), []string{"team", "approve", "--team", "alpha", "--only", "colour", "--set", "x"}, ExitRefused, "colour"},
		"approve of a bad request": {encodeDoc(t, alphaDoc()), []string{"team", "approve", "--team", "alpha", "--only", "humans", "--set", "x"}, ExitRefused, "humans"},
	} {
		if out := w.verb(c.stdin, c.args...); out.code != c.code || out.stdout != "" || !strings.Contains(out.stderr, c.text) {
			t.Errorf("%s: %+v", name, out)
		}
	}
}

func TestApproveWithoutTheOwnerGroupIs78(t *testing.T) {
	w := newWorld(t)
	w.gidErr = errors.New("no owner-group file")
	if out := w.verb(encodeDoc(t, alphaDoc()), "team", "approve", "--team", "alpha"); out.code != ExitConfig || !strings.Contains(out.stderr, "owner-group") {
		t.Fatalf("%+v", out)
	}
}

// TestAFailedStatusRefreshStillPrintsTheBundle: the credential exists, so it is not lost.
func TestAFailedStatusRefreshStillPrintsTheBundle(t *testing.T) {
	w := newWorld(t)
	w.applied(t)
	w.statErr = errors.New("disk full")
	out := w.verb("", addArgs("3", "password")...)
	if out.code != term.ExitFailure || decodeDoc(t, out).Bundle == nil || !strings.Contains(out.stderr, "disk full") {
		t.Fatalf("%+v", out)
	}
}

// TestAChildsAnswerIsCheckedAgainstTheRequest: root builds a bundle only for the handle and the
// credential kind it asked for (C 3.3: the answer is written by a less-privileged process).
func TestAChildsAnswerIsCheckedAgainstTheRequest(t *testing.T) {
	cases := map[string]struct {
		args []string
		lie  func(*Answer)
	}{
		"another prefix's handle":  {addArgs("4", "password"), func(a *Answer) { a.Handle = "other.4+desk.podman" }},
		"another seat":             {addArgs("4", "password"), func(a *Answer) { a.Handle = "myrepo.5+desk.podman" }},
		"another prefix for next":  {addArgs("next", "password"), func(a *Answer) { a.Handle = "other.1+desk.podman" }},
		"another handle":           {on("reissue", "--credential", "password"), func(a *Answer) { a.Handle = coord }},
		"another kind":             {addArgs("4", "password"), func(a *Answer) { a.Credential.Kind = apply.CredToken }},
		"no credential for an add": {addArgs("4", "password"), func(a *Answer) { a.Credential = nil }},
		"a credential for a park":  {on("park"), func(a *Answer) { a.Credential = &apply.Credential{Kind: apply.CredPassword, Secret: "x"} }},
		"a handle for role apply":  {[]string{"role", "apply", "--team", "alpha"}, func(a *Answer) { a.Handle = coord }},
		"no main room for an add":  {addArgs("4", "password"), func(a *Answer) { a.MainRoom = "" }},
		"a main room for a park":   {on("park"), func(a *Answer) { a.MainRoom = sampleRoom }},
		"a view for a park":        {on("park"), func(a *Answer) { a.View = &health.View{} }},
		"a notice for role apply":  {[]string{"role", "apply", "--team", "alpha"}, func(a *Answer) { a.Notice = &health.Notice{Posted: true} }},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			w.applied(t)
			w.ok(t, "", addArgs("3", "password")...)
			bundled := len(w.bundled)
			w.lie = c.lie
			out := w.verb("", c.args...)
			if out.code != term.ExitFailure || out.stdout != "" || len(w.bundled) != bundled || !strings.Contains(out.stderr, "not what it was asked") {
				t.Fatalf("%+v, bundled %v", out, w.bundled)
			}
		})
	}
}

// TestATeamApplyAnswerIsHeldToItsOp: team.apply's answer carries changed lines only; a handle,
// notes, a credential or a main room in it is refused as a lie, as for the member verbs.
func TestATeamApplyAnswerIsHeldToItsOp(t *testing.T) {
	cases := map[string]func(*Answer){
		"a handle":     func(a *Answer) { a.Handle = coord },
		"notes":        func(a *Answer) { a.Notes = []string{"a note"} },
		"a credential": func(a *Answer) { a.Credential = &apply.Credential{Kind: apply.CredPassword, Secret: "x"} },
		"a main room":  func(a *Answer) { a.MainRoom = sampleRoom },
	}
	for name, lie := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			in := encodeDoc(t, alphaDoc())
			w.ok(t, in, "team", "approve", "--team", "alpha")
			w.lie = lie
			out := w.verb(in, "team", "apply")
			if out.code != term.ExitFailure || !strings.Contains(out.stderr, "not what it was asked") {
				t.Fatalf("%+v", out)
			}
		})
	}
}

func TestVerbsNeedTheirSeams(t *testing.T) {
	for _, args := range [][]string{addArgs("3", "password"), {"team", "apply"}, {"team", "remove", "--team", "alpha"}, {"team", "approve", "--team", "alpha"}} {
		var stdout, stderr bytes.Buffer
		code := Run(t.Context(), Env{Args: args, Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr, Proc: rootProc()})
		if code != term.ExitFailure || !strings.Contains(stderr.String(), "not wired") {
			t.Errorf("%v: %d %s", args, code, stderr.String())
		}
	}
}

func TestExitCodeOfTheHostAndTheChild(t *testing.T) {
	cases := map[error]term.ExitCode{
		&host.Error{Verb: "team apply", Step: "lock", Err: &teamlock.BusyError{Path: "p", Holder: "h"}}:          ExitBusy,
		&host.Error{Verb: "team apply", Step: "refusals", Drift: true, Err: errors.New("no")}:                    ExitRefused,
		&host.Error{Verb: "team apply", Step: "bootstrap", Err: &ChildError{Class: ClassRefused, Message: "no"}}: ExitRefused,
		&ChildError{Class: ClassUnreachable, Message: "down"}:                                                    ExitUnreachable,
		&host.Error{Verb: "team apply", Step: "unit", Err: errors.New("boom")}:                                   term.ExitFailure,
	}
	for err, want := range cases {
		if got := ExitCodeOf(err); got != want {
			t.Errorf("%v: %d, want %d", err, got, want)
		}
	}
}
