package ftpcamera

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	testRemote = "lib:/PHOTO/LIBRARY"
	testRCAddr = "localhost:5573"
)

func mountRoot(k *kit) string { return filepath.Join(filepath.Dir(k.config), "mnt", "lib") }

// remoteDest is where the remote's path lands inside the mount.
func remoteDest(k *kit) string { return filepath.Join(mountRoot(k), "PHOTO", "LIBRARY") }

// remoteKit is a kit with the remote configured, the mount up serving the remote's root, its
// rclone process on --rc-addr, and the RC credential written.
func remoteKit(t *testing.T) *kit {
	t.Helper()
	k := newKit(t)
	k.path("rclone", "exiftool")
	k.writeConfig("RCLONE_REMOTE=" + testRemote + "\n")
	root, dest := mountRoot(k), remoteDest(k)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	k.fake.On([]string{"findmnt", "-n", "-o", "TARGET", "-t", "fuse.rclone"}, execx.Result{Stdout: []byte("/elsewhere\n/broken\n" + root + "\n")})
	k.fake.On([]string{"findmnt", "-n", "-o", "SOURCE", "/elsewhere"}, execx.Result{Stdout: []byte("other:\n")})
	k.fake.On([]string{"findmnt", "-n", "-o", "SOURCE", "/broken"}, execx.Result{ExitCode: 1})
	k.fake.On([]string{"findmnt", "-n", "-o", "SOURCE", root}, execx.Result{Stdout: []byte("lib:/\n")})
	k.fake.On([]string{"findmnt", "-n", "-o", "TARGET", "--target", dest}, execx.Result{Stdout: []byte(root + "\n")})
	k.proc["31/cmdline"] = &fstest.MapFile{Data: []byte("rclone\x00mount\x00lib:/\x00" + root + "\x00--rc-addr=" + testRCAddr + "\x00")}
	writeFile(t, filepath.Join(k.env["HOME"], ".config", "rclone", "rc-auth.env"), "RCLONE_RC_USER=u\nRCLONE_RC_PASS=p\n")
	k.fake.On([]string{"rclone", "rc", "--url=http://" + testRCAddr}, execx.Result{Stdout: []byte("{}")})
	return k
}

// E23: the prerequisites, in order.
func TestRemotePrerequisites(t *testing.T) {
	k := newKit(t)
	k.wantCode(k.run("--copy-preflight"), term.ExitFailure)
	k.wantErr("ERROR: " + k.config + " not found — run play-ftp-camera.yml first\n")

	k.writeConfig("RCLONE_REMOTE=\n")
	k.wantCode(k.run("--copy-preflight"), term.ExitFailure)
	k.wantErr("ERROR: RCLONE_REMOTE not set in " + k.config + "\n  Set ftp_camera_rclone_remote in host_vars/localhost.yml and re-run play-ftp-camera.yml\n")

	k.writeConfig("RCLONE_REMOTE=lib:/PHOTO/LIBRARY/Photos/\n")
	k.wantCode(k.run("--copy-preflight"), term.ExitFailure)
	k.wantErr(
		"ERROR: ftp_camera_rclone_remote must point at the library root,\n",
		"  not a photos/videos subdirectory. Got: lib:/PHOTO/LIBRARY/Photos/\n",
		"  The local sort places files into photos/ and videos/\n",
		"    ftp_camera_rclone_remote: \"lib:/PHOTO/LIBRARY\"\n",
		"  then re-run play-ftp-camera.yml.\n",
	)

	k.writeConfig("RCLONE_REMOTE=" + testRemote + "\n")
	k.wantCode(k.run("--copy-preflight"), term.ExitFailure)
	k.wantErr("ERROR: rclone not found — run play-rclone.yml first\n")

	k.path("rclone")
	if err := os.RemoveAll(k.upload); err != nil {
		t.Fatal(err)
	}
	k.wantCode(k.run("--copy-preflight"), term.ExitFailure)
	k.wantErr("ERROR: Upload directory not found: " + k.upload + "\n")
}

// E24, E25, E28, E30: the address on stdout, nothing else there.
func TestPreflightPrintsTheRCAddress(t *testing.T) {
	k := remoteKit(t)
	k.wantCode(k.run("--copy-preflight"), term.ExitOK)
	if k.out.String() != testRCAddr+"\n" {
		t.Errorf("stdout %q", k.out.String())
	}
	k.wantRan("rclone rc --url=http://" + testRCAddr + " core/stats")
	if len(k.fake.Execs()) != 0 {
		t.Error("--copy-preflight is not inhibit-wrapped")
	}
	k.wantNotRan("cp ")
}

func TestPreflightNoMount(t *testing.T) {
	k := remoteKit(t)
	k.fake.On([]string{"findmnt", "-n", "-o", "TARGET", "-t", "fuse.rclone"}, execx.Result{ExitCode: 1})
	k.wantCode(k.run("--copy-preflight"), term.ExitFailure)
	k.wantErr("ERROR: No rclone mount found for remote 'lib'\n  Is rclone-lib.service running?\n  Start it: systemctl --user start rclone-lib\n")
}

