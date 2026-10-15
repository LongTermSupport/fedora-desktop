package ftpcamera

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	vsftpdLog      = "/var/log/vsftpd.log"
	runtimeConf    = "/run/vsftpd-camera.conf"
	overrideDir    = "/run/systemd/system/vsftpd.service.d"
	dataPorts      = "30000-30100/tcp"
	hotspotZone    = "nm-shared"
	defaultProfile = "Hotspot"
	// interruptGrace is how long an interrupt may lag the monitor it also stopped (E55).
	interruptGrace = time.Second
	// uploadQueue is how many completed uploads may wait for the handler.
	uploadQueue = 4096
	// everything is the shell glob in the landing-zone advisory's clean-up command.
	everything = "*"
)

// session is one FTP-server run (E41-E63).
type session struct {
	c        *cli
	start    time.Time
	remote   string
	mount    string
	hotspot  hotspotInfo
	profile  string
	ownsAP   bool
	ip       string
	countsOn bool
	tally    asyncTally
	stopBg   context.CancelFunc
	bg       sync.WaitGroup
	ended    chan stopped
}

// stopped is a background part of the session that ended by itself.
type stopped struct {
	who string
	err error
}

// serve runs the session and always tears down what it set up (E41, E55-E57).
func (c *cli) serve(ctx context.Context) term.ExitCode {
	s := &session{c: c, start: c.env.Now(), ended: make(chan stopped, 3)}
	bg, cancel := context.WithCancel(ctx)
	s.stopBg = cancel
	defer cancel()
	code := s.run(ctx, bg)
	if err := s.teardown(context.WithoutCancel(ctx)); err != nil {
		return term.ExitFailure
	}
	return code
}

func (s *session) run(ctx, bg context.Context) term.ExitCode {
	if err := s.prepare(ctx); err != nil {
		if ctx.Err() != nil {
			return term.ExitOK
		}
		return term.ExitFailure
	}
	s.follow(bg)
	select {
	case <-ctx.Done():
		return term.ExitOK
	case end := <-s.ended:
		if ctx.Err() != nil || s.c.env.Sleep(ctx, interruptGrace) != nil {
			return term.ExitOK
		}
		reason := "it exited"
		if end.err != nil {
			reason = end.err.Error()
		}
		return s.c.fail("%s stopped: %s", end.who, reason)
	}
}

// prepare is everything before the monitors start (E42-E51).
func (s *session) prepare(ctx context.Context) error {
	c := s.c
	if c.opts.asyncCopy {
		if err := s.prepareCopy(ctx); err != nil {
			return err
		}
	}
	s.countsOn = c.opts.async
	if err := s.checkViewer(); err != nil {
		return err
	}
	if c.opts.hotspot {
		if err := s.hotspotUp(ctx); err != nil {
			return err
		}
	}
	s.hotspot = c.detectHotspot(ctx)
	wifi := c.detectWifi(ctx)
	c.link.mu.Lock()
	c.link.wifi = wifi
	c.link.mu.Unlock()
	ip, err := c.localIP(ctx, s.hotspot)
	if err != nil {
		return err
	}
	s.ip = ip
	c.out.f("%sStarting camera FTP server...%s\n", c.out.b, c.out.n)
	if err := s.startVsftpd(ctx); err != nil {
		return c.failErr("could not start the FTP server: %v", err)
	}
	c.out.f("\n")
	s.banner()
	if c.opts.viewer != "" && c.opts.async {
		today := filepath.Join(c.upload, photoDir, c.env.Now().Format("2006/01/02"), c.opts.viewer)
		if err := c.asRoot(ctx, "mkdir", "-p", today); err != nil {
			return c.failErr("could not create %s: %v", today, err)
		}
		if err := s.show(ctx, today); err != nil {
			return c.failErr("%v", err)
		}
	}
	return nil
}

// remoteName is the remote up to its first ":".
func remoteName(remote string) string {
	name, path := splitRemote(remote)
	if path == remote {
		return remote
	}
	return name
}

// prepareCopy checks the remote and its mount before vsftpd starts (E42).
func (s *session) prepareCopy(ctx context.Context) error {
	c := s.c
	remote, err := c.remoteConfig()
	if err != nil {
		return err
	}
	s.remote = remote
	if s.mount, err = c.mountPath(ctx, remote); err != nil {
		return c.failErr("%v", err)
	}
	if !isDir(s.mount) {
		c.err.f("ERROR: rclone mount path not found: %s\n  Is rclone-%s.service running?\n", s.mount, remoteName(remote))
		return errFailed
	}
	return nil
}

