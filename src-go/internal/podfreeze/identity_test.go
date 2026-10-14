package podfreeze

import (
	"strings"
	"testing"
)

func mustAxis(t *testing.T, name string) identityAxis {
	t.Helper()
	ax, err := axisByName(name)
	if err != nil {
		t.Fatal(err)
	}
	return ax
}

// ---- identity_value_of: one axis, one container (TP 8) ----

func TestIdentityValueOfOneAxisOneContainer(t *testing.T) {
	_, inv := fixture(t)
	cases := []struct{ axis, name, want string }{
		{"github", "alpha_yolo_2", "octo"},
		{"token", "bravo_browser", "work"},
		{"ssh-key", "alpha_yolo_2", "key_alpha key_shared"}, // several keys, verbatim
		// An unlabelled session and a labelled one with "none" on the axis are BOTH empty here;
		// the values alone cannot tell them apart, which is why the record carries labelled.
		{"github", "echo_yolo_3", ""},
		{"github", "zulu_yolo", ""},
		{"token", "mike", ""},
	}
	for _, tc := range cases {
		if got := mustAxis(t, tc.axis).value(recordFor(t, inv, tc.name)); got != tc.want {
			t.Errorf("%s of %s: %q, want %q", tc.axis, tc.name, got, tc.want)
		}
	}
}

func TestIdentityAxisUnknownIsFatalAndNamed(t *testing.T) {
	// An axis that is not one of the three is an internal error and dies, rather than resolving
	// to an empty value, which would be an empty group, silently.
	_, err := axisByName("bogus")
	if msg := dieText(t, err); !strings.Contains(msg, "bogus") || !strings.Contains(msg, "unknown identity axis") {
		t.Fatalf("got %q", msg)
	}
}

// ---- identity_matches (TP 11 + 3 + 5) ----

func TestIdentityMatchesWholeValueForGithubAndToken(t *testing.T) {
	for _, axis := range []string{"github", "token"} {
		ax := mustAxis(t, axis)
		if !ax.matches("octo", "octo") {
			t.Errorf("%s matches whole", axis)
		}
		if ax.matches("oct", "octo") {
			t.Errorf("%s must not match a prefix", axis)
		}
		if ax.matches("octo", "octopus") {
			t.Errorf("%s must not match a suffix", axis)
		}
		if ax.matches("zz-glob-victim", "*") {
			t.Errorf("%s must not treat a label of * as a glob", axis)
		}
	}
}

func TestIdentityMatchesAWordOfASshKeyLabel(t *testing.T) {
	ax := mustAxis(t, "ssh-key")
	for _, tc := range []struct {
		want, have string
		ok         bool
	}{
		{"key_shared", "key_alpha key_shared", true},
		{"key_alpha", "key_alpha key_shared", true},
		{"key_shared", "key_shared", true},
		{"key_absent", "key_alpha key_shared", false},
		{"key_share", "key_alpha key_shared", false}, // a prefix of a key
		{"key_shared", "", false},                    // no keys at all
		// SSH key basenames do not normally contain spaces. A wanted value with a space can never
		// match, because the haystack is split on whitespace; pinned so a refactor that starts
		// comparing whole values is visible rather than silently widening.
		{"key a", "key a", false},
		{"alpha", "alpha beta", true},
		{"beta", "alpha beta", true},
		{"gamma", "alpha beta", false},
		// bug 3 / the glob case: a label of "*" is a literal word, never a wildcard.
		{"zz-glob-victim", "*", false},
		{"*", "*", true},
	} {
		if got := ax.matches(tc.want, tc.have); got != tc.ok {
			t.Errorf("ssh-key %q in %q: %v, want %v", tc.want, tc.have, got, tc.ok)
		}
	}
}

// On a single-valued axis empty want and empty have COMPARE EQUAL, and nothing inside the
// function stops it; select_identity is the guard, by refusing a value not in identity_values
// (which never emits ""). Pinned so that moving the guard shows up here. ssh-key disagrees:
// an empty haystack has no words.
func TestIdentityMatchesTheEmptyValueIsGuardedByTheCaller(t *testing.T) {
	if !mustAxis(t, "github").matches("", "") || !mustAxis(t, "token").matches("", "") {
		t.Fatal("empty matches empty on the single-valued axes")
	}
	if mustAxis(t, "ssh-key").matches("", "") {
		t.Fatal("ssh-key: empty does not match empty")
	}
}

// ---- identity_values (TP 5) ----

