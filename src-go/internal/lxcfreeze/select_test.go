package lxcfreeze

// Groups, selection and the table (spec 5.3 to 5.5): the bridge axis, the menu rows with what
// choosing each does, the select hook, and the BRIDGE and IPV4 columns.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/freeze"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/freeze/cli"
)

func bridgeEntry(name, bridge string) freeze.Entry[record] {
	return freeze.Entry[record]{Name: name, State: "RUNNING", Record: record{bridge: bridge}}
}

func inventoryOf(entries ...freeze.Entry[record]) freeze.Inventory[record] {
	return freeze.NewInventory(Config(), entries)
}

// bare is an engine that has loaded nothing, for the tests that hand it an inventory.
func bare() *Engine { return newEngine(&execx.Fake{}) }

// ---- bridge_names: the bridges in use, and only those ----

func TestBridgeNamesAreEachBridgeOnceSorted(t *testing.T) {
	_, inv := fixture(t)
	// lxcbr0 is held by two containers and virbr1 by two more, interleaved in the inventory.
	eqStrings(t, "bridges", bridgeNames(inv), []string{"lxcbr0", "virbr1"})
}

// Neither non-bridge label is offered as a group: "no network" is not a bridge, and "could not
// read" is not a set anyone can ask to freeze.
func TestBridgeNamesExcludeTheTwoNonBridgeLabels(t *testing.T) {
	_, inv := fixture(t)
	entries := inv.Entries()
	entries[4].Record = record{bridge: bridgeUnreadable} // bravo, virbr1's other member, is now unreadable
	entries[1].Record = record{bridge: bridgeNone}       // and so is mike, so virbr1 is no longer in use
	eqStrings(t, "bridges", bridgeNames(inventoryOf(entries...)), []string{"lxcbr0"})
	got := bridgeNames(inventoryOf(bridgeEntry("a", bridgeNone), bridgeEntry("b", bridgeNone), bridgeEntry("c", bridgeUnreadable)))
	if len(got) != 0 {
		t.Fatalf("a machine with no bridges at all offers none: %v", got)
	}
}

// Bug 9: `sort -u` ordered by locale; Go orders bytewise, so menu rows stop depending on
// LC_COLLATE.
func TestBridgeNamesSortBytewise(t *testing.T) {
	got := bridgeNames(inventoryOf(bridgeEntry("a", "br2"), bridgeEntry("b", "Zbr"), bridgeEntry("c", "abr"), bridgeEntry("d", "br10")))
	eqStrings(t, "bridges", got, []string{"Zbr", "abr", "br10", "br2"})
}

// ---- the menu rows ----

func TestGroupsAreEverythingThenOneRowPerBridgeInUseAndNoPerContainerRows(t *testing.T) {
	e, inv := fixture(t)
	groups := e.Groups(inv)
	keys := make([]string, 0, len(groups))
	for _, g := range groups {
		keys = append(keys, g.Key)
	}
	eqStrings(t, "keys", keys, []string{"all", "bridge:lxcbr0", "bridge:virbr1"})
	if groups[0].Description != "all LXC containers" || groups[1].Description != "bridge: lxcbr0" {
		t.Fatalf("got %+v", groups)
	}
	eqStrings(t, "all", groups[0].Members, []string{"zulu", "mike", "alpha", "kilo", "bravo"})
	eqStrings(t, "lxcbr0", groups[1].Members, []string{"zulu", "alpha"})
	eqStrings(t, "virbr1", groups[2].Members, []string{"mike", "bravo"})
}

// What choosing a row does is the library's TargetEffect over the group's own members: lxcbr0
// holds one running container and virbr1 holds one; a row that counted the inventory would
// promise to act on containers outside the group it names.
func TestGroupLabelsSayWhatChoosingEachRowDoes(t *testing.T) {
	e, inv := fixture(t)
	groups := e.Groups(inv)
	if got := inv.TargetEffect(freeze.VerbDerived, groups[0].Members...); !strings.Contains(got, "FREEZE 2") {
		t.Errorf("the first row says what acting on everything would do: %q", got)
	}
	if got := inv.TargetEffect(freeze.VerbDerived, groups[1].Members...); !strings.Contains(got, "FREEZE 1") {
		t.Errorf("a bridge row counts only its own members: %q", got)
	}
}

