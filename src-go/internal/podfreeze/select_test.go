package podfreeze

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/freeze"
)

func callLines(f *execx.Fake) []string {
	calls := f.Calls()
	lines := make([]string, 0, len(calls))
	for _, c := range calls {
		lines = append(lines, c.Line())
	}
	return lines
}

// ---- the menu rows (TP freeze_hook_menu_rows; no bash cases, spec 4.5) ----

func TestGroupsAreTheMenuRowsInOrder(t *testing.T) {
	e, inv := fixture(t)
	groups := e.Groups(inv)
	got := make([]string, 0, len(groups))
	for _, g := range groups {
		got = append(got, g.Key+" | "+g.Description)
	}
	eqStrings(t, "rows", got, []string{
		"ccy | all CCY (Claude) containers",
		"all | all containers",
		"net:appnet | network: appnet",
		"net:podman | network: podman",
		"id:github:hubot | github: hubot",
		"id:github:octo | github: octo",
		"id:token:work | token: work",
		"id:ssh-key:key_alpha | ssh-key: key_alpha",
		"id:ssh-key:key_shared | ssh-key: key_shared",
	})
	byKey := map[string][]string{}
	for _, g := range groups {
		byKey[g.Key] = g.Members
	}
	eqStrings(t, "ccy members", byKey["ccy"], []string{"zulu_yolo", "alpha_yolo_2", "bravo_browser", "echo_yolo_3"})
	eqStrings(t, "all members", byKey["all"], inv.Names())
	eqStrings(t, "net members", byKey["net:appnet"], []string{"alpha_yolo_2", "kilo"})
	eqStrings(t, "id members", byKey["id:ssh-key:key_shared"], []string{"alpha_yolo_2", "bravo_browser"})
}

// An identity row is offered only when the axis tells two CCY sessions apart: with one token on
// every session, "token: work" and "all CCY containers" are the same button.
func TestGroupsOfferNoIdentityRowThatIsASecondNameForAllCCY(t *testing.T) {
	e, inv := load(t, uniformPS, uniformLabels, "")
	groups := e.Groups(inv)
	keys := make([]string, 0, len(groups))
	for _, g := range groups {
		keys = append(keys, g.Key)
	}
	eqStrings(t, "keys", keys, []string{"ccy", "all"})
}

func TestGroupsKeepAnEmptyGroupsRow(t *testing.T) {
	e, inv := load(t, "mike|running|none\n", "", "")
	groups := e.Groups(inv)
	if groups[0].Key != "ccy" || len(groups[0].Members) != 0 {
		t.Fatalf("an empty group still gets its row (\"nothing to do\"): %+v", groups)
	}
}

// ---- Select: the menu path ----

func TestSelectResolvesEveryGroupKind(t *testing.T) {
	e, inv := fixture(t)
	ctx := context.Background()
	for key, want := range map[string][]string{
		"ccy":                  {"zulu_yolo", "alpha_yolo_2", "bravo_browser", "echo_yolo_3"},
		"all":                  inv.Names(),
		"net:podman":           {"zulu_yolo", "alpha_yolo_2", "bravo_browser"},
		"id:github:octo":       {"alpha_yolo_2"},
		"id:token:work":        {"alpha_yolo_2", "bravo_browser"},
		"id:ssh-key:key_alpha": {"alpha_yolo_2"},
	} {
		got, err := e.Select(ctx, inv, key)
		if err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		eqStrings(t, key, got, want)
	}
}

func TestSelectSplitsOnlyTheAxisOffAnIdentityKey(t *testing.T) {
	// The value may itself contain a colon: only the axis is split off.
	e, inv := load(t, "a_yolo|running|none\n", "a_yolo|team:octo|none|none\n", "")
	got, err := e.Select(context.Background(), inv, "id:github:team:octo")
	if err != nil {
		t.Fatal(err)
	}
	eqStrings(t, "members", got, []string{"a_yolo"})
}

