package ftpcamera

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sudo"
)

// summaryListLimit is how many received files the upload summary lists one by one (E57).
const summaryListLimit = 20

// teardown stops everything the session started and reports what it received (E56, E57).
// ctx is not cancelled by the interrupt that ended the session.
func (s *session) teardown(ctx context.Context) error {
	c, o := s.c, s.c.out
	o.f("\n%sStopping FTP server...%s\n", o.b, o.n)
	s.stopBg()
	s.bg.Wait()
	if err := s.stopServer(ctx); err != nil {
		return c.failErr("could not stop the FTP server: %v", err)
	}
	o.f("%sFTP server stopped.%s\n\n", o.g, o.n)
	s.uploadSummary()
	if c.opts.async {
		if s.countsOn {
			s.asyncSummary(ctx)
		}
		return nil
	}
	if err := c.sortPass(ctx); err != nil {
		c.err.f("note: the sort pass did not complete — continuing with teardown.\n")
	}
	return nil
}

// stopServer stops vsftpd, removes the runtime files, closes the firewall and takes down a
// hotspot this session brought up (E56). The first failing step stops it.
func (s *session) stopServer(ctx context.Context) error {
	c := s.c
	if err := c.ready(ctx); err != nil {
		return errors.New("sudo is not ready")
	}
	active, activeErr := c.sd.UnitActive(ctx, "vsftpd")
	if activeErr != nil {
		return fmt.Errorf("systemctl is-active vsftpd: %w", activeErr)
	}
	if active {
		if err := c.sd.StopAsRoot(ctx, c.root, "vsftpd"); err != nil {
			return err
		}
	}
	if err := c.asRoot(ctx, "rm", "-rf", overrideDir, runtimeConf); err != nil {
		return err
	}
	if err := c.sd.DaemonReloadAsRoot(ctx, c.root); err != nil {
		return err
	}
	if err := s.closeFirewall(ctx, ""); err != nil {
		return err
	}
	zones, zonesErr := c.rootCapture(ctx, firewallCmd, "--get-zones")
	if zonesErr != nil {
		return fmt.Errorf("firewall-cmd --get-zones: %s", failureText(zones, zonesErr))
	}
	if slices.Contains(strings.Fields(string(zones.Stdout)), hotspotZone) {
		if err := s.closeFirewall(ctx, "--zone="+hotspotZone); err != nil {
			return err
		}
	}
	return s.hotspotDown(ctx)
}

// closeFirewall removes the ftp service and the passive ports where a query says they are.
func (s *session) closeFirewall(ctx context.Context, zone string) error {
	for _, kind := range []string{"service=ftp", "port=" + dataPorts} {
		present, err := s.firewallHas(ctx, zone, "--query-"+kind)
		if err != nil {
			return err
		}
		if present {
			if err := s.firewall(ctx, zone, "--remove-"+kind); err != nil {
				return err
			}
		}
	}
	return nil
}

// firewallHas reads a firewall-cmd query in zone: 0 present, 1 absent, anything else an
// error (B14).
func (s *session) firewallHas(ctx context.Context, zone, query string) (bool, error) {
	var res execx.Result
	var err error
	if zone == "" {
		res, err = s.c.rootCapture(ctx, firewallCmd, query, "--quiet")
	} else {
		res, err = s.c.rootCapture(ctx, firewallCmd, zone, query, "--quiet")
	}
	if err == nil {
		return true, nil
	}
	if exitErr, ok := errors.AsType[*execx.ExitError](err); ok && exitErr != nil && exitErr.Code == 1 && !errors.Is(err, sudo.ErrNeedsPassword) {
		return false, nil
	}
	return false, fmt.Errorf("%s: %s", strings.Join(slices.DeleteFunc([]string{firewallCmd, zone, query, "--quiet"}, func(a string) bool { return a == "" }), " "), failureText(res, err))
}

