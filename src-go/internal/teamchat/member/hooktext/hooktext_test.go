package hooktext

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

func team(t *testing.T, s string) paths.TeamID {
	t.Helper()
	id, err := paths.ParseTeamID(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// The answers are one compact JSON object each, all ASCII, with no control character.
func TestAnswers(t *testing.T) {
	for _, tc := range []struct {
		a    Answer
		want string
	}{
		{Empty(), `{}`},
		{Context("SessionStart", "a\nb \"c\" é"), `{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"a\nb \"c\" ` + `\` + `u00e9"}}`},
		{Context("UserPromptSubmit", "x"), `{"hookSpecificOutput":{"hookEventName":"UserPromptSubmit","additionalContext":"x"}}`},
		{Context("Stop", "x"), `{}`},
		{Context("SessionStart", ""), `{}`},
		{Block("stop \u0085here"), `{"decision":"block","reason":"stop \u0085here"}`},
		{Block(""), `{}`},
	} {
		if string(tc.a.Line) != tc.want {
			t.Fatalf("%s, want %s", tc.a.Line, tc.want)
		}
		v, err := proto.Parse(tc.a.Line)
		if err != nil || v.Kind() != proto.KindObject {
			t.Fatalf("%s: %v", tc.a.Line, err)
		}
		for _, c := range tc.a.Line {
			if c < 0x20 || c > 0x7e {
				t.Fatalf("%q holds byte %#x", tc.a.Line, c)
			}
		}
	}
}

// The frame is A 13.5's text, then each team's block, then the delivery mode.
func TestFrame(t *testing.T) {
	alpha, beta, gamma := team(t, "alpha"), team(t, "beta"), team(t, "gamma")
	got := Frame([]FrameTeam{
		{Team: alpha, Handle: "myrepo.2+desk.podman", Role: proto.RoleCoordinator, Coordinator: "myrepo.2+desk.podman", Description: "Builds the thing.", Current: true},
		{Team: beta, Handle: "myrepo.3+desk.podman", Role: proto.RoleWorker, Description: "bad\x01description", Current: true},
		{Team: gamma, Handle: "evil name", Role: "boss", Coordinator: "also evil", Current: true},
	}, state.DeliverySocket)
	want := proto.FrameText + "\n\n" +
		"team alpha: you are myrepo.2+desk.podman, the coordinator (coordinator: myrepo.2+desk.podman). Builds the thing.\n" +
		"In team alpha you direct the team's agents; ask the humans with teamchat ask --team alpha, and follow up with teamchat questions --team alpha.\n" +
		"team beta: you are myrepo.3+desk.podman, the worker; the team has no coordinator now.\n" +
		"In team beta take direction from the team's humans and its coordinator; report with teamchat report --team beta.\n" +
		"team gamma: you are ?, the member (coordinator: ?).\n" +
		"Deliveries arrive in this conversation by themselves."
	if got != want {
		t.Fatalf("frame:\n%s\nwant:\n%s", got, want)
	}
	for mode, line := range map[string]string{
		state.DeliveryWait: "Keep teamchat wait running in the background",
		state.DeliveryNone: "run teamchat recv to read them",
		"other":            "run teamchat recv to read them",
	} {
		if f := Frame(nil, mode); !strings.HasPrefix(f, proto.FrameText+"\n\n") || !strings.Contains(f, line) {
			t.Fatalf("%s: %s", mode, f)
		}
	}
	if f := Frame([]FrameTeam{{Team: alpha, Handle: "myrepo.2+desk.podman", Role: proto.RoleProbe, Current: true}}, state.DeliverySocket); !strings.Contains(f, "In team alpha this session is the team's acceptance probe.") {
		t.Fatalf("probe: %s", f)
	}
}

// Issue 192, class 1: a team's frame block states its role and coordinator only from a record
// verified current. Any other record (not verified yet, stopped, untrusted), with or without a
// coordinator in it, says they are not known yet and how to look, never nothing and never an
// absence, and gives no role guidance.
func TestTheFrameNeverClaimsARoleOrAnAbsenceFromARecordNotCurrent(t *testing.T) {
	alpha := team(t, "alpha")
	const me, coord = "myrepo.3+desk.podman", "myrepo.2+desk.podman"
	notKnown := "team alpha: you are " + me + "; your role and the team's coordinator are not known yet: run teamchat status --team alpha."
	for _, c := range []struct {
		name              string
		current           bool
		role, coordinator string
		want              []string
	}{
		{"verified, a coordinator", true, proto.RoleWorker, coord, []string{
			"team alpha: you are " + me + ", the worker (coordinator: " + coord + ").",
			"In team alpha take direction from the team's humans and its coordinator; report with teamchat report --team alpha.",
		}},
		{"verified, none", true, proto.RoleWorker, "", []string{
			"team alpha: you are " + me + ", the worker; the team has no coordinator now.",
			"In team alpha take direction from the team's humans and its coordinator; report with teamchat report --team alpha.",
		}},
		{"not verified yet", false, "", "", []string{notKnown}},
		{"stale, a coordinator", false, proto.RoleWorker, coord, []string{notKnown}},
		{"stale, none", false, proto.RoleWorker, "", []string{notKnown}},
		{"stale coordinator", false, proto.RoleCoordinator, me, []string{notKnown}},
	} {
		got := Frame([]FrameTeam{{Team: alpha, Handle: me, Role: c.role, Coordinator: c.coordinator, Current: c.current}}, state.DeliverySocket)
		want := strings.Join(append(append([]string{proto.FrameText, ""}, c.want...), "Deliveries arrive in this conversation by themselves."), "\n")
		if got != want {
			t.Errorf("%s:\n%s\nwant:\n%s", c.name, got, want)
		}
	}
}

// The counted lines carry counts only.
func TestCounts(t *testing.T) {
	c := Counts{Total: 7, Humans: 1, Agents: 2, Pings: 3, Notices: 1, Timeouts: 0}
	for _, tc := range []struct{ got, want string }{
		{Pending(c), "teamchat: 7 pending (1 from humans, 2 from agents, 3 pings, 1 notices, 0 timeouts): run teamchat recv"},
		{Stalled(c), "teamchat: deliveries have failed for 2 minutes; 7 pending (1 from humans, 2 from agents, 3 pings, 1 notices, 0 timeouts): run teamchat recv"},
		{StopPending(c), "7 pending (1 from humans, 2 from agents, 3 pings, 1 notices, 0 timeouts). Run teamchat recv before stopping."},
		{Missing(3), "teamchat: a delivery did not reach the conversation; 3 items are pending again: run teamchat recv"},
		{Missing(1), "teamchat: a delivery did not reach the conversation; 1 item is pending again: run teamchat recv"},
		{StopNoWaker(), "Delivery here is by teamchat wait, and none is running: start teamchat wait in the background before stopping."},
	} {
		if tc.got != tc.want {
			t.Fatalf("%q, want %q", tc.got, tc.want)
		}
	}
}

// OF1: a stopped team's and a down team's lines stand beside a running team's pending count;
// each names its own team only.
func TestTeamLines(t *testing.T) {
	alpha, beta := team(t, "alpha"), team(t, "beta")
	for _, tc := range []struct{ got, want string }{
		{TeamFailed(alpha, state.FailedForbidden), "teamchat: team alpha stopped (forbidden); run teamchat status --team alpha and tell a human"},
		{TeamFailed(alpha, "made-up"), "teamchat: team alpha stopped (?); run teamchat status --team alpha and tell a human"},
		{TeamDown(beta), "teamchat: team beta cannot reach its server; its messages wait there"},
		{DesiredRefused("schema"), "teamchat: this session's membership file could not be read (schema); the last good membership stays; tell a human"},
		{DesiredRefused("Not A Code"), "teamchat: this session's membership file could not be read (?); the last good membership stays; tell a human"},
		{AddRefused(beta), "teamchat: team beta's seat is held by another session; tell a human"},
	} {
		if tc.got != tc.want {
			t.Fatalf("%q, want %q", tc.got, tc.want)
		}
	}
	both := strings.Join([]string{TeamFailed(alpha, state.FailedTrust), Pending(Counts{Total: 1, Agents: 1})}, "\n")
	if a := Context("UserPromptSubmit", both); !strings.Contains(string(a.Line), "team alpha stopped") || !strings.Contains(string(a.Line), "1 pending") {
		t.Fatalf("%s", a.Line)
	}
}

func TestMembershipAndRecovery(t *testing.T) {
	if got := Membership([]string{"MEMBERSHIP one", "bad\x1bline", "", "two"}); got != "MEMBERSHIP one\ntwo" {
		t.Fatalf("membership %q", got)
	}
	if got := Membership(nil); got != "" {
		t.Fatalf("no lines: %q", got)
	}
	msg := "teamchat is restarting (next try in 5 s); team messages wait on the server"
	if got := Recovery(msg); got != msg {
		t.Fatalf("recovery %q", got)
	}
	for _, bad := range []string{"", "two\nlines", "a\x00b", "\xff", strings.Repeat("a", maxRecovery+1)} {
		if got := Recovery(bad); got != string(tmplRecoveryUnknown) {
			t.Fatalf("%q: %q", bad, got)
		}
	}
}

// Every output is lines with no control character, and every teamchat command a text names
// is one the CLI parses (B 2.8: hook texts name commands in B's form).
func TestAllIsClean(t *testing.T) {
	commandRE := regexp.MustCompile(`teamchat (recv|wait|status|ask|questions|report)( --team [a-z0-9-]+)?`)
	for _, text := range All() {
		if text == "" {
			t.Fatal("an empty sample")
		}
		for _, r := range text {
			if r != '\n' && unicode.IsControl(r) {
				t.Fatalf("%q holds a control", text)
			}
		}
		for _, cmd := range commandRE.FindAllString(text, -1) {
			if _, err := cli.Parse(strings.Fields(cmd)[1:]); err != nil {
				t.Fatalf("%q in %q: %v", cmd, text, err)
			}
		}
	}
}

// All fills every template: a template const that no sample shows fails, and the count is
// pinned so a new sample is a decision.
func TestAllCoversEveryTemplate(t *testing.T) {
	all := strings.Join(All(), "\n")
	templates := templateTexts(t)
	for name, text := range templates {
		for part := range strings.SplitSeq(text, slot) {
			if !strings.Contains(all, part) {
				t.Fatalf("%s: no sample shows %q", name, part)
			}
		}
	}
	if len(templates) != 21 || len(All()) != 16 {
		t.Fatalf("%d templates, %d samples", len(templates), len(All()))
	}
}

// templateTexts is every tmpl const of hooktext.go by name, read from its source.
func templateTexts(t *testing.T) map[string]string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "hooktext.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	ast.Inspect(f, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok || !strings.HasPrefix(vs.Names[0].Name, "tmpl") {
			return true
		}
		lit, ok := vs.Values[0].(*ast.BasicLit)
		if !ok {
			t.Fatalf("%s is not a literal", vs.Names[0].Name)
		}
		text, err := strconv.Unquote(lit.Value)
		if err != nil {
			t.Fatal(err)
		}
		out[vs.Names[0].Name] = text
		return false
	})
	return out
}
