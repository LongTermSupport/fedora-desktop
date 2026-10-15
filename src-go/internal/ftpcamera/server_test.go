package ftpcamera

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const tailPrefix = "sudo -n tail -F -n 0 --pid=4242 /var/log/vsftpd.log"

var tailArgv = strings.Fields(tailPrefix)

// serverKit is a machine a session can start on: a LAN address, root that succeeds, vsftpd
// active at the end, the firewall rules present, exiftool installed.
func serverKit(t *testing.T) *kit {
	t.Helper()
	k := newKit(t)
	k.path("exiftool")
	k.fake.On([]string{"sudo", "-n"}, execx.Result{})
	k.fake.On([]string{"sudo", "-n", "-v"}, execx.Result{})
	k.realRoot()
	k.fake.On([]string{"ip", "-4", "route", "show", "default"}, execx.Result{Stdout: []byte("default via 192.0.2.1 dev wlp1 proto dhcp\n")})
	ipAddr(k, "wlp1", lanIP)
	k.fake.On([]string{"systemctl", "is-active", "--quiet", "vsftpd"}, execx.Result{})
	k.fake.On([]string{"sudo", "-n", "firewall-cmd", "--get-zones"}, execx.Result{Stdout: []byte("block public nm-shared\n")})
	return k
}

// interruptAt cancels the run at the grace the session gives an interrupt after its monitor
// ends. It waits for the held feeds first, so the test's Ctrl+C lands once every scripted line
// is handled.
func interruptAt(k *kit) context.Context {
	return cancelAtSleep(k, interruptGrace, k.awaitFeeds)
}

// cancelAtSleep cancels the run at the first sleep of d, once first has returned.
func cancelAtSleep(k *kit, d time.Duration, first func()) context.Context {
	ctx, cancel := context.WithCancel(k.t.Context())
	k.t.Cleanup(cancel)
	k.onSleep = func(slept time.Duration) {
		if slept == d {
			first()
			cancel()
		}
	}
	return ctx
}

