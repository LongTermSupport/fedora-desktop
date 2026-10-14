package freeze

import (
	"slices"
	"testing"
)

var choiceRows = []string{"alpha", "bravo", "charlie", "delta", "echo"}

// Row 1 is "all", so the first container is row 2.
func TestParseMemberChoiceResolvesRows(t *testing.T) {
	cases := []struct {
		what, reply string
		want        []string
	}{
		{"row 2 is the FIRST container, not the second", "2", []string{"alpha"}},
		{"row 3 is the second", "3", []string{"bravo"}},
		{"the last row is the last container", "6", []string{"echo"}},
		{"commas separate", "2,4,5", []string{"alpha", "charlie", "delta"}},
		{"so do spaces", "2 4 5", []string{"alpha", "charlie", "delta"}},
		{"and a mix of both", "2, 4", []string{"alpha", "charlie"}},
		{"order is the order typed, not sorted", "5,2", []string{"delta", "alpha"}},
	}
	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			got, ok := ParseMemberChoice(c.reply, choiceRows)
			if !ok || !slices.Equal(got, c.want) {
				t.Fatalf("got %v, %v", got, ok)
			}
		})
	}
}

// Every rejection must leave the caller to re-prompt rather than resolving to some container:
// picking the wrong container is silent and irreversible.
func TestParseMemberChoiceRefuses(t *testing.T) {
	for _, bad := range []string{"1", "0", "7", "99", "-1", "abc", "2abc", "", "   ", "2,abc", "2,0", "2,99", "99999999999999999999"} {
		t.Run("'"+bad+"' is refused", func(t *testing.T) {
			if got, ok := ParseMemberChoice(bad, choiceRows); ok {
				t.Fatalf("resolved to %v", got)
			}
		})
	}
	// 1 deserves its own word: it is the ALL row, handled by the caller. If it ever resolved
	// here it would silently mean "the first container".
	t.Run("and 1 in particular resolves to nothing", func(t *testing.T) {
		got, ok := ParseMemberChoice("1", choiceRows)
		mustEq(t, "rows", len(got), 0)
		mustEq(t, "ok", ok, false)
	})
	// ONE bad token rejects the WHOLE reply: a partial answer would act on some of what was
	// typed and not the rest, with nothing said about which.
	t.Run("one bad token rejects the whole reply", func(t *testing.T) {
		if _, ok := ParseMemberChoice("2,99,4", choiceRows); ok {
			t.Fatal("partial acceptance")
		}
	})
	// Discrimination control: every rejection above would pass against a function that refused
	// unconditionally.
	t.Run("a valid choice and a refusal are distinct", func(t *testing.T) {
		_, okValid := ParseMemberChoice("2", choiceRows)
		_, okBad := ParseMemberChoice("abc", choiceRows)
		if okValid == okBad {
			t.Fatal("both gave the same answer")
		}
	})
}

// The rows are addressed positionally, so a SHORTER row list moves the bound with it: with a
// member skipped from the menu, row 3 is the container printed at row 3 and there is no row 4.
func TestParseMemberChoiceBoundFollowsTheDisplayedRows(t *testing.T) {
	short := []string{"alpha", "charlie"}
	t.Run("a skipped member does not shift the rows below it", func(t *testing.T) {
		got, ok := ParseMemberChoice("3", short)
		if !ok || !slices.Equal(got, []string{"charlie"}) {
			t.Fatalf("got %v, %v", got, ok)
		}
	})
	t.Run("and the bound moves with the list", func(t *testing.T) {
		if got, ok := ParseMemberChoice("4", short); ok {
			t.Fatalf("row 4 resolved on a two-row menu: %v", got)
		}
	})
}

// Bug 4: `2,2` named the same container twice and the second pause failed.
func TestParseMemberChoiceDeduplicatesFirstOccurrenceWins(t *testing.T) {
	got, ok := ParseMemberChoice("3,2,3", choiceRows)
	if !ok || !slices.Equal(got, []string{"bravo", "alpha"}) {
		t.Fatalf("got %v, %v", got, ok)
	}
}