// checkViewer refuses a viewer with nothing to run or show it (E43).
func (s *session) checkViewer() error {
	c := s.c
	if c.opts.viewer == "" {
		return nil
	}
	if !c.onPath("geeqie") {
		c.err.f("ERROR: --view / --view-jpg requires geeqie, not found in PATH\n")
		c.err.f("  Install via play-photography.yml, or drop the viewer flag.\n")
		return errFailed
	}
	if c.env.Getenv("WAYLAND_DISPLAY") == "" && c.env.Getenv("DISPLAY") == "" {
		c.err.f("ERROR: --view / --view-jpg has nothing to display — no GUI session\n")
		c.err.f("  Both WAYLAND_DISPLAY and DISPLAY are unset. You may be running\n")
		c.err.f("  over ssh without forwarding. Drop the viewer flag and re-run.\n")
		return errFailed
	}
	return nil
}

// hotspotUp brings the hotspot profile up unless it already is (E44).
func (s *session) hotspotUp(ctx context.Context) error {
	c := s.c
	s.profile = defaultProfile
	if _, err := os.Stat(c.env.ConfigPath); err == nil {
		cfg, err := c.loadConfig()
		if err != nil {
			return err
		}
		if name := cfg["HOTSPOT_CONNECTION"]; name != "" {
			s.profile = name
		}
	}
	if !c.onPath(nmcliBin) {
		return c.failErr("--hotspot needs nmcli (NetworkManager CLI)")
	}
	if err := c.assertHotspotSupported(ctx); err != nil {
		return err
	}
	all, err := c.connections(ctx, false)
	if err != nil {
		return c.failErr("could not list the NetworkManager profiles: %v", err)
	}
	if !slices.Contains(all, s.profile) {
		s.profileMissing()
		return errFailed
	}
	active, err := c.connections(ctx, true)
	if err != nil {
		return c.failErr("could not list the active NetworkManager profiles: %v", err)
	}
	o := c.out
	if slices.Contains(active, s.profile) {
		o.f("%sHotspot '%s' already active — using as-is, will not tear down on exit.%s\n", o.y, s.profile, o.n)
		return nil
	}
	o.f("%sActivating hotspot '%s'...%s\n", o.b, s.profile, o.n)
	if err := c.asRoot(ctx, nmcliBin, "connection", "up", s.profile); err != nil {
		return c.failErr("could not bring the hotspot up: %v", err)
	}
	s.ownsAP = true
	return c.env.Sleep(ctx, 2*time.Second)
}

func (s *session) profileMissing() {
	e := s.c.err
	e.f("ERROR: NetworkManager connection '%s' not found\n\n", s.profile)
	e.f("  play-ftp-camera.yml creates this AP profile for you, but only\n")
	e.f("  once a WPA2 passphrase has been vaulted. Run, from the repo:\n\n")
	e.f("    ./vault.bash set ftp_camera_hotspot_password    # 8-63 chars\n")
	e.f("    ansible-playbook playbooks/imports/optional/common/play-ftp-camera.yml\n\n")
	e.f("  Then re-run this command. Do NOT create the profile by hand —\n")
	e.f("  the playbook owns it, and a hand-made one will not be tuned.\n")
	e.f("  (Override the profile name via ftp_camera_hotspot_connection\n")
	e.f("  in host_vars/localhost.yml if you've renamed it.)\n")
}

