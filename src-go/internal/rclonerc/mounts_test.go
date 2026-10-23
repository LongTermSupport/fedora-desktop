package rclonerc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

const findmntLine = "findmnt -J -l -o SOURCE,TARGET -t fuse.rclone"

func findmntJSON(f *execx.Fake, body string) {
	f.On([]string{"findmnt", "-J"}, execx.Result{Stdout: []byte(body)})
}

// Mounts: findmnt's rclone mounts in its order, each with its name, remote path and the
// arguments of the process serving it (SPEC-rclone-tail D3, E4, E5).
func TestMounts(t *testing.T) {
	f := &execx.Fake{}
	findmntJSON(f, `{"filesystems":[`+
		`{"source":"lib:/Photos","target":"/home/u/mnt/lib"},`+
		`{"source":"other:","target":"/home/u/mnt/other"},`+
		`{"source":"bare","target":"/home/u/mnt/with space"}]}`)
	proc := mountProc()
	proc["50/cmdline"] = cmdline("rclone", "mount", "bare", "/home/u/mnt/with space")
	c := client(t, f, "", proc)
	mounts, err := c.Mounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if f.Calls()[0].Line() != findmntLine {
		t.Fatalf("argv %q", f.Calls()[0].Line())
	}
	if len(mounts) != 3 {
		t.Fatalf("%+v", mounts)
	}
	want := []struct{ name, remote, target, arg string }{
		{"lib", "Photos", "/home/u/mnt/lib", "--rc-addr=localhost:5573"},
		{"other", "", "/home/u/mnt/other", "--rc-addr=localhost:5572"},
		{"bare", "", "/home/u/mnt/with space", "bare"},
	}
	for i, w := range want {
		m := mounts[i]
		if m.Name != w.name || m.RemotePath != w.remote || m.Target != w.target || !slices.Contains(m.Args, w.arg) {
			t.Errorf("mount %d: %+v, want %+v", i, m, w)
		}
	}
}

// findmnt's exit 1 with nothing on stdout is no mount; any other failure is an error.
func TestMountsNoneAndFailures(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"findmnt"}, execx.Result{ExitCode: 1})
	c := client(t, f, "", mountProc())
	if mounts, err := c.Mounts(context.Background()); err != nil || len(mounts) != 0 {
		t.Fatalf("none: %+v %v", mounts, err)
	}
	cases := map[string]execx.Result{
		"exit 2":               {ExitCode: 2, Stderr: []byte("findmnt: bad usage\n")},
		"exit 1 with output":   {ExitCode: 1, Stdout: []byte("{}")},
		"not json":             {Stdout: []byte("not json")},
		"two values":           {Stdout: []byte(`{"filesystems":[]} {}`)},
		"filesystems not list": {Stdout: []byte(`{"filesystems":{}}`)},
	}
	for name, res := range cases {
		t.Run(name, func(t *testing.T) {
			f := &execx.Fake{}
			f.On([]string{"findmnt"}, res)
			c := client(t, f, "", mountProc())
			if _, err := c.Mounts(context.Background()); err == nil ||
				!strings.HasPrefix(err.Error(), "findmnt could not list the rclone mounts: ") {
				t.Fatalf("got %v", err)
			}
		})
	}
}

// /proc that cannot be read is an error, not a list of mounts with no RC.
func TestMountsUnreadableProc(t *testing.T) {
	f := &execx.Fake{}
	findmntJSON(f, `{"filesystems":[{"source":"lib:","target":"/home/u/mnt/lib"}]}`)
	c := client(t, f, "", nil)
	c.Proc = os.DirFS(filepath.Join(t.TempDir(), "absent"))
	if _, err := c.Mounts(context.Background()); err == nil || !strings.Contains(err.Error(), "read /proc") {
		t.Fatalf("got %v", err)
	}
}

