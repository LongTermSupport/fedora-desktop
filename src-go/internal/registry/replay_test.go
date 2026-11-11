package registry

import (
	"slices"
	"strings"
	"testing"
)

// TestReplayArgsAgreesWithTheLaunchersOwnFilter holds the Go replay filter to the launcher's
// (ccy_registry_launch_args then ccy_registry_replay_args for ccy): a declared record carries the
// arguments a launch with the same argv would have recorded, word for word.
func TestReplayArgsAgreesWithTheLaunchersOwnFilter(t *testing.T) {
	cases := [][]string{
		nil,
		{"--token", "work", "--ssh-key", "/k/id", "--teams", "1@alpha"},
		{"--rebuild", "--prompt", "hello", "--compose", "start", "--no-restore", "--supervise"},
		{"first message", "--model", "opus", "--teams=1@a", "--until", "18:00"},
		{"--ssh-agent", "--no-ssh", "--debug", "--", "--prompt", "kept", "--no-restore", ""},
		{"--token", "--prompt", "--network", "net", "--rebuild=1", "--update-token=x", "--stop", "--list-containers"},
		{"--github-443", "--engine", "podman", "--max-age", "3", "--connect", "n", "--run-for", "2h"},
		{"--token", "--no-restore", "work", "--unknown", "--no-restore", "value"},
	}
	for _, args := range cases {
		got := ReplayArgs(args)
		want := bashLines(bashWithArgs(t, `mapfile -t launch < <(ccy_registry_launch_args "$@")
ccy_registry_replay_args ccy ${launch[@]+"${launch[@]}"}`, args...))
		if !slices.Equal(got, want) {
			t.Errorf("%q: go %q, bash %q", args, got, want)
		}
	}
}

// TestEveryFlagTheLauncherListsHasTheSameClassInGo reads the launcher's four lists themselves, so a
// flag added to one in bash and not in Go is caught here.
func TestEveryFlagTheLauncherListsHasTheSameClassInGo(t *testing.T) {
	words := bashLines(bashWithArgs(t, `printf '%s\n' "${CCY_REGISTRY_DROP_FLAGS[@]}" "${CCY_REGISTRY_DROP_VALUE_FLAGS[@]}" "${CCY_REGISTRY_KEEP_VALUE_FLAGS[@]}" "${CCY_REGISTRY_KEEP_FLAGS[@]}"`))
	if len(words) < 30 {
		t.Fatalf("read only %d flags from the launcher's lists", len(words))
	}
	for _, w := range words {
		out := strings.TrimSpace(bashWithArgs(t, `ccy_registry_flag_class "$1"`, w))
		if got := FlagClass(w); string(got) != out {
			t.Errorf("%s: bash %q, go %q", w, out, got)
		}
	}
}

// bashLines is a bash function's one-word-per-line output as words; nothing printed is no words.
func bashLines(out string) []string {
	if out == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(out, "\n"), "\n")
}
