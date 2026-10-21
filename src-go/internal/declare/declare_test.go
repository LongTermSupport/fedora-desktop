package declare

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

const prefix = "env PYTHONPATH=/root /usr/bin/python3 -P -m helpers.agent_bus.declare"

func newClient(f *execx.Fake) *Client {
	return &Client{Runner: f, Root: "/root", File: "/hv.yml"}
}

func TestNewClientDefaultsErrToTheProcessStderr(t *testing.T) {
	c := NewClient(&execx.Fake{}, "/root", "/hv.yml")
	if c.Err != os.Stderr || c.Root != "/root" || c.File != "/hv.yml" {
		t.Fatalf("got %+v", c)
	}
}

func TestSuccessfulRunStderrIsForwardedEvenWithNoErrWriter(t *testing.T) {
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	processStderr := os.Stderr
	os.Stderr = pw
	f := &execx.Fake{}
	f.On([]string{"env"}, execx.Result{Stdout: []byte("CHANGED\tx\n"), Stderr: []byte("kept at /hv.yml.backup.1\n")})
	_, runErr := newClient(f).Remove(context.Background(), "alpha", false)
	os.Stderr = processStderr
	if closeErr := pw.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if runErr != nil {
		t.Fatal(runErr)
	}
	got, readErr := io.ReadAll(pr)
	if readErr != nil || string(got) != "kept at /hv.yml.backup.1\n" {
		t.Fatalf("stderr %q err %v", got, readErr)
	}
}

func script(f *execx.Fake, sub, stdout string) {
	f.On([]string{"env", "PYTHONPATH=/root", "/usr/bin/python3", "-P", "-m", "helpers.agent_bus.declare", sub},
		execx.Result{Stdout: []byte(stdout)})
}

func lastLine(t *testing.T, f *execx.Fake) string {
	t.Helper()
	calls := f.Calls()
	if len(calls) == 0 {
		t.Fatal("nothing ran")
	}
	last := calls[len(calls)-1]
	if last.Dir != "" {
		t.Fatalf("declare must run in the caller's directory, ran in %q", last.Dir)
	}
	return last.Line()
}

