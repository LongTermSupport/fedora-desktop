package sessions

import (
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// probeTables scripts the host the detail probe reads: two sessions, one with a container.
func probeTables(h *harness, home string) {
	h.sessions("ccy-a 0 "+home+"/proj", "cc-b 1 /srv/other dir")
	h.tmuxOut("list-panes", "ccy-a 100\ncc-b 200\n")
	h.fake.On([]string{"ps"}, execx.Result{Stdout: []byte(strings.Join([]string{
		"100 1 bash -c trampoline", "110 100 podman run --rm -it --name a_yolo img claude",
		"120 1 /entrypoint.sh", "200 1 bash -c trampoline", "210 200 claude",
	}, "\n") + "\n")})
	h.fake.On([]string{"podman", "ps"}, execx.Result{Stdout: []byte("a_yolo|[a-net]|work|key_0\n")})
	h.fake.On([]string{"podman", "inspect"}, execx.Result{Stdout: []byte("a_yolo 120\n")})
}

func TestListPrintsTheHeadingThenOneRowPerSessionOnStdout(t *testing.T) {
	h := newHarness(t)
	probeTables(h, h.env["HOME"])
	exitCodeIs(t, h.run("--list"), 0, h)
	lines := strings.Split(strings.TrimSuffix(h.stdout(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("heading and two rows, got %q", h.stdout())
	}
	heading := "SESSION                       STATE              CPU  NETWORK                 TOKEN             SSH KEY           DIRECTORY"
	if lines[0] != heading {
		t.Fatalf("heading %q", lines[0])
	}
	fields := strings.Fields(lines[1])
	if fields[0] != "ccy-a" || fields[1] != "detached" || fields[3] != "a-net" || fields[4] != "work" || fields[5] != "key_0" || fields[6] != "~/proj" {
		t.Fatalf("ccy row %q", lines[1])
	}
	if !strings.HasSuffix(lines[2], "/srv/other dir") || !strings.Contains(lines[2], "open elsewhere") ||
		!strings.Contains(lines[2], "no container") {
		t.Fatalf("cc row %q (a directory may hold a space)", lines[2])
	}
	if !strings.HasPrefix(lines[1], "ccy-a"+strings.Repeat(" ", 23)+"  detached") {
		t.Fatalf("columns are padded: %q", lines[1])
	}
}

// A redraw asks the host each question once: the same command twice is a wasted process.
func TestOneDrawRunsNoCommandTwice(t *testing.T) {
	h := newHarness(t)
	probeTables(h, h.env["HOME"])
	exitCodeIs(t, h.run("--list"), 0, h)
	seen := map[string]bool{}
	for _, line := range h.ran() {
		if seen[line] {
			t.Errorf("%q ran twice in one draw: %v", line, h.ran())
		}
		seen[line] = true
	}
	if len(seen) == 0 {
		t.Fatal("nothing ran")
	}
}

func TestListWithNoSessionsIsSilentOnStdout(t *testing.T) {
	h := newHarness(t)
	h.sessions()
	exitCodeIs(t, h.run("-l"), 0, h)
	if h.stdout() != "" || !strings.Contains(h.stderr(), "No CCY or cc sessions are running.") {
		t.Fatalf("stdout %q stderr %q", h.stdout(), h.stderr())
	}
}

func TestListWithAnUnreadableSessionListFails(t *testing.T) {
	h := newHarness(t)
	h.tmuxFail("list-sessions", "lost server")
	h.tmuxOut("list-panes", "")
	exitCodeIs(t, h.run("--list"), 1, h)
	contains(t, "unreadable list", h.stderr(), "ERROR: tmux list-sessions failed")
	if h.stdout() != "" {
		t.Fatalf("stdout %q", h.stdout())
	}
}

func TestAFailedProbeLeavesTheColumnsUnknownAndSaysSo(t *testing.T) {
	h := newHarness(t)
	h.sessions("ccy-a 0 /srv/a")
	h.tmuxOut("list-panes", "ccy-a 100\n")
	h.fake.On([]string{"ps"}, execx.Result{ExitCode: 1, Stderr: []byte("ps: broken")})
	exitCodeIs(t, h.run("--list"), 0, h)
	contains(t, "probe failure", h.stderr(), "the process table could not be read",
		"The CPU, network, token and SSH key columns read 'unknown' for every session — see the error above.")
	fields := strings.Fields(strings.Split(h.stdout(), "\n")[1])
	if fields[2] != "unknown" || fields[3] != "unknown" || fields[4] != "unknown" || fields[5] != "unknown" {
		t.Fatalf("row %v", fields)
	}
}

func TestASessionMissingFromTheProbeReadsUnknown(t *testing.T) {
	h := newHarness(t)
	h.sessions("ccy-a 0 /srv/a")
	h.tmuxOut("list-panes", "")
	exitCodeIs(t, h.run("--list"), 0, h)
	row := strings.Split(h.stdout(), "\n")[1]
	if strings.Count(row, "unknown") != 4 {
		t.Fatalf("a session with no pane is unknown in all four columns: %q", row)
	}
}

func TestRowTextAbbreviatesOnlyAtADirectoryBoundary(t *testing.T) {
	// Spec bug 9.10: /srv/u2/x is not under /srv/u, so it must not read ~2/x.
	r := tableRow{Name: "n", Dir: "/srv/u2/x", Network: "a", Token: "t", Keys: "k", CPU: "1%"}
	if got := r.text("/srv/u"); !strings.HasSuffix(got, "/srv/u2/x") {
		t.Fatalf("got %q", got)
	}
	r.Dir = "/srv/u/x"
	if got := r.text("/srv/u"); !strings.HasSuffix(got, "~/x") {
		t.Fatalf("got %q", got)
	}
	r.Dir = "/srv/u"
	if got := r.text("/srv/u"); !strings.HasSuffix(got, "~") {
		t.Fatalf("got %q", got)
	}
	if !r.elsewhere() == false {
		t.Fatal("zero attached is not elsewhere")
	}
	r.Attached = 2
	if !strings.Contains(r.text(""), "open elsewhere") || !r.elsewhere() {
		t.Fatal("an attached session is open elsewhere")
	}
}
