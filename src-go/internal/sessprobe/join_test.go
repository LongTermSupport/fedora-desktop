package sessprobe_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sessprobe"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tmuxccy"
)

const panesText = `ccy-alpha 100
ccy-beta 200
ccy-deep 300
cc-gamma 400
ccy-gone 500`

const procsText = `1 0 /usr/lib/systemd/systemd --user
50 1 tmux -L ccy
100 50 bash -c trampoline
110 100 /bin/bash /var/local/claude-yolo/claude-yolo
112 110 podman run --rm -it --name alpha_yolo --network alpha-network img claude
113 112 podman run --rm -it --name alpha_yolo --network alpha-network img claude
200 50 bash -c trampoline
210 200 /bin/bash /var/local/claude-yolo/claude-yolo
211 210 /usr/bin/podman run --rm --name=beta_yolo img claude
300 50 bash -c trampoline
310 300 /bin/bash /var/local/claude-yolo/claude-yolo
311 310 /bin/sh -c wrapper
312 311 /usr/bin/docker run --rm --name deep_yolo img claude
400 50 bash -c trampoline
410 400 claude --dangerously-skip-permissions
500 50 bash -c trampoline
510 500 /usr/bin/editor --name not-a-container
900 1 podman run --rm --name orphan_yolo img claude`

func panes(t *testing.T, text string) []tmuxccy.Pane {
	t.Helper()
	var out []tmuxccy.Pane
	for line := range strings.SplitSeq(text, "\n") {
		session, pid, ok := strings.Cut(line, " ")
		if !ok {
			t.Fatalf("bad pane line %q", line)
		}
		var n int
		for _, c := range pid {
			n = n*10 + int(c-'0')
		}
		out = append(out, tmuxccy.Pane{Session: session, PID: n})
	}
	return out
}

