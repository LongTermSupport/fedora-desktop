package ftpcamera

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// asyncTally is the per-session count of what the handler did (E57, E59-E63).
type asyncTally struct {
	mu                                                            sync.Mutex
	moved, skipped, errored, copied, copyErr, truncated, unsorted int
	failed                                                        []string
}

func (t *asyncTally) add(field *int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	*field++
}

// sortOutcome is what sorting one arrival did (E62).
type sortOutcome int

const (
	sortMoved sortOutcome = iota
	sortUnsupported
	sortNoDate
	sortFailed
	sortVanished
)

var (
	xferBytes = regexp.MustCompile(`[0-9]+ bytes`)
	xferRate  = regexp.MustCompile(`[0-9.]+Kbyte/sec`)
)

func (s *session) stamp() string { return s.c.env.Now().Format("15:04:05") }

// handle is the per-upload handler of the async modes (E59-E63, E66). Only a viewer that cannot
// start is an error: every other outcome is reported and counted. Once the session is
// stopping, an upload is counted as not sorted rather than handled (E66).
func (s *session) handle(ctx context.Context, u upload) error {
	c := s.c
	rel := c.rel(u.path)
	if strings.HasPrefix(rel, photoDir+"/") || strings.HasPrefix(rel, videoDir+"/") {
		return nil
	}
	if stopping(ctx) {
		s.notSorted()
		return nil
	}
	name := filepath.Base(u.path)
	if n := c.link.arrived(name); int64(n) >= c.tun.repeat {
		c.warnWeakLink(fmt.Sprintf("%s has now arrived %d times — the camera is re-sending because no attempt has completed", name, n))
	}
	s.checkRate(u.meta)
	if !isRegular(u.path) {
		return nil
	}
	return s.file(ctx, u.path, name)
}