// A bridge holding nothing that can be frozen still gets a row, saying what it CAN do.
func TestAWhollyFrozenBridgeOffersToThawIt(t *testing.T) {
	frozen := inventoryOf(
		freeze.Entry[record]{Name: "zulu", State: "FROZEN", Record: record{bridge: "lxcbr0"}},
		freeze.Entry[record]{Name: "mike", State: "FROZEN", Record: record{bridge: "virbr1"}},
	)
	groups := bare().Groups(frozen)
	if got := frozen.TargetEffect(freeze.VerbDerived, groups[1].Members...); !strings.Contains(got, "THAW") {
		t.Fatalf("got %q", got)
	}
}

// ---- select_bridge and the select hook ----

func TestSelectBridgeReturnsItsMembersInInventoryOrder(t *testing.T) {
	e, inv := fixture(t)
	// zulu is index 0 and alpha index 2 with mike between them: not adjacent, so a loop that
	// stopped at the first match returns half the group.
	got, err := e.Select(context.Background(), inv, "bridge:lxcbr0")
	if err != nil {
		t.Fatal(err)
	}
	eqStrings(t, "lxcbr0", got, []string{"zulu", "alpha"})
	got, err = e.Select(context.Background(), inv, "bridge:virbr1")
	if err != nil {
		t.Fatal(err)
	}
	eqStrings(t, "virbr1", got, []string{"mike", "bravo"})
}

func TestSelectReturnsAFreshSelectionEachTime(t *testing.T) {
	e, inv := fixture(t)
	first, err := e.Select(context.Background(), inv, "bridge:lxcbr0")
	if err != nil {
		t.Fatal(err)
	}
	first[0] = "stale_name"
	again, err := e.Select(context.Background(), inv, "bridge:lxcbr0")
	if err != nil {
		t.Fatal(err)
	}
	eqStrings(t, "a second selection replaces the first", again, []string{"zulu", "alpha"})
}

func TestSelectAllKeySelectsEverything(t *testing.T) {
	e, inv := fixture(t)
	got, err := e.Select(context.Background(), inv, "all")
	if err != nil {
		t.Fatal(err)
	}
	eqStrings(t, "all", got, []string{"zulu", "mike", "alpha", "kilo", "bravo"})
}

// A bridge that emptied between the menu being drawn and the row being chosen is RECOVERABLE:
// the engine says so and returns ErrGroupGone, and the library's loop re-prompts.
func TestSelectAnUnknownBridgeIsSaidAndRecoverable(t *testing.T) {
	e, inv := fixture(t)
	got, err := e.Select(context.Background(), inv, "bridge:nosuchbr")
	if !errors.Is(err, freeze.ErrGroupGone) || got != nil {
		t.Fatalf("an unknown bridge returns rather than selecting: %v, %v", got, err)
	}
	want := "lxcfreeze: no running or frozen container is on bridge 'nosuchbr'.\n  In use right now: lxcbr0 virbr1\n"
	if said(t, e) != want {
		t.Fatalf("got %q", said(t, e))
	}
}

// A machine where NOTHING is on a bridge gets a different message: there is no list to offer.
func TestSelectWhenNothingIsOnAnyBridgeSaysThatInstead(t *testing.T) {
	e := bare()
	none := inventoryOf(bridgeEntry("a", bridgeNone), bridgeEntry("b", bridgeUnreadable))
	if _, err := e.Select(context.Background(), none, "bridge:lxcbr0"); !errors.Is(err, freeze.ErrGroupGone) {
		t.Fatalf("got %v", err)
	}
	nothing := said(t, e)
	mustContain(t, "saying nothing is on any bridge", nothing, "any bridge")
	_, inv := fixture(t)
	if _, err := e.Select(context.Background(), inv, "bridge:nosuchbr"); !errors.Is(err, freeze.ErrGroupGone) {
		t.Fatal(err)
	}
	if rest := strings.TrimPrefix(said(t, e), nothing); rest == nothing || !strings.Contains(rest, "In use right now") {
		t.Fatalf("the two bridge refusals are distinct: %q then %q", nothing, rest)
	}
}