// RCAddr: none is "", one is it, several is the refusal (SPEC-rclone-tail E5).
func TestMountRCAddr(t *testing.T) {
	cases := []struct {
		args       []string
		want, fail string
	}{
		{nil, "", ""},
		{[]string{"rclone", "mount", "x:", "/m"}, "", ""},
		{[]string{"rclone", "mount", "x:", "/m", "--rc-addr=h:1"}, "h:1", ""},
		{[]string{"rclone", "mount", "x:", "/m", "--rc-addr=a:1", "--rc-addr=b:2"}, "", "more than one --rc-addr: a:1 b:2"},
	}
	for _, tc := range cases {
		got, err := Mount{Args: tc.args}.RCAddr()
		if tc.fail != "" {
			if err == nil || err.Error() != tc.fail {
				t.Errorf("%q: got %q %v, want error %q", tc.args, got, err, tc.fail)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("%q: got %q %v, want %q", tc.args, got, err, tc.want)
		}
	}
}

func failure(t *testing.T, err error) *RCError {
	t.Helper()
	var f *RCError
	if !errors.As(err, &f) {
		t.Fatalf("not a Failure: %v", err)
	}
	return f
}

// Ask: the answer, or a Failure whose reason is the report's text and whose detail is for
// stderr (SPEC-rclone-tail E8, SPEC-rclone-cache-status E12).
func TestAsk(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"rclone", "rc", "--url=http://" + testAddr, "vfs/stats"}, execx.Result{Stdout: []byte("{}")})
	c := client(t, f, authLines, nil)
	out, err := c.Ask(context.Background(), testAddr, "vfs/stats")
	if err != nil || string(out) != "{}" {
		t.Fatalf("%q %v", out, err)
	}
	cases := []struct {
		name         string
		res          execx.Result
		err          error
		reason, said string
	}{
		{
			"401",
			execx.Result{ExitCode: 1, Stderr: []byte("Failed to rc: HTTP error 401: Unauthorized\n")},
			nil,
			"rc rejected credentials (401)", "Failed to rc: HTTP error 401: Unauthorized",
		},
		{
			"Unauthorized on stdout",
			execx.Result{ExitCode: 1, Stdout: []byte("Unauthorized\n")},
			nil,
			"rc rejected credentials (401)", "Unauthorized",
		},
		{
			"refused",
			execx.Result{ExitCode: 1, Stderr: []byte("connection refused\n")},
			nil,
			"rc unreachable", "connection refused",
		},
		{"no output", execx.Result{}, errors.New("exec: no such file"), "rc unreachable", "exec: no such file"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &execx.Fake{}
			f.OnFunc([]string{"rclone", "rc"}, func(execx.Cmd) (execx.Result, error) { return tc.res, tc.err })
			c := client(t, f, authLines, nil)
			_, err := c.Ask(context.Background(), testAddr, "core/stats")
			if fl := failure(t, err); fl.Reason != tc.reason || fl.Detail != tc.said || fl.Error() != tc.reason {
				t.Fatalf("%+v", fl)
			}
		})
	}
}

// A missing credential is its own reason, the advice its detail; rclone is not run.
func TestAskCredentialMissing(t *testing.T) {
	f := &execx.Fake{}
	c := client(t, f, "", nil)
	_, err := c.Ask(context.Background(), testAddr, "vfs/stats")
	fl := failure(t, err)
	if fl.Reason != "rc credential missing — run play-rclone.yml" || !strings.Contains(fl.Detail, "rclone RC credential file not found") {
		t.Fatalf("%+v", fl)
	}
	if len(f.Calls()) != 0 {
		t.Fatalf("rclone ran: %v", f.Calls())
	}
}

// A call that fails because the run was stopped is the context's error, not a Failure: it is
// neither printed nor diagnosed (SPEC-rclone-tail E15).
func TestAskCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	f := &execx.Fake{}
	f.OnFunc([]string{"rclone", "rc"}, func(execx.Cmd) (execx.Result, error) {
		cancel()
		return execx.Result{}, errors.New("signal: killed")
	})
	c := client(t, f, authLines, fstest.MapFS{})
	_, err := c.Ask(ctx, testAddr, "vfs/stats")
	var fl *RCError
	if !errors.Is(err, context.Canceled) || errors.As(err, &fl) {
		t.Fatalf("got %v", err)
	}
}
