package readywait

import (
	"strings"
	"testing"
)

// mustFindings is Findings for a script that must parse.
func mustFindings(t *testing.T, text string) []int {
	t.Helper()
	got, err := Findings(text)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// R14: a script that does not parse as bash is an error, never a skip.
func TestAScriptThatDoesNotParseIsAnError(t *testing.T) {
	for _, script := range []string{"if true; then\n", "echo \"unterminated\n", "case x in\n", "for do done ("} {
		if got, err := Findings(script); err == nil || !strings.Contains(err.Error(), "cannot parse as bash") {
			t.Errorf("%q: findings %v, error %v", script, got, err)
		}
	}
}

// R14: the quoted parts of a double-quoted string are not code, but a command substitution in it is.
func TestACommandSubstitutionInDoubleQuotesStaysCode(t *testing.T) {
	script := "x=\"a & $(w &) 'q' $(echo \"in & ner\")\"\n"
	shell, err := readShell(script)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(shell.code, "&") != 1 {
		t.Fatalf("want only the ampersand of the substitution to survive:\n%s", shell.code)
	}
}
