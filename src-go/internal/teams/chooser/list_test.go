package chooser

import (
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
)

func entryOf(team string, s declare.Standing, about string, down bool) entry {
	return entry{Choice: declare.Choice{Team: team, Standing: s, Description: about}, down: down}
}

func TestRowsAlignTheColumnsAndEndWithTheCreateRow(t *testing.T) {
	rows := listRows(repo, []entry{
		entryOf("alpha", declare.StandingCovers, "Builds the site", false),
		entryOf("beta", declare.StandingWiden, "", true),
	})
	want := []string{
		"alpha  covers " + repo + "            Builds the site",
		"beta   would add " + repo + " (down)  (no description)",
		"+ Create a team for " + repo,
	}
	if len(rows) != len(want) {
		t.Fatalf("rows %q", rows)
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Errorf("row %d: %q, want %q", i, rows[i], want[i])
		}
	}
}

func TestRowsOutsideAGitHubCheckoutNameNoRepository(t *testing.T) {
	rows := listRows("", []entry{entryOf("alpha", "", "x", false)})
	if !strings.Contains(rows[0], notGitHub) || rows[1] != createPrefix {
		t.Fatalf("rows %q", rows)
	}
}

func TestADescriptionThatCannotBeShownSaysWhy(t *testing.T) {
	e := declare.Choice{Team: "a", DescriptionState: declare.DescriptionVaulted}
	if got := describe(e); got != "(description vaulted)" {
		t.Fatalf("got %q", got)
	}
}

func TestOnlyDownExcludedAndUnreadableTeamsAreBlocked(t *testing.T) {
	cases := []struct {
		e    entry
		want string
	}{
		{entryOf("a", declare.StandingCovers, "", true), "a's homeserver is down: ccy-teams apply a."},
		{entryOf("b", declare.StandingExcluded, "", false), "b excludes " + repo + " on purpose"},
		{entryOf("c", declare.StandingUnreadable, "", false), "c's scope cannot be read"},
		{entryOf("d", declare.StandingWiden, "", false), ""},
		{entryOf("e", declare.StandingCovers, "", false), ""},
		{entryOf("f", "", "", false), ""},
	}
	for _, tc := range cases {
		got := blocked(repo, tc.e)
		if tc.want == "" && got != "" || !strings.Contains(got, tc.want) {
			t.Errorf("%s: %q, want %q", tc.e.Team, got, tc.want)
		}
	}
}
