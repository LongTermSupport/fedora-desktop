package ftpcamera

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// remoteConfig is the configured rclone remote, after the checks every remote mode makes (E23).
func (c *cli) remoteConfig() (string, error) {
	cfg, err := c.loadConfig()
	if err != nil {
		return "", err
	}
	remote := cfg["RCLONE_REMOTE"]
	if remote == "" {
		c.err.f("ERROR: RCLONE_REMOTE not set in %s\n", c.env.ConfigPath)
		c.err.f("  Set ftp_camera_rclone_remote in host_vars/localhost.yml and re-run play-ftp-camera.yml\n")
		return "", errFailed
	}
	stripped := strings.TrimSuffix(remote, "/")
	lower := strings.ToLower(stripped)
	for _, media := range []string{"/photos", "/photo", "/videos", "/video"} {
		if strings.HasSuffix(lower, media) {
			parent := stripped[:strings.LastIndex(stripped, "/")]
			e := c.err
			e.f("ERROR: ftp_camera_rclone_remote must point at the library root,\n")
			e.f("  not a photos/videos subdirectory. Got: %s\n", remote)
			e.f("  The local sort places files into %s/ and %s/\n", photoDir, videoDir)
			e.f("  subtrees that mirror the library layout. Update host_vars/localhost.yml:\n")
			e.f("    ftp_camera_rclone_remote: \"%s\"\n", parent)
			e.f("  then re-run play-ftp-camera.yml.\n")
			return "", errFailed
		}
	}
	if !c.onPath("rclone") {
		return "", c.failErr("rclone not found — run play-rclone.yml first")
	}
	return remote, nil
}

// splitRemote is the remote's name (up to its first ":") and its path (after it).
func splitRemote(remote string) (string, string) {
	name, path, found := strings.Cut(remote, ":")
	if !found {
		return remote, remote
	}
	return name, path
}

// mountPath is where the remote's path is inside its local rclone mount (E24). The error's
// text is the explanation to print after "ERROR: ".
func (c *cli) mountPath(ctx context.Context, remote string) (string, error) {
	name, path := splitRemote(remote)
	target, source := c.findMount(ctx, name)
	if target == "" {
		return "", advise("No rclone mount found for remote '%s'\n"+
			"  Is rclone-%s.service running?\n"+
			"  Start it: systemctl --user start rclone-%s", name, name, name)
	}
	srcPath := strings.SplitN(source, ":", 2)
	rel := strings.TrimPrefix(strings.TrimPrefix(path, srcPath[len(srcPath)-1]), "/")
	if rel == "" {
		return target, nil
	}
	return target + "/" + rel, nil
}

// findMount is the first rclone mount whose source is the named remote, and that source.
func (c *cli) findMount(ctx context.Context, name string) (string, string) {
	res, err := c.env.Runner.Run(ctx, execx.Cmd{Name: findmntBin, Args: []string{"-n", "-o", "TARGET", "-t", "fuse.rclone"}})
	if err != nil {
		return "", ""
	}
	for mnt := range strings.SplitSeq(string(res.Stdout), "\n") {
		if mnt == "" {
			continue
		}
		src, err := c.env.Runner.Run(ctx, execx.Cmd{Name: findmntBin, Args: []string{"-n", "-o", "SOURCE", mnt}})
		if err != nil {
			continue
		}
		if source := strings.TrimSpace(string(src.Stdout)); strings.HasPrefix(source, name+":") {
			return mnt, source
		}
	}
	return "", ""
}

// push ships the upload tree to the remote with rclone copy (E29).
func (c *cli) push(ctx context.Context) error {
	remote, err := c.remoteConfig()
	if err != nil {
		return err
	}
	if !isDir(c.upload) {
		return c.failErr("Upload directory not found: %s", c.upload)
	}
	o := c.out
	o.f("%sPushing sorted uploads:%s\n", o.b, o.n)
	o.f("  Source: %s%s%s\n", o.c, c.upload, o.n)
	o.f("  Dest:   %s%s%s\n\n", o.c, remote, o.n)
	_, err = c.env.Runner.Run(ctx, execx.Cmd{
		Name:   "rclone",
		Args:   []string{"copy", c.upload, remote, "--progress", "--transfers", "4", "--checkers", "8", "--update"},
		Stdout: c.env.Stdout, Stderr: c.env.Stderr,
	})
	if err != nil {
		return c.failErr("rclone copy failed: %v", err)
	}
	o.f("\n%sPush complete.%s\n", o.g, o.n)
	o.f("%sLocal copy retained at:%s %s%s%s\n", o.y, o.n, o.c, c.upload, o.n)
	o.f("%sVerify on the remote, then remove locally when ready.%s\n", o.y, o.n)
	if mount, err := c.mountPath(ctx, remote); err == nil {
		o.f("\n%sTo browse via mount without re-downloading:%s\n", o.b, o.n)
		o.f("  rclone-cache-warm %s%s%s\n", o.c, mount, o.n)
	}
	return nil
}