func TestPreflightMountDestinationMissing(t *testing.T) {
	k := remoteKit(t)
	if err := os.RemoveAll(remoteDest(k)); err != nil {
		t.Fatal(err)
	}
	k.wantCode(k.run("--copy-preflight"), term.ExitFailure)
	k.wantErr("ERROR: Mount destination not found: " + remoteDest(k) + "\n  Is the rclone mount active?\n")
}

func TestPreflightNoRCAddress(t *testing.T) {
	k := remoteKit(t)
	delete(k.proc, "31/cmdline")
	k.wantCode(k.run("--copy-preflight"), term.ExitFailure)
	k.wantErr("ERROR: could not determine the RC address for "+remoteDest(k)+"\n", "  no rclone mount process is running\n", "  Re-run play-rclone.yml, which writes the unit with both.\n")
}

func TestPreflightRCRefuses(t *testing.T) {
	k := remoteKit(t)
	k.fake.On([]string{"rclone", "rc"}, execx.Result{Stderr: []byte("HTTP 401\n"), ExitCode: 1})
	k.wantCode(k.run("--copy-preflight"), term.ExitFailure)
	k.wantErr("  rc probe failed: HTTP 401\n", "ERROR: rclone remote control did not answer at "+testRCAddr+"\n", "  with HTTP 401, re-run play-rclone.yml — it writes both the unit\n")
	if k.out.Len() != 0 {
		t.Errorf("stdout %q", k.out.String())
	}
}

// E24: a remote with no path is the mount root.
func TestMountPathOfTheMountRoot(t *testing.T) {
	k := remoteKit(t)
	c := newCLI(k.app().Env)
	got, err := c.mountPath(t.Context(), "lib:/")
	if err != nil || got != mountRoot(k) {
		t.Fatalf("%q %v", got, err)
	}
}

// E29.
func TestPush(t *testing.T) {
	k := remoteKit(t)
	k.fake.On([]string{"rclone", "copy"}, execx.Result{Stdout: []byte("Transferred: 3\n")})
	k.wantCode(k.run("--push"), term.ExitOK)
	k.wantRan("rclone copy " + k.upload + " " + testRemote + " --progress --transfers 4 --checkers 8 --update")
	k.wantOut("Pushing sorted uploads:\n  Source: "+k.upload+"\n  Dest:   "+testRemote+"\n\n", "Transferred: 3\n",
		"Push complete.\n", "Local copy retained at: "+k.upload+"\n", "To browse via mount without re-downloading:\n  rclone-cache-warm "+remoteDest(k)+"\n")

	k.fake.On([]string{"findmnt", "-n", "-o", "TARGET", "-t", "fuse.rclone"}, execx.Result{})
	k.wantCode(k.run("--push"), term.ExitOK)
	if strings.Contains(k.out.String()+k.err.String(), "rclone-cache-warm") || strings.Contains(k.err.String(), "No rclone mount") {
		t.Errorf("no mount: no hint and no error:\n%s%s", k.out.String(), k.err.String())
	}

	k.fake.On([]string{"rclone", "copy"}, execx.Result{ExitCode: 7})
	k.wantCode(k.run("--push"), term.ExitFailure)
	k.wantErr("ERROR: rclone copy failed: rclone exited 7\n")
}

func vfs(inProgress, queued, errored int) string {
	return `{"diskCache":{"uploadsInProgress":` + strconv.Itoa(inProgress) + `,"uploadsQueued":` + strconv.Itoa(queued) +
		`,"erroredFiles":` + strconv.Itoa(errored) + `}}`
}

// scriptRC answers vfs/stats and core/stats from two lists, one entry per poll; the last entry
// repeats.
func scriptRC(k *kit, vfsReplies, coreReplies []string) {
	next := func(list *[]string) []byte {
		r := (*list)[0]
		if len(*list) > 1 {
			*list = (*list)[1:]
		}
		return []byte(r)
	}
	k.fake.OnFunc([]string{"rclone", "rc", "--url=http://" + testRCAddr, "vfs/stats"}, func(execx.Cmd) (execx.Result, error) {
		return execx.Result{Stdout: next(&vfsReplies)}, nil
	})
	k.fake.OnFunc([]string{"rclone", "rc", "--url=http://" + testRCAddr, "core/stats"}, func(execx.Cmd) (execx.Result, error) {
		return execx.Result{Stdout: next(&coreReplies)}, nil
	})
}

