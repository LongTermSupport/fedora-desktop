package ftpcamera

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func okUpload(name string, bytes int, rate string) string {
	return `Thu Oct 15 09:31:03 2026 [pid 3] [camera] OK UPLOAD: Client "::ffff:192.0.2.30", "/` + name + `", ` +
		strconv.Itoa(bytes) + ` bytes, ` + rate + `Kbyte/sec`
}

// E53, E59-E62: async mode sorts each upload as it arrives.
func TestAsyncSession(t *testing.T) {
	k := serverKit(t)
	k.exifDates(map[string]string{"A.JPG": "2026/10/15", "C.MOV": "2026/10/15"})
	k.fake.On([]string{"exiftool", "-s3", "-a"}, execx.Result{Stdout: []byte("3\n")})
	k.fake.On([]string{"exiftool", "-validate"}, execx.Result{})
	k.put("A.JPG", jpegBytes(), testStart.Add(time.Minute))
	k.put("B.JPG", []byte{0xFF, 0xD8, 0x00}, testStart.Add(time.Minute)) // truncated
	k.put("C.MOV", []byte("mov"), testStart.Add(time.Minute))
	k.put("D.JPG", jpegBytes(), testStart.Add(time.Minute)) // no date
	k.put("E.txt", []byte("x"), testStart.Add(time.Minute)) // unsupported
	k.put("photos/2026/10/15/JPG/Z.JPG", jpegBytes(), testStart.Add(time.Minute))
	k.feedOn(false, tailArgv,
		okUpload("A.JPG", 6, "900.00"),
		okUpload("B.JPG", 3, "1.00"),
		okUpload("C.MOV", 3, "1.00"),
		okUpload("D.JPG", 6, "1.00"),
		okUpload("E.txt", 1, "1.00"),
		okUpload("photos/2026/10/15/JPG/Z.JPG", 6, "1.00"),
		okUpload("GONE.JPG", 6, "1.00"),
		`Thu Oct 15 09:31:03 2026 [pid 3] [camera] OK UPLOAD: Client "::ffff:192.0.2.30"`,
	)
	ctx := interruptAt(k)
	k.wantCode(k.runCtx(ctx, "--async"), term.ExitOK)
	for _, rel := range []string{"photos/2026/10/15/JPG/A.JPG", "videos/2026/10/15/MOV/C.MOV", "B.JPG", "D.JPG", "E.txt"} {
		if !exists(t, filepath.Join(k.upload, rel)) {
			t.Errorf("%s is not where it belongs", rel)
		}
	}
	k.wantOut(
		"  Mode:         async (sort each upload on arrival)\n",
		"  [09:30:00] sort  A.JPG  → photos/2026/10/15/JPG/\n",
		"  [09:30:00] sort  C.MOV  → videos/2026/10/15/MOV/\n",
		"  [09:30:00] skip  D.JPG  (no EXIF date, left in place)\n",
		"  [09:30:00] unverified  E.txt  (no completeness check for this type)\n",
		"  [09:30:00] skip  E.txt  (unsupported type, left in place)\n",
		"Async summary:\n  Sorted: 2  Skipped: 2  Sort errors: 0\n",
		"  Truncated (rejected): 1\n",
	)
	k.wantErr("  [09:30:00] partial  B.JPG  (truncated at 3 bytes — NOT sorted; camera will retry)\n",
		"Detected: B.JPG arrived truncated — the data connection dropped mid-transfer\n")
	if strings.Contains(k.out.String()+k.err.String(), "GONE.JPG") {
		t.Error("a vanished arrival was reported (B4)")
	}
	if strings.Contains(k.out.String(), "  Z.JPG  (") || strings.Contains(k.out.String(), "sort  Z.JPG") {
		t.Error("a file already in the sorted tree was handled")
	}
	if strings.Contains(k.out.String(), "Sort complete") {
		t.Error("async mode ran the closing sort pass")
	}
}