// copyTarget is what the preflight establishes: the remote, the mount path, the RC address.
type copyTarget struct {
	remote, dest, addr string
}

// preflight is everything --copy establishes before it moves a byte (E30).
func (c *cli) preflight(ctx context.Context) (copyTarget, error) {
	var t copyTarget
	remote, err := c.remoteConfig()
	if err != nil {
		return t, err
	}
	t.remote = remote
	if !isDir(c.upload) {
		return t, c.failErr("Upload directory not found: %s", c.upload)
	}
	if t.dest, err = c.mountPath(ctx, remote); err != nil {
		return t, c.failErr("%v", err)
	}
	if !isDir(t.dest) {
		c.err.f("ERROR: Mount destination not found: %s\n  Is the rclone mount active?\n", t.dest)
		return t, errFailed
	}
	if t.addr, err = c.rc.AddrForMount(ctx, t.dest); err != nil {
		e := c.err
		e.f("ERROR: could not determine the RC address for %s\n", t.dest)
		e.f("  %v\n", err)
		e.f("  The mount must be running and started with --rc-addr.\n")
		e.f("  Re-run play-rclone.yml, which writes the unit with both.\n")
		return t, errFailed
	}
	if err := c.rc.Available(ctx, t.addr); err != nil {
		e := c.err
		e.f("  rc probe failed: %v\n", err)
		e.f("ERROR: rclone remote control did not answer at %s\n", t.addr)
		e.f("  Confirm the mount is running and was started with --rc:\n")
		e.f("    systemctl --user status rclone-<remote> --no-pager -l\n")
		e.f("  If --rc is missing from its ExecStart, or the call was refused\n")
		e.f("  with HTTP 401, re-run play-rclone.yml — it writes both the unit\n")
		e.f("  and the RC credential file.\n")
		return t, errFailed
	}
	return t, nil
}

// preflightMode is --copy-preflight: the address on stdout, the diagnostics on stderr.
func (c *cli) preflightMode(ctx context.Context) error {
	t, err := c.preflight(ctx)
	if err != nil {
		return err
	}
	c.out.f("%s\n", t.addr)
	return nil
}

// countFiles is the number of regular files under dir.
func countFiles(dir string) (int, error) {
	n := 0
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			n++
		}
		return nil
	})
	return n, err
}

// copyToMount copies the tree into the mount, waits for the upload, verifies (E31-E34).
func (c *cli) copyToMount(ctx context.Context) error {
	t, err := c.preflight(ctx)
	if err != nil {
		return err
	}
	count, err := countFiles(c.upload)
	if err != nil {
		return c.failErr("could not count the files under %s: %v", c.upload, err)
	}
	o := c.out
	o.f("%sCopying to rclone mount:%s\n", o.b, o.n)
	o.f("  Source: %s%s%s\n", o.c, c.upload, o.n)
	o.f("  Dest:   %s%s%s\n", o.c, t.dest, o.n)
	o.f("  Files:  %s%d%s\n\n", o.c, count, o.n)
	if res, err := c.rc.Call(ctx, t.addr, "core/stats-reset"); err != nil {
		c.err.f("  NOTE: could not reset rclone counters (%s).\n", failureText(res, err))
		c.err.f("        Progress figures below include earlier mount activity.\n")
	}
	cp := execx.Cmd{Name: "cp", Args: []string{"-r", c.upload + "/.", t.dest + "/"}, Stdout: c.out.w, Stderr: c.err.w}
	if _, err := c.env.Runner.Run(ctx, cp); err != nil {
		return c.failErr("Failed to copy files to mount")
	}
	o.f("  %s%d files copied to mount.%s\n\n", o.g, count, o.n)
	o.f("%sWaiting for uploads to complete (Ctrl+C to abort)...%s\n", o.b, o.n)
	if err := c.env.Sleep(ctx, 3*time.Second); err != nil {
		return err
	}
	if err := c.waitForUploads(ctx, t.addr); err != nil {
		return err
	}
	return c.verifyOnRemote(ctx, t.remote, count)
}

// verifyOnRemote checks every local file reached the remote (E34).
func (c *cli) verifyOnRemote(ctx context.Context, remote string, count int) error {
	o := c.out
	o.f("\n%sVerifying files on Drive...%s\n", o.b, o.n)
	out, err := execx.Combined(ctx, c.env.Runner, execx.Cmd{Name: "rclone", Args: []string{"check", c.upload, remote, "--one-way"}})
	if err != nil {
		e := c.err
		e.f("%sVerification failed:%s\n%s\n\n", e.r, e.n, strings.TrimRight(out, "\n"))
		e.f("%sSome files may not have reached Drive.%s\n", e.y, e.n)
		e.f("Run %sftp-camera --push%s to push remaining files directly.\n", e.c, e.n)
		return errFailed
	}
	o.f("%sAll %d files verified on Drive.%s\n", o.g, count, o.n)
	o.f("Files are cached locally — browsing the mount will be instant.\n\n")
	o.f("%sLocal copy retained at:%s %s%s%s\n", o.y, o.n, o.c, c.upload, o.n)
	o.f("%sVerify on the remote, then remove locally when ready.%s\n", o.y, o.n)
	return nil
}