// startVsftpd writes the runtime config and override, starts vsftpd and opens the firewall
// (E48).
func (s *session) startVsftpd(ctx context.Context) error {
	c := s.c
	if err := c.asRoot(ctx, "cp", "/etc/vsftpd/vsftpd.conf", runtimeConf); err != nil {
		return err
	}
	if err := c.rootWrite(ctx, runtimeConf, "pasv_address="+s.ip+"\n", true); err != nil {
		return err
	}
	if c.opts.debugFTP {
		if err := c.rootWrite(ctx, runtimeConf, "log_ftp_protocol=YES\n", true); err != nil {
			return err
		}
		o := c.out
		o.f("%s%s--debug-ftp: FTP protocol tracing enabled.%s\n", o.y, o.b, o.n)
		o.f("  Every command the camera sends is logged to %s%s%s.\n", o.c, vsftpdLog, o.n)
		o.f("  This is verbose and slows the log tail — use it for diagnosis, not routine runs.\n")
		o.f("  Afterwards, extract the trace with the plan triage script:\n")
		o.f("    %sCLAUDE/Plan/Cancelled/00066-ftp-camera-airbnb-wifi-and-hotspot-triage/triage.bash%s\n\n", o.c, o.n)
	}
	if err := c.asRoot(ctx, "mkdir", "-p", "/run/vsftpd/empty"); err != nil {
		return err
	}
	if err := c.asRoot(ctx, "chmod", "0755", "/run/vsftpd", "/run/vsftpd/empty"); err != nil {
		return err
	}
	if err := c.asRoot(ctx, "mkdir", "-p", overrideDir); err != nil {
		return err
	}
	override := "[Service]\nExecStart=\nExecStart=/usr/sbin/vsftpd " + runtimeConf + "\n"
	if err := c.rootWrite(ctx, overrideDir+"/camera-ftp.conf", override, false); err != nil {
		return err
	}
	if err := c.ready(ctx); err != nil {
		return err
	}
	if err := c.sd.DaemonReloadAsRoot(ctx, c.root); err != nil {
		return err
	}
	if err := c.sd.StartAsRoot(ctx, c.root, "vsftpd"); err != nil {
		return err
	}
	return s.openFirewall(ctx)
}

func (s *session) openFirewall(ctx context.Context) error {
	zones := []string{""}
	if s.hotspot.active {
		zones = append(zones, "--zone="+hotspotZone)
	}
	for _, zone := range zones {
		for _, rule := range []string{"--add-service=ftp", "--add-port=" + dataPorts} {
			if err := s.firewall(ctx, zone, rule); err != nil {
				return err
			}
		}
	}
	return nil
}

// firewall runs one firewall-cmd change as root, in zone ("" is the default zone).
func (s *session) firewall(ctx context.Context, zone, op string) error {
	if zone == "" {
		return s.c.asRoot(ctx, firewallCmd, op, "--quiet")
	}
	return s.c.asRoot(ctx, firewallCmd, zone, op, "--quiet")
}

// banner is the startup summary (E49).
func (s *session) banner() {
	c, o := s.c, s.c.out
	hr := "══════════════════════════════════════════════════════════"
	o.f("%s%s%s%s\n", o.b, o.g, hr, o.n)
	o.f("  %sftp-camera ready%s\n", o.b, o.n)
	o.f("%s%s──────────────────────────────────────────────────────────%s\n", o.b, o.g, o.n)
	row := func(label, value string) { o.f("  %-13s %s\n", label, value) }
	row("Mode:", s.modeText())
	s.hotspotRows()
	s.viewerRows(row)
	if c.opts.asyncCopy {
		row("Rclone:", s.remote)
		row("Mount:", s.mount)
	}
	if c.opts.debugFTP {
		o.f("  %-13s on   (FTP verb stream → %s)\n", "Debug:", vsftpdLog)
	}
	if w := c.link.wifi; w.band != "" {
		signal := w.signal
		if signal == "" {
			signal = "?"
		}
		o.f("  %-13s %sGHz (%s MHz), %s dBm  [this laptop, not the camera]\n", "WiFi:", w.band, w.freqMHz, signal)
	}
	row("Upload dir:", c.upload)
	o.f("  %-13s ftp://camera@%s/\n", "FTP URL:", s.ip)
	o.f("%s%s%s%s\n\n", o.b, o.g, hr, o.n)
	o.f("  %sSony A7V camera settings%s (Menu → Network → FTP Transfer Func.):\n", o.b, o.n)
	o.f("    Server:      %s%s%s\n", o.c, s.ip, o.n)
	o.f("    Port:        %s21%s\n", o.c, o.n)
	o.f("    User:        %scamera%s\n", o.c, o.n)
	o.f("    Password:    %srun 'ftp-camera --pass' to view%s\n", o.c, o.n)
	o.f("    Passive:     %sOn%s\n", o.c, o.n)
	o.f("    Destination: %s/%s\n\n", o.c, o.n)
	s.landingAdvisory()
	o.f("%sMonitoring for uploads — press Ctrl+C to stop%s\n\n", o.y, o.n)
}

func (s *session) hotspotRows() {
	o := s.c.out
	if !s.hotspot.active {
		o.f("  %-13s off\n", "Hotspot:")
		return
	}
	ssid := s.hotspot.ssid
	if ssid == "" {
		ssid = "-"
	}
	own := "pre-existing, left alone on exit"
	if s.ownsAP {
		own = "managed by ftp-camera, will close on exit"
	}
	o.f("  %-13s on   (SSID: %s, IP: %s, dev: %s)\n", "Hotspot:", ssid, s.hotspot.ip, s.hotspot.dev)
	o.f("  %-13s      (%s)\n", "", own)
}

