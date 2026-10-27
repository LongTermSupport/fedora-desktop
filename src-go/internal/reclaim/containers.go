package reclaim

import (
	"context"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// engine is one container engine's half of E22: how its storage is read and pruned, and
// what a failed read says.
type engine struct {
	name     string
	title    string
	df       execx.Cmd
	prune    execx.Cmd
	question string
	pruned   string
	// unread is said when df fails, given what df printed.
	unread func(r *run, out string)
}

var podmanEngine = engine{
	name: progPodman, title: "Podman",
	df:       execx.Cmd{Name: progPodman, Args: []string{argSystem, "df"}},
	prune:    execx.Cmd{Name: progPodman, Args: []string{argSystem, "prune", "-af"}},
	question: "  Prune unused podman data (images, containers, volumes)?",
	pruned:   "  Podman pruned.",
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
	prune:    execx.Cmd{Name: progDocker, Args: []string{argSystem, "prune", "-af"}},
	question: "  Prune unused docker data?",
	pruned:   "  Docker pruned.",
	unread: func(r *run, _ string) {
		r.dimLine("  docker is installed but not usable (daemon down, or not in the docker group) — skipping.")
	},
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
	if err != nil {
		e.unread(r, out)
		return true
	}
	r.info("  " + e.title + " storage usage:")
	r.show(out)
	if !r.confirm(e.question) {
		r.say("  Skipped " + e.name + ".")
		return true
	}
	if err := r.stream(ctx, e.prune); err != nil {
		r.errLine("  " + e.name + " system prune reported an error.")
		return false
	}
	r.okLine(e.pruned)
	return true
}