// E59: the repeat and rate signals.
func TestAsyncWeakLinkSignals(t *testing.T) {
	k := serverKit(t)
	k.env["LINK_REPEAT_THRESHOLD"] = "2"
	k.feedOn(false, tailArgv, okUpload("GONE.JPG", 6, "1.00"), okUpload("GONE.JPG", 6, "1.00"))
	ctx := interruptAt(k)
	k.wantCode(k.runCtx(ctx, "--async"), term.ExitOK)
	k.wantErr("Detected: GONE.JPG has now arrived 2 times — the camera is re-sending because no attempt has completed\n")

	k = serverKit(t)
	k.env["LINK_SLOW_KBS"] = "250"
	k.feedOn(false, tailArgv, okUpload("FAST.JPG", 2000000, "300.5"), okUpload("SMALL.JPG", 10, "1.0"), okUpload("SLOW.JPG", 2000000, "120.25"))
	ctx = interruptAt(k)
	k.wantCode(k.runCtx(ctx, "--async"), term.ExitOK)
	k.wantErr("Detected: transfer ran at 120.25 KB/s (below 250 KB/s)\n")
}

// E66, B25: Ctrl+C with uploads still queued. The interrupt lands just as the first file's date is
// read, before its move; nothing is reported as skipped or failed, no password is asked for,
// and the summary says how many completed uploads were left for --sort.
func TestAsyncInterruptWithBacklog(t *testing.T) {
	k := serverKit(t)
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	k.fake.OnFunc([]string{"exiftool", "-s3", "-d"}, func(execx.Cmd) (execx.Result, error) {
		cancel()
		return execx.Result{Stdout: []byte("2026/10/15\n")}, nil
	})
	for _, n := range []string{"A.JPG", "B.JPG", "C.JPG"} {
		k.put(n, jpegBytes(), testStart)
	}
	// GONE.JPG is no longer there and B.JPG arrives twice: neither adds to the count.
	k.feedOn(true, tailArgv, okUpload("A.JPG", 6, "900"), okUpload("B.JPG", 6, "900"), okUpload("GONE.JPG", 6, "900"),
		okUpload("B.JPG", 6, "900"), okUpload("C.JPG", 6, "900"))
	k.wantCode(k.runCtx(ctx, "--async"), term.ExitOK)
	for _, n := range []string{"A.JPG", "B.JPG", "C.JPG"} {
		if !exists(t, filepath.Join(k.upload, n)) {
			t.Errorf("%s was not left in place", n)
		}
	}
	for _, bogus := range []string{"skip", "fail", "sudo asks", "ERROR"} {
		if strings.Contains(k.out.String()+k.err.String(), bogus) {
			t.Errorf("an interrupted sort was reported (%q):\nstdout:\n%s\nstderr:\n%s", bogus, k.out.String(), k.err.String())
		}
	}
	k.wantNotRan("sudo -n mv")
	k.wantOut(
		"Sorted: 0  Skipped: 0  Sort errors: 0\n",
		"  3 completed upload(s) not sorted — run ftp-camera --sort\n",
	)
}

// E62: a failed move is a sort error, on stderr.
func TestAsyncSortError(t *testing.T) {
	k := serverKit(t)
	k.exifDates(map[string]string{"A.JPG": "2026/10/15"})
	k.put("A.JPG", jpegBytes(), testStart)
	k.fake.On([]string{"sudo", "-n", "mv"}, execx.Result{ExitCode: 1})
	k.feedOn(false, tailArgv, okUpload("A.JPG", 6, "900"))
	ctx := interruptAt(k)
	k.wantCode(k.runCtx(ctx, "--async"), term.ExitOK)
	k.wantErr("  [09:30:00] fail  A.JPG  (sort error, left in place)\n")
	k.wantOut("Sort errors: 1\n")
}

// asyncCopyKit is a server kit with the remote and its mount configured.
func asyncCopyKit(t *testing.T) *kit {
	t.Helper()
	k := remoteKit(t)
	k.path("exiftool")
	k.fake.On([]string{"sudo", "-n"}, execx.Result{})
	k.fake.On([]string{"sudo", "-n", "-v"}, execx.Result{})
	k.realRoot()
	k.fake.On([]string{"ip", "-4", "route", "show", "default"}, execx.Result{Stdout: []byte("default via 192.0.2.1 dev wlp1\n")})
	ipAddr(k, "wlp1", lanIP)
	k.fake.On([]string{"systemctl", "is-active", "--quiet", "vsftpd"}, execx.Result{})
	k.fake.On([]string{"sudo", "-n", "firewall-cmd", "--get-zones"}, execx.Result{Stdout: []byte("public\n")})
	k.fake.On([]string{"findmnt", "-T", remoteDest(k), "-t", "fuse.rclone", "--noheadings"}, execx.Result{})
	return k
}