func TestSelectRefusesAnUnknownKey(t *testing.T) {
	e, inv := fixture(t)
	for _, key := range []string{"bogus", "id:github", "id:", "net", ""} {
		_, err := e.Select(context.Background(), inv, key)
		if got := dieText(t, err); got != "internal error: unknown target key '"+key+"'." {
			t.Errorf("key %q: %q", key, got)
		}
	}
}

// Bug 5: a vanished identity value used to exit the whole session from the menu while a vanished
// network re-prompted. Both re-prompt now, and say why first.
func TestSelectAVanishedIdentityValueIsRecoverableFromTheMenu(t *testing.T) {
	e, inv := fixture(t)
	got, err := e.Select(context.Background(), inv, "id:github:ghost")
	if !errors.Is(err, freeze.ErrGroupGone) || got != nil {
		t.Fatalf("got %v, %v", got, err)
	}
	want := "podfreeze: no running or frozen CCY session has --github 'ghost'\n  Known values:\n    hubot\n    octo\n"
	if said(t, e) != want {
		t.Fatalf("got %q", said(t, e))
	}
}

func TestSelectAVanishedNetworkIsRecoverableFromTheMenu(t *testing.T) {
	e, inv := fixture(t)
	_, err := e.Select(context.Background(), inv, "net:nosuchnet")
	if !errors.Is(err, freeze.ErrGroupGone) {
		t.Fatalf("got %v", err)
	}
	want := "podfreeze: no such network: 'nosuchnet'\n  Known networks:\n    podman\n    appnet\n    bridge\n"
	if said(t, e) != want {
		t.Fatalf("got %q", said(t, e))
	}
}

func TestSelectFailuresThatAreNotAVanishedGroupStayFatal(t *testing.T) {
	f := scripted(fixturePS, fixtureLabels, "")
	f.On(networkPrefix, execx.Result{Stderr: []byte("connection refused\n"), ExitCode: 125})
	e := newEngine(f)
	inv, err := e.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.Select(context.Background(), inv, "net:podman")
	if errors.Is(err, freeze.ErrGroupGone) {
		t.Fatal("a failed network ls is not a vanished network")
	}
	if got := dieText(t, err); !strings.HasPrefix(got, "podman network ls failed: ") || !strings.Contains(got, "connection refused") {
		t.Fatalf("got %q", got)
	}
}

// ---- select_network (TP 13), one-shot path ----

func TestSelectNetworkAKnownNetworkIsReadFromTheInventory(t *testing.T) {
	f := scripted(fixturePS, fixtureLabels, fixtureNetworks)
	e := newEngine(f)
	inv, err := e.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got, err := e.SelectTarget(context.Background(), inv, "--network", "appnet", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Bug 13: the members are the inventory's, in inventory order; the fourth podman query
	// (ps --filter network=) is gone, so a menu count and an action cannot disagree.
	eqStrings(t, "members", got, []string{"alpha_yolo_2", "kilo"})
	for _, line := range callLines(f) {
		if strings.Contains(line, "--filter network=") {
			t.Fatalf("no per-network ps query any more: %s", line)
		}
	}
	if !f.Called("podman network ls --format {{.Name}}") {
		t.Fatal("the existence check stays")
	}
}

func TestSelectNetworkAnUnknownNetworkIsRefusedWithTheKnownOnes(t *testing.T) {
	e, inv := fixture(t)
	got, err := e.SelectTarget(context.Background(), inv, "--network", "nosuchnet", nil)
	if got != nil {
		t.Fatalf("nothing selected, got %v", got)
	}
	msg := dieText(t, err)
	for _, want := range []string{"no such network: 'nosuchnet'", "Known networks:", "    appnet"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("missing %q in %q", want, msg)
		}
	}
	if said(t, e) != "" {
		t.Fatal("the one-shot path reports through the returned error, once")
	}
}

