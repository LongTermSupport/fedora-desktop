package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const digest = "0123456789abcdef0123456789abcdef"

type world struct {
	out, err bytes.Buffer
}

func (w *world) run(digest string, args ...string) term.ExitCode {
	return Run(Env{Args: args, Stdin: strings.NewReader(""), Stdout: &w.out, Stderr: &w.err, Digest: digest})
}

func (w *world) lastError(t *testing.T) exits.Error {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(w.err.String(), "\n"), "\n")
	e, err := exits.Decode([]byte(lines[len(lines)-1]))
	if err != nil {
		t.Fatalf("stderr %q: %v", w.err.String(), err)
	}
	return e
}

func TestDashDashVersionIsTheBareDigest(t *testing.T) {
	var w world
	if w.run(digest, "--version") != term.ExitOK || w.out.String() != digest+"\n" || w.err.Len() != 0 {
		t.Errorf("stdout %q stderr %q", w.out.String(), w.err.String())
	}
}

func TestVersionIsARecord(t *testing.T) {
	var w world
	if w.run(digest, "version") != term.ExitOK {
		t.Fatal(w.err.String())
	}
	if w.out.String() != `{"kind":"version","v":1,"digest":"`+digest+`","records":1}`+"\n" {
		t.Errorf("%q", w.out.String())
	}
}

// Both forms exit 1 without a built digest: a binary that cannot name its source is a bug.
func TestNoDigestIsInternal(t *testing.T) {
	for _, d := range []string{"", "not-a-digest"} {
		for _, args := range [][]string{{"--version"}, {"version"}} {
			var w world
			if w.run(d, args...) != term.ExitFailure || w.out.Len() != 0 {
				t.Errorf("%q %q: stdout %q", d, args, w.out.String())
			}
			if e := w.lastError(t); e.Code != exits.CodeInternal {
				t.Errorf("%+v", e)
			}
		}
	}
}

func TestAnUnbuiltVerbNamesItsSlice(t *testing.T) {
	for _, c := range []struct {
		args []string
		msg  string
	}{
		{[]string{"recv"}, "teamchat recv is not built in this binary; it lands with slice n3c"},
		{[]string{"guest", "check", "--user", "agent", "--team", "alpha"}, "teamchat guest check is not built in this binary; it lands with slice n3a guest"},
		{[]string{"hook", "stop"}, "teamchat hook is not built in this binary; it lands with slice n3e"},
		{[]string{"run", "--plain", "--kind", "other", "--", "bash"}, "teamchat run is not built in this binary; it lands with slice n3c"},
	} {
		var w world
		if w.run(digest, c.args...) != term.ExitFailure || w.out.Len() != 0 {
			t.Errorf("%q: stdout %q", c.args, w.out.String())
		}
		e := w.lastError(t)
		if e.Code != exits.CodeInternal || e.Message != c.msg {
			t.Errorf("%q: %+v", c.args, e)
		}
	}
	var w world
	w.run(digest, "recv", "--team", "alpha")
	if e := w.lastError(t); e.Team == nil || e.Team.String() != "alpha" {
		t.Errorf("the team was not carried: %+v", e)
	}
}

func TestAUsageErrorIs64(t *testing.T) {
	var w world
	if w.run(digest, "--team", "alpha", "recv") != term.ExitUsage {
		t.Error("not 64")
	}
	if e := w.lastError(t); e.Code != exits.CodeUsage {
		t.Errorf("%+v", e)
	}
}

func TestHelpListsEveryVerb(t *testing.T) {
	var w world
	if w.run(digest, "help") != term.ExitOK {
		t.Fatal(w.err.String())
	}
	for _, v := range Verbs() {
		if !strings.Contains(w.out.String(), Usage(v)+"\n") {
			t.Errorf("help does not show %q", Usage(v))
		}
	}
	if !strings.Contains(w.out.String(), "teamchat help exit-codes") {
		t.Error("help does not point at the exit codes")
	}
}

func TestHelpOfOneVerb(t *testing.T) {
	for _, args := range [][]string{{"help", "reply"}, {"help", "seat", "set-aside"}} {
		var w world
		if w.run(digest, args...) != term.ExitOK {
			t.Fatal(w.err.String())
		}
		v, ok := lookup(strings.Join(args[1:], " "))
		if !ok || !strings.HasPrefix(w.out.String(), Usage(v)+"\n") || !strings.Contains(w.out.String(), v.Does) {
			t.Errorf("%q: %q", args, w.out.String())
		}
	}
}

func TestHelpOfNoSuchVerbIs64(t *testing.T) {
	for _, args := range [][]string{{"help", "nonsense"}, {"help", "seat"}, {"help", "recv", "x"}} {
		var w world
		if w.run(digest, args...) != term.ExitUsage {
			t.Errorf("%q: not 64", args)
		}
	}
}

// CL4: help exit-codes prints the table from the same data.
func TestHelpExitCodesIsTheTable(t *testing.T) {
	var w world
	if w.run(digest, "help", "exit-codes") != term.ExitOK {
		t.Fatal(w.err.String())
	}
	for _, r := range exits.Table() {
		line := helpExitLine(r)
		if !strings.Contains(w.out.String(), line+"\n") {
			t.Errorf("no line %q", line)
		}
	}
	for _, c := range exits.Classes {
		if !strings.Contains(w.out.String(), c.Does()) {
			t.Errorf("class %s not shown", c)
		}
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

func TestAFailedStdoutIsInternal(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"help", "recv"}, {"help", "exit-codes"}, {"version"}, {"--version"}} {
		var errOut bytes.Buffer
		if Run(Env{Args: args, Stdout: failWriter{}, Stderr: &errOut, Digest: digest}) != term.ExitFailure {
			t.Errorf("%q: not internal", args)
		}
	}
}