func TestIdentityValuesAreDistinctSortedAndBuiltFromWhatIsRunning(t *testing.T) {
	_, inv := fixture(t)
	// alpha (octo) is at index 2 and bravo (hubot) at index 4, so SORTED output reverses
	// inventory order.
	eqStrings(t, "github", identityValues(mustAxis(t, "github"), inv), []string{"hubot", "octo"})
	eqStrings(t, "token (one value shared by two sessions is one value)", identityValues(mustAxis(t, "token"), inv), []string{"work"})
	eqStrings(t, "ssh keys are split and deduplicated", identityValues(mustAxis(t, "ssh-key"), inv), []string{"key_alpha", "key_shared"})
	_, none := load(t, "plain|running|none\n", "", "")
	if got := identityValues(mustAxis(t, "github"), none); len(got) != 0 {
		t.Fatalf("an axis nothing carries is empty: %q", got)
	}
}

// Bug 3: the bash split an ssh-key label with an unquoted `for word in $value`, so a label of
// "*" listed the working directory's files in the menu. The split is on whitespace only.
func TestIdentityValuesAnSshKeyLabelOfStarIsALiteralWord(t *testing.T) {
	_, inv := load(t, "glob_yolo|running|none\n", "glob_yolo|none|none|*\n", "")
	eqStrings(t, "values", identityValues(mustAxis(t, "ssh-key"), inv), []string{"*"})
}

// Values sort bytewise, not by locale (bug 9).
func TestNewIdentityValuesSortBytewise(t *testing.T) {
	ps := "a_yolo|running|none\nb_yolo|running|none\nc_yolo|running|none\n"
	labels := "a_yolo|b|none|none\nb_yolo|B|none|none\nc_yolo|a|none|none\n"
	_, inv := load(t, ps, labels, "")
	eqStrings(t, "github", identityValues(mustAxis(t, "github"), inv), []string{"B", "a", "b"})
}

// ---- identity_names (TP 6) ----

func TestIdentityNamesAreInInventoryOrder(t *testing.T) {
	_, inv := fixture(t)
	eqStrings(t, "single member", identityNames(mustAxis(t, "github"), "octo", inv), []string{"alpha_yolo_2"})
	// alpha is index 2 and bravo index 4 with kilo between them: not adjacent, so a loop that
	// stopped at the first match returns half the group.
	eqStrings(t, "two members", identityNames(mustAxis(t, "ssh-key"), "key_shared", inv), []string{"alpha_yolo_2", "bravo_browser"})
	eqStrings(t, "a key only one holds", identityNames(mustAxis(t, "ssh-key"), "key_alpha", inv), []string{"alpha_yolo_2"})
	eqStrings(t, "a value nobody carries", identityNames(mustAxis(t, "github"), "nosuch", inv), nil)
	// The consequence of empty-matches-empty, spelled out: asking for "" returns every
	// container WITHOUT a value, non-CCY ones included. Unreachable through select_identity.
	eqStrings(t, "an empty value selects everything unlabelled on that axis",
		identityNames(mustAxis(t, "github"), "", inv), []string{"zulu_yolo", "mike", "kilo", "echo_yolo_3"})
	eqStrings(t, "ssh-key returns nothing for an empty value", identityNames(mustAxis(t, "ssh-key"), "", inv), nil)
}

// ---- identity_axis_discriminates: COVERAGE, not cardinality (TP 6) ----

func TestIdentityAxisDiscriminatesByCoverage(t *testing.T) {
	_, inv := fixture(t)
	if !discriminates(mustAxis(t, "github"), inv) {
		t.Error("two github accounts tell sessions apart")
	}
	if !discriminates(mustAxis(t, "ssh-key"), inv) {
		t.Error("two ssh keys tell sessions apart")
	}
	// THE case a cardinality proxy gets wrong: one token held by 2 of the 4 CCY sessions is a
	// single distinct value that still splits the set.
	if !discriminates(mustAxis(t, "token"), inv) {
		t.Error("one value covering SOME of the CCY sessions discriminates")
	}
	_, empty := load(t, "plain|running|none\n", "", "")
	if discriminates(mustAxis(t, "github"), empty) {
		t.Error("an axis nothing carries offers no rows")
	}
	// One value covering EVERY CCY session is the same button as "all CCY containers".
	_, uniform := load(t, uniformPS, uniformLabels, "")
	if discriminates(mustAxis(t, "token"), uniform) {
		t.Error("one value covering ALL of the CCY sessions is suppressed")
	}
	// And the boundary is genuinely coverage: one more CCY session outside it, and the same
	// single value starts discriminating.
	_, more := load(t, uniformPS+"three_yolo|running|none\n", uniformLabels, "")
	if !discriminates(mustAxis(t, "token"), more) {
		t.Error("the same single value, with one session outside it, discriminates")
	}
}