// A key the engine does not recognise is an internal error, not a re-prompt: the library built
// it from a row this engine supplied, so a mismatch is a bug rather than a stale menu.
func TestSelectAnUnrecognisedKeyIsFatalAndNamesIt(t *testing.T) {
	e, inv := fixture(t)
	_, err := e.Select(context.Background(), inv, "nonsense:key")
	if errors.Is(err, freeze.ErrGroupGone) {
		t.Fatal("an unrecognised key is not recoverable")
	}
	mustContain(t, "names the key", dieText(t, err), "nonsense:key")
}

// ---- SelectTarget: the command line's targets ----

func TestSelectTargetResolvesEachKindOfTarget(t *testing.T) {
	e, inv := fixture(t)
	ctx := context.Background()
	cases := []struct {
		mode, value string
		names       []string
		want        []string
	}{
		{"--all", "", nil, []string{"zulu", "mike", "alpha", "kilo", "bravo"}},
		{"--bridge", "virbr1", nil, []string{"mike", "bravo"}},
		{cli.ModeNames, "", []string{"bravo", "zulu"}, []string{"bravo", "zulu"}},
		// Bug 4: a name given twice is one container.
		{cli.ModeNames, "", []string{"bravo", "bravo"}, []string{"bravo"}},
	}
	for _, c := range cases {
		got, err := e.SelectTarget(ctx, inv, c.mode, c.value, c.names)
		if err != nil {
			t.Fatalf("%s: %v", c.mode, err)
		}
		eqStrings(t, c.mode, got, c.want)
	}
}

// From the command line a vanished bridge ends the run with the same words the menu says.
func TestSelectTargetAVanishedBridgeIsFatalWithTheMenusWords(t *testing.T) {
	e, inv := fixture(t)
	_, err := e.SelectTarget(context.Background(), inv, "--bridge", "nosuchbr", nil)
	want := "no running or frozen container is on bridge 'nosuchbr'.\n  In use right now: lxcbr0 virbr1"
	if msg := dieText(t, err); msg != want {
		t.Fatalf("got %q", msg)
	}
	if said(t, e) != "" {
		t.Fatalf("the die carries the words once: %q", said(t, e))
	}
}

func TestSelectTargetAnUnknownNameNamesItAndTheListNote(t *testing.T) {
	e, inv := fixture(t)
	_, err := e.SelectTarget(context.Background(), inv, cli.ModeNames, "", []string{"alpha", "nosuch"})
	msg := dieText(t, err)
	mustContain(t, "names it", msg, "nosuch")
	mustContain(t, "points at list", msg, "lxcfreeze list")
}

func TestSelectTargetAnUnknownModeIsAnInternalError(t *testing.T) {
	e, inv := fixture(t)
	_, err := e.SelectTarget(context.Background(), inv, "--ccy", "", nil)
	mustContain(t, "internal error", dieText(t, err), "internal error: unknown target mode '--ccy'.")
}

// ---- the table: the BRIDGE and IPV4 columns are this engine's ----

// col is the nth fixed-width column of a row, trimmed. The width is the engine's own constant:
// a second copy of the number in the test is the "two things that must agree" shape.
func col(row string, n int) string {
	runes := []rune(row)
	return strings.TrimRight(string(runes[(n-1)*tableColWidth:n*tableColWidth]), " ")
}

func TestColumnsHeaderNamesBothColumnsAndIsPadded(t *testing.T) {
	want := fmt.Sprintf("%-*s%-*s", tableColWidth, "BRIDGE", tableColWidth, "IPV4")
	if got := bare().Columns(); got != want {
		t.Fatalf("got %q", got)
	}
}

