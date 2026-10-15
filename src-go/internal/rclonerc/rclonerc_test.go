package rclonerc

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

const testAddr = "localhost:5573"

// client is a Client over a fake runner, a credential file in a temporary home, and /proc as
// the given map.
func client(t *testing.T, f *execx.Fake, cred string, proc fstest.MapFS) *Client {
	t.Helper()
	home := t.TempDir()
	if cred != "" {
		dir := filepath.Join(home, ".config", "rclone")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "rc-auth.env"), []byte(cred), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	env := map[string]string{"HOME": home}
	return &Client{Runner: f, Getenv: func(k string) string { return env[k] }, Proc: proc}
}

const authLines = "# generated\nRCLONE_RC_USER=rcuser\nRCLONE_RC_PASS=pa=ss\n"

// E27: the credential reaches rclone in the child's environment, never in its arguments.
func TestCallPassesTheCredentialInTheEnvironment(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"rclone", "rc"}, execx.Result{Stdout: []byte("{}")})
	c := client(t, f, authLines, nil)
	res, err := c.Call(context.Background(), testAddr, "vfs/stats")
	if err != nil || string(res.Stdout) != "{}" {
		t.Fatalf("%q %v", res.Stdout, err)
	}
	call := f.Calls()[0]
	if call.Line() != "rclone rc --url=http://localhost:5573 vfs/stats" {
		t.Errorf("argv %q", call.Line())
	}
	if !slices.Equal(call.Env, []string{"RCLONE_USER=rcuser", "RCLONE_PASS=pa=ss"}) {
		t.Errorf("env %q", call.Env)
	}
	if strings.Contains(call.Line(), "pa=ss") {
		t.Error("the password is in argv")
	}
	// Read once per run (E26): a second call does not reread a file that has since gone.
	if err := os.RemoveAll(filepath.Join(c.Getenv("HOME"), ".config")); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Call(context.Background(), testAddr, "core/stats"); err != nil {
		t.Fatalf("second call: %v", err)
	}
}

// E26: RCLONE_RC_AUTH_FILE overrides the default path.
func TestCredentialFileOverride(t *testing.T) {
	f := &execx.Fake{}
	c := client(t, f, "", nil)
	path := filepath.Join(t.TempDir(), "auth.env")
	if err := os.WriteFile(path, []byte("RCLONE_RC_USER=u\nRCLONE_RC_PASS=p\nRCLONE_RC_USER=other\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c.Getenv = func(k string) string {
		if k == "RCLONE_RC_AUTH_FILE" {
			return path
		}
		return ""
	}
	user, pass, err := c.Credential()
	if err != nil || user != "u" || pass != "p" {
		t.Fatalf("%q %q %v", user, pass, err)
	}
}

func TestCredentialMissingFileNamesThePlay(t *testing.T) {
	c := client(t, &execx.Fake{}, "", nil)
	_, err := c.Call(context.Background(), testAddr, "core/stats")
	if err == nil || !strings.Contains(err.Error(), "rclone RC credential file not found: ") ||
		!strings.Contains(err.Error(), "play-rclone.yml") || !strings.Contains(err.Error(), "Do NOT create it by hand.") {
		t.Fatalf("got %v", err)
	}
}

func TestCredentialEmptyValueIsRefused(t *testing.T) {
	c := client(t, &execx.Fake{}, "RCLONE_RC_USER=u\nRCLONE_RC_PASS=\n", nil)
	_, _, err := c.Credential()
	if err == nil || !strings.Contains(err.Error(), "has no RCLONE_RC_USER/RCLONE_RC_PASS value.") {
		t.Fatalf("got %v", err)
	}
}

// E28: core/stats is the probe; its failure carries rclone's merged output.
func TestAvailable(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"rclone", "rc", "--url=http://" + testAddr, "core/stats"}, execx.Result{})
	c := client(t, f, authLines, nil)
	if err := c.Available(context.Background(), testAddr); err != nil {
		t.Fatal(err)
	}
	f.On([]string{"rclone", "rc"}, execx.Result{Stderr: []byte("HTTP 401\n"), ExitCode: 1})
	err := c.Available(context.Background(), testAddr)
	if err == nil || !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("got %v", err)
	}
}

