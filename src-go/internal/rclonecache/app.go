// Package rclonecache is the rclone-cache-status command (Plan 00192 SPEC-rclone-cache-status.md,
// E1-E21): each rclone mount's VFS cache use, from findmnt, the mount processes' arguments in
// /proc and one RC call (vfs/stats through rclonerc, the credential in the child's
// environment), and how much of one directory inside a mount its cache mirror holds.
package rclonecache

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/rclonerc"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

//go:embed usage.txt
var usageText string

const tool = "rclone-cache-status"

// Env is everything a run touches outside its own memory and the directories it walks.
type Env struct {
	Runner           execx.Runner
	Getenv           func(string) string
	Getwd            func() (string, error)
	LookPath         func(string) (string, error)
	Proc             fs.FS
	Stdout           io.Writer
	Stderr           io.Writer
	StdoutIsTerminal bool
}

// NewEnv is the production environment.
func NewEnv() Env {
	return Env{
		Runner: execx.OS{}, Getenv: os.Getenv, Getwd: os.Getwd, LookPath: exec.LookPath,
		Proc: os.DirFS("/proc"), Stdout: os.Stdout, Stderr: os.Stderr,
		StdoutIsTerminal: term.FileIsTerminal(os.Stdout),
	}
}

// App is one rclone-cache-status invocation.
type App struct {
	Env Env
}

// mount is one rclone mount and what its process says about it (E8, E9).
type mount struct {
	name, remotePath, target string
	rcAddr, cacheDir         string
	rcErr                    string // set when the RC address cannot be decided
}

// run holds one invocation's state.
type run struct {
	env    Env
	rc     *rclonerc.Client
	colour bool
	p      term.Printer
	failed bool
}

// Run is the whole command; it returns the exit code.
func (a App) Run(ctx context.Context, args []string) term.ExitCode {
	env := a.Env
	r := &run{
		env:    env,
		rc:     &rclonerc.Client{Runner: env.Runner, Getenv: env.Getenv, Proc: env.Proc},
		colour: env.StdoutIsTerminal && env.Getenv("NO_COLOR") == "",
	}
	opts, err := parseArgs(args)
	if err != nil {
		r.diagf("%s — see '%s --help'.", err, tool)
		return term.ExitUsage
	}
	if opts.help {
		r.p.Print(env.Stdout, usageText)
		return r.finish()
	}
	if _, err := env.LookPath("rclone"); err != nil {
		r.diagf("rclone is not installed; it is installed by play-rclone.yml.")
		return term.ExitFailure
	}
	if err := r.report(ctx, opts); err != nil {
		r.diagf("%s", err)
		return term.ExitFailure
	}
	return r.finish()
}

func (r *run) finish() term.ExitCode {
	if err := r.p.Err(); err != nil {
		r.diagf("cannot write the report: %v", err)
		return term.ExitFailure
	}
	if r.failed {
		return term.ExitFailure
	}
	return term.ExitOK
}

// diagf writes one diagnostic line on stderr; one that cannot be written fails the run.
func (r *run) diagf(format string, args ...any) {
	if _, err := fmt.Fprintf(r.env.Stderr, tool+": "+format+"\n", args...); err != nil {
		r.failed = true
	}
}

func (r *run) line(s string) { r.p.Print(r.env.Stdout, s+"\n") }

// paint wraps s in an escape code on a colour terminal (E20).
func (r *run) paint(code, s string) string {
	if !r.colour {
		return s
	}
	return code + s + reset
}

const (
	bold   = "\x1b[1m"
	dim    = "\x1b[2m"
	green  = "\x1b[0;32m"
	yellow = "\x1b[1;33m"
	cyan   = "\x1b[0;36m"
	red    = "\x1b[0;31m"
	reset  = "\x1b[0m"
)

// report is E4-E19 in order: what is asked about, the mounts, the summary and the analysis.
func (r *run) report(ctx context.Context, opts options) error {
	queryPath, filter, err := r.subject(opts)
	if err != nil {
		return err
	}
	mounts, err := r.listMounts(ctx)
	if err != nil {
		return err
	}
	var queryMount *mount
	if queryPath != "" {
		queryMount = containing(mounts, queryPath)
		if queryMount == nil {
			if opts.hasTarget {
				return fmt.Errorf("%s is not inside any rclone mount", queryPath)
			}
			queryPath = "" // the working directory is outside every mount: the whole summary (E5)
		}
	}
	heading := "rclone cache status"
	if opts.label != "" {
		heading += " (" + opts.label + ")"
	}
	r.line(r.paint(bold, "=== "+heading+" ==="))

	var shown []mount
	for _, m := range mounts {
		if filter != "" && m.name != filter || queryMount != nil && m.target != queryMount.target {
			continue
		}
		shown = append(shown, m)
	}
	if len(shown) == 0 {
		r.line(r.paint(yellow, noMountText(filter)))
		return nil
	}
	for _, m := range shown {
		r.describe(ctx, m)
		if queryMount != nil {
			r.directory(m, queryPath)
		}
	}
	return nil
}

