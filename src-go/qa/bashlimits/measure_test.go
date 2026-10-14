package bashlimits

import (
	"strings"
	"testing"
)

func TestMeasureCodeLines(t *testing.T) {
	src := "#!/usr/bin/bash\n# a comment\n\n   \n\techo one\n  # indented comment\necho two # trailing\n"
	if got := m(t, src).Code; got != 2 {
		t.Fatalf("code lines = %d, want 2 (blank, comment and shebang lines do not count)", got)
	}
}

func TestMeasureFunctions(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		{"paren brace", "foo() {\n:\n}\n", 1},
		{"space paren", "foo () {\n:\n}\n", 1},
		{"function keyword", "function foo {\n:\n}\n", 1},
		{"function keyword with parens", "function foo() {\n:\n}\n", 1},
		{"brace on the next line", "foo()\n{\n:\n}\n", 1},
		{"indented", "  inner() {\n:\n  }\n", 1},
		{"namespaced", "ns::fn() {\n:\n}\n", 1},
		{"four", "a() { :; }\nb() { :; }\nc() { :; }\nd() { :; }\n", 4},
		{"commented out", "# foo() {\n", 0},
		{"array assignment", "arr=()\nx=$(foo)\n", 0},
		{"call, not definition", "foo\nfoo bar\n", 0},
		{"in a heredoc", "cat <<EOF\nfoo() {\nEOF\n", 0},
		{"after a heredoc", "cat <<EOF\nx\nEOF\nfoo() { :; }\n", 1},
	}
	for _, c := range cases {
		if got := m(t, c.src).Funcs; got != c.want {
			t.Errorf("%s: funcs = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestMeasureAssociativeArrays(t *testing.T) {
	yes := []string{
		"declare -A m",
		"local -A m=()",
		"typeset -A m",
		"declare -gA m",
		"declare -Ar m=([a]=1)",
		"declare -rA m",
		"declare -g -A m",
		"f() { local -A m; }",
		"  readonly -A m=()",
		"true && declare -A m",
	}
	no := []string{
		"declare -a m",
		"declare -x M",
		"local m",
		"# declare -A m",
		"echo hi # declare -A m",
		"echo \"declare -A m\"",
		"cat <<EOF\ndeclare -A m\nEOF",
		"declare -i n",
	}
	for _, s := range yes {
		if !m(t, s+"\n").Assoc {
			t.Errorf("not detected: %q", s)
		}
	}
	for _, s := range no {
		if m(t, s+"\n").Assoc {
			t.Errorf("false positive: %q", s)
		}
	}
}

func TestMeasureInteractiveUI(t *testing.T) {
	yes := []string{
		"gum choose a b",
		"fzf --height=40%",
		"whiptail --yesno x 8 40",
		"dialog --msgbox x 8 40",
		"x=$(gum input)",
		"x=\"$(fzf)\"",
		"list | fzf",
		"list | fzf --multi",
		"if gum confirm; then :; fi",
		"ok && gum spin -- sleep 1",
		"sudo gum choose a",
		"FZF_DEFAULT_OPTS=x fzf",
		"exec fzf",
		"select opt in a b c; do break; done",
		"  select opt in \"$@\"; do :; done",
		"if true; then fzf; fi",
		"x=`gum input`",
		"cmd1 || dialog --infobox x 3 3",
	}
	no := []string{
		"command -v gum",
		"if ! command -v fzf >/dev/null; then :; fi",
		"echo \"install gum first\"",
		"# gum choose a b",
		"echo hi # fzf",
		"fzf_cmd=1",
		"which dialog",
		"selected=1",
		"selectable in",
		"case $x in fzf) :;; esac",
		"cat <<EOF\ngum choose\nEOF",
		"echo select the thing",
	}
	for _, s := range yes {
		if !m(t, s+"\n").UI {
			t.Errorf("not detected: %q", s)
		}
	}
	for _, s := range no {
		if m(t, s+"\n").UI {
			t.Errorf("false positive: %q", s)
		}
	}
}

func TestMeasureHeredocForms(t *testing.T) {
	forms := []string{
		"cat <<'EOF'\ngum choose\nEOF\n",
		"cat <<\"EOF\"\ngum choose\nEOF\n",
		"cat <<-EOF\n\tgum choose\n\tEOF\n",
		"cat <<\\EOF\ngum choose\nEOF\n",
		"cat > f <<END_X\ngum choose\nEND_X\n",
	}
	for _, s := range forms {
		if m(t, s).UI {
			t.Errorf("heredoc body scanned: %q", s)
		}
	}
	// A here-string and an arithmetic shift open no heredoc, so what follows is code.
	for _, s := range []string{"cat <<< x\ngum choose\n", "(( x = 1 << n ))\ngum choose\n"} {
		if !m(t, s).UI {
			t.Errorf("code after a non-heredoc was skipped: %q", s)
		}
	}
}

func TestMeasureHeredocLinesCountAsCode(t *testing.T) {
	got := m(t, "cat <<EOF\nline one\n\nline two\nEOF\n")
	if got.Code != 4 {
		t.Fatalf("code = %d, want 4 (cat, two body lines, EOF)", got.Code)
	}
}

func TestMeasureQuotedHashIsNotAComment(t *testing.T) {
	got := m(t, "echo \"a # b\"; declare -A m\necho ${#arr[@]} $# ; declare -A n\n")
	if !got.Assoc || got.Code != 2 {
		t.Fatalf("got %+v", got)
	}
}

func TestBreaches(t *testing.T) {
	ok := Measurement{Code: MaxCodeLines, Funcs: MaxFuncs}
	if len(ok.Breaches()) != 0 || !ok.WithinLimits() {
		t.Fatalf("a file at the limits breaches: %v", ok.Breaches())
	}
	bad := Measurement{Code: MaxCodeLines + 1, Funcs: MaxFuncs + 1, Assoc: true, UI: true}
	got := strings.Join(bad.Breaches(), "|")
	for _, want := range []string{"61 code lines", "5 functions", "associative array", "interactive UI"} {
		if !strings.Contains(got, want) {
			t.Errorf("breaches %q lack %q", got, want)
		}
	}
	if bad.WithinLimits() {
		t.Fatal("a breaching file is within limits")
	}
}

func TestFlags(t *testing.T) {
	if got := (Measurement{}).Flags(); len(got) != 0 {
		t.Fatalf("flags = %v", got)
	}
	if got := strings.Join(Measurement{Assoc: true, UI: true}.Flags(), ","); got != "assoc,ui" {
		t.Fatalf("flags = %q", got)
	}
}