// A network NAME is a literal. As a regular expression `podma.` would pass the check by
// matching `podman`, and a typo containing a metacharacter would be reported as "Nothing in that
// group" instead of as the unknown network it is.
func TestSelectNetworkTheExistenceCheckIsALiteralMatch(t *testing.T) {
	e, inv := fixture(t)
	if _, err := e.SelectTarget(context.Background(), inv, "--network", "podma.", nil); err == nil {
		t.Fatal("a regex metacharacter must not pass the existence check")
	}
	if _, err := e.SelectTarget(context.Background(), inv, "--network", "podman", nil); err != nil {
		t.Fatalf("the literal network name is still accepted: %v", err)
	}
}

func TestSelectNetworkWithNoMembersIsAnEmptySelectionNotAnError(t *testing.T) {
	e, inv := fixture(t)
	got, err := e.SelectTarget(context.Background(), inv, "--network", "bridge", nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("a real network nothing is on: %v, %v", got, err)
	}
}

func TestSelectNetworkAFailedLsIsFatalAndReportsWhatPodmanSaid(t *testing.T) {
	f := scripted(fixturePS, fixtureLabels, "")
	f.On(networkPrefix, execx.Result{Stderr: []byte("podman network ls: connection refused\n"), ExitCode: 1})
	e := newEngine(f)
	inv, err := e.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.SelectTarget(context.Background(), inv, "--network", "appnet", nil)
	if got := dieText(t, err); !strings.Contains(got, "connection refused") || !strings.HasPrefix(got, "podman network ls failed: ") {
		t.Fatalf("got %q", got)
	}
}

// ---- select_identity (TP 10), one-shot path ----

func TestSelectIdentityAKnownValueSelectsItsGroupAndDisclosesTheBlindSpot(t *testing.T) {
	e, inv := fixture(t)
	got, err := e.SelectTarget(context.Background(), inv, "--ssh-key", "key_shared", nil)
	if err != nil {
		t.Fatal(err)
	}
	eqStrings(t, "group", got, []string{"alpha_yolo_2", "bravo_browser"})
	if !strings.Contains(said(t, e), "zulu_yolo") {
		t.Fatalf("it discloses the sessions it could not consider: %q", said(t, e))
	}
	e, inv = fixture(t)
	got, err = e.SelectTarget(context.Background(), inv, "--github", "octo", nil)
	if err != nil {
		t.Fatal(err)
	}
	eqStrings(t, "a single-member group", got, []string{"alpha_yolo_2"})
}

func TestSelectIdentityAnUnknownValueIsAnErrorNeverAnEmptySet(t *testing.T) {
	e, inv := fixture(t)
	_, err := e.SelectTarget(context.Background(), inv, "--github", "nosuch", nil)
	want := "no running or frozen CCY session has --github 'nosuch'\n  Known values:\n    hubot\n    octo"
	unknown := dieText(t, err)
	if unknown != want {
		t.Fatalf("got %q", unknown)
	}
	// An axis NOTHING carries gets a different message: there is no list to offer, and the
	// reason is almost always that every session predates CCY 3.40.0.
	e, bare := load(t, "a_yolo|running|none\n", "", "")
	_, err = e.SelectTarget(context.Background(), bare, "--github", "octo", nil)
	absent := dieText(t, err)
	wantAbsent := "no running or frozen CCY session has --github 'octo'\n" +
		"  No session carries a github label at all. Sessions started by\n" +
		"  a CCY older than 3.40.0 are unlabelled — relaunch to label them."
	if absent != wantAbsent {
		t.Fatalf("got %q", absent)
	}
	if unknown == absent {
		t.Fatal("the two identity refusals must differ")
	}
	// The whole-value axes are exact: a prefix of a real value is refused rather than widened.
	e, inv = fixture(t)
	if _, err := e.SelectTarget(context.Background(), inv, "--github", "oct", nil); err == nil {
		t.Fatal("a prefix of a known value is refused")
	}
}

// ---- SelectTarget: the one-shot modes ----

