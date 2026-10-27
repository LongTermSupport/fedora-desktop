package reclaim

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/rclonecache"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

const (
	kernelsKept        = 2
	journalDefaultSize = "500M"
	dirsListed         = 20
	cacheItemsListed   = 15
	filesListedDefault = "20"
)

var journalSize = regexp.MustCompile(`^[0-9]+[KMGT]?$`)

func isDir(p string) string {
	if info, err := os.Stat(p); err == nil && info.IsDir() {
		return ""
	}
	return "Not a directory: " + p
}

func positive(s string) string {
	if n, err := strconv.Atoi(s); err == nil && n > 0 && !strings.HasPrefix(s, "+") {
		return ""
	}
	return "Not a positive number: " + s
}

func validSize(s string) string {
	if journalSize.MatchString(s) {
		return ""
	}
	return "Not a valid size (e.g. 500M, 1G): " + s
}

// bigDirs is E17.
func (r *run) bigDirs(ctx context.Context) bool {
	dir, ok := r.askValue("Analyse which directory?", r.home, isDir)
	if !ok {
		return true
	}
	done := true
	if r.have("ncdu") {
		r.info("  Launching ncdu on " + dir + " (press 'q' to return)…")
		exit, err := r.env.Fg.Foreground(execx.Cmd{Name: "ncdu", Args: []string{"-x", dir}})
		switch {
		case err != nil:
			r.errLine("  ncdu could not start: " + err.Error())
			done = false
		case exit.Code != 0:
			r.warn(fmt.Sprintf("  ncdu exited %d.", exit.Code))
		}
	} else {
		r.warn("  ncdu is not installed — showing a du summary instead.")
		r.info("  Top-level usage under " + dir + ":")
		done = r.duSummary(ctx, []string{"-x", duBytes, "--max-depth=1", dir}, dir, dirsListed)
	}
	desktop := r.env.Getenv("DISPLAY") != "" || r.env.Getenv("WAYLAND_DISPLAY") != ""
	if r.have("baobab") && desktop && r.confirm("  Also open baobab (graphical analyser) on "+dir+"?") {
		done = r.baobab(ctx, dir) && done
	}
	return done
}

// baobab starts baobab detached, in a session of its own, its output to /dev/null.
func (r *run) baobab(ctx context.Context, dir string) bool {
	null, err := r.env.DevNull()
	if err != nil {
		r.errLine("  baobab not started: " + err.Error())
		return false
	}
	_, err = r.env.Runner.Run(ctx, execx.Cmd{Name: "setsid", Args: []string{"-f", "baobab", dir}, Stdout: null, Stderr: null})
	if cerr := null.Close(); err == nil && cerr != nil {
		err = cerr
	}
	if err != nil {
		r.errLine("  baobab could not start: " + err.Error())
		return false
	}
	r.okLine("  baobab launched in the background.")
	return true
}

// duSummary lists the n largest entries `du <args>` prints, largest first; false when du
// sized nothing.
func (r *run) duSummary(ctx context.Context, args []string, what string, n int) bool {
	res, err := r.env.Runner.Run(ctx, execx.Cmd{Name: "du", Args: args})
	entries := parseDu(string(res.Stdout))
	if len(entries) == 0 {
		r.errLine("  du could not size " + what + ".")
		if err != nil {
			r.failf("du: %v", err)
		}
		return false
	}
	slices.SortStableFunc(entries, func(a, b duEntry) int {
		if a.bytes != b.bytes {
			if a.bytes > b.bytes {
				return -1
			}
			return 1
		}
		return strings.Compare(a.path, b.path)
	})
	for _, e := range entries[:min(n, len(entries))] {
		r.say(fmt.Sprintf("  %8s  %s", rclonecache.IEC(e.bytes), e.path))
	}
	if err != nil {
		r.dimLine("  (some entries could not be read)")
	}
	return true
}

// bigFiles is E18.
func (r *run) bigFiles(context.Context) bool {
	dir, ok := r.askValue("Find big files under which directory?", r.home, isDir)
	if !ok {
		return true
	}
	countText, ok := r.askValue("How many to list?", filesListedDefault, positive)
	if !ok {
		return true
	}
	count, err := strconv.Atoi(countText)
	if err != nil {
		r.errLine("  Not a positive number: " + countText)
		return false
	}
	r.info("  Scanning " + dir + " (staying on one filesystem)…")
	found, err := largestFiles(dir, count)
	if err != nil {
		r.errLine("  Could not scan " + dir + ": " + err.Error())
		return false
	}
	for _, f := range found.files {
		r.say(fmt.Sprintf("  %8s  %s", rclonecache.IEC(f.size), f.path))
	}
	if found.unreadable > 0 {
		r.warn(fmt.Sprintf("  %d entries could not be read (first: %s)", found.unreadable, found.first))
	}
	return true
}