func TestStatusSortsInstalledAndParsesRows(t *testing.T) {
	f := &execx.Fake{}
	script(f, "status", "STATUS\talpha\tcurrent\t-\nSTATUS\tbeta\toutdated\tconfig,software\n")
	got, err := newClient(f).Status(context.Background(), "/src", "/applied", []string{"beta", "alpha"})
	if err != nil {
		t.Fatal(err)
	}
	want := []Status{{Team: "alpha", State: "current"}, {Team: "beta", State: "outdated", Detail: "config,software"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	line := prefix + " status --file /hv.yml --source /src --applied /applied --installed alpha --installed beta"
	if got := lastLine(t, f); got != line {
		t.Fatalf("ran %q", got)
	}
}

func TestStatusRejectsAShortLine(t *testing.T) {
	f := &execx.Fake{}
	script(f, "status", "STATUS\talpha\tcurrent\n")
	if _, err := newClient(f).Status(context.Background(), "/s", "/a", nil); err == nil {
		t.Fatal("accepted a short STATUS line")
	}
}

func TestAddRepo(t *testing.T) {
	f := &execx.Fake{}
	script(f, "add-repo", "CHANGED\tteam beta trusts example-org/proj\n")
	got, err := newClient(f).AddRepo(context.Background(), "beta", "example-org/proj")
	if err != nil || !got.Changed || got.Text != "team beta trusts example-org/proj" {
		t.Fatalf("%+v %v", got, err)
	}
	if line := lastLine(t, f); line != prefix+" add-repo --file /hv.yml --team beta --repo example-org/proj" {
		t.Fatalf("ran %q", line)
	}
}

func TestChecks(t *testing.T) {
	f := &execx.Fake{}
	script(f, "check-address", "")
	script(f, "check", "")
	c := newClient(f)
	ctx := context.Background()
	if err := c.CheckAddress(ctx, "192.0.2.10"); err != nil {
		t.Fatal(err)
	}
	if line := lastLine(t, f); line != prefix+" check-address --address 192.0.2.10" {
		t.Fatalf("ran %q", line)
	}
	e := Entry{
		Team: "alpha", Humans: []string{"a", "b"}, Org: "example-org",
		Repos: []string{"example-org/x", "example-org/y"}, AllowFrom: []string{"192.0.2.0/24"},
		Description: "Builds the site",
	}
	if err := c.Check(ctx, e); err != nil {
		t.Fatal(err)
	}
	want := prefix + " check --file /hv.yml --team alpha --human a --human b --org example-org" +
		" --repo example-org/x --repo example-org/y --allow-from 192.0.2.0/24 --description Builds the site"
	if line := lastLine(t, f); line != want {
		t.Fatalf("ran %q", line)
	}
	if err := c.Check(ctx, Entry{Repos: []string{"example-org/x"}}); err != nil {
		t.Fatal(err)
	}
	if line := lastLine(t, f); line != prefix+" check --file /hv.yml --repo example-org/x" {
		t.Fatalf("ran %q", line)
	}
}

func TestDescribeSetsAndReportsUnchanged(t *testing.T) {
	f := &execx.Fake{}
	script(f, "describe", "CHANGED\tteam alpha description\n")
	c := newClient(f)
	got, err := c.Describe(context.Background(), "alpha", "Builds the site")
	if err != nil || !got.Changed || got.Text != "team alpha description" {
		t.Fatalf("%+v %v", got, err)
	}
	if line := lastLine(t, f); line != prefix+" describe --file /hv.yml --team alpha --description Builds the site" {
		t.Fatalf("ran %q", line)
	}
	script(f, "describe", "UNCHANGED\talpha\n")
	if got, err = c.Describe(context.Background(), "alpha", "Builds the site"); err != nil || got.Changed {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestCreate(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"env"}, execx.Result{
		Stdout: []byte("CHANGED\tteam alpha, port 6167, in /hv.yml\n"),
		Stderr: []byte("Team alpha is declared in /hv.yml; the file as it was is kept at /hv.yml.backup.1.\n"),
	})
	errw := &bytes.Buffer{}
	c := newClient(f)
	c.Err = errw
	e := Entry{Team: "alpha", Humans: []string{"owner"}, Org: "example-org", Address: "192.0.2.10", Description: "Builds the site"}
	got, err := c.Create(context.Background(), e)
	if err != nil || !got.Changed || got.Text != "team alpha, port 6167, in /hv.yml" {
		t.Fatalf("%+v %v", got, err)
	}
	want := prefix + " create --file /hv.yml --team alpha --human owner --org example-org --address 192.0.2.10" +
		" --description Builds the site"
	if line := lastLine(t, f); line != want {
		t.Fatalf("ran %q", line)
	}
	if !strings.Contains(errw.String(), "kept at /hv.yml.backup.1") {
		t.Fatalf("the backup note was not passed on: %q", errw.String())
	}
}

func TestCreateDryRunReturnsTheEntryText(t *testing.T) {
	f := &execx.Fake{}
	script(f, "create", "  - team: alpha\n    port: 6167\n")
	e := Entry{Team: "alpha", Humans: []string{"o"}, Repos: []string{"example-org/x"}}
	got, err := newClient(f).CreateDryRun(context.Background(), e)
	if err != nil || got != "  - team: alpha\n    port: 6167\n" {
		t.Fatalf("%q %v", got, err)
	}
	want := prefix + " create --file /hv.yml --team alpha --human o --repo example-org/x --dry-run"
	if line := lastLine(t, f); line != want {
		t.Fatalf("ran %q", line)
	}
}

func TestElementChangedAndUnchanged(t *testing.T) {
	f := &execx.Fake{}
	script(f, "element", "CHANGED\tElement profile for alpha, in /hv.yml\n")
	c := newClient(f)
	got, err := c.Element(context.Background(), "alpha")
	if err != nil || !got.Changed || got.Text != "Element profile for alpha, in /hv.yml" {
		t.Fatalf("%+v %v", got, err)
	}
	if line := lastLine(t, f); line != prefix+" element --file /hv.yml --team alpha" {
		t.Fatalf("ran %q", line)
	}
	script(f, "element", "UNCHANGED\talpha\n")
	got, err = c.Element(context.Background(), "alpha")
	if err != nil || got.Changed || got.Text != "alpha" {
		t.Fatalf("%+v %v", got, err)
	}
	script(f, "element", "nothing useful\n")
	if _, err = c.Element(context.Background(), "alpha"); err == nil {
		t.Fatal("accepted output with neither CHANGED nor UNCHANGED")
	}
}

func TestDropLimitAndRemove(t *testing.T) {
	f := &execx.Fake{}
	script(f, "drop-limit", "CHANGED\tteam alpha may name any path, in /hv.yml\n")
	script(f, "remove", "CHANGED\tteam alpha absent, its data kept (purge: false), in /hv.yml\n")
	c := newClient(f)
	ctx := context.Background()
	got, err := c.DropLimit(ctx, "alpha")
	if err != nil || !got.Changed {
		t.Fatalf("%+v %v", got, err)
	}
	if line := lastLine(t, f); line != prefix+" drop-limit --file /hv.yml --team alpha" {
		t.Fatalf("ran %q", line)
	}
	if _, err = c.Remove(ctx, "alpha", false); err != nil {
		t.Fatal(err)
	}
	if line := lastLine(t, f); line != prefix+" remove --file /hv.yml --team alpha" {
		t.Fatalf("ran %q", line)
	}
	if _, err = c.Remove(ctx, "alpha", true); err != nil {
		t.Fatal(err)
	}
	if line := lastLine(t, f); line != prefix+" remove --file /hv.yml --team alpha --purge" {
		t.Fatalf("ran %q", line)
	}
}

func TestExitCodesBecomeTypedErrors(t *testing.T) {
	cases := []struct {
		code int
		kind error
	}{
		{64, ErrUsage}, {78, ErrRefused}, {130, ErrInterrupted}, {3, ErrFailed},
	}
	for _, tc := range cases {
		f := &execx.Fake{}
		f.On([]string{"env"}, execx.Result{ExitCode: tc.code, Stderr: []byte("declare: --team: nope\n")})
		err := newClient(f).CheckAddress(context.Background(), "x")
		if !errors.Is(err, tc.kind) {
			t.Fatalf("exit %d: %v", tc.code, err)
		}
		failure, ok := errors.AsType[*Error](err)
		if !ok || failure.Code != tc.code || failure.Message() != "--team: nope" || failure.Error() != "declare: --team: nope" {
			t.Fatalf("exit %d: %+v", tc.code, failure)
		}
	}
	if (&Error{Code: 3}).Error() != "declare exited 3" {
		t.Fatal("empty stderr text")
	}
}

func TestApplyStatesAreDeclaresWords(t *testing.T) {
	want := map[ApplyState]string{
		ApplyCurrent: "current", ApplyOutdated: "outdated", ApplyUnbuilt: "unbuilt",
		ApplyRemoved: "removed", ApplyDecide: "decide", ApplyUnreadable: "unreadable", ApplyAbsent: "",
	}
	for state, word := range want {
		if string(state) != word {
			t.Errorf("%q is not %q", string(state), word)
		}
	}
}

// SPEC-many-teams.md S-F2: a SETTING line per key; a key the entry does not set has no value.
func TestSetPassesEveryValueInOneRun(t *testing.T) {
	f := &execx.Fake{}
	script(f, "set", "CHANGED\tteam alpha: humans, org\n")
	c := newClient(f)
	values := []Value{{Key: "humans", JSON: `["owner","second"]`}, {Key: "org", JSON: `""`}}
	got, err := c.Set(context.Background(), "alpha", values)
	if err != nil || !got.Changed || got.Text != "team alpha: humans, org" {
		t.Fatalf("%+v %v", got, err)
	}
	want := prefix + ` set --file /hv.yml --team alpha --value humans ["owner","second"] --value org ""`
	if line := lastLine(t, f); line != want {
		t.Fatalf("ran %q", line)
	}
	if err := c.CheckSet(context.Background(), "alpha", values); err != nil {
		t.Fatal(err)
	}
	if line := lastLine(t, f); line != want+" --check" {
		t.Fatalf("ran %q", line)
	}
}