// stopping is whether the session is being stopped: work begun after it is not reported.
func stopping(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

// notSorted counts a completed upload the async handler did not get to (E66).
func (s *session) notSorted() {
	if s.c.opts.async {
		s.tally.add(&s.tally.unsorted)
	}
}

// file screens, sorts, shows and copies one arrival. An outcome the interrupt caused (a check
// or a move cut short) is not reported as the file's: the file is counted as not sorted (E66).
func (s *session) file(ctx context.Context, path, name string) error {
	c := s.c
	ts := s.stamp()
	v := completeness(ctx, c.env.Runner, c.onPath, path)
	if stopping(ctx) {
		s.notSorted()
		return nil
	}
	if !s.screen(v, path, name, ts) {
		return nil
	}
	sorted, outcome := s.sortOne(ctx, path)
	if outcome != sortMoved && stopping(ctx) {
		s.notSorted()
		return nil
	}
	if !s.sorted(outcome, sorted, name, ts) {
		return nil
	}
	if stopping(ctx) {
		s.unshipped(sorted)
		return nil
	}
	if c.opts.viewer != "" && viewerMatches(c.opts.viewer, sorted) {
		if err := s.show(ctx, sorted); err != nil {
			return err
		}
	}
	if c.opts.asyncCopy {
		s.copyOut(ctx, sorted)
	}
	return nil
}

// screen is the completeness gate of one arrival (E61): false keeps it out of the library.
func (s *session) screen(v verdict, path, name, ts string) bool {
	c := s.c
	switch v {
	case truncated:
		e := c.err
		e.f("  [%s%s%s] %spartial%s  %s  (truncated at %s bytes — NOT sorted; camera will retry)\n", e.c, ts, e.n, e.r, e.n, name, sizeText(path))
		s.tally.add(&s.tally.truncated)
		c.warnWeakLink(name + " arrived truncated — the data connection dropped mid-transfer")
		return false
	case unverifiable:
		o := c.out
		o.f("  [%s%s%s] %sunverified%s  %s  (no completeness check for this type)\n", o.c, ts, o.n, o.y, o.n, name)
	case complete:
	}
	return true
}

// sorted reports one arrival's sort and counts it (E62); true when it was moved.
func (s *session) sorted(outcome sortOutcome, sorted, name, ts string) bool {
	o, e := s.c.out, s.c.err
	switch outcome {
	case sortMoved:
		o.f("  [%s%s%s] %ssort%s  %s  → %s/\n", o.c, ts, o.n, o.g, o.n, name, filepath.Dir(s.c.rel(sorted)))
		s.tally.add(&s.tally.moved)
		return true
	case sortUnsupported:
		o.f("  [%s%s%s] %sskip%s  %s  (unsupported type, left in place)\n", o.c, ts, o.n, o.y, o.n, name)
		s.tally.add(&s.tally.skipped)
	case sortNoDate:
		o.f("  [%s%s%s] %sskip%s  %s  (no EXIF date, left in place)\n", o.c, ts, o.n, o.y, o.n, name)
		s.tally.add(&s.tally.skipped)
	case sortFailed:
		e.f("  [%s%s%s] %sfail%s  %s  (sort error, left in place)\n", e.c, ts, e.n, e.r, e.n, name)
		s.tally.add(&s.tally.errored)
	case sortVanished:
	}
	return false
}

// checkRate raises the advisory for a large transfer that ran slowly (E59).
func (s *session) checkRate(meta string) {
	c := s.c
	bytesText := strings.TrimSuffix(xferBytes.FindString(meta), " bytes")
	rateText := strings.TrimSuffix(xferRate.FindString(meta), "Kbyte/sec")
	if bytesText == "" || rateText == "" {
		return
	}
	if awkNumber(bytesText) >= float64(c.tun.rateMin) && awkNumber(rateText) < c.tun.slowKBS {
		c.warnWeakLink(fmt.Sprintf("transfer ran at %s KB/s (below %s KB/s)", rateText, c.tun.slowText))
	}
}

// sortOne files one arrival (E62): where it went, and what happened.
func (s *session) sortOne(ctx context.Context, path string) (string, sortOutcome) {
	c := s.c
	if !isRegular(path) {
		return "", sortVanished
	}
	class := classOf(path)
	if class == "" {
		return "", sortUnsupported
	}
	date := c.exifDate(ctx, path, class)
	if date == "" {
		return "", sortNoDate
	}
	dest := filepath.Join(c.upload, classDir(class), date, typeDir(extOf(path)))
	if err := c.moveAsRoot(ctx, path, dest); err != nil {
		return "", sortFailed
	}
	return filepath.Join(dest, filepath.Base(path)), sortMoved
}

// copyOut copies a sorted file into the mount and reports it (E63).
func (s *session) copyOut(ctx context.Context, sorted string) {
	c := s.c
	name := filepath.Base(sorted)
	err := s.copyIntoMount(ctx, sorted)
	if err != nil && stopping(ctx) {
		s.unshipped(sorted)
		return
	}
	ts := s.stamp()
	if err == nil {
		c.out.f("  [%s%s%s] %scopy%s  %s  → mount cache (VFS will upload)\n", c.out.c, ts, c.out.n, c.out.g, c.out.n, name)
		s.tally.add(&s.tally.copied)
		return
	}
	e := c.err
	e.f("  [%s%s%s] %scopy fail%s  %s  (rc=1; sorted locally, rerun ftp-camera --push)\n", e.c, ts, e.n, e.r, e.n, name)
	for line := range strings.SplitSeq(err.Error(), "\n") {
		e.f("    %s\n", line)
	}
	s.tally.add(&s.tally.copyErr)
	s.unshipped(sorted)
}

// unshipped lists a sorted file that did not reach the mount, for the summary (E57, E66).
func (s *session) unshipped(sorted string) {
	if !s.c.opts.asyncCopy {
		return
	}
	s.tally.mu.Lock()
	defer s.tally.mu.Unlock()
	s.tally.failed = append(s.tally.failed, sorted)
}

// copyIntoMount writes sorted into the mount under a temporary name, then renames it, so an
// interrupted copy never sits in the cache under the real name (E63).
func (s *session) copyIntoMount(ctx context.Context, sorted string) error {
	if _, err := s.c.env.Runner.Run(ctx, execx.Cmd{Name: findmntBin, Args: []string{"-T", s.mount, "-t", "fuse.rclone", "--noheadings"}}); err != nil {
		return fmt.Errorf("%s is no longer served by an rclone mount", s.mount)
	}
	destDir := strings.TrimSuffix(s.mount, "/") + "/" + filepath.Dir(s.c.rel(sorted))
	name := filepath.Base(sorted)
	tmp := destDir + "/.ftp-camera.tmp." + name
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", destDir, err)
	}
	if err := copyFile(sorted, tmp); err != nil {
		return errors.Join(err, removeIfThere(tmp))
	}
	if err := os.Rename(tmp, destDir+"/"+name); err != nil {
		return errors.Join(fmt.Errorf("rename into place: %w", err), removeIfThere(tmp))
	}
	return nil
}

func removeIfThere(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}

// copyFile copies src's bytes to a new dst with src's permission bits (no ownership: a cloud
// mount cannot hold it).
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer closeQuietly(in)
	info, err := in.Stat()
	if err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		return errors.Join(fmt.Errorf("copy to %s: %w", dst, err), out.Close())
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("finish %s: %w", dst, err)
	}
	return nil
}

// viewerMatches is whether a file is of the viewer's class (E54).
func viewerMatches(class, path string) bool {
	switch ext := strings.ToLower(extOf(path)); class {
	case viewRAW:
		return ext == extARW
	case viewJPG:
		return ext == extJPG || ext == extJPEG
	}
	return false
}

// view is the default-mode viewer's handler: a completed upload of the class is shown (E53).
func (s *session) view(ctx context.Context, u upload) error {
	if stopping(ctx) || !viewerMatches(s.c.opts.viewer, u.path) {
		return nil
	}
	return s.show(ctx, u.path)
}

// show opens path in geeqie, detached, routed to the running window (E54).
func (s *session) show(ctx context.Context, path string) error {
	c := s.c
	c.out.f("  [%s%s%s] %sview%s  %s  → geeqie\n", c.out.c, s.stamp(), c.out.n, c.out.g, c.out.n, filepath.Base(path))
	_, err := c.env.Runner.Run(ctx, execx.Cmd{
		Name: "setsid", Args: []string{"-f", "geeqie", "--file=" + path},
		Env: []string{"GQ_NEW_INSTANCE="}, Stdout: io.Discard, Stderr: io.Discard,
	})
	if err != nil {
		return fmt.Errorf("geeqie could not be started: %w", err)
	}
	return nil
}
