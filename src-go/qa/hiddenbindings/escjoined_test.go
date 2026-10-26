package hiddenbindings

import (
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/fzfbind"
)

// joinedBind is one --bind word binding every Esc-joined key to abort.
func joinedBind() string {
	pairs := make([]string, 0, len(fzfbind.EscJoined()))
	for _, k := range fzfbind.EscJoined() {
		pairs = append(pairs, k+":abort")
	}
	return "--bind='" + strings.Join(pairs, ",") + "'"
}

func escLines(t *testing.T, src string) []int {
	t.Helper()
	found, err := ShellEscFindings([]byte(src), "x.bash")
	if err != nil {
		t.Fatal(err)
	}
	lines := make([]int, 0, len(found))
	for _, f := range found {
		lines = append(lines, f.Line)
		if !strings.HasSuffix(f.Message, "["+EscRule+"]") {
			t.Errorf("message %q does not name %s", f.Message, EscRule)
		}
	}
	return lines
}

// An fzf the script starts must bind every Esc-joined key to abort in its own words.
func TestShellFzfMustLeaveOnEveryEscJoinedKey(t *testing.T) {
	all := joinedBind()
	half := strings.Replace(all, ",alt-a:abort", "' --bind 'alt-a:abort", 1)
	src := `pick() {
    fzf --bind='q:abort'
    printf '%s\n' a | fzf --height=40% ` + all + `
    command fzf --ansi
    FOO=1 fzf ` + half + `
    fzf --bind='alt-up:abort,alt-down:abort'
    fzf "${opts[@]}"
    /usr/bin/fzf ` + strings.Replace(all, "alt-q:abort", "alt-q:accept", 1) + `
    if [[ -z "$(command -v fzf)" ]]; then return 1; fi
    command -v fzf >/dev/null
    printf 'fzf\n'
    fzf --version
    out="$(fzf --bash)"
    rank | fzf --read0 --filter "$2"
    fzf -f x
}
`
	if got, want := escLines(t, src), []int{2, 4, 6, 7, 8}; !slices.Equal(got, want) {
		t.Errorf("lines = %v, want %v", got, want)
	}
}

// fzf's word editing keys bound to abort throw the typed query away: each such fzf is reported.
func TestShellFzfMustKeepWordEditing(t *testing.T) {
	all := joinedBind()
	src := `fzf ` + all + ` --bind='alt-b:abort'
fzf ` + all + ` --bind='alt-b:backward-word,alt-f:forward-word'
fzf ` + all + ` --bind='alt-f:abort,alt-d:abort,alt-bs:abort'
`
	found, err := ShellEscFindings([]byte(src), "x.bash")
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 2 || found[0].Line != 1 || found[1].Line != 3 {
		t.Fatalf("want lines 1 and 3, got %+v", found)
	}
	if !strings.Contains(found[1].Message, "alt-f, alt-d, alt-bs") || !strings.Contains(found[1].Message, "word editing") {
		t.Errorf("message %q names the word keys", found[1].Message)
	}
	for _, k := range fzfbind.WordEditing() {
		if strings.Contains(all, "'"+k+":") || strings.Contains(all, ","+k+":") {
			t.Errorf("the Esc-joined bindings bind %s, a word editing key", k)
		}
	}
}

func TestShellEscUnparseableIsAnError(t *testing.T) {
	if _, err := ShellEscFindings([]byte("fzf (\n"), "x.bash"); err == nil {
		t.Fatal("want a parse error")
	}
}