// packages is E19.
func (r *run) packages(ctx context.Context) bool {
	r.info("  Package cache and orphaned dependencies:")
	if !r.rootReady(ctx, "Listing and removing orphaned packages needs root.") {
		return false
	}
	res, err := r.env.Sudo.Capture(ctx, "dnf", "repoquery", "--unneeded", "-q")
	if err != nil {
		r.previewFailed(res, err, "  dnf could not list the orphaned packages.")
		return false
	}
	orphans := lines(string(res.Stdout))
	if len(orphans) > 0 {
		r.say(fmt.Sprintf("  Orphaned packages (%d):", len(orphans)))
		for _, p := range orphans {
			r.say("    " + p)
		}
	} else {
		r.dimLine("  (no orphaned packages detected in preview)")
	}
	if !r.confirm("  Run 'dnf autoremove' + 'dnf clean all' now?") {
		r.say("  Skipped.")
		return true
	}
	err = r.env.Sudo.Run(ctx, "dnf", "-y", "autoremove")
	if err == nil {
		err = r.env.Sudo.Run(ctx, "dnf", "clean", "all")
	}
	if err != nil {
		r.errLine("  dnf reported an error — see output above.")
		return false
	}
	r.okLine("  Package cleanup complete.")
	return true
}

// oldKernels is E20.
func (r *run) oldKernels(ctx context.Context) bool {
	if !r.rootReady(ctx, "Listing and removing old kernels needs root.") {
		return false
	}
	res, err := r.env.Sudo.Capture(ctx, "dnf", "repoquery", "--installonly",
		"--latest-limit=-"+strconv.Itoa(kernelsKept), "-q")
	if err != nil {
		r.previewFailed(res, err, "  dnf could not list the installed kernels.")
		return false
	}
	old := lines(string(res.Stdout))
	if len(old) == 0 {
		r.okLine(fmt.Sprintf("  No superfluous kernels — nothing to remove (keeping newest %d).", kernelsKept))
		return true
	}
	r.warn(fmt.Sprintf("  These old kernels can be removed (keeping the newest %d):", kernelsKept))
	for _, k := range old {
		r.say("    " + k)
	}
	if !r.confirm(fmt.Sprintf("  Remove %d old kernel package(s)?", len(old))) {
		r.say("  Skipped.")
		return true
	}
	if err := r.env.Sudo.Run(ctx, append([]string{"dnf", "-y", "remove"}, old...)...); err != nil {
		r.errLine("  dnf remove reported an error.")
		return false
	}
	r.okLine("  Old kernels removed.")
	return true
}

// journal is E21. A usage line that could not be read is said and its reason printed; the
// vacuum is still offered, since a full journal is when the figure is most likely missing.
func (r *run) journal(ctx context.Context) bool {
	if !r.have("journalctl") {
		r.errLine("  journalctl is not available.")
		return true
	}
	r.info("  Current journal usage:")
	r.say("    " + r.journalUsage(ctx))
	size, ok := r.askValue("Vacuum journal down to what size?", journalDefaultSize, validSize)
	if !ok {
		return true
	}
	if !r.confirm("  Vacuum system journal to " + size + "?") {
		r.say("  Skipped.")
		return true
	}
	if !r.rootReady(ctx, "Vacuuming the system journal needs root.") {
		return false
	}
	if err := r.env.Sudo.Run(ctx, "journalctl", "--vacuum-size="+size); err != nil {
		r.errLine("  journalctl --vacuum-size reported an error.")
		return false
	}
	r.okLine("  Journal vacuumed. Now: " + r.journalUsage(ctx))
	return true
}

// journalUsage is E5's line for a screen, whose reader sees a failure in the line itself.
func (r *run) journalUsage(ctx context.Context) string {
	line, read := r.journalLine(ctx)
	if !read {
		return line + " (see the reason above)"
	}
	return line
}

// rootReady is E25: sudo's credential, with the reason said first.
func (r *run) rootReady(ctx context.Context, why string) bool {
	if !r.have("sudo") {
		r.errLine("  This action needs root through sudo, and sudo is not installed.")
		return false
	}
	return r.env.Sudo.Ready(ctx, why) == nil
}

// previewFailed reports a root preview that failed: what it printed, then what failed.
func (r *run) previewFailed(res execx.Result, err error, msg string) {
	r.errOut.Print(r.env.Stderr, string(res.Stderr))
	if len(res.Stderr) == 0 {
		r.failf("%v", err)
	}
	r.errLine(msg)
}

func lines(out string) []string {
	var keep []string
	for l := range strings.SplitSeq(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			keep = append(keep, l)
		}
	}
	return keep
}