// The column is PADDED, not bare: the drill-down prints the verb after it, so an unpadded value
// makes the verb column ragged. Each column is cut by position so asserting one cannot pass by
// reading the last non-blank cell.
func TestRowCarriesTheBridgeAndTheAddressEachInItsOwnColumn(t *testing.T) {
	e, inv := fixture(t)
	row := func(name string) string { return e.Row(entryOf(t, inv, name)) }
	cases := []struct{ name, bridge, addr string }{
		{"zulu", "lxcbr0", "192.0.2.11"},
		{"alpha", "lxcbr0", "192.0.2.12"},
		// The no-network label is shown VERBATIM, not blanked: a blank cell reads as "unknown".
		{"kilo", bridgeNone, ""},
		// BLANK, not a placeholder: no address prints nothing, and the row keeps its bridge, so
		// the blank is the ADDRESS being absent rather than the row being short.
		{"mike", "virbr1", ""},
	}
	for _, c := range cases {
		if col(row(c.name), 1) != c.bridge || col(row(c.name), 2) != c.addr {
			t.Errorf("%s: %q | %q, want %q | %q", c.name, col(row(c.name), 1), col(row(c.name), 2), c.bridge, c.addr)
		}
	}
}

func TestRowAndHeaderAreTheSameWidth(t *testing.T) {
	e, inv := fixture(t)
	width := len([]rune(e.Columns()))
	for _, name := range inv.Names() {
		if got := len([]rune(e.Row(entryOf(t, inv, name)))); got != width {
			t.Errorf("%s: row %d, header %d", name, got, width)
		}
	}
}

// Narrowing the column to the longest label would still pass a whole-row measurement when a
// second column pads the total; the cut is taken before trimming so the field is measured.
func TestTheBridgeColumnIsWiderThanTheLongestLabelItCanHold(t *testing.T) {
	row := bare().Row(bridgeEntry("x", bridgeUnreadable))
	field := string([]rune(row)[:tableColWidth])
	if len([]rune(field)) <= len([]rune(bridgeUnreadable)) {
		t.Fatalf("field %d <= label %d, so they would touch", len([]rune(field)), len([]rune(bridgeUnreadable)))
	}
	if col(row, 1) != bridgeUnreadable {
		t.Fatalf("the longest label survives intact: %q", col(row, 1))
	}
}

func TestAnUnreadableAddressReachesTheRow(t *testing.T) {
	row := bare().Row(freeze.Entry[record]{Name: "x", State: "RUNNING", Record: record{bridge: "lxcbr0", ipv4: ipv4Unreadable}})
	if col(row, 2) != ipv4Unreadable {
		t.Fatalf("got %q", col(row, 2))
	}
}

// A row for an entry this engine never loaded is not a crash.
func TestRowOfAnEntryWithNoRecordIsBlankNotAPanic(t *testing.T) {
	if got := bare().Row(freeze.Entry[record]{Name: "x"}); strings.TrimSpace(got) != "" {
		t.Fatalf("got %q", got)
	}
}

// print_table is the library's, and this is the assembled result: the shared columns plus this
// engine's, which is the seam most likely to be wired up wrong.
func TestTheAssembledTableHasTheSharedColumnsAndTheEngines(t *testing.T) {
	e := newEngine(scripted(fixtureBoxes()))
	var out bytes.Buffer
	s := freeze.NewSession(Config(), freeze.Env{Out: &out, Err: &out}, e, nil)
	if err := s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	table := s.Table("alpha", "kilo")
	for what, needle := range map[string]string{
		"the shared columns": "NAME", "this engine's column": "BRIDGE", "a row names its container": "alpha",
		"with its state": "RUNNING", "and its bridge": "lxcbr0", "and its address": "192.0.2.12",
	} {
		mustContain(t, what, table, needle)
	}
}