// hotspotDown takes down a hotspot this session brought up, if it is still up (E56).
func (s *session) hotspotDown(ctx context.Context) error {
	if !s.ownsAP {
		return nil
	}
	c := s.c
	active, err := c.connections(ctx, true)
	if err != nil {
		return fmt.Errorf("list the active NetworkManager profiles: %w", err)
	}
	if !slices.Contains(active, s.profile) {
		return nil
	}
	c.out.f("%sDeactivating hotspot '%s'...%s\n", c.out.b, s.profile, c.out.n)
	return c.asRoot(ctx, nmcliBin, "connection", "down", s.profile)
}

// uploadSummary lists what arrived during the session (E57).
func (s *session) uploadSummary() {
	c, o := s.c, s.c.out
	if !isDir(c.upload) {
		return
	}
	var paths []string
	var sizes []int64
	var total int64
	err := regularFiles(c.upload, func(path string, info fs.FileInfo) {
		if info.ModTime().After(s.start) {
			paths = append(paths, path)
			sizes = append(sizes, info.Size())
			total += info.Size()
		}
	})
	o.f("%sUpload summary:%s\n", o.b, o.n)
	if err != nil {
		c.err.f("  could not list what arrived — %v\n", err)
		return
	}
	if len(paths) == 0 {
		o.f("  (no files received)\n")
		return
	}
	o.f("  Files: %s%d%s\n", o.c, len(paths), o.n)
	o.f("  Total: %s%sB%s\n", o.c, iec(total, true), o.n)
	o.f("  Path:  %s%s%s\n", o.c, c.upload, o.n)
	if len(paths) > summaryListLimit {
		return
	}
	o.f("\n")
	for i, p := range paths {
		o.f("  %s  %sB\n", p, iec(sizes[i], true))
	}
}

// asyncSummary is the handler's counts and, for --async-copy, the copy state (E57).
func (s *session) asyncSummary(ctx context.Context) {
	c, o := s.c, s.c.out
	t := &s.tally
	o.f("\n%sAsync summary:%s\n", o.b, o.n)
	o.f("  Sorted: %s%d%s  Skipped: %s%d%s  Sort errors: %s%d%s\n", o.c, t.moved, o.n, o.c, t.skipped, o.n, o.c, t.errored, o.n)
	if t.unsorted > 0 {
		o.f("  %s%d completed upload(s) not sorted — run ftp-camera --sort%s\n", o.y, t.unsorted, o.n)
	}
	if t.truncated > 0 {
		o.f("  %sTruncated (rejected): %d%s\n", o.r, t.truncated, o.n)
		o.f("  %sThe link dropped the data connection mid-transfer on those.%s\n", o.y, o.n)
		o.f("  %sThey were kept out of the library. If the camera keeps re-sending%s\n", o.y, o.n)
		o.f("  %sthe same frame, this is why: it never completes one.%s\n", o.y, o.n)
	}
	if !c.opts.asyncCopy {
		return
	}
	o.f("  Copied: %s%d%s  Copy errors: %s%d%s\n", o.c, t.copied, o.n, o.c, t.copyErr, o.n)
	if len(t.failed) > 0 {
		o.f("%sSome files were sorted locally but did not reach the remote.%s\n", o.y, o.n)
		for _, f := range t.failed {
			o.f("  %s%s%s\n", o.r, c.rel(f), o.n)
		}
		o.f("%sRun:%s %sftp-camera --push%s %sto ship them.%s\n", o.y, o.n, o.c, o.n, o.y, o.n)
	}
	name := remoteName(s.remote)
	o.f("\n%sBackground uploads (rclone mount continues after exit):%s\n", o.b, o.n)
	if c.onPath("rclone-tail") {
		_, err := c.env.Runner.Run(ctx, execx.Cmd{Name: "rclone-tail", Args: []string{"--once", name}, Stdout: c.out.w, Stderr: c.err.w})
		if err != nil {
			o.f("  %scould not read upload status — no matching mount, or the rclone RC is unreachable%s\n", o.y, o.n)
		}
		o.f("  Watch live:            %srclone-tail %s%s\n", o.c, name, o.n)
	} else {
		o.f("  %srclone-tail not installed — re-run play-rclone.yml to get it%s\n", o.y, o.n)
	}
	o.f("  Verify/retry on remote:%s ftp-camera --push%s %s(--update skips files already uploaded)%s\n", o.c, o.n, o.y, o.n)
}