func cmdline(args ...string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte(strings.Join(args, "\x00") + "\x00")}
}

func mountProc() fstest.MapFS {
	return fstest.MapFS{
		"self/cmdline": cmdline("ftp-camera"),
		"7/cmdline":    cmdline("bash"),
		"12/cmdline":   cmdline("/usr/bin/rclone", "mount", "other:", "/home/u/mnt/other", "--rc-addr=localhost:5572"),
		"40/cmdline":   cmdline("/usr/bin/rclone", "mount", "lib:", "/home/u/mnt/lib", "--rc", "--rc-addr=localhost:5573"),
	}
}

func findmntRoot(f *execx.Fake, path, root string) {
	f.On([]string{"findmnt", "-n", "-o", "TARGET", "--target", path}, execx.Result{Stdout: []byte(root + "\n")})
}

// E25: the mount root of the path, the rclone process serving it, its one --rc-addr.
func TestAddrForMount(t *testing.T) {
	f := &execx.Fake{}
	findmntRoot(f, "/home/u/mnt/lib/PHOTO", "/home/u/mnt/lib")
	c := client(t, f, "", mountProc())
	addr, err := c.AddrForMount(context.Background(), "/home/u/mnt/lib/PHOTO")
	if err != nil || addr != testAddr {
		t.Fatalf("%q %v", addr, err)
	}
}

func TestAddrForMountRefusals(t *testing.T) {
	cases := []struct {
		name string
		proc fstest.MapFS
		root string
		want string
	}{
		{"no rclone mount at all", fstest.MapFS{"7/cmdline": cmdline("bash")}, "/home/u/mnt/lib", "no rclone mount process is running"},
		{"nobody serves the root", mountProc(), "/home/u/mnt/none", "no rclone mount process serves /home/u/mnt/none"},
		{"no --rc-addr", fstest.MapFS{"9/cmdline": cmdline("rclone", "mount", "lib:", "/home/u/mnt/lib")}, "/home/u/mnt/lib", "was started without --rc-addr"},
		{"two --rc-addr", fstest.MapFS{"9/cmdline": cmdline("rclone", "mount", "lib:", "/home/u/mnt/lib", "--rc-addr=a:1", "--rc-addr=b:2")}, "/home/u/mnt/lib", "has more than one --rc-addr: a:1 b:2"},
		{"empty findmnt answer", mountProc(), "", "findmnt named no mount for /x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &execx.Fake{}
			findmntRoot(f, "/x", tc.root)
			c := client(t, f, "", tc.proc)
			_, err := c.AddrForMount(context.Background(), "/x")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}

func TestAddrForMountPathOutsideAnyMount(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"findmnt"}, execx.Result{ExitCode: 1})
	c := client(t, f, "", mountProc())
	_, err := c.AddrForMount(context.Background(), "/x")
	if err == nil || !strings.Contains(err.Error(), "/x is not inside any mount") {
		t.Fatalf("got %v", err)
	}
	if _, err := c.AddrForMount(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "no mountpoint given") {
		t.Fatalf("empty: %v", err)
	}
}

// A process that ends between the listing and the read is passed over.
func TestAddrForMountSkipsAVanishedProcess(t *testing.T) {
	f := &execx.Fake{}
	findmntRoot(f, "/home/u/mnt/lib", "/home/u/mnt/lib")
	proc := mountProc()
	proc["3"] = &fstest.MapFile{Mode: 0o555 | os.ModeDir}
	c := client(t, f, "", proc)
	if addr, err := c.AddrForMount(context.Background(), "/home/u/mnt/lib"); err != nil || addr != testAddr {
		t.Fatalf("%q %v", addr, err)
	}
}