// E42, E63, E57: --async-copy copies each sorted file into the mount through a temporary name.
func TestAsyncCopySession(t *testing.T) {
	k := asyncCopyKit(t)
	k.exifDates(map[string]string{"A.JPG": "2026/10/15", "B.JPG": "2026/10/15"})
	k.put("A.JPG", jpegBytes(), testStart)
	k.put("B.JPG", jpegBytes(), testStart)
	// B's copy fails: its destination name is taken by a directory.
	if err := os.MkdirAll(filepath.Join(remoteDest(k), "photos/2026/10/15/JPG/B.JPG/x"), 0o755); err != nil {
		t.Fatal(err)
	}
	k.feedOn(false, tailArgv, okUpload("A.JPG", 6, "900"), okUpload("B.JPG", 6, "900"))
	k.path("rclone-tail")
	k.fake.On([]string{"rclone-tail", "--once", "lib"}, execx.Result{Stdout: []byte("queue: 1\n")})
	ctx := interruptAt(k)
	k.wantCode(k.runCtx(ctx, "--async-copy"), term.ExitOK)
	copied := filepath.Join(remoteDest(k), "photos/2026/10/15/JPG/A.JPG")
	if data, err := os.ReadFile(copied); err != nil || string(data) != string(jpegBytes()) {
		t.Fatalf("the copy in the mount: %q %v", data, err)
	}
	if exists(t, filepath.Join(remoteDest(k), "photos/2026/10/15/JPG/.ftp-camera.tmp.A.JPG")) ||
		exists(t, filepath.Join(remoteDest(k), "photos/2026/10/15/JPG/.ftp-camera.tmp.B.JPG")) {
		t.Error("a temporary file was left in the mount")
	}
	k.wantOut(
		"  Mode:         async-copy (sort + per-file rclone copy)\n",
		"  Rclone:       "+testRemote+"\n  Mount:        "+remoteDest(k)+"\n",
		"  [09:30:00] copy  A.JPG  → mount cache (VFS will upload)\n",
		"  Copied: 1  Copy errors: 1\n",
		"Some files were sorted locally but did not reach the remote.\n",
		"  photos/2026/10/15/JPG/B.JPG\n",
		"Background uploads (rclone mount continues after exit):\nqueue: 1\n",
		"  Watch live:            rclone-tail lib\n",
	)
	k.wantErr("  [09:30:00] copy fail  B.JPG  (rc=1; sorted locally, rerun ftp-camera --push)\n    ")
}

// E63: a mount that is gone is a copy failure naming it; rclone-tail failing is said.
func TestAsyncCopyMountGone(t *testing.T) {
	k := asyncCopyKit(t)
	k.exifDates(map[string]string{"A.JPG": "2026/10/15"})
	k.put("A.JPG", jpegBytes(), testStart)
	k.fake.On([]string{"findmnt", "-T"}, execx.Result{ExitCode: 1})
	k.feedOn(false, tailArgv, okUpload("A.JPG", 6, "900"))
	k.path("rclone-tail")
	k.fake.On([]string{"rclone-tail"}, execx.Result{ExitCode: 1})
	ctx := interruptAt(k)
	k.wantCode(k.runCtx(ctx, "--async-copy"), term.ExitOK)
	k.wantErr("    " + remoteDest(k) + " is no longer served by an rclone mount\n")
	k.wantOut("  could not read upload status — no matching mount, or the rclone RC is unreachable\n")
}

// E42: the mount must be there before vsftpd starts; no async summary when the counters never
// opened.
func TestAsyncCopyMountMissing(t *testing.T) {
	k := asyncCopyKit(t)
	if err := os.RemoveAll(remoteDest(k)); err != nil {
		t.Fatal(err)
	}
	k.wantCode(k.run("--async-copy"), term.ExitFailure)
	k.wantErr("ERROR: rclone mount path not found: " + remoteDest(k) + "\n  Is rclone-lib.service running?\n")
	k.wantNotRan("sudo -n systemctl start")
	if strings.Contains(k.out.String(), "Async summary") {
		t.Error("async summary without counters")
	}
}

