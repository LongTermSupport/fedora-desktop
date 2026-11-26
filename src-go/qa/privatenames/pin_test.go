package privatenames

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// Every name below is invented: this file is committed to a public repository.

// pinInventory is what play-git-hooks-security.yml hands `pin` on stdin: the machine's own team
// declarations, the teams its bus host serves, and the owner's vaulted names, decrypted.
const pinInventory = `{
  "teamchat_teams": [
    {"team": "quillworks", "name": "Quill Works Guild", "port": 8448,
     "coordinator": "inkwell-press.1+local.host",
     "duties": {"marblehead-yard.2+local.podman": "host"},
     "humans": ["someone"], "description": "a team",
     "trust": {"org": "copperfield-labs", "exclude": ["ledgerline"],
               "repos": [{"repo": "LongTermSupport/fedora-desktop"}, {"repo": "brightwater-co/tidepool-app"}]}},
    {"team": "dev", "state": "absent"}
  ],
  "teamchat_lxc_members": [{"team": "quillworks", "container": "otterbank-box", "user": "agent", "seat": 1, "checkout": "/home/agent/x"}],
  "agent_bus_teams": [{"team": "harborlight", "port": 8008}],
  "agent_bus_lxc_members": [{"team": "harborlight", "container": "fernhollow-ct", "user": "lanternfish", "repo": "boathouse", "host": "seaglass"}],
  "teamchat_site": "driftwood-site",
  "agent_bus_element_teams": [{"team": "quillworks", "base_url": "http://192.0.2.1:8448"}, {"team": "saltmarsh"}],
  "served_teams": ["quillworks", "brambleton", "lost+found", "publicname"],
  "private_name_terms": ["Kestrel Analytics", "kiwi", "publicname"]
}`

// pinnedNames is the exact union pinInventory makes: a team id, display name, handle (in each
// form the guard learns a handle: whole, without its site, its project), container and served
// team, each specific and not public; and every owner item as written, whatever its length.
var pinnedNames = []string{
	"boathouse", "brambleton", "brightwater-co", "copperfield-labs", "driftwood-site",
	"fernhollow-ct", "harborlight", "inkwell-press", "inkwell-press.1",
	"inkwell-press.1+local.host", "Kestrel Analytics", "kiwi", "lanternfish", "ledgerline",
	"marblehead-yard", "marblehead-yard.2", "marblehead-yard.2+local.podman", "otterbank-box",
	"publicname", "Quill Works Guild", "quillworks", "saltmarsh", "seaglass", "tidepool-app",
}

// allPinNames are every invented name above, none of which may ever be printed.
var allPinNames = []string{
	"quillworks", "quill works", "inkwell", "marblehead", "otterbank", "harborlight",
	"fernhollow", "saltmarsh", "brambleton", "kestrel", "kiwi", "publicname", "copperfield",
	"ledgerline", "brightwater", "tidepool", "lanternfish", "boathouse", "seaglass", "driftwood",
}

const allowlistFile = "/repo/" + AllowlistPath

// pinWorld is one pin run's world: the rig and the paths ReplaceFile was called on.
type pinWorld struct {
	rig
	replaced *[]string
}

func pinRig(stdin string, fl files) pinWorld {
	w := pinWorld{rig: newRig(false, nil, fl, stdin), replaced: new([]string)}
	w.env.ReplaceFile = func(path string, data []byte) error {
		*w.replaced = append(*w.replaced, path)
		return fl.write(path, data)
	}
	return w
}

