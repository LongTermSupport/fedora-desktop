package restart

import (
	"maps"
	"testing"
)

func TestRunningContainersAreReadFromPodmansLabelledList(t *testing.T) {
	out := "c1\trunning\tccy-proj\nc2\texited\tccy-old\nbad line\nc3\trunning\tccy-two\n"
	want := map[string]string{"ccy-proj": "c1", "ccy-two": "c3"}
	if got := runningContainers(out); !maps.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestAJobSaysWhatWillBeDoneToIt(t *testing.T) {
	for _, tc := range []struct {
		j    job
		want string
	}{
		{job{action: actInPlace}, "running, so it restarts in place at its next idle moment, resuming its conversation in a fresh container"},
		{job{action: actStart, id: "abc"}, "stopped, so it is started, resuming conversation abc"},
		{job{result: resultLeft, note: "left running: why"}, "left running: why"},
		{job{result: resultFailed, note: "why"}, "cannot be restarted: why"},
	} {
		if got := tc.j.plan(); got != tc.want {
			t.Fatalf("got %q, want %q", got, tc.want)
		}
	}
}