func procs(t *testing.T, text string) []sessprobe.Proc {
	t.Helper()
	got, err := sessprobe.ParseProcs(text)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestParseProcs(t *testing.T) {
	got := procs(t, "  1 0 /sbin/init splash\n\n 22   1   podman   run  --name x \n")
	want := []sessprobe.Proc{{PID: 1, PPID: 0, Args: "/sbin/init splash"}, {PID: 22, PPID: 1, Args: "podman   run  --name x"}}
	if !slices.Equal(got, want) {
		t.Fatalf("got %+v", got)
	}
	if got = procs(t, ""); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
	for _, bad := range []string{"x 1 a", "1 y a", "1"} {
		if _, err := sessprobe.ParseProcs(bad); err == nil {
			t.Errorf("%q: want error", bad)
		}
	}
}

func TestSessionContainersGoldenCases(t *testing.T) {
	got := sessprobe.SessionContainers(panes(t, panesText), procs(t, procsText))
	want := []sessprobe.Owner{
		{Session: "ccy-alpha", Engine: "podman", Container: "alpha_yolo"},
		{Session: "ccy-beta", Engine: "podman", Container: "beta_yolo"},
		{Session: "ccy-deep", Engine: "docker", Container: "deep_yolo"},
		{Session: "cc-gamma"},
		{Session: "ccy-gone"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %+v", got)
	}
	if got[3].HasContainer() || !got[0].HasContainer() {
		t.Fatal("HasContainer")
	}
}

func TestSessionContainersFirstClientWinsAndHopsAreCapped(t *testing.T) {
	ps := procs(t, "100 1 bash\n110 100 podman run --name first img\n120 100 docker run --name second img")
	got := sessprobe.SessionContainers([]tmuxccy.Pane{{Session: "s", PID: 100}}, ps)
	if len(got) != 1 || got[0].Container != "first" {
		t.Fatalf("got %+v", got)
	}
	// A cycle never reaches a pane and must not spin.
	cyc := procs(t, "10 11 podman run --name c img\n11 10 bash")
	if got = sessprobe.SessionContainers([]tmuxccy.Pane{{Session: "s", PID: 99}}, cyc); got[0].HasContainer() {
		t.Fatalf("got %+v", got)
	}
	// A chain longer than 32 hops is not followed to its pane.
	var b strings.Builder
	b.WriteString("1 0 bash\n")
	for i := 2; i <= 40; i++ {
		b.WriteString(strings.Join([]string{itoa(i), itoa(i - 1), "bash\n"}, " "))
	}
	b.WriteString("41 40 podman run --name far img\n")
	if got = sessprobe.SessionContainers([]tmuxccy.Pane{{Session: "s", PID: 1}}, procs(t, b.String())); got[0].HasContainer() {
		t.Fatalf("a 40-hop chain should not reach: %+v", got)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for ; n > 0; n /= 10 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
	}
	return string(digits)
}

func TestSessionContainersEngineAndNameParsing(t *testing.T) {
	cases := []struct{ args, container string }{
		{"podman run --name=x img", "x"},
		{"/usr/bin/docker run --name x img", "x"},
		{"podman run --name", ""},
		{"podman run img", ""},
		{"podmanx run --name x img", ""},
		{"podman", ""},
		{"/opt/not-podman run --name x img", ""},
		{"podman --name=first run --name second", "first"},
	}
	for _, c := range cases {
		got := sessprobe.SessionContainers([]tmuxccy.Pane{{Session: "s", PID: 1}}, procs(t, "1 0 sh\n2 1 "+c.args))
		if got[0].Container != c.container {
			t.Errorf("%q: got %q, want %q", c.args, got[0].Container, c.container)
		}
	}
}

func TestSessionContainersEmptyPanes(t *testing.T) {
	if got := sessprobe.SessionContainers(nil, procs(t, procsText)); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

const cpuRoots = `ccy-alpha 100
ccy-alpha 700
cc-gamma 400
ccy-idle 800`

const cpuProcs = `1 0 systemd
100 1 bash -c trampoline
110 100 /bin/bash claude-yolo
111 110 podman run --name alpha_yolo img
650 1 conmon
700 650 /entrypoint.sh
701 700 claude
702 701 npm test
400 1 bash -c trampoline
410 400 claude
411 410 git status
800 1 bash -c trampoline
900 1 unrelated`

func roots(t *testing.T, text string) []sessprobe.Root {
	t.Helper()
	ps := panes(t, text)
	out := make([]sessprobe.Root, 0, len(ps))
	for _, p := range ps {
		out = append(out, sessprobe.Root{Session: p.Session, PID: p.PID})
	}
	return out
}

func TestSessionPIDsGoldenCases(t *testing.T) {
	got := sessprobe.SessionPIDs(roots(t, cpuRoots), procs(t, cpuProcs))
	want := []sessprobe.SessionProcs{
		{Session: "ccy-alpha", PIDs: []int{100, 110, 111, 700, 701, 702}},
		{Session: "cc-gamma", PIDs: []int{400, 410, 411}},
		{Session: "ccy-idle", PIDs: []int{800}},
	}
	if len(got) != 3 {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i].Session != want[i].Session || !slices.Equal(got[i].PIDs, want[i].PIDs) {
			t.Errorf("got %+v, want %+v", got[i], want[i])
		}
	}
}

func TestSessionPIDsCycleDoesNotSpin(t *testing.T) {
	got := sessprobe.SessionPIDs([]sessprobe.Root{{Session: "s", PID: 5}}, procs(t, "10 11 a\n11 10 b"))
	if len(got) != 1 || len(got[0].PIDs) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestCPUWordsGoldenCases(t *testing.T) {
	sessions := []sessprobe.SessionProcs{
		{Session: "busy", PIDs: []int{1, 2}},
		{Session: "idle", PIDs: []int{3}},
		{Session: "newcomer", PIDs: []int{9}},
		{Session: "twocores", PIDs: []int{4}},
		{Session: "gone", PIDs: []int{5}},
	}
	before := map[int]int64{1: 100, 2: 50, 3: 10, 4: 0, 5: 70}
	after := map[int]int64{1: 150, 2: 60, 3: 10, 4: 150, 9: 5}
	got, err := sessprobe.CPUWords(sessions, before, after, 1_000_000, 100)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"busy": "60%", "idle": "0%", "newcomer": "5%", "twocores": "150%", "gone": "0%"}
	for _, c := range got {
		if want[c.Session] != c.Percent {
			t.Errorf("%s: got %s, want %s", c.Session, c.Percent, want[c.Session])
		}
	}
	half, err := sessprobe.CPUWords(sessions[:1], before, after, 500_000, 100)
	if err != nil || half[0].Percent != "120%" {
		t.Fatalf("half a second: %+v, %v", half, err)
	}
}

func TestCPUWordsRoundsHalfUp(t *testing.T) {
	// 1 tick over 3 seconds at 100 Hz is 0.333% of a core: 0%. 3 ticks is 1%.
	sessions := []sessprobe.SessionProcs{{Session: "s", PIDs: []int{1}}}
	got, err := sessprobe.CPUWords(sessions, map[int]int64{1: 0}, map[int]int64{1: 1}, 3_000_000, 100)
	if err != nil || got[0].Percent != "0%" {
		t.Fatalf("got %+v, %v", got, err)
	}
	if got, err = sessprobe.CPUWords(sessions, map[int]int64{1: 0}, map[int]int64{1: 3}, 3_000_000, 100); err != nil || got[0].Percent != "1%" {
		t.Fatalf("got %+v, %v", got, err)
	}
	if got, err = sessprobe.CPUWords(sessions, map[int]int64{1: 0}, map[int]int64{1: 3}, 6_000_000, 100); err != nil || got[0].Percent != "1%" {
		t.Fatalf("half rounds up: %+v, %v", got, err)
	}
}

func TestCPUWordsNeedsTimeAndATickRate(t *testing.T) {
	_, err := sessprobe.CPUWords(nil, nil, nil, 0, 100)
	if err == nil || err.Error() != "a CPU sample needs time to pass and a tick rate; got 0 us at 100 ticks a second" {
		t.Fatalf("got %v", err)
	}
	if _, err = sessprobe.CPUWords(nil, nil, nil, 5, 0); err == nil {
		t.Fatal("zero ticks is an error")
	}
	if _, err = sessprobe.CPUWords(nil, nil, nil, -5, 100); err == nil {
		t.Fatal("a clock that went back is an error")
	}
}

func writeStat(t *testing.T, root string, pid int, line string) {
	t.Helper()
	dir := filepath.Join(root, itoa(pid))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stat"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestReadSample(t *testing.T) {
	root := t.TempDir()
	// The command name may hold spaces and parentheses: fields are counted after the last ") ".
	writeStat(t, root, 10, "10 (my (odd) name) S 1 10 10 0 -1 4194304 100 0 0 0 120 30 0 0 20 0 1 0 1000 1 1 18446744073709551615\n")
	writeStat(t, root, 11, "11 (bash) R 1 11 11 0 -1 4194304 1 0 0 0 7 3 0 0 20 0 1 0 1000 1 1 0\n")
	at := time.UnixMicro(1_700_000_000_123_456)
	got, err := sessprobe.ReadSample(root, []int{10, 11, 12}, at)
	if err != nil {
		t.Fatal(err)
	}
	if got.Micros != 1_700_000_000_123_456 || len(got.Ticks) != 2 || got.Ticks[10] != 150 || got.Ticks[11] != 10 {
		t.Fatalf("got %+v", got)
	}
	if _, present := got.Ticks[12]; present {
		t.Fatal("a pid that exited is skipped")
	}
}

// sampledAt is the instant the malformed-stat samples are taken at: fixed, not the wall clock.
var sampledAt = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

func TestReadSampleMalformed(t *testing.T) {
	root := t.TempDir()
	writeStat(t, root, 10, "10 bash S 1\n")
	if _, err := sessprobe.ReadSample(root, []int{10}, sampledAt); err == nil {
		t.Fatal("no closing paren is an error")
	}
	writeStat(t, root, 11, "11 (bash) S 1 2 3\n")
	if _, err := sessprobe.ReadSample(root, []int{11}, sampledAt); err == nil {
		t.Fatal("too few fields is an error")
	}
	writeStat(t, root, 12, "12 (bash) S 1 10 10 0 -1 4194304 1 0 0 0 x 3 0 0\n")
	if _, err := sessprobe.ReadSample(root, []int{12}, sampledAt); err == nil {
		t.Fatal("a non-numeric time is an error")
	}
	writeStat(t, root, 13, "13 (bash) S 1 10 10 0 -1 4194304 1 0 0 0 1 y 0 0\n")
	if _, err := sessprobe.ReadSample(root, []int{13}, sampledAt); err == nil {
		t.Fatal("a non-numeric system time is an error")
	}
	// A pid directory that is a file is not "exited": that is a real read failure.
	if err := os.WriteFile(filepath.Join(root, "14"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := sessprobe.ReadSample(root, []int{14}, sampledAt); err == nil {
		t.Fatal("ENOTDIR is not an exited process")
	}
}

func TestNetworkWordGoldenCases(t *testing.T) {
	known := sessprobe.ParseEnginePS(`alpha_yolo|[alpha-network]||
beta_yolo|[]||
deep_yolo|[front back]||
docker_yolo|front,back||
solo_yolo|lone-network||
bare_yolo|||`)
	cases := map[string]string{
		"alpha_yolo": "alpha-network", "beta_yolo": "none", "deep_yolo": "front,back", "docker_yolo": "front,back",
		"solo_yolo": "lone-network", "bare_yolo": "none", "": "no container", "ghost_yolo": "no container",
	}
	for container, want := range cases {
		if got := sessprobe.NetworkWord(container, known); got != want {
			t.Errorf("%q: got %q, want %q", container, got, want)
		}
	}
	if got := sessprobe.NetworkWord("alpha_yolo", nil); got != "no container" {
		t.Fatalf("an empty table is not none: %q", got)
	}
}

func TestIdentityWordsGoldenCases(t *testing.T) {
	known := sessprobe.ParseEnginePS(`alpha_yolo|n|work|key_0
beta_yolo|n|none|none
deep_yolo|n|personal|key_0 key_1
old_yolo|n||`)
	cases := map[string]sessprobe.Identity{
		"alpha_yolo": {Token: "work", Keys: "key_0"},
		"beta_yolo":  {Token: "none", Keys: "none"},
		"deep_yolo":  {Token: "personal", Keys: "key_0,key_1"},
		"old_yolo":   {Token: "unlabelled", Keys: "unlabelled"},
		"":           {Token: "-", Keys: "-"},
		"ghost_yolo": {Token: "-", Keys: "-"},
	}
	for container, want := range cases {
		if got := sessprobe.IdentityWords(container, known); got != want {
			t.Errorf("%q: got %+v, want %+v", container, got, want)
		}
	}
}

func TestParseEnginePS(t *testing.T) {
	got := sessprobe.ParseEnginePS("a|n1|t1|k1 k2\n\n|skipped|x|y\nb|n2\nc|n3|t3|k|with|bars\n")
	want := []sessprobe.Container{
		{Name: "a", Networks: "n1", Token: "t1", Keys: "k1 k2"},
		{Name: "b", Networks: "n2"},
		{Name: "c", Networks: "n3", Token: "t3", Keys: "k|with|bars"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %+v", got)
	}
}

func TestParseInspect(t *testing.T) {
	got, err := sessprobe.ParseInspect("/docker_yolo 4242\npodman_yolo 0\n\n")
	if err != nil || got["docker_yolo"] != 4242 || got["podman_yolo"] != 0 || len(got) != 2 {
		t.Fatalf("got %v, %v", got, err)
	}
	for _, bad := range []string{"name", "name x"} {
		if _, err = sessprobe.ParseInspect(bad); err == nil {
			t.Errorf("%q: want error", bad)
		}
	}
}
