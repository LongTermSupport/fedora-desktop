package ccylaunch

import (
	"context"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/engine"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// F20, --list-containers ENGINE [--all] [FILTER...]: the names of the containers `ps` selects
// (every one with --all, else the running ones), each FILTER an engine --filter expression,
// one a line on stdout, exit 0; none is an empty stdout and exit 0. An engine that could not
// list is exit 1 with its cause, never "no containers".
//
// F21, --stop-containers ENGINE NAME...: each container stopped in turn, a line on stderr for
// each: "✓" stopped, "already gone" when the stop failed and the engine's store no longer has
// it (asked of the store, never read from the stop's words), or "✗" with the engine's words.
// Exit 0 when every one is stopped or gone, 1 when any is not, every one still tried.
//
// F22, --remove-network ENGINE NETWORK: exit 0, silent, removed; ExitGone (5) when the removal
// failed and the engine no longer has the network; exit 1 with the engine's words when it
// could not be removed (or whether it is still there could not be asked).
//
// Anything but those arguments is exit 64. All three are answered under CCY_LEGACY too: the
// bash asks them.

const (
	listContainersFlag = "--list-containers"
	stopContainersFlag = "--stop-containers"
	removeNetworkFlag  = "--remove-network"
	listAllFlag        = "--all"
	listUsage          = "usage: claude-yolo " + listContainersFlag + " ENGINE [--all] [FILTER...]\n"
	stopUsage          = "usage: claude-yolo " + stopContainersFlag + " ENGINE NAME...\n"
	removeUsage        = "usage: claude-yolo " + removeNetworkFlag + " ENGINE NETWORK\n"
)

// ExitGone is --remove-network's exit for a network the engine no longer has.
const ExitGone term.ExitCode = 5

// listContainers is F20.
func (f Front) listContainers(ctx context.Context) term.ExitCode {
	if len(f.Args) < 2 {
		return f.usage(listUsage)
	}
	const unlisted = "ccy: the containers could not be listed: "
	eng, err := engine.New(engine.Kind(f.Args[1]), f.Runner)
	if err != nil {
		return f.fail(unlisted + err.Error())
	}
	q := engine.ContainerQuery{Filters: f.Args[2:]}
	if len(q.Filters) > 0 && q.Filters[0] == listAllFlag {
		q.All, q.Filters = true, q.Filters[1:]
	}
	names, err := eng.Containers(ctx, q)
	if err != nil {
		return f.fail(unlisted + strings.TrimSpace(err.Error()))
	}
	var p term.Printer
	for _, name := range names {
		p.Print(f.Stdout, name+"\n")
	}
	if err := p.Err(); err != nil {
		return f.fail("ccy: could not write the answer: " + err.Error())
	}
	return term.ExitOK
}

// stopContainers is F21.
func (f Front) stopContainers(ctx context.Context) term.ExitCode {
	if len(f.Args) < 3 {
		return f.usage(stopUsage)
	}
	eng, err := engine.New(engine.Kind(f.Args[1]), f.Runner)
	if err != nil {
		return f.fail("ccy: no container could be stopped: " + err.Error())
	}
	code := term.ExitOK
	var p term.Printer
	for _, name := range f.Args[2:] {
		gone, why := settle(ctx, eng, engine.KindContainer, name, eng.Stop(ctx, name))
		switch {
		case why != "":
			code = term.ExitFailure
			p.Print(f.Stderr, "  Stopping "+name+"... ✗ "+why+"\n")
		case gone:
			p.Print(f.Stderr, "  Stopping "+name+"... already gone\n")
		default:
			p.Print(f.Stderr, "  Stopping "+name+"... ✓\n")
		}
	}
	if p.Err() != nil {
		return term.ExitFailure
	}
	return code
}

// removeNetwork is F22.
func (f Front) removeNetwork(ctx context.Context) term.ExitCode {
	if len(f.Args) != 3 {
		return f.usage(removeUsage)
	}
	network := f.Args[2]
	unremoved := "ccy: the network " + network + " could not be removed: "
	eng, err := engine.New(engine.Kind(f.Args[1]), f.Runner)
	if err != nil {
		return f.fail(unremoved + err.Error())
	}
	gone, why := settle(ctx, eng, engine.KindNetwork, network, eng.RemoveNetwork(ctx, network))
	switch {
	case why != "":
		return f.fail(unremoved + why)
	case gone:
		return f.failWith(ExitGone, "ccy: the network "+network+" is not there any more")
	}
	return term.ExitOK
}

// settle reads a stop's or a removal's error: none is done; a failure of an object the store
// no longer has is gone; any other is why, the engine's words (and the store's, when it could
// not be asked).
func settle(ctx context.Context, eng engine.Engine, kind, name string, err error) (bool, string) {
	if err == nil {
		return false, ""
	}
	cause := strings.TrimSpace(err.Error())
	there, askErr := eng.StoreHas(ctx, kind, name)
	switch {
	case askErr != nil:
		return false, cause + "; and whether it is still there could not be asked: " + strings.TrimSpace(askErr.Error())
	case !there:
		return true, ""
	}
	return false, cause
}