// stats is one poll of the mount's RC (E32).
type stats struct {
	inProgress, queued, errored int64
	bytes, totalBytes           int64
	done, total                 int64
	eta                         string
}

// whole reads a JSON number as a whole number, as Python's int() did; absent or null is 0.
func whole(m map[string]any, key string) (int64, error) {
	switch v := m[key].(type) {
	case nil:
		return 0, nil
	case float64:
		return int64(v), nil
	default:
		return 0, fmt.Errorf("%s is not a number", key)
	}
}

func parseStats(vfsJSON, coreJSON []byte) (stats, error) {
	var s stats
	var vfsReply, core map[string]any
	if err := json.Unmarshal(vfsJSON, &vfsReply); err != nil {
		return s, fmt.Errorf("vfs/stats: %w", err)
	}
	if err := json.Unmarshal(coreJSON, &core); err != nil {
		return s, fmt.Errorf("core/stats: %w", err)
	}
	cache, ok := vfsReply["diskCache"].(map[string]any)
	if !ok {
		cache = map[string]any{}
	}
	fields := []struct {
		from map[string]any
		key  string
		dst  *int64
	}{
		{cache, "uploadsInProgress", &s.inProgress},
		{cache, "uploadsQueued", &s.queued},
		{cache, "erroredFiles", &s.errored},
		{core, "bytes", &s.bytes},
		{core, "totalBytes", &s.totalBytes},
		{core, "transfers", &s.done},
		{core, "totalTransfers", &s.total},
	}
	for _, f := range fields {
		n, err := whole(f.from, f.key)
		if err != nil {
			return s, err
		}
		*f.dst = n
	}
	eta, err := whole(core, "eta")
	if err != nil {
		return s, err
	}
	if core["eta"] != nil {
		s.eta = strconv.FormatInt(eta, 10)
	}
	return s, nil
}

func minutes(secs int64) string { return fmt.Sprintf("%dm%02ds", secs/60, secs%60) }

// waitForUploads polls the RC until two consecutive polls show nothing in flight (E32, E33).
func (c *cli) waitForUploads(ctx context.Context, addr string) error {
	start := c.env.Now()
	var empty, prevBytes, prevSecs int64
	for {
		s, err := c.poll(ctx, addr)
		if err != nil {
			return err
		}
		secs := int64(c.env.Now().Sub(start) / time.Second)
		o := c.out
		if s.inProgress != 0 || s.queued != 0 {
			empty = 0
		} else if empty++; empty >= 2 {
			o.f("  [%s%s%s] %sall uploads complete%s\n", o.c, minutes(secs), o.n, o.g, o.n)
			if s.errored > 0 {
				c.err.f("  %s%d file(s) errored during upload — see rclone log%s\n", c.err.r, s.errored, c.err.n)
			}
			return nil
		}
		var rate int64
		if dt := secs - prevSecs; dt > 0 && s.bytes >= prevBytes {
			rate = (s.bytes - prevBytes) / dt
		}
		prevBytes, prevSecs = s.bytes, secs
		o.f("  [%s%s%s] queued: %s%d%s · active: %s%d%s · done: %s%d/%d%s (%sB/%sB) · %s%sB/s%s · ETA %s%s%s\n",
			o.c, minutes(secs), o.n, o.c, s.queued, o.n, o.c, s.inProgress, o.n, o.c, s.done, s.total, o.n,
			iec(s.bytes, false), iec(s.totalBytes, false), o.g, iec(rate, false), o.n, o.y, etaText(s.eta), o.n)
		if err := c.env.Sleep(ctx, 5*time.Second); err != nil {
			return err
		}
	}
}

// etaText is the ETA as minutes and seconds, or a dash when rclone has none.
func etaText(eta string) string {
	n, err := strconv.ParseInt(eta, 10, 64)
	if err != nil || n == 0 {
		return "—"
	}
	return minutes(n)
}

// poll reads vfs/stats and core/stats once.
func (c *cli) poll(ctx context.Context, addr string) (stats, error) {
	var replies [2][]byte
	for i, endpoint := range []string{"vfs/stats", "core/stats"} {
		res, err := c.rc.Call(ctx, addr, endpoint)
		if err != nil {
			if ctx.Err() != nil {
				return stats{}, fmt.Errorf("interrupted: %w", ctx.Err())
			}
			c.err.f("ERROR: rclone RC at %s stopped answering:\n%s\n", addr, failureText(res, err))
			return stats{}, errFailed
		}
		replies[i] = res.Stdout
	}
	s, err := parseStats(replies[0], replies[1])
	if err != nil {
		return s, c.failErr("Failed to parse rclone stats")
	}
	return s, nil
}