func (s *session) modeText() string {
	switch {
	case s.c.opts.asyncCopy:
		return "async-copy (sort + per-file rclone copy)"
	case s.c.opts.async:
		return "async (sort each upload on arrival)"
	default:
		return "default (collect now; sort at Ctrl+C)"
	}
}

func (s *session) viewerRows(row func(label, value string)) {
	c := s.c
	if c.opts.viewer == "" {
		c.out.f("  %-13s off\n", "Viewer:")
		return
	}
	watch := c.upload + "/  (upload root; " + c.opts.viewer + " arrivals only)"
	prewarm := "no (default mode — root may contain stale files)"
	if c.opts.async {
		watch = c.upload + "/" + photoDir + "/" + c.env.Now().Format("2006/01/02") + "/" + c.opts.viewer + "/"
		prewarm = "yes (geeqie opened against the watch folder)"
	}
	c.out.f("  %-13s on  (class filter: %s)\n", "Viewer:", c.opts.viewer)
	row("Watching:", watch)
	row("Pre-warm:", prewarm)
}

const gib = 1 << 30

// regularFiles calls fn for each regular file under dir. An entry below dir that cannot be
// read is not visited, so an advisory or a summary undercounts rather than stops the session
// (E50, E57); dir itself unreadable is the error.
func regularFiles(dir string, fn func(path string, info fs.FileInfo)) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == dir {
				return err
			}
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			fn(path, info)
		}
		return nil
	})
}

// staleBytes is the size of the files whose age in whole days is above days (find -mtime +N).
func staleBytes(dir string, now time.Time, days int64) (int64, error) {
	var total int64
	err := regularFiles(dir, func(_ string, info fs.FileInfo) {
		if int64(now.Sub(info.ModTime())/(24*time.Hour)) > days {
			total += info.Size()
		}
	})
	return total, err
}

// landingAdvisory suggests clearing old uploads out of the landing zone (E50).
func (s *session) landingAdvisory() {
	c, o := s.c, s.c.out
	if !isDir(c.upload) {
		return
	}
	stale, err := staleBytes(c.upload, c.env.Now(), c.tun.warnAgeDays)
	if err != nil {
		c.err.f("note: no landing-zone check — %v\n", err)
		return
	}
	if stale < c.tun.warnGB*gib {
		return
	}
	o.f("  %s%s⚠  Landing-zone cleanup suggested%s\n", o.y, o.b, o.n)
	o.f("     %s GB of files older than %d days are sitting in\n", strconv.FormatFloat(float64(stale)/gib, 'f', 1, 64), c.tun.warnAgeDays)
	o.f("     %s%s%s — likely already pushed to the remote.\n", o.c, c.upload, o.n)
	o.f("     Verify they're on the remote, then reclaim the space:\n")
	o.f("       rclone check %s <your-remote>   # 0 differences = safe\n", c.upload)
	o.f("       sudo rm -rf %s/%s\n\n", c.upload, everything)
}

var passVerb = regexp.MustCompile(`FTP command:.*"PASS`)

// logTag is the tag and colour of a vsftpd log line (E52); "" is a line not printed.
func (s *session) logTag(line string) (string, string) {
	o := s.c.out
	switch {
	case strings.Contains(line, "FAIL LOGIN"):
		return "[fail]", o.r
	case strings.Contains(line, "OK LOGIN"):
		return "[auth]", o.g
	case strings.Contains(line, "CONNECT"):
		return "[conn]", o.c
	case strings.Contains(line, "OK UPLOAD"):
		return "", ""
	case strings.Contains(line, "FAIL UPLOAD"):
		return "[fail]", o.r
	case passVerb.MatchString(line):
		return "", ""
	case strings.Contains(line, "FTP command:"), strings.Contains(line, "FTP response:"):
		return "[verb]", o.c
	}
	return "[ftp ]", o.y
}

// logLine prints one vsftpd log line with its tag (E52).
func (s *session) logLine(line string) {
	if tag, colour := s.logTag(line); tag != "" {
		s.c.out.f("  %s%s%s %s\n", colour, tag, s.c.out.n, line)
	}
}

// upload is one completed transfer from the log: the file and vsftpd's size and rate tail.
type upload struct {
	path, meta string
}

