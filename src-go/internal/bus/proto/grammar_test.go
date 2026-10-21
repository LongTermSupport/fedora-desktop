package proto

import "testing"

func TestGrammarRefusesATrailingNewline(t *testing.T) {
	for _, pred := range []func(string) bool{IsTeamName, IsSeat, IsHumanLocalpart, IsBranch, IsOwner} {
		if pred("ab\n") {
			t.Fatal("a trailing newline matched")
		}
	}
}

func TestRefFields(t *testing.T) {
	r, ok := ParseRef("pr:o/r#7@0123456789abcdef0123456789abcdef01234567")
	if !ok || r.Form != "pr" || r.Num != 7 || r.FullRepo() != "o/r" || r.Path != "" {
		t.Fatalf("%+v %v", r, ok)
	}
}