// ---- unlabelled_ccy_names and the blind-spot note (TP 3 + 5) ----

func TestUnlabelledCCYNamesIsNotTheSameAsNoIdentity(t *testing.T) {
	_, inv := fixture(t)
	// zulu_yolo has no labels; echo_yolo_3 has labels and "none" on every axis. If these two
	// collapsed, the owner of a current session would be told to relaunch it so it can be
	// labelled: advice that cannot work.
	eqStrings(t, "only the session that predates the labels", unlabelledCCY(inv), []string{"zulu_yolo"})
	_, uniform := load(t, uniformPS, uniformLabels, "")
	eqStrings(t, "a fully labelled machine has no blind spot", unlabelledCCY(uniform), nil)
	_, noCCY := load(t, "mike|running|none\nkilo|running|none\n", "", "")
	eqStrings(t, "an ordinary container is not reported", unlabelledCCY(noCCY), nil)
}

func TestWarnIdentityBlindSpotDisclosesAndNeverBlocks(t *testing.T) {
	e, inv := fixture(t)
	if err := e.warnBlindSpot(inv); err != nil {
		t.Fatal(err)
	}
	want := "podfreeze: NOTE — 1 CCY session(s) carry no identity labels,\n" +
		"  so they are in no identity group and are NOT in this selection:\n" +
		"    zulu_yolo\n" +
		"  They predate CCY 3.40.0. Use --ccy to cover every session regardless\n" +
		"  of identity, or relaunch them to make them selectable by identity.\n"
	if got := said(t, e); got != want {
		t.Fatalf("got %q", got)
	}
	e, uniform := load(t, uniformPS, uniformLabels, "")
	if err := e.warnBlindSpot(uniform); err != nil || said(t, e) != "" {
		t.Fatalf("silence when every session is labelled: %q, %v", said(t, e), err)
	}
}

// ---- ccy_names, network_map (TP 2 + 8 + 2) ----

func TestCCYNamesAreEveryKindOfSessionInInventoryOrder(t *testing.T) {
	_, inv := fixture(t)
	// The non-CCY containers sit at indices 1 and 3 between CCY ones, so a filter that stopped
	// at the first mismatch would return only zulu_yolo.
	eqStrings(t, "ccy", ccyNames(inv), []string{"zulu_yolo", "alpha_yolo_2", "bravo_browser", "echo_yolo_3"})
}

func TestNetworkMapIsBuiltFromTheInventory(t *testing.T) {
	_, inv := fixture(t)
	m := networkMap(inv)
	eqStrings(t, "the networks in use, and only those", networkNames(m), []string{"appnet", "podman"})
	// "podman, appnet" is one field with a comma AND a space; both are handled, so alpha is a
	// member of both. Members keep inventory order, not the order the map hashes into.
	eqStrings(t, "a container on two networks joins both", m["appnet"], []string{"alpha_yolo_2", "kilo"})
	eqStrings(t, "members keep inventory order", m["podman"], []string{"zulu_yolo", "alpha_yolo_2", "bravo_browser"})
	// "none" is what a container with no networks gets; it must not become a network.
	if _, ok := m["none"]; ok {
		t.Fatal("'none' is not a network")
	}
	for _, members := range m {
		for _, name := range members {
			if name == "mike" || name == "echo_yolo_3" {
				t.Fatalf("%s has no network but is in a group", name)
			}
		}
	}
	eqStrings(t, "an unknown network yields nothing", m["nosuchnet"], nil)
}

func TestNetworkMapSkipsEmptyNetworkNamesAndRebuildsFresh(t *testing.T) {
	// An empty networks field is not "none" (the loader's default makes that word) and must not
	// become a network named "".
	_, inv := load(t, "weird|running|,podman, ,\n", "", "")
	eqStrings(t, "stray separators", networkNames(networkMap(inv)), []string{"podman"})
	// Rebuilding replaces: a stale member is how a menu row acts on a container that left the
	// network a screen ago.
	_, first := fixture(t)
	_, second := load(t, "kilo|running|appnet\n", "", "")
	if len(networkMap(first)["podman"]) == 0 {
		t.Fatal("the first inventory has podman members")
	}
	m := networkMap(second)
	eqStrings(t, "a second build replaces the first", m["podman"], nil)
	eqStrings(t, "and keeps only what is there now", m["appnet"], []string{"kilo"})
}
