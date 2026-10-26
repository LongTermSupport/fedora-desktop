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

func TestShellEscUnparseableIsAnError(t *testing.T) {
	if _, err := ShellEscFindings([]byte("fzf (\n"), "x.bash"); err == nil {
		t.Fatal("want a parse error")
	}
}