// noMountText is E10's line. A query path always has its mount, so only a filter or nothing
// can find none.
func noMountText(filter string) string {
	if filter != "" {
		return "  No rclone mount matching '" + filter + "' found."
	}
	return "  No rclone mounts found."
}

// subject is the query path or the mount name filter (E4, E5).
func (r *run) subject(opts options) (string, string, error) {
	if opts.hasTarget {
		return r.targetSubject(opts.target)
	}
	if opts.noDir {
		return "", "", nil
	}
	cwd, err := r.env.Getwd()
	if err != nil {
		return "", "", fmt.Errorf("cannot read the working directory: %w", err)
	}
	// A working directory that cannot be resolved is still the path the person stands in.
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}
	return filepath.Clean(cwd), "", nil
}

// targetSubject reads a TARGET: an existing directory is a query path, anything else a name.
func (r *run) targetSubject(target string) (string, string, error) {
	path := target
	if !filepath.IsAbs(path) {
		cwd, err := r.env.Getwd()
		if err != nil {
			return "", "", fmt.Errorf("cannot read the working directory: %w", err)
		}
		path = filepath.Join(cwd, path)
	}
	if !isDirectory(path) {
		return "", target, nil
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", "", fmt.Errorf("cannot resolve %s: %w", path, err)
	}
	return resolved, "", nil
}

func isDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// containing is the deepest mount whose target is path or above it (E6).
func containing(mounts []mount, path string) *mount {
	var best *mount
	for i := range mounts {
		t := mounts[i].target
		if path == t || strings.HasPrefix(path, strings.TrimSuffix(t, "/")+"/") {
			if best == nil || len(t) > len(best.target) {
				best = &mounts[i]
			}
		}
	}
	return best
}

// listMounts is findmnt's rclone mounts, each with its process's RC address and cache
// directory (E7-E9).
func (r *run) listMounts(ctx context.Context) ([]mount, error) {
	res, err := r.env.Runner.Run(ctx, execx.Cmd{
		Name: "findmnt", Args: []string{"-J", "-l", "-o", "SOURCE,TARGET", "-t", "fuse.rclone"},
	})
	var exitErr *execx.ExitError
	if errors.As(err, &exitErr) && exitErr.Code == 1 && len(bytes.TrimSpace(res.Stdout)) == 0 {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("findmnt could not list the rclone mounts: %w", err)
	}
	var listing struct {
		Filesystems []struct {
			Source string `json:"source"`
			Target string `json:"target"`
		} `json:"filesystems"`
	}
	if err := json.Unmarshal(res.Stdout, &listing); err != nil {
		return nil, fmt.Errorf("findmnt could not list the rclone mounts: %w", err)
	}
	mounts := make([]mount, 0, len(listing.Filesystems))
	for _, fsys := range listing.Filesystems {
		parts := strings.SplitN(fsys.Source, ":", 2)
		m := mount{name: parts[0], target: fsys.Target}
		if len(parts) == 2 {
			m.remotePath = strings.TrimPrefix(parts[1], "/")
		}
		args, err := r.rc.MountProcess(fsys.Target)
		if err != nil {
			return nil, err
		}
		switch addrs := rclonerc.FlagValues(args, "--rc-addr"); len(addrs) {
		case 0:
		case 1:
			m.rcAddr = addrs[0]
		default:
			m.rcErr = "more than one --rc-addr: " + strings.Join(addrs, " ")
		}
		m.cacheDir = filepath.Join(r.env.Getenv("HOME"), ".cache", "rclone")
		if dirs := rclonerc.FlagValues(args, "--cache-dir"); len(dirs) > 0 {
			m.cacheDir = dirs[0]
		}
		mounts = append(mounts, m)
	}
	return mounts, nil
}

