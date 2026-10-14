package readywait

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestShellRuleCases(t *testing.T) {
	for _, c := range ruleCases {
		t.Run(c.spec+"/"+c.name, func(t *testing.T) {
			got := Findings(c.script)
			if len(got) != len(c.want) || (len(got) > 0 && !slices.Equal(got, c.want)) {
				t.Fatalf("findings %v, want %v\n%s", got, c.want, c.script)
			}
		})
	}
}

// R14: the blanked text keeps every offset and every newline.
func TestCodeOnlyKeepsLinesAndOffsets(t *testing.T) {
	text := "a='x\ny'\n# c\nb \"q\nr\"\ncat <<EOF\nserver &\nEOF\nz=$'a\\'b' &\n"
	got := codeOnly(text)
	if len(got) != len(text) {
		t.Fatalf("length %d, want %d", len(got), len(text))
	}
	if strings.Count(got, "\n") != strings.Count(text, "\n") {
		t.Fatal("newlines moved")
	}
	for i := range text {
		if text[i] == '\n' && got[i] != '\n' {
			t.Fatalf("newline at %d lost", i)
		}
	}
	if strings.Contains(strings.SplitN(got, "z=", 2)[0], "&") {
		t.Fatalf("an ampersand in a here-document survived:\n%s", got)
	}
	if !strings.Contains(got, "z=$' ") {
		t.Fatalf("an escape inside $'...' was not blanked:\n%s", got)
	}
}

// R14: <<EOF with a dash strips tabs from the terminator; a here-string is not a here-document.
func TestHereDocumentForms(t *testing.T) {
	loop := "for _ in 1 2; do\n\t[ -e r ] && break\n\tsleep 1\ndone\n"
	stripped := "w &\np=$!\ncat <<-EOF\n\tx &\n\tEOF\n" + loop
	if got := Findings(stripped); !slices.Equal(got, []int{6}) {
		t.Fatalf("<<-: %v", got)
	}
	herestring := "cat <<<x &\nsleep 1\n" + loop
	if got := Findings(herestring); !slices.Equal(got, []int{3}) {
		t.Fatalf("<<<: %v", got)
	}
	quoted := "w &\ncat <<\"EOF\"\nfor _ in 1; do sleep 1; break; done\nEOF\n" + loop
	if got := Findings(quoted); !slices.Equal(got, []int{5}) {
		t.Fatalf("quoted delimiter: %v", got)
	}
}

// R23: two loops starting on one line are one finding.
func TestOneFindingPerLine(t *testing.T) {
	script := "w &\np=$!\nuntil [ -e a ]; do sleep 1; done; until [ -e b ]; do sleep 1; done\n"
	if got := Findings(script); !slices.Equal(got, []int{3}) {
		t.Fatalf("%v", got)
	}
}

// R24: bytes that are not UTF-8 are ordinary bytes.
func TestInvalidUTF8IsOrdinary(t *testing.T) {
	script := "echo \xff\xfe \"\xc3\" # \xff\nw &\np=$!\nuntil [ -e a ]; do\n\tsleep 1\ndone\n"
	if got := Findings(script); !slices.Equal(got, []int{4}) {
		t.Fatalf("%v", got)
	}
}

// R18: a loop in a function an armed scope calls is a wait; a function called with && or || is
// still a call.
func TestCalledFunctionsLoopsAreWaits(t *testing.T) {
	script := "waiter() {\n\tuntil [ -e r ]; do\n\t\tsleep 1\n\tdone\n}\nw &\np=$!\ntrue && waiter\n"
	if got := Findings(script); !slices.Equal(got, []int{2}) {
		t.Fatalf("%v", got)
	}
}

// R21: wait -n is a liveness check and does not disarm.
func TestWaitNDoesNotDisarm(t *testing.T) {
	script := "w &\np=$!\nwait -n\nuntil [ -e r ]; do\n\tsleep 1\ndone\n"
	if got := Findings(script); !slices.Equal(got, []int{4}) {
		t.Fatalf("%v", got)
	}
}

func TestFixtureMarks(t *testing.T) {
	text := "a\n  #  ruleid:  ready-wait-ignores-child-exit  \nloop\n# ok: ready-wait-ignores-child-exit\nb\n# ruleid: other\n"
	if got := FixtureMarks(text); !slices.Equal(got, []int{3}) {
		t.Fatalf("%v", got)
	}
}

// R7: the project's own fixture is the contract the gate proves on every run.
func TestTheProjectFixtureAnnotationsMatchTheFindings(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", ".semgrep", "ready-wait.bash"))
	if err != nil {
		t.Fatal(err)
	}
	marks := FixtureMarks(string(data))
	if len(marks) == 0 {
		t.Fatal("the fixture marks no line that must fire")
	}
	if got := Findings(string(data)); !slices.Equal(got, marks) {
		t.Fatalf("fixture marks %v, rule reports %v", marks, got)
	}
}
