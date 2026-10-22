package lxcadd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// repoRoot is the checkout holding the real declare.py, above the test's directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "helpers", "agent_bus", "declare.py")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no helpers/agent_bus/declare.py above the test directory")
		}
		dir = parent
	}
}

// realDeclare runs the real declare.py for the checks and the edit of an LXC member, against the
// world's host_vars file, so what the owner saw is what the test sees.
func (r *rig) realDeclare(t *testing.T) {
	t.Helper()
	root := repoRoot(t)
	for _, sub := range []string{"lxc-check", "lxc-add", "lxc-compare"} {
		r.Fake.OnFunc(strings.Fields(r.DeclarePrefix(sub)), func(c execx.Cmd) (execx.Result, error) {
			c.Args = slices.Clone(c.Args)
			c.Args[0] = "PYTHONPATH=" + root
			res, err := execx.OS{}.Run(context.Background(), c)
			if refused, ok := errors.AsType[*execx.ExitError](err); ok && refused.Code != 0 {
				return res, nil
			}
			return res, err
		})
	}
}

func (r *rig) hostVars(t *testing.T) string {
	t.Helper()
	text, err := os.ReadFile(r.Host.HostVars)
	if err != nil {
		t.Fatal(err)
	}
	return string(text)
}

// declaredMember is the owner's entry: written by an earlier lxc-add whose play then failed.
var declaredMember = declare.LxcMember{Team: "alpha", Container: "container-a", User: "agent", Checkout: "/srv/agent/one"}

// The owner's defect: lxc-add of the member an earlier lxc-add had written asked for the user
// three times over "agent_bus_lxc_members entries 2 and 3 both declare agent@container-a in team
// alpha. Try again." and gave up. The member is recognised, and the team applied, as after a write.
func TestAMemberDeclaredAlreadyIsRecognisedAndTheTeamApplied(t *testing.T) {
	r := newRig(t)
	r.WriteHostVars(t, teamtest.HostVarsWithLxc(declaredMember))
	before := r.hostVars(t)
	r.realDeclare(t)
	r.happy()
	if code := r.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	said := r.ErrOut.String()
	for _, refused := range []string{"both declare", "Try again", "Giving up"} {
		if strings.Contains(said, refused) {
			t.Fatalf("the declared member was refused (%q):\n%s", refused, said)
		}
	}
	if !strings.Contains(said, "agent@container-a is already declared in team alpha") {
		t.Fatalf("stderr does not say the member is declared already:\n%s", said)
	}
	if !slices.Equal(r.applied, []string{"alpha"}) || r.hostVars(t) != before {
		t.Fatalf("applied %v; the file changed: %v", r.applied, r.hostVars(t) != before)
	}
	if got := r.Out.String(); !strings.HasPrefix(got, "UNCHANGED\t") {
		t.Fatalf("stdout %q", got)
	}
	if q := r.Script.GumLog[len(r.Script.GumLog)-1]; !strings.Contains(q, "Apply alpha") || strings.Contains(q, "Add agent@container-a") {
		t.Fatalf("the last question is not the apply: %q", q)
	}
	if !strings.Contains(said, "is joined to alpha") && !strings.Contains(said, "is a member of alpha") {
		t.Fatalf("what follows a write is not said:\n%s", said)
	}
}

// A member declared otherwise is told key by key, and the owner chooses to update or stop.
func TestAMemberDeclaredOtherwiseNamesTheKeysAndUpdatesOnlyWhenChosen(t *testing.T) {
	other := declaredMember
	other.Checkout = "/srv/agent/two"
	for _, choice := range []string{updateChoice(1), stopChoice} {
		r := newRig(t)
		r.WriteHostVars(t, teamtest.HostVarsWithLxc(other))
		before := r.hostVars(t)
		r.realDeclare(t)
		r.answers(choose("container-a"), choose("agent"), choose("worker"), choose(choice), "YES")
		code := r.run("alpha")
		said := r.ErrOut.String()
		if !strings.Contains(said, "agent@container-a is already declared in team alpha, as entry 1 of agent_bus_lxc_members") ||
			!strings.Contains(said, `checkout: "/srv/agent/two" in the file, "/srv/agent/one" as answered`) {
			t.Fatalf("%s: the differences are not named:\n%s", choice, said)
		}
		if choice == stopChoice {
			if code != term.ExitFailure || r.hostVars(t) != before || len(r.applied) != 0 || !strings.Contains(said, "Nothing was changed.") {
				t.Fatalf("stop: exit %d, applied %v: %s", code, r.applied, said)
			}
			continue
		}
		if code != term.ExitOK || !slices.Equal(r.applied, []string{"alpha"}) {
			t.Fatalf("update: exit %d, applied %v: %s", code, r.applied, said)
		}
		if after := r.hostVars(t); !strings.Contains(after, `checkout: "/srv/agent/one"`) || strings.Contains(after, "/srv/agent/two") {
			t.Fatalf("the entry was not updated:\n%s", after)
		}
	}
}

// A refusal of the host_vars file is not asked again: it stops at once, naming the file.
func TestARefusalOfTheFileStopsAtOnceNamingItNotAskedAgain(t *testing.T) {
	r := newRig(t)
	r.answers(choose("container-a"), choose("agent"), choose("agent"), choose("agent"))
	r.OnErr(r.DeclarePrefix("lxc-check"), "declare: agent_bus_lxc_members entries 1 and 2 both declare agent@container-a in team alpha\n", 78)
	code := r.run("alpha")
	said := r.ErrOut.String()
	if code != 78 || strings.Contains(said, "Try again") || strings.Contains(said, "Giving up") {
		t.Fatalf("exit %d:\n%s", code, said)
	}
	if n := strings.Count(strings.Join(r.Script.GumLog, "\n"), "User in container-a"); n != 1 {
		t.Fatalf("the user was asked %d times", n)
	}
	wants := []string{
		"entries 1 and 2 both declare agent@container-a", term.AbbrevHome(r.Host.HostVars, r.Host.Home),
		"not asked again", "ccy-teams lxc-add alpha again", "nothing was changed",
	}
	for _, want := range wants {
		if !strings.Contains(said, want) {
			t.Fatalf("stderr lacks %q:\n%s", want, said)
		}
	}
	if r.addLine() != "" || len(r.applied) != 0 {
		t.Fatalf("add %q applied %v", r.addLine(), r.applied)
	}
}

// The declared coordinator that is the member itself is not offered a takeover of itself.
func TestTheDeclaredCoordinatorReAddedIsNotOfferedATakeoverOfItself(t *testing.T) {
	r := newRig(t)
	own := declaredMember
	own.Role = "coordinator"
	r.WriteHostVars(t, teamtest.HostVarsWithLxc(own))
	r.realDeclare(t)
	r.answers(choose("container-a"), choose("agent"), choose(keepChoice("alpha")), "YES")
	if code := r.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	if q := r.roleQuestion(); strings.Contains(q, "Take over") || !strings.Contains(q, keepChoice("alpha")) {
		t.Fatalf("the role question: %q", q)
	}
	if !strings.Contains(r.ErrOut.String(), "is already declared in team alpha") || len(r.setRoles) != 0 {
		t.Fatalf("set-role %v: %s", r.setRoles, r.ErrOut.String())
	}
}