// E41, E47-E49, E52, E53, E55-E57: a default session from start to Ctrl+C.
func TestDefaultSession(t *testing.T) {
	k := serverKit(t)
	k.exifDates(map[string]string{"DSC1.JPG": "2026/10/15"})
	k.feedOn(true, tailArgv,
		`Thu Oct 15 09:31:00 2026 [pid 2] CONNECT: Client "::ffff:192.0.2.30"`,
		`Thu Oct 15 09:31:01 2026 [pid 1] [camera] OK LOGIN: Client "::ffff:192.0.2.30"`,
		`Thu Oct 15 09:31:02 2026 [pid 1] [camera] FAIL LOGIN: Client "::ffff:192.0.2.30"`,
		`Thu Oct 15 09:31:03 2026 [pid 3] [camera] OK UPLOAD: Client "::ffff:192.0.2.30", "/DSC1.JPG", 6 bytes, 1.00Kbyte/sec`,
		`Thu Oct 15 09:31:04 2026 [pid 3] [camera] FAIL UPLOAD: Client "::ffff:192.0.2.30", "/DSC2.JPG"`,
		`Thu Oct 15 09:31:05 2026 [pid 3] [camera] FTP command: Client "::ffff:192.0.2.30", "PASS secret"`,
		`Thu Oct 15 09:31:05 2026 [pid 3] [camera] FTP command: Client "::ffff:192.0.2.30", "STOR DSC1.JPG"`,
		`Thu Oct 15 09:31:06 2026 [pid 3] [camera] FTP response: Client "::ffff:192.0.2.30", "226 Transfer complete."`,
		`something else`,
	)
	k.feedOn(false, []string{"inotifywait", "-m", "-r", "-q"}, "  [09:31:03] CLOSE_WRITE,CLOSE  "+k.upload+"/DSC1.JPG")
	k.put("DSC1.JPG", jpegBytes(), testStart.Add(time.Minute))
	k.put("old.JPG", jpegBytes(), testStart.Add(-time.Hour))
	ctx := interruptAt(k)
	k.wantCode(k.runCtx(ctx), term.ExitOK)

	k.wantRan(
		"sudo -n cp /etc/vsftpd/vsftpd.conf /run/vsftpd-camera.conf",
		"sudo -n tee -a /run/vsftpd-camera.conf",
		"sudo -n mkdir -p /run/vsftpd/empty",
		"sudo -n chmod 0755 /run/vsftpd /run/vsftpd/empty",
		"sudo -n mkdir -p /run/systemd/system/vsftpd.service.d",
		"sudo -n tee /run/systemd/system/vsftpd.service.d/camera-ftp.conf",
		"sudo -n systemctl daemon-reload",
		"sudo -n systemctl start vsftpd",
		"sudo -n firewall-cmd --add-service=ftp --quiet",
		"sudo -n firewall-cmd --add-port=30000-30100/tcp --quiet",
		tailPrefix,
		"inotifywait -m -r -q -e close_write,moved_to --format   [%T] %e  %w%f --timefmt %H:%M:%S "+k.upload,
		// Teardown.
		"sudo -n systemctl stop vsftpd",
		"sudo -n rm -rf /run/systemd/system/vsftpd.service.d /run/vsftpd-camera.conf",
		"sudo -n firewall-cmd --remove-service=ftp --quiet",
		"sudo -n firewall-cmd --remove-port=30000-30100/tcp --quiet",
		"sudo -n firewall-cmd --zone=nm-shared --remove-service=ftp --quiet",
	)
	k.wantNotRan("sudo -n firewall-cmd --zone=nm-shared --add")
	writes := teeWrites(t, k)
	if writes["-n tee -a /run/vsftpd-camera.conf"] != "pasv_address="+lanIP+"\n" {
		t.Errorf("pasv: %q", writes)
	}
	if writes["-n tee /run/systemd/system/vsftpd.service.d/camera-ftp.conf"] != "[Service]\nExecStart=\nExecStart=/usr/sbin/vsftpd /run/vsftpd-camera.conf\n" {
		t.Errorf("override: %q", writes)
	}
	k.wantOut(
		"Starting camera FTP server...\n",
		"  ftp-camera ready\n",
		"  Mode:         default (collect now; sort at Ctrl+C)\n",
		"  Hotspot:      off\n",
		"  Viewer:       off\n",
		"  Upload dir:   "+k.upload+"\n",
		"  FTP URL:      ftp://camera@"+lanIP+"/\n",
		"    Server:      "+lanIP+"\n",
		"Monitoring for uploads — press Ctrl+C to stop\n",
		"  [conn] Thu Oct 15 09:31:00 2026 [pid 2] CONNECT",
		"  [auth] Thu Oct 15 09:31:01",
		"  [fail] Thu Oct 15 09:31:02",
		"  [fail] Thu Oct 15 09:31:04",
		"  [verb] Thu Oct 15 09:31:05 2026 [pid 3] [camera] FTP command: Client \"::ffff:192.0.2.30\", \"STOR",
		"  [verb] Thu Oct 15 09:31:06",
		"  [ftp ] something else\n",
		"  [09:31:03] CLOSE_WRITE,CLOSE  "+k.upload+"/DSC1.JPG\n",
		"\nStopping FTP server...\n",
		"FTP server stopped.\n",
		"Upload summary:\n  Files: 1\n  Total: 6B\n  Path:  "+k.upload+"\n\n  "+k.upload+"/DSC1.JPG  6B\n",
		"  move  DSC1.JPG  → photos/2026/10/15/JPG/\n",
		"Sort complete: 1 moved, 1 skipped\n",
	)
	if strings.Contains(k.out.String(), "PASS secret") || strings.Contains(k.out.String(), "OK UPLOAD") {
		t.Error("the PASS verb or an OK UPLOAD line was printed")
	}
	if strings.Contains(k.out.String(), "Async summary") {
		t.Error("default mode has no async summary")
	}
}