// uploadOf reads an OK UPLOAD line (E53); ok false for any other line.
func (s *session) uploadOf(line string) (upload, bool) {
	if !strings.Contains(line, "OK UPLOAD: Client") {
		return upload{}, false
	}
	fields := strings.Split(line, `"`)
	if len(fields) < 4 || fields[3] == "" {
		return upload{}, false
	}
	u := upload{path: s.c.upload + "/" + strings.TrimPrefix(fields[3], "/")}
	if len(fields) > 4 {
		u.meta = fields[4]
	}
	return u, true
}

// follow starts the log follower, the async handler or the default monitor, and the
// default-mode viewer (E52-E54).
func (s *session) follow(bg context.Context) {
	c := s.c
	var queue chan upload
	var workers sync.WaitGroup
	who := "the vsftpd log follower"
	switch {
	case c.opts.async:
		queue = make(chan upload, uploadQueue)
		who = "the upload monitor"
		s.worker(bg, &workers, who, queue, s.handle)
	case c.opts.viewer != "":
		queue = make(chan upload, uploadQueue)
		s.worker(bg, &workers, "the viewer", queue, s.view)
	}
	s.bg.Go(func() {
		err := s.tailLog(bg, func(line string) {
			s.logLine(line)
			if u, ok := s.uploadOf(line); ok && queue != nil {
				select {
				case queue <- u:
				case <-bg.Done():
					s.notSorted()
				}
			}
		})
		if queue != nil {
			close(queue)
		}
		workers.Wait()
		s.report(bg, who, err)
	})
	if !c.opts.async {
		s.bg.Go(func() { s.report(bg, "the upload monitor", s.inotify(bg)) })
	}
}

// inotify prints the upload directory's close and move events as they come (E53).
func (s *session) inotify(bg context.Context) error {
	c := s.c
	_, err := c.env.Runner.Run(bg, execx.Cmd{
		Name:   "inotifywait",
		Args:   []string{"-m", "-r", "-q", "-e", "close_write,moved_to", "--format", "  [%T] %e  %w%f", "--timefmt", "%H:%M:%S", c.upload},
		Stdout: c.out.w, Stderr: c.err.w,
	})
	return err
}

// worker handles each queued upload in arrival order; a failure ends the session, and what
// is still queued then is counted as not sorted (E66).
func (s *session) worker(bg context.Context, workers *sync.WaitGroup, who string, q <-chan upload, handle func(context.Context, upload) error) {
	workers.Go(func() {
		failed := false
		for u := range q {
			if failed {
				s.notSorted()
				continue
			}
			if err := handle(bg, u); err != nil {
				s.report(bg, who, err)
				failed = true
			}
		}
	})
}

// report says a background part ended, unless the session is already stopping it.
func (s *session) report(bg context.Context, who string, err error) {
	if bg.Err() != nil {
		return
	}
	select {
	case s.ended <- stopped{who: who, err: err}:
	default:
	}
}

// tailLog follows the vsftpd log as root, its stderr discarded, handing each line to handle.
// A line it cannot read stops tail, so the failure ends the session at once (E55).
func (s *session) tailLog(bg context.Context, handle func(string)) error {
	c := s.c
	if err := c.ready(bg); err != nil {
		return errors.New("sudo is not ready")
	}
	tailCtx, stopTail := context.WithCancel(bg)
	defer stopTail()
	pr, pw := io.Pipe()
	quiet := *c.root
	quiet.Term = &term.Term{Err: io.Discard, IsTerminal: c.env.IsTerminal, Getenv: c.env.Getenv}
	quiet.Stdin = nil
	quiet.Stdout = pw
	done := make(chan error, 1)
	go func() {
		err := quiet.Run(tailCtx, "tail", "-F", "-n", "0", "--pid="+strconv.Itoa(c.env.PID), vsftpdLog)
		pw.CloseWithError(io.EOF)
		done <- err
	}()
	sc := bufio.NewScanner(pr)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		handle(sc.Text())
	}
	scanErr := sc.Err()
	if scanErr != nil {
		stopTail()
	}
	if _, err := io.Copy(io.Discard, pr); err != nil {
		scanErr = errors.Join(scanErr, err)
	}
	tailErr := <-done
	if scanErr != nil {
		return fmt.Errorf("read %s: %w", vsftpdLog, scanErr)
	}
	if tailErr != nil {
		return fmt.Errorf("tail %s: %w", vsftpdLog, tailErr)
	}
	return nil
}