// E31-E34.
func TestCopy(t *testing.T) {
	k := remoteKit(t)
	k.put("photos/2026/10/14/JPG/A.JPG", jpegBytes(), testStart)
	k.put("photos/2026/10/14/RAW/A.ARW", []byte("raw"), testStart)
	k.fake.On([]string{"rclone", "rc", "--url=http://" + testRCAddr, "core/stats-reset"}, execx.Result{Stderr: []byte("HTTP 500\n"), ExitCode: 1})
	k.fake.On([]string{"cp", "-r"}, execx.Result{})
	k.fake.On([]string{"rclone", "check"}, execx.Result{})
	// The first core/stats reply is the preflight's probe.
	scriptRC(k,
		[]string{vfs(1, 1, 0), vfs(0, 0, 0), vfs(0, 0, 2)},
		[]string{
			`{}`,
			`{"bytes":0,"totalBytes":2048,"transfers":0,"totalTransfers":2,"eta":null}`,
			`{"bytes":10240,"totalBytes":20480,"transfers":1,"totalTransfers":2,"eta":125}`,
			`{"bytes":20480,"totalBytes":20480,"transfers":2,"totalTransfers":2}`,
		})
	k.wantCode(k.run("--copy"), term.ExitOK)
	k.wantRan("cp -r "+k.upload+"/. "+remoteDest(k)+"/", "rclone check "+k.upload+" "+testRemote+" --one-way")
	k.wantErr("  NOTE: could not reset rclone counters (HTTP 500).\n        Progress figures below include earlier mount activity.\n",
		"  2 file(s) errored during upload — see rclone log\n")
	k.wantOut(
		"Copying to rclone mount:\n  Source: "+k.upload+"\n  Dest:   "+remoteDest(k)+"\n  Files:  2\n",
		"  2 files copied to mount.\n",
		"  [0m00s] queued: 1 · active: 1 · done: 0/2 (0B/2.0KB) · 0B/s · ETA —\n",
		"  [0m05s] queued: 0 · active: 0 · done: 1/2 (10KB/20KB) · 2.0KB/s · ETA 2m05s\n",
		"  [0m10s] all uploads complete\n",
		"All 2 files verified on Drive.\n",
	)
	if got := k.slept; len(got) != 3 || got[0] != 3*time.Second || got[1] != 5*time.Second {
		t.Errorf("slept %v", got)
	}
}

func TestCopyCannotCopy(t *testing.T) {
	k := remoteKit(t)
	k.fake.On([]string{"cp", "-r"}, execx.Result{ExitCode: 1})
	k.wantCode(k.run("--copy"), term.ExitFailure)
	k.wantErr("ERROR: Failed to copy files to mount\n")
}

func TestCopyStatsNotJSON(t *testing.T) {
	k := remoteKit(t)
	k.fake.On([]string{"cp", "-r"}, execx.Result{})
	scriptRC(k, []string{"not json"}, []string{"{}"})
	k.wantCode(k.run("--copy"), term.ExitFailure)
	k.wantErr("ERROR: Failed to parse rclone stats\n")
}

func TestCopyRCStopsAnswering(t *testing.T) {
	k := remoteKit(t)
	k.fake.On([]string{"cp", "-r"}, execx.Result{})
	k.fake.On([]string{"rclone", "rc", "--url=http://" + testRCAddr, "vfs/stats"}, execx.Result{Stderr: []byte("connection refused\n"), ExitCode: 1})
	k.wantCode(k.run("--copy"), term.ExitFailure)
	k.wantErr("ERROR: rclone RC at " + testRCAddr + " stopped answering:\nconnection refused\n")
}

func TestCopyVerificationFails(t *testing.T) {
	k := remoteKit(t)
	k.fake.On([]string{"cp", "-r"}, execx.Result{})
	scriptRC(k, []string{vfs(0, 0, 0)}, []string{"{}"})
	k.fake.On([]string{"rclone", "check"}, execx.Result{Stderr: []byte("1 differences found\n"), ExitCode: 1})
	k.wantCode(k.run("--copy"), term.ExitFailure)
	k.wantErr("Verification failed:\n1 differences found\n", "Run ftp-camera --push to push remaining files directly.\n")
}

// E35: an interrupt while waiting ends the run with 130.
func TestCopyInterrupted(t *testing.T) {
	k := remoteKit(t)
	k.fake.On([]string{"cp", "-r"}, execx.Result{})
	scriptRC(k, []string{vfs(1, 0, 0)}, []string{"{}"})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	k.onSleep = func(d time.Duration) {
		if d == 5*time.Second {
			cancel()
		}
	}
	k.wantCode(k.runCtx(ctx, "--copy"), term.ExitCancelled)
}

func TestParseStatsValues(t *testing.T) {
	m, err := parseStats([]byte(`{}`), []byte(`{"bytes":null,"eta":7.9}`))
	if err != nil || m.inProgress != 0 || m.bytes != 0 || m.eta != "7" {
		t.Fatalf("%+v %v", m, err)
	}
	if m, err = parseStats([]byte(`{"diskCache":{"uploadsQueued":"x"}}`), []byte(`{}`)); err == nil {
		t.Errorf("a string count was accepted: %+v", m)
	}
	if m, err = parseStats([]byte(`{}`), []byte(`[`)); err == nil {
		t.Errorf("bad core JSON accepted: %+v", m)
	}
}