func pinnedBody(t *testing.T, text string) []string {
	t.Helper()
	if !strings.HasPrefix(text, PinnedHeader) {
		t.Fatalf("the pinned list does not start with the generated header: %q", text)
	}
	var out []string
	for line := range strings.SplitSeq(strings.TrimPrefix(text, PinnedHeader), "\n") {
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func assertNoNamePrinted(t *testing.T, r rig) {
	t.Helper()
	lower := strings.ToLower(r.text())
	for _, n := range allPinNames {
		if strings.Contains(lower, n) {
			t.Fatalf("a name was printed: %s", r.text())
		}
	}
}

func TestPinWritesTheExactUnionSortedWithTheHeader(t *testing.T) {
	fl := files{allowlistFile: "public_tokens:\n  - publicname\n  - LongTermSupport\n"}
	w := pinRig(pinInventory, fl)
	if code := w.run("pin"); code != ExitOK {
		t.Fatalf("exit %d: %s", code, w.text())
	}
	if len(*w.replaced) != 1 || (*w.replaced)[0] != pinnedFile {
		t.Fatalf("the pinned list was not replaced whole at %s: %v", pinnedFile, *w.replaced)
	}
	got := pinnedBody(t, fl[pinnedFile])
	if strings.Join(got, "\n") != strings.Join(pinnedNames, "\n") {
		t.Fatalf("pinned names:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(pinnedNames, "\n"))
	}
	if !strings.Contains(w.out.String(), "24 names") {
		t.Fatalf("the count is not reported: %s", w.text())
	}
	assertNoNamePrinted(t, w.rig)
}

// The guard reads the file pin writes: every pinned name is read back as a pin.
func TestThePinnedListPinWritesIsTheOneTheGuardReads(t *testing.T) {
	fl := files{allowlistFile: "public_tokens:\n  - publicname\n  - LongTermSupport\n"}
	w := pinRig(pinInventory, fl)
	if code := w.run("pin"); code != ExitOK {
		t.Fatalf("exit %d: %s", code, w.text())
	}
	pinned, err := app{env: w.env}.pinned(pinnedFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(pinned) != len(pinnedNames) {
		t.Fatalf("the guard read %d pinned names, want %d", len(pinned), len(pinnedNames))
	}
}

// A team removed from the inventory is gone from the list at the next run: the list is the
// inventory's, rewritten whole, never added to.
func TestATeamRemovedFromTheInventoryDropsOut(t *testing.T) {
	fl := files{pinnedFile: PinnedHeader + "oldteam-gone\nquillworks\n"}
	w := pinRig(`{"teamchat_teams": [{"team": "quillworks"}]}`, fl)
	if code := w.run("pin"); code != ExitOK {
		t.Fatalf("exit %d: %s", code, w.text())
	}
	if got := pinnedBody(t, fl[pinnedFile]); strings.Join(got, ",") != "quillworks" {
		t.Fatalf("pinned names: %v", got)
	}
}

func TestAnUnchangedListIsNotRewritten(t *testing.T) {
	fl := files{pinnedFile: PinnedHeader + "quillworks\n"}
	w := pinRig(`{"teamchat_teams": [{"team": "quillworks"}]}`, fl)
	if code := w.run("pin"); code != ExitOK {
		t.Fatalf("exit %d: %s", code, w.text())
	}
	if len(*w.replaced) != 0 || !strings.Contains(w.out.String(), "unchanged") {
		t.Fatalf("an unchanged list was rewritten (%v): %s", *w.replaced, w.text())
	}
}

// A machine with teams whose every name is too short or generic to block would get a list that
// guards none of them: refused, never written.
func TestAnEmptyResultOnAMachineWithTeamsIsRefused(t *testing.T) {
	for name, in := range map[string]string{
		"declared": `{"teamchat_teams": [{"team": "dev"}], "agent_bus_element_teams": [{"team": "infra"}]}`,
		"served":   `{"served_teams": ["test"]}`,
	} {
		w := pinRig(in, files{})
		if code := w.run("pin"); code != ExitCannotRun || !strings.Contains(w.text(), "private_name_terms") {
			t.Errorf("%s: exit %d: %s", name, code, w.text())
		}
		if len(*w.replaced) != 0 {
			t.Errorf("%s: an empty list was written", name)
		}
	}
}

func TestAMachineWithNoTeamsAndNoTermsGetsAnEmptyList(t *testing.T) {
	fl := files{}
	w := pinRig(`{}`, fl)
	if code := w.run("pin"); code != ExitOK {
		t.Fatalf("exit %d: %s", code, w.text())
	}
	if fl[pinnedFile] != PinnedHeader {
		t.Fatalf("pinned list: %q", fl[pinnedFile])
	}
}

// A list written by hand (no generated header) holds names the owner chose; replacing it would
// lose them. It is refused, naming the route for them, never its content.
func TestAHandWrittenPinnedListIsRefusedNotOverwritten(t *testing.T) {
	hand := "# my neighbours\notterbank\n"
	fl := files{pinnedFile: hand}
	w := pinRig(`{"teamchat_teams": [{"team": "quillworks"}]}`, fl)
	if code := w.run("pin"); code != ExitCannotRun {
		t.Fatalf("exit %d: %s", code, w.text())
	}
	if len(*w.replaced) != 0 || fl[pinnedFile] != hand {
		t.Fatal("the hand-written list was replaced")
	}
	for _, want := range []string{PinnedName, "private_name_terms"} {
		if !strings.Contains(w.text(), want) {
			t.Errorf("the refusal does not name %s: %s", want, w.text())
		}
	}
	assertNoNamePrinted(t, w.rig)
}

func TestAnUnreadableExistingPinnedListIsRefused(t *testing.T) {
	w := pinRig(`{}`, files{pinnedFile: "UNREADABLE"})
	if code := w.run("pin"); code != ExitCannotRun || len(*w.replaced) != 0 {
		t.Fatalf("exit %d: %s", code, w.text())
	}
}

// An owner item the guard would misread (a # line is a comment, a newline two names, a blank
// nothing) is refused by its position in the list, never by its value.
func TestABadPrivateNameTermsItemIsRefusedByPositionOnly(t *testing.T) {
	for _, item := range []string{"", "   ", "#kiwi", "kiwi\nsaltmarsh"} {
		raw, err := json.Marshal(map[string][]string{"private_name_terms": {"brambleton", item}})
		if err != nil {
			t.Fatal(err)
		}
		w := pinRig(string(raw), files{})
		if code := w.run("pin"); code != ExitCannotRun || !strings.Contains(w.text(), "private_name_terms item 2") {
			t.Errorf("%q: exit %d: %s", item, code, w.text())
		}
		if len(*w.replaced) != 0 {
			t.Errorf("%q: the list was written", item)
		}
		assertNoNamePrinted(t, w.rig)
	}
}

func TestPinRefusesInputItCannotReadWithoutEchoingIt(t *testing.T) {
	for name, in := range map[string]string{
		"not json":      `{"teamchat_teams": [kiwi`,
		"unknown key":   `{"teamchat_team": [{"team": "quillworks"}]}`,
		"wrong type":    `{"served_teams": "quillworks"}`,
		"trailing data": `{} {"served_teams": ["saltmarsh"]}`,
		"no input":      ``,
	} {
		w := pinRig(in, files{})
		if in == "" {
			w.env.Stdin = strings.NewReader("")
		}
		if code := w.run("pin"); code != ExitCannotRun {
			t.Errorf("%s: exit %d: %s", name, code, w.text())
		}
		if len(*w.replaced) != 0 {
			t.Errorf("%s: the list was written", name)
		}
		assertNoNamePrinted(t, w.rig)
	}
}

func TestPinTakesNoArguments(t *testing.T) {
	w := pinRig(`{}`, files{})
	if code := w.run("pin", "extra"); code != ExitCannotRun {
		t.Fatalf("exit %d: %s", code, w.text())
	}
}

// pinInputKeys, the keys the input may hold, are exactly pinInput's fields.
func TestPinInputKeysAreThePinInputFields(t *testing.T) {
	var tags []string
	ty := reflect.TypeFor[pinInput]()
	for i := range ty.NumField() {
		tags = append(tags, ty.Field(i).Tag.Get("json"))
	}
	slices.Sort(tags)
	if !slices.Equal(tags, pinInputKeys) {
		t.Fatalf("pinInputKeys %v, fields %v", pinInputKeys, tags)
	}
}
