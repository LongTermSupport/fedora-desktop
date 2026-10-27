package reclaim

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/rclonecache"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// report is `reclaim report` (E3-E8): every section printed, exit 1 when one could not be read.
func (r *run) report(ctx context.Context) term.ExitCode {
	complete := true
	p := func(s string) { r.out.Print(r.env.Stdout, s+"\n") }
	p("reclaim disk report — " + r.hostname())
	p("")
	p("Filesystems:")
	fsText, ok := r.filesystems(ctx)
	complete = complete && ok
	if ok {
		p(strings.TrimRight(fsText, "\n"))
	} else {
		p("  (filesystem usage unavailable)")
	}
	p("")
	p("Journal:")
	journal, ok := r.journalLine(ctx)
	complete = complete && ok
	p(journal)
	p("")
	p("Notable directories:")
	for _, dir := range r.notableDirs() {
		line, exists, ok := r.dirLine(ctx, dir)
		complete = complete && ok
		if exists {
			p(line)
		}
	}
	if r.have(progPodman) {
		p("")
		p("Podman storage:")
		storage, ok := r.podmanStorage(ctx)
		complete = complete && ok
		if ok {
			p(strings.TrimRight(storage, "\n"))
		} else {
			p("  (podman storage unavailable)")
		}
	}
	if !complete {
		return term.ExitFailure
	}
	return term.ExitOK
}

// podmanStorage is E7's `podman system df` output, read again under podman unshare when it was
// refused permission (a volume's data owned by a container's uid), each failure named; false
// when neither read.
func (r *run) podmanStorage(ctx context.Context) (string, bool) {
	cmd := podmanEngine.df
	res, err := r.env.Runner.Run(ctx, cmd)
	if retry, ok := podmanEngine.retry(err); ok {
		r.failf("%s: %v", cmd.Line(), err)
		r.failf("reading it again inside podman's user namespace: %s", retry.Line())
		cmd = retry
		res, err = r.env.Runner.Run(ctx, cmd)
	}
	if err != nil {
		r.failf("%s: %v", cmd.Line(), err)
		return "", false
	}
	return string(res.Stdout), true
}

func (r *run) hostname() string {
	name, err := r.env.Hostname()
	if err != nil {
		r.failf("could not read the host name: %v", err)
		return "this host"
	}
	return name
}

func (r *run) notableDirs() []string {
	dirs := make([]string, 0, 2+len(r.env.VarDirs))
	dirs = append(dirs, filepath.Join(r.home, ".cache"), filepath.Join(r.home, ".local", "share", "Trash"))
	return append(dirs, r.env.VarDirs...)
}

// filesystems is E4: duf, else the filtered df, else df -h; false when all failed.
func (r *run) filesystems(ctx context.Context) (string, bool) {
	if r.have("duf") {
		if res, err := r.env.Runner.Run(ctx, execx.Cmd{Name: "duf", Args: []string{"--only", "local"}}); err == nil {
			return string(res.Stdout), true
		}
	}
	res, err := r.env.Runner.Run(ctx, execx.Cmd{Name: "df", Args: []string{
		"-h", "-x", "tmpfs", "-x", "devtmpfs", "-x", "efivarfs", "-x", "overlay",
		"--output=target,size,used,avail,pcent",
	}})
	if err == nil {
		return string(res.Stdout), true
	}
	res, err = r.env.Runner.Run(ctx, execx.Cmd{Name: "df", Args: []string{"-h"}})
	if err == nil {
		return string(res.Stdout), true
	}
	r.failf("df: %v", err)
	return "", false
}

// journalLine is E5's line; false when journalctl failed.
func (r *run) journalLine(ctx context.Context) (string, bool) {
	if !r.have("journalctl") {
		return "journal: journalctl not present", true
	}
	res, err := r.env.Runner.Run(ctx, execx.Cmd{Name: "journalctl", Args: []string{"--disk-usage"}})
	if err != nil {
		r.failf("journalctl --disk-usage: %v", err)
		return "journal: usage unavailable", false
	}
	return strings.TrimSpace(string(res.Stdout)), true
}

// dirLine is E6's line for dir, whether dir exists, and false when du gave no figure.
func (r *run) dirLine(ctx context.Context, dir string) (string, bool, bool) {
	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", false, true
		}
		r.failf("cannot read %s: %v", dir, err)
		return "?\t" + dir, true, false
	}
	size, partial, err := r.size(ctx, dir)
	if err != nil {
		r.failf("du %s: %v", dir, err)
		return "?\t" + dir, true, false
	}
	line := size + "\t" + dir
	if partial {
		line += "\t(at least: some entries could not be read)"
	}
	return line, true, true
}

// size is dir's `du -s -B1` figure in numfmt's IEC form, and whether du said some entry
// inside could not be read (its figure is then a lower bound; du's own words are shown, B6).
func (r *run) size(ctx context.Context, dir string) (string, bool, error) {
	res, err := r.env.Runner.Run(ctx, execx.Cmd{Name: "du", Args: []string{"-s", duBytes, dir}})
	entries := parseDu(string(res.Stdout))
	if len(entries) == 0 {
		if err == nil {
			err = errors.New("du printed no figure")
		}
		return "", false, err
	}
	if err != nil {
		r.duComplaints(dir, string(res.Stderr))
	}
	return rclonecache.IEC(entries[0].bytes), err != nil, nil
}

// duRefusals are the openings of du's lines for an entry it was refused, before the quoted path.
var duRefusals = []string{"du: cannot read directory ", "du: cannot access "}

const duDenied = ": Permission denied"

// duComplaints shows what du said on stderr while sizing root: its permission refusals as one
// line, counted, the first named (a system directory gives dozens), every other line as it is.
func (r *run) duComplaints(root, stderr string) {
	var first string
	refused := 0
	for l := range strings.SplitSeq(strings.TrimRight(stderr, "\n"), "\n") {
		if path, ok := duRefused(l); ok {
			if refused == 0 {
				first = path
			}
			refused++
			continue
		}
		if l != "" {
			r.errOut.Print(r.env.Stderr, l+"\n")
		}
	}
	if refused == 0 {
		return
	}
	noun := "entries"
	if refused == 1 {
		noun = "entry"
	}
	r.failf("du %s: %d %s could not be read (permission denied), the first %s; the size is a lower bound.",
		root, refused, noun, first)
}

// duRefused is the path, as du quoted it, of a line saying an entry was refused permission.
func duRefused(line string) (string, bool) {
	rest, ok := strings.CutSuffix(line, duDenied)
	if !ok {
		return "", false
	}
	for _, opening := range duRefusals {
		if path, ok := strings.CutPrefix(rest, opening); ok {
			return path, true
		}
	}
	return "", false
}

// duEntry is one line of `du -B1`: a size in bytes and its path.
type duEntry struct {
	bytes int64
	path  string
}

// parseDu reads `du -B1` lines; a line that is not "<bytes>\t<path>" is left out.
func parseDu(out string) []duEntry {
	var entries []duEntry
	for l := range strings.SplitSeq(out, "\n") {
		num, path, ok := strings.Cut(l, "\t")
		if !ok {
			continue
		}
		n, err := strconv.ParseInt(num, 10, 64)
		if err != nil {
			continue
		}
		entries = append(entries, duEntry{bytes: n, path: path})
	}
	return entries
}
