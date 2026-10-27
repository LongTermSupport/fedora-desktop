package reclaim

import (
	"context"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// engine is one container engine's half of E22: how its storage is read and pruned, and
// what a failed read says.
type engine struct {
	name  string
	title string
	df    execx.Cmd
	// unshared, when set, is df inside the engine's user namespace, where a volume's data
	// owned by a container's uid can be read; it is tried when df was refused permission.
	unshared execx.Cmd
	prune    execx.Cmd
	question string
	pruned   string
	// unread is said when df fails, given what df printed.
	unread func(r *run, out string)
	// partial, when set, replaces unread when df was still refused permission: what can be read
	// without df shown, and what is safe to prune offered; false when that prune failed.
	partial func(r *run, ctx context.Context, out string) bool
}

var podmanEngine = engine{
	name: progPodman, title: "Podman",
	df:       execx.Cmd{Name: progPodman, Args: []string{argSystem, "df"}},
	unshared: execx.Cmd{Name: progPodman, Args: []string{"unshare", progPodman, argSystem, "df"}},
	prune:    execx.Cmd{Name: progPodman, Args: []string{argSystem, argPrune, argAllForce}},
	question: "  Prune unused podman data (images, containers, volumes)?",
	pruned:   "  Podman pruned.",
	partial:  (*run).podmanPartialPrune,
	unread: func(r *run, out string) {
		r.warn("  Cannot query podman — skipping podman prune. Reason:")
		r.show(out)
		if strings.Contains(out, "stale temp dir") {
			r.warn("  Your rootless podman store is wedged by stale temp dirs")
			r.warn("  (owned by mapped sub-UIDs; 'podman unshare' cannot clear")
			r.warn("  them). Repair it on the HOST — this only removes orphaned")
			r.warn("  scratch as root and does NOT touch running containers/sessions:")
			r.say("    ansible-playbook CLAUDE/Plan/00062-disk-reclaim-tui/podman-store-repair.yml")
		}
	},
}

// dockerEngine is pruned as the user whose `docker system df` answered (B4).
var dockerEngine = engine{
	name: progDocker, title: "Docker",
	df:       execx.Cmd{Name: progDocker, Args: []string{argSystem, "df"}},
	prune:    execx.Cmd{Name: progDocker, Args: []string{argSystem, argPrune, argAllForce}},
	question: "  Prune unused docker data?",
	pruned:   "  Docker pruned.",
	unread: func(r *run, _ string) {
		r.dimLine("  docker is installed but not usable (daemon down, or not in the docker group) — skipping.")
	},
}

// retry is the read to make after df failed with err: e's unshared read when df was refused
// permission, else none.
func (e engine) retry(err error) (execx.Cmd, bool) {
	if e.unshared.Name == "" || err == nil || !strings.Contains(err.Error(), "permission denied") {
		return execx.Cmd{}, false
	}
	return e.unshared, true
}

// containers is E22.
func (r *run) containers(ctx context.Context) bool {
	var engines []engine
	for _, e := range []engine{podmanEngine, dockerEngine} {
		if r.have(e.name) {
			engines = append(engines, e)
		}
	}
	if len(engines) == 0 {
		r.warn("  Neither podman nor docker is installed.")
		return true
	}
	done := true
	for _, e := range engines {
		done = r.pruneEngine(ctx, e) && done
	}
	return done
}

// pruneEngine shows e's storage and prunes it on a yes; an engine that cannot be read is not
// offered, and is not a failure, since nothing was run.
func (r *run) pruneEngine(ctx context.Context, e engine) bool {
	out, err := execx.Combined(ctx, r.env.Runner, e.df)
	if retry, ok := e.retry(err); ok {
		r.warn("  " + e.df.Line() + " was refused permission; reading it again inside " + e.name +
			"'s user namespace (" + retry.Line() + "). Reason:")
		r.show(out)
		out, err = execx.Combined(ctx, r.env.Runner, retry)
	}
	if err != nil {
		if e.partial != nil && refusedPermission(out) {
			return e.partial(r, ctx, out)
		}
		e.unread(r, out)
		return true
	}
	r.info("  " + e.title + " storage usage:")
	r.show(out)
	if !r.confirm(ctx, e.question) {
		r.say("  Skipped " + e.name + ".")
		return true
	}
	if err := r.stream(finished(ctx), e.prune); err != nil {
		r.errLine("  " + e.name + " system prune reported an error.")
		return false
	}
	r.okLine(e.pruned)
	return true
}