// teeWrites is what each root tee was given, by its arguments.
func teeWrites(t *testing.T, k *kit) map[string]string {
	t.Helper()
	writes := map[string]string{}
	for _, c := range k.fake.Calls() {
		if c.Name == "sudo" && len(c.Args) > 1 && c.Args[1] == "tee" {
			writes[strings.Join(c.Args, " ")] = readAll(t, c)
		}
	}
	return writes
}

// readAll is what a command was given on stdin.
func readAll(t *testing.T, c execx.Cmd) string {
	t.Helper()
	if c.Stdin == nil {
		return ""
	}
	r, ok := c.Stdin.(*strings.Reader)
	if !ok {
		t.Fatalf("stdin is %T", c.Stdin)
	}
	if _, err := r.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	b := new(strings.Builder)
	if _, err := r.WriteTo(b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// E55, B13: a monitor that stops by itself ends the session with an error, after teardown.
func TestMonitorStoppingIsAnError(t *testing.T) {
	k := serverKit(t)
	k.feedOn(true, tailArgv)
	k.fake.On([]string{"inotifywait"}, execx.Result{Stderr: []byte("Couldn't watch\n"), ExitCode: 1})
	k.wantCode(k.run("--no-tui"), term.ExitFailure)
	k.wantErr("ERROR: the upload monitor stopped: inotifywait exited 1")
	k.wantOut("FTP server stopped.\n")
}

// E55: a log line the follower cannot read stops it at once, which ends the session; tail is
// stopped rather than left to run to teardown.
func TestLogLineTooLongStopsTheFollower(t *testing.T) {
	k := serverKit(t)
	k.feedOn(true, tailArgv, strings.Repeat("x", 1<<20+1), okUpload("A.JPG", 6, "900"))
	k.wantCode(k.run("--async"), term.ExitFailure)
	k.wantErr("ERROR: the upload monitor stopped: read /var/log/vsftpd.log: bufio.Scanner: token too long\n")
	k.wantOut("FTP server stopped.\n")
}

// E41, E56: a start that fails still tears down, and the session exits 1.
func TestStartFailureTearsDown(t *testing.T) {
	k := serverKit(t)
	k.fake.On([]string{"sudo", "-n", "systemctl", "start"}, execx.Result{ExitCode: 1})
	k.wantCode(k.run("--async"), term.ExitFailure)
	k.wantErr("ERROR: could not start the FTP server: systemctl start vsftpd: ")
	k.wantOut("Stopping FTP server...\n", "FTP server stopped.\n", "Async summary:\n")
	k.wantNotRan("sudo -n firewall-cmd --add")
}

// E56: a teardown step that fails stops the teardown, exit 1.
func TestTeardownFailureStops(t *testing.T) {
	k := serverKit(t)
	k.feedOn(true, tailArgv)
	k.feedOn(false, []string{"inotifywait"})
	k.fake.On([]string{"sudo", "-n", "firewall-cmd", "--query-service=ftp", "--quiet"}, execx.Result{ExitCode: 252})
	ctx := interruptAt(k)
	k.wantCode(k.runCtx(ctx), term.ExitFailure)
	k.wantErr("ERROR: could not stop the FTP server: firewall-cmd --query-service=ftp --quiet")
	if strings.Contains(k.out.String(), "FTP server stopped.") {
		t.Error("teardown went on after a failed step")
	}
}

// B14: a query answering 1 is absent, so nothing is removed.
func TestTeardownLeavesAbsentRulesAlone(t *testing.T) {
	k := serverKit(t)
	k.feedOn(true, tailArgv)
	k.feedOn(false, []string{"inotifywait"})
	k.fake.On([]string{"systemctl", "is-active"}, execx.Result{ExitCode: 3})
	k.fake.On([]string{"sudo", "-n", "firewall-cmd", "--query-service=ftp"}, execx.Result{ExitCode: 1})
	k.fake.On([]string{"sudo", "-n", "firewall-cmd", "--get-zones"}, execx.Result{Stdout: []byte("public\n")})
	ctx := interruptAt(k)
	k.wantCode(k.runCtx(ctx), term.ExitOK)
	k.wantNotRan("sudo -n firewall-cmd --remove-service")
	k.wantNotRan("sudo -n systemctl stop")
	k.wantNotRan("sudo -n firewall-cmd --zone=nm-shared")
}

// E48: --debug-ftp appends the protocol log and names the triage script at its real path (B18).
func TestDebugFTP(t *testing.T) {
	k := serverKit(t)
	k.feedOn(true, tailArgv)
	k.feedOn(false, []string{"inotifywait"})
	ctx := interruptAt(k)
	k.wantCode(k.runCtx(ctx, "--debug-ftp"), term.ExitOK)
	k.wantOut("--debug-ftp: FTP protocol tracing enabled.\n",
		"    CLAUDE/Plan/Cancelled/00066-ftp-camera-airbnb-wifi-and-hotspot-triage/triage.bash\n",
		"  Debug:        on   (FTP verb stream → /var/log/vsftpd.log)\n")
	n := 0
	for _, c := range k.fake.Calls() {
		if c.Line() == "sudo -n tee -a /run/vsftpd-camera.conf" && readAll(t, c) == "log_ftp_protocol=YES\n" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("log_ftp_protocol appended %d times", n)
	}
}

// E50: the landing-zone advisory counts only files older than the age limit.
func TestLandingZoneAdvisory(t *testing.T) {
	k := serverKit(t)
	k.feedOn(true, tailArgv)
	k.feedOn(false, []string{"inotifywait"})
	k.env["LANDING_WARN_GB"] = "0"
	k.env["LANDING_WARN_AGE_DAYS"] = "1"
	k.put("photos/2026/10/01/JPG/A.JPG", make([]byte, 2048), testStart.Add(-72*time.Hour))
	k.put("photos/2026/10/14/JPG/B.JPG", make([]byte, 4096), testStart.Add(-36*time.Hour))
	ctx := interruptAt(k)
	k.wantCode(k.runCtx(ctx), term.ExitOK)
	k.wantOut("  ⚠  Landing-zone cleanup suggested\n     0.0 GB of files older than 1 days are sitting in\n",
		"       rclone check "+k.upload+" <your-remote>   # 0 differences = safe\n")

	k2 := serverKit(t)
	k2.feedOn(true, tailArgv)
	k2.feedOn(false, []string{"inotifywait"})
	k2.put("A.JPG", make([]byte, 10), testStart.Add(-30*24*time.Hour))
	ctx = interruptAt(k2)
	k2.wantCode(k2.runCtx(ctx), term.ExitOK)
	if strings.Contains(k2.out.String(), "Landing-zone") {
		t.Error("advised below the size limit")
	}
}

func TestStaleBytes(t *testing.T) {
	dir := t.TempDir()
	old := testStart.Add(-49 * time.Hour)
	for name, size := range map[string]int{"a": 100, "sub/b": 50} {
		path := filepath.Join(dir, name)
		writeFile(t, path, strings.Repeat("x", size))
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	now := testStart
	if got, err := staleBytes(dir, now, 1); err != nil || got != 150 {
		t.Errorf("49h old, limit 1 day: %d %v", got, err)
	}
	if got, err := staleBytes(dir, now, 2); err != nil || got != 0 {
		t.Errorf("49h old, limit 2 days: %d %v", got, err)
	}
}

// E50, E57: an upload root that cannot be read is an error, not "no files".
func TestRegularFilesRootUnreadable(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "gone")
	err := regularFiles(missing, func(string, fs.FileInfo) { t.Error("visited a file") })
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("err = %v", err)
	}
	if got, err := staleBytes(missing, testStart, 1); err == nil || got != 0 {
		t.Errorf("staleBytes = %d %v", got, err)
	}
}
