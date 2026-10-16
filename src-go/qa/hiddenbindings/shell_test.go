package hiddenbindings

import (
	"slices"
	"strings"
	"testing"
)

func shellLines(t *testing.T, src string) []int {
	t.Helper()
	found, err := ShellFindings([]byte(src), "x.bash")
	if err != nil {
		t.Fatal(err)
	}
	lines := make([]int, 0, len(found))
	for _, f := range found {
		lines = append(lines, f.Line)
	}
	return lines
}

func TestShellFlagsKeyOnlyActions(t *testing.T) {
	src := `pick() {
    fzf --expect=ctrl-n
    fzf --expect ctrl-x
    fzf --bind='ctrl-a:become(x)'
    fzf --bind 'ctrl-x:execute(rm {})'
    fzf --bind "ctrl-o:become(x)"
    local -a opts=(--expect="$3")
    opts+=(--bind="$spec")
    opts+=(--bind)
    fzf "${opts[@]}" --bind="focus:transform-header^${h}^,ctrl-a:become(x)"
    fzf --bind="ctrl-a:become(x)${rest}"
    fzf --bind='%s:become(x)'
    fzf --bind='ctrl-a'
}
`
	want := []int{2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13}
	if got := shellLines(t, src); !slices.Equal(got, want) {
		t.Errorf("lines = %v, want %v", got, want)
	}
}

func TestShellAllowsNavigationAndEvents(t *testing.T) {
	src := `pick() {
    fzf --bind='q:abort' --bind=start:last --bind ctrl-r:toggle-sort --bind='tab:toggle+down'
    fzf --bind='start:transform-query:printf %s "${q}"'
    fzf --bind="focus:transform-header^${h};; esac^"
    local -a opts=(--bind='start:last')
    grep --expect-this file
    [[ "$a" == --expect ]]
}
`
	if got := shellLines(t, src); len(got) != 0 {
		t.Errorf("lines = %v, want none", got)
	}
}

// A scalar spread unquoted into fzf is split on whitespace, so its value is judged the same
// way, word by word (Fable's N2).
func TestShellJudgesScalarValuesWordByWord(t *testing.T) {
	src := `opts="--height=40% --expect=ctrl-a"
local more='--bind ctrl-x:execute(rm)'
export FZF_DEFAULT_OPTS='--bind=ctrl-k:become(x)'
keys="--bind=$spec"
fine="--bind=q:abort --bind ctrl-a:accept"
label="press --expect later"
fzf $opts $more
`
	want := []int{1, 2, 3, 4}
	if got := shellLines(t, src); !slices.Equal(got, want) {
		t.Errorf("lines = %v, want %v", got, want)
	}
}

func TestShellOnlyJudgesFilesThatRunFzf(t *testing.T) {
	src := "some-tool --expect ctrl-a --bind=x:accept\n"
	if got := shellLines(t, src); len(got) != 0 {
		t.Errorf("a file that never runs fzf: lines = %v, want none", got)
	}
	src = "command fzf-tmux --expect ctrl-a\n"
	if got := shellLines(t, src); !slices.Equal(got, []int{1}) {
		t.Errorf("fzf-tmux behind command: lines = %v, want [1]", got)
	}
	src = "\"$HOME/bin/fzf\" --expect ctrl-a\n"
	if got := shellLines(t, src); !slices.Equal(got, []int{1}) {
		t.Errorf("fzf by path: lines = %v, want [1]", got)
	}
}

func TestShellMessagesNameTheRule(t *testing.T) {
	found, err := ShellFindings([]byte("fzf --expect=ctrl-n --bind='ctrl-a:become(x)'\n"), "x.bash")
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 2 {
		t.Fatalf("found %v, want 2", found)
	}
	for _, f := range found {
		if !strings.Contains(f.Message, Rule) || f.Path != "x.bash" {
			t.Errorf("finding %+v must name %s and the file", f, Rule)
		}
	}
	if !strings.Contains(found[1].Message, "ctrl-a:become") {
		t.Errorf("message %q must name the binding", found[1].Message)
	}
}

func TestShellUnparseableIsAnError(t *testing.T) {
	if _, err := ShellFindings([]byte("fzf ( \n"), "x.bash"); err == nil {
		t.Error("an unparseable file must be an error, never clean")
	}
}