// E43, E51, E54: the viewer checks, pre-warm and the per-file view.
func TestViewer(t *testing.T) {
	k := serverKit(t)
	k.wantCode(k.run("--async", "--view"), term.ExitFailure)
	k.wantErr("ERROR: --view / --view-jpg requires geeqie, not found in PATH\n")
	k.path("geeqie")
	k.wantCode(k.run("--async", "--view"), term.ExitFailure)
	k.wantErr("ERROR: --view / --view-jpg has nothing to display — no GUI session\n")

	k.env["WAYLAND_DISPLAY"] = "wayland-0"
	k.exifDates(map[string]string{"A.ARW": "2026/10/15", "B.JPG": "2026/10/15"})
	k.fake.On([]string{"exiftool", "-s3", "-a"}, execx.Result{Stdout: []byte("3\n")})
	k.fake.On([]string{"exiftool", "-validate"}, execx.Result{})
	k.put("A.ARW", []byte("raw"), testStart)
	k.put("B.JPG", jpegBytes(), testStart)
	k.fake.On([]string{"setsid"}, execx.Result{})
	k.feedOn(false, tailArgv, okUpload("A.ARW", 3, "900"), okUpload("B.JPG", 6, "900"))
	ctx := interruptAt(k)
	k.wantCode(k.runCtx(ctx, "--async", "--view"), term.ExitOK)
	today := filepath.Join(k.upload, "photos/2026/10/15/RAW")
	k.wantRan("sudo -n mkdir -p "+today, "setsid -f geeqie --file="+today, "setsid -f geeqie --file="+filepath.Join(today, "A.ARW"))
	k.wantNotRan("setsid -f geeqie --file=" + filepath.Join(k.upload, "photos/2026/10/15/JPG/B.JPG"))
	k.wantOut("  Viewer:       on  (class filter: RAW)\n", "  Watching:     "+k.upload+"/photos/2026/10/15/RAW/\n",
		"  Pre-warm:     yes (geeqie opened against the watch folder)\n", "  [09:30:00] view  A.ARW  → geeqie\n")
	for _, c := range k.fake.Calls() {
		if c.Name == "setsid" && (len(c.Env) != 1 || c.Env[0] != "GQ_NEW_INSTANCE=") {
			t.Errorf("geeqie env %q", c.Env)
		}
	}
}

// E53, E54: the default-mode viewer follows the log; a geeqie that cannot start ends the
// session.
func TestDefaultModeViewer(t *testing.T) {
	k := serverKit(t)
	k.path("geeqie")
	k.env["DISPLAY"] = ":0"
	k.feedOn(false, tailArgv, okUpload("A.JPG", 6, "900"), okUpload("B.ARW", 6, "900"))
	k.feedOn(true, []string{"inotifywait"})
	k.fake.On([]string{"setsid"}, execx.Result{})
	ctx := interruptAt(k)
	k.wantCode(k.runCtx(ctx, "--view-jpg"), term.ExitOK)
	k.wantRan("setsid -f geeqie --file=" + filepath.Join(k.upload, "A.JPG"))
	k.wantNotRan("setsid -f geeqie --file=" + filepath.Join(k.upload, "B.ARW"))
	k.wantOut("  Watching:     "+k.upload+"/  (upload root; JPG arrivals only)\n", "  Pre-warm:     no (default mode — root may contain stale files)\n")

	k = serverKit(t)
	k.path("geeqie")
	k.env["DISPLAY"] = ":0"
	k.feedOn(true, tailArgv, okUpload("A.JPG", 6, "900"))
	k.feedOn(true, []string{"inotifywait"})
	k.fake.OnError([]string{"setsid"}, &execx.NotInstalledError{Name: "setsid"})
	k.wantCode(k.run("--view-jpg"), term.ExitFailure)
	k.wantErr("ERROR: the viewer stopped: geeqie could not be started: setsid is not installed\n")
}