// flatpak is E23.
func (r *run) flatpak(ctx context.Context) bool {
	if !r.have(progFlatpak) {
		r.warn("  flatpak is not installed.")
		return true
	}
	r.info("  Unused flatpak runtimes:")
	if err := r.stream(ctx, execx.Cmd{Name: progFlatpak, Args: []string{"uninstall", "--unused", "--assumeno"}}); err != nil {
		r.dimLine("  (nothing unused, or preview unavailable)")
	}
	if !r.confirm("  Remove unused flatpak runtimes?") {
		r.say("  Skipped.")
		return true
	}
	if err := r.stream(ctx, execx.Cmd{Name: progFlatpak, Args: []string{"uninstall", "--unused", "-y"}}); err != nil {
		r.errLine("  flatpak uninstall reported an error.")
		return false
	}
	r.okLine("  Unused runtimes removed.")
	return true
}

// stream runs c with its output on the terminal side (E28).
func (r *run) stream(ctx context.Context, c execx.Cmd) error {
	c.Stdout, c.Stderr = r.env.Stderr, r.env.Stderr
	_, err := r.env.Runner.Run(ctx, c)
	return err
}

// cache is E24.
func (r *run) cache(ctx context.Context) bool {
	cache := filepath.Join(r.home, ".cache")
	if isDir(cache) != "" {
		r.okLine("  No ~/.cache directory.")
		return true
	}
	r.info("  Largest items in ~/.cache:")
	sized := r.duSummary(ctx, []string{duBytes, "--max-depth=1", cache}, cache, cacheItemsListed)
	r.warn("  Note: applications recreate their caches on next use.")
	if !r.confirm("  Delete the CONTENTS of ~/.cache now?") {
		r.say("  Skipped.")
		return sized
	}
	if !r.clearInside(cache) {
		r.errLine("  Some cache items could not be removed.")
		return false
	}
	r.okLine("  ~/.cache cleared.")
	return sized
}

// trash is E26.
func (r *run) trash(ctx context.Context) bool {
	if r.have("trash-empty") {
		return r.trashCLI(ctx)
	}
	trash := filepath.Join(r.home, ".local", "share", "Trash")
	if isDir(trash) != "" {
		r.okLine("  No trash directory.")
		return true
	}
	size, partial, err := r.size(ctx, trash)
	if err != nil {
		r.failf("du %s: %v", trash, err)
		size = "?"
	} else if partial {
		size += " (at least)"
	}
	r.info("  Trash size: " + size)
	if !r.confirm("  Empty ~/.local/share/Trash now?") {
		r.say("  Skipped.")
		return err == nil
	}
	if !r.clearInside(filepath.Join(trash, "files"), filepath.Join(trash, "info")) {
		r.errLine("  Some trash items could not be removed.")
		return false
	}
	r.okLine("  Trash emptied.")
	return err == nil
}

func (r *run) trashCLI(ctx context.Context) bool {
	r.info("  Trash contents:")
	if err := r.stream(ctx, execx.Cmd{Name: "trash-list"}); err != nil {
		r.dimLine("  (trash empty or unavailable)")
	}
	if !r.confirm("  Empty the trash now?") {
		r.say("  Skipped.")
		return true
	}
	if err := r.stream(ctx, execx.Cmd{Name: "trash-empty", Args: []string{"-f"}}); err != nil {
		r.errLine("  trash-empty reported an error.")
		return false
	}
	r.okLine("  Trash emptied.")
	return true
}

// sweep is E27.
func (r *run) sweep(ctx context.Context) bool {
	r.warn("  Safe sweep runs, in order:")
	r.say("    • dnf autoremove + clean all")
	r.say("    • journal vacuum (default " + journalDefaultSize + ")")
	r.say("    • podman system prune (if podman present)")
	r.say("    • flatpak unused runtimes (if flatpak present)")
	r.say("    • empty trash")
	r.say("  Each step asks for its own confirmation.")
	if !r.confirm("  Begin the safe sweep?") {
		r.say("  Sweep cancelled.")
		return true
	}
	steps := []action{{name: namePackages, do: (*run).packages}, {name: nameJournal, do: (*run).journal}}
	if r.have(progPodman) {
		steps = append(steps, action{name: nameContainers, do: (*run).containers})
	}
	if r.have(progFlatpak) {
		steps = append(steps, action{name: nameFlatpak, do: (*run).flatpak})
	}
	steps = append(steps, action{name: nameTrash, do: (*run).trash})
	done := true
	for _, s := range steps {
		r.heading(ui.Here("reclaim", "Safe sweep", s.name))
		if !s.do(r, ctx) {
			r.warn("  That step did not complete; the sweep goes on.")
			done = false
		}
	}
	r.okLine("  Safe sweep finished.")
	return done
}