// describe prints a mount's summary line (E12-E14).
func (r *run) describe(ctx context.Context, m mount) {
	name := "  " + r.paint(cyan, m.name) + "  "
	if m.rcErr == "" && m.rcAddr == "" {
		r.line(name + r.paint(dim, "(no --rc; cannot query)"))
		return
	}
	s, reason := r.query(ctx, m)
	if reason != "" {
		r.line(name + r.paint(red, "error: "+reason))
		return
	}
	usage := IEC(s.used) + " / ∞"
	if s.max > 0 {
		pct := s.used * 100 / s.max
		colour := green
		if pct >= 95 {
			colour = red
		} else if pct >= 80 {
			colour = yellow
		}
		usage = IEC(s.used) + " / " + IEC(s.max) + " " + r.paint(colour, fmt.Sprintf("(%d%%)", pct))
	}
	line := fmt.Sprintf("%scache: %s · %d files · max-age %s", name, usage, s.files, r.paint(dim, Age(s.maxAgeS)))
	if s.queued > 0 {
		line += fmt.Sprintf(" · queued: %d", s.queued)
	}
	if s.inProgress > 0 {
		line += fmt.Sprintf(" · uploading: %d", s.inProgress)
	}
	if s.errored > 0 {
		line += " · " + r.paint(red, fmt.Sprintf("%d errored", s.errored))
	}
	if s.outOfSpace {
		line += " · " + r.paint(red, "OUT OF SPACE")
	}
	r.line(line)
}

// query asks a mount's RC for vfs/stats; a non-empty reason is why it could not (E12).
func (r *run) query(ctx context.Context, m mount) (stats, string) {
	if m.rcErr != "" {
		return stats{}, m.rcErr
	}
	if _, _, err := r.rc.Credential(); err != nil {
		r.diagf("%s: %v", m.name, err)
		return stats{}, "rc credential missing — run play-rclone.yml"
	}
	res, err := r.rc.Call(ctx, m.rcAddr, "vfs/stats")
	if err != nil {
		out := strings.TrimSpace(string(res.Stdout) + string(res.Stderr))
		if out == "" {
			out = err.Error()
		}
		r.diagf("%s: %s", m.name, out)
		if strings.Contains(out, "401") || strings.Contains(out, "Unauthorized") {
			return stats{}, "rc rejected credentials (401)"
		}
		return stats{}, "rc unreachable"
	}
	s, err := parseStats(res.Stdout)
	if err != nil {
		r.diagf("%s: vfs/stats: %v", m.name, err)
		return stats{}, "parse failed"
	}
	return s, ""
}

// directory prints the analysis of queryPath against its cache mirror (E16-E19).
func (r *run) directory(m mount, queryPath string) {
	mirror := filepath.Join(m.cacheDir, "vfs", m.name)
	if m.remotePath != "" {
		mirror = filepath.Join(mirror, m.remotePath)
	}
	if rel := strings.TrimPrefix(strings.TrimPrefix(queryPath, m.target), "/"); rel != "" {
		mirror = filepath.Join(mirror, rel)
	}
	r.line("    " + r.paint(dim, "└─ scanning "+queryPath+" ..."))
	cov := analyse(queryPath, mirror)
	r.line("    " + r.paint(dim, "└─ in "+queryPath))
	for _, err := range cov.unreadable {
		r.diagf("cannot read %v", err)
	}
	switch {
	case cov.totalFiles > 0:
		r.counts(m, cov)
	case len(cov.unreadable) == 0:
		r.line("       " + r.paint(dim, "(no files)"))
	}
	if n := len(cov.unreadable); n > 0 {
		r.failed = true
		noun := "entries"
		if n == 1 {
			noun = "entry"
		}
		r.line("       " + r.paint(red, fmt.Sprintf("incomplete: %d %s could not be read", n, noun)))
	}
}

// counts is E18's coverage lines.
func (r *run) counts(m mount, cov coverage) {
	filePct := cov.cachedFiles * 100 / cov.totalFiles
	var sizePct int64
	if cov.totalSize > 0 {
		sizePct = cov.cachedSize * 100 / cov.totalSize
	}
	r.line(fmt.Sprintf("       %d files · %s total", cov.totalFiles, IEC(cov.totalSize)))
	r.line(fmt.Sprintf("       %s:   %d files (%d%%)  ·  %s (%d%%)", r.paint(green, "cached"),
		cov.cachedFiles, filePct, IEC(cov.cachedSize), sizePct))
	r.line(fmt.Sprintf("       %s: %d files (%d%%)  ·  %s (%d%%)", r.paint(yellow, "uncached"),
		cov.totalFiles-cov.cachedFiles, 100-filePct, IEC(cov.totalSize-cov.cachedSize), 100-sizePct))
	if len(cov.top) == 0 {
		return
	}
	r.line("       " + r.paint(dim, "top uncached:"))
	for _, f := range cov.top {
		r.line(fmt.Sprintf("         %8s  %s", IEC(f.size), strings.TrimPrefix(f.path, m.target+"/")))
	}
}