func TestSelectTargetByMode(t *testing.T) {
	e, inv := fixture(t)
	ctx := context.Background()
	got, err := e.SelectTarget(ctx, inv, "--all", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	eqStrings(t, "--all", got, inv.Names())
	got, err = e.SelectTarget(ctx, inv, "--ccy", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	eqStrings(t, "--ccy", got, []string{"zulu_yolo", "alpha_yolo_2", "bravo_browser", "echo_yolo_3"})
	got, err = e.SelectTarget(ctx, inv, "--token", "work", nil)
	if err != nil {
		t.Fatal(err)
	}
	eqStrings(t, "--token", got, []string{"alpha_yolo_2", "bravo_browser"})
	_, err = e.SelectTarget(ctx, inv, "--bogus", "", nil)
	if got := dieText(t, err); got != "internal error: unknown target mode '--bogus'." {
		t.Fatalf("got %q", got)
	}
}

func TestSelectTargetNoCCYSessionsGivesAnEmptySelection(t *testing.T) {
	// An inventory with no CCY session selects an empty set, which the caller reports as
	// "nothing matched" rather than failing on an unset slice.
	e, inv := load(t, "mike|running|none\n", "", "")
	got, err := e.SelectTarget(context.Background(), inv, "--ccy", "", nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestSelectNamesKeepTheOrderTheUserGaveAndAnUnknownNameIsFatal(t *testing.T) {
	e, inv := fixture(t)
	ctx := context.Background()
	// bravo_browser is index 4 and alpha_yolo_2 index 2: a selector that walked the inventory
	// would silently swap them, and the drill-down numbers these rows.
	got, err := e.SelectTarget(ctx, inv, "names", "", []string{"bravo_browser", "alpha_yolo_2"})
	if err != nil {
		t.Fatal(err)
	}
	eqStrings(t, "order", got, []string{"bravo_browser", "alpha_yolo_2"})
	got, err = e.SelectTarget(ctx, inv, "names", "", []string{"mike"})
	if err != nil {
		t.Fatal(err)
	}
	eqStrings(t, "a single name", got, []string{"mike"})
	// bug 4: bash kept a repeated name twice, so `freeze mike mike` offered "FREEZE 2" for one
	// container and the second pause failed. Once now.
	got, err = e.SelectTarget(ctx, inv, "names", "", []string{"mike", "mike"})
	if err != nil {
		t.Fatal(err)
	}
	eqStrings(t, "a repeated name is kept once", got, []string{"mike"})

	_, err = e.SelectTarget(ctx, inv, "names", "", []string{"nosuch"})
	msg := dieText(t, err)
	if !strings.Contains(msg, "nosuch") || !strings.Contains(msg, "podfreeze list") {
		t.Fatalf("it names the container and points at the command that lists them: %q", msg)
	}
	if _, err := e.SelectTarget(ctx, inv, "names", "", []string{"mike", "nosuch"}); err == nil {
		t.Fatal("one unknown name among known ones is still fatal")
	}
}

// ---- a failed write is surfaced, not lost ----

type brokenPipe struct{}

func (brokenPipe) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

// failAfter writes n times, then fails.
type failAfter struct{ n int }

func (f *failAfter) Write(p []byte) (int, error) {
	if f.n <= 0 {
		return 0, errors.New("disk full")
	}
	f.n--
	return len(p), nil
}

func TestNewAGoneErrorCarriesItsMessage(t *testing.T) {
	if got := (&goneError{msg: "no such network"}).Error(); got != "no such network" {
		t.Fatalf("got %q", got)
	}
}

func TestNewNotesThatCannotBeWrittenAreErrorsNotSilence(t *testing.T) {
	e, inv := fixture(t)
	e.Err = brokenPipe{}
	if err := e.warnBlindSpot(inv); err == nil {
		t.Fatal("the blind-spot note failed to write and nobody was told")
	}
	if _, err := e.Select(context.Background(), inv, "id:github:ghost"); err == nil || errors.Is(err, freeze.ErrGroupGone) {
		t.Fatalf("a vanished group that could not be explained is a failure: %v", err)
	}
	// Every line of the note is a write that can fail.
	for _, n := range []int{1, 2} {
		e.Err = &failAfter{n: n}
		if err := e.warnBlindSpot(inv); err == nil {
			t.Fatalf("a write failing after %d lines was lost", n)
		}
	}
}
