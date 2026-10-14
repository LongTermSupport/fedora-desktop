// Package lxcfreeze is the LXC engine of the freeze tools: the lxc-ls / lxc-info / config
// inventory, the bridge groups, lxc-freeze / lxc-unfreeze, and the DHCP renewal after a thaw.
// Everything the person touches (menu, drill-down, derived verb, dry run, act loop) is in
// internal/freeze. LXC here is rootful, so every command goes through internal/sudo.
package lxcfreeze

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/freeze"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sudo"
)

const (
	tool = "lxcfreeze"

	// configDir is where LXC keeps each container's config; it is root-only.
	configDir = "/var/lib/lxc"

	readyWhy = "LXC here is rootful: /var/lib/lxc is root-only, so even listing containers needs root."
	actWhy   = "Freezing and thawing LXC containers needs root."
)

// lxcTools are the programs the inventory and the verbs need, in the order they are reported.
var lxcTools = []string{"lxc-ls", "lxc-info", "lxc-freeze", "lxc-unfreeze"}

// Config is what lxcfreeze declares to the library about this engine.
func Config() freeze.Config {
	return freeze.Config{
		Tool:         tool,
		StateRunning: stateRunning,
		StateFrozen:  stateFrozen,
		HostOnlyNote: "  The host's LXC is not reachable from in here, so every container would look\n" +
			"  absent and this tool would report a machine with nothing on it.",
		TargetHint: "NAME..., --bridge BR,\n  or --all",
		ListNote:   "  A STOPPED container is not listed: it can be neither frozen nor thawed.",
		FreezeNote: "  While frozen, the container answers nothing: every ssh session into it\n" +
			"  dies with the frozen TCP connection, and so does any agent socket forwarded\n" +
			"  over one. Thaw renews the DHCP lease, so the address comes back even after a\n" +
			"  freeze longer than the lease — the connections do not. Reconnect after thawing\n" +
			"  rather than trusting an old session.",
	}
}

// Engine is the LXC engine. It reads and acts only as root, through Sudo.
type Engine struct {
	Sudo     *sudo.Sudo
	LookPath func(string) (string, error)
	// Err is where the engine's own notes go (the unreadable list, a vanished bridge).
	Err io.Writer

	// unreadable are the containers the last Load could not read, for the list footer.
	unreadable []string
}

// record is the engine's per-container data, carried in freeze.Entry.Data.
type record struct {
	bridge string // the first interface's bridge, or one of the two non-bridge labels
	ipv4   string // the first IPv4 address; blank when there is none, ipv4Unreadable when unasked
}

// recordOf is the engine's record of an entry; an entry it never loaded has none.
func recordOf(e freeze.Entry) record {
	rec, ok := e.Data.(record)
	if !ok {
		return record{}
	}
	return rec
}

// Preflight checks the LXC tools are installed and that sudo exists. Root itself is asked for at
// the start of each Load and each act batch.
func (e *Engine) Preflight(context.Context) error {
	var missing []string
	for _, name := range lxcTools {
		if _, err := e.LookPath(name); err != nil {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return freeze.Die("LXC is not installed on this host — missing: %s\n"+
			"  This is not 'no containers': nothing here can answer the question at all.\n"+
			"  LXC is declared in playbooks/imports/play-lxc-install-config.yml, which\n"+
			"  playbook-main.yml imports. Deploy it with:\n"+
			"    ansible-playbook playbooks/imports/play-lxc-install-config.yml\n"+
			"  Do NOT install it by hand.", strings.Join(missing, " "))
	}
	if _, err := e.LookPath("sudo"); err != nil {
		return freeze.Die("no sudo on this host, and every LXC query needs root.")
	}
	return nil
}

// Columns is the header of the two columns after NAME and STATE, padded to the last.
func (e *Engine) Columns() string { return columns(bridgeHeader, "IPV4") }

// Row is the bridge and the address of one container.
func (e *Engine) Row(entry freeze.Entry) string {
	rec := recordOf(entry)
	return columns(rec.bridge, rec.ipv4)
}

const bridgeHeader = "BRIDGE"

func columns(bridge, addr string) string {
	return fmt.Sprintf("%-*s%-*s", tableColWidth, bridge, tableColWidth, addr)
}

// ListFooter says an empty machine is empty, but only when nothing was unreadable: a list that
// could not be read is not known to be empty.
func (e *Engine) ListFooter(inv freeze.Inventory) string {
	if inv.Len() > 0 || len(e.unreadable) > 0 {
		return ""
	}
	return "\n  LXC is installed and has no running or frozen containers.\n" +
		"  (A STOPPED container is not counted — see 'sudo lxc-ls -f'.)\n"
}

// BeforeAct readies root before a batch of acts, so a menu left idle past sudo's timestamp does
// not prompt for a password with no explanation.
func (e *Engine) BeforeAct(ctx context.Context) error {
	if err := e.Sudo.Ready(ctx, actWhy); err != nil {
		return freeze.Die("sudo was refused, so nothing was frozen or thawed.\n" +
			"  Nothing was changed. This is NOT 'nothing to do' — the question was\n" +
			"  never asked.")
	}
	return nil
}

// Act freezes or thaws one container and returns lxc's own words on a failure. A thaw renews the
// DHCP lease, but only after an unfreeze that worked.
func (e *Engine) Act(ctx context.Context, verb freeze.Verb, name string) (string, error) {
	switch verb {
	case freeze.VerbFreeze:
		return e.runAct(ctx, "lxc-freeze", "-n", name)
	case freeze.VerbThaw:
		if out, err := e.runAct(ctx, "lxc-unfreeze", "-n", name); err != nil {
			return out, err
		}
		return e.renewDHCP(ctx, name)
	case freeze.VerbDerived:
	}
	return "", freeze.Die("internal error: act was handed no verb for '%s'.", name)
}

// Load reads the machine as root: every container's state, then for the running and frozen ones
// the bridge and the address. A container whose state cannot be read is in no group and is named
// once, after this load. stdout is parsed; stderr only ever feeds an error's text.
func (e *Engine) Load(ctx context.Context) (freeze.Inventory, error) {
	if err := e.Sudo.Ready(ctx, readyWhy); err != nil {
		return freeze.Inventory{}, freeze.Die("sudo was refused, so no LXC state could be read.\n" +
			"  Nothing was changed. This is NOT 'no containers found' — the question was\n" +
			"  never asked, and reporting an empty machine here would be a guess.")
	}
	listed, err := e.Sudo.Capture(ctx, "lxc-ls", "-1")
	if err != nil {
		return freeze.Inventory{}, e.fatal("lxc-ls failed, so the container list is unknown", listed, err)
	}
	var entries []freeze.Entry
	e.unreadable = nil
	for line := range strings.SplitSeq(string(listed.Stdout), "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		entry, readable, err := e.read(ctx, name)
		if err != nil {
			return freeze.Inventory{}, err
		}
		if !readable {
			e.unreadable = append(e.unreadable, name)
			continue
		}
		if entry != nil {
			entries = append(entries, *entry)
		}
	}
	return freeze.NewInventory(Config(), entries), e.warnUnreadable()
}

// runAct runs one lxc command as root; its words are the container's failure text.
func (e *Engine) runAct(ctx context.Context, argv ...string) (string, error) {
	res, err := e.Sudo.Capture(ctx, argv...)
	if err != nil {
		return freeze.FailureText(res, err), err
	}
	return "", nil
}

// read is one container: its entry when it is running or frozen, no entry when it is stopped, and
// readable false when its state could not be had.
func (e *Engine) read(ctx context.Context, name string) (*freeze.Entry, bool, error) {
	info, ok, err := e.probe(ctx, "lxc-info", "-n", name, "-s")
	if err != nil || !ok {
		return nil, false, err
	}
	state := parseState(info)
	switch state {
	case stateRunning, stateFrozen:
	case stateStopped:
		return nil, true, nil
	default:
		return nil, false, nil
	}
	config, ok, err := e.probe(ctx, "cat", configDir+"/"+name+"/config")
	if err != nil {
		return nil, false, err
	}
	addr, addrOK, err := e.probe(ctx, "lxc-info", "-n", name, "-iH")
	if err != nil {
		return nil, false, err
	}
	rec := record{bridge: bridgeLabel(ok, config), ipv4: ipv4Label(addrOK, addr)}
	return &freeze.Entry{Name: name, State: state, Data: rec}, true, nil
}

// probe runs a read-only command as root. ok is false when the command ran and said no (the
// container vanished, or the file is unreadable); an error means the question could not be asked
// at all, which is fatal and never an empty answer.
func (e *Engine) probe(ctx context.Context, argv ...string) (string, bool, error) {
	res, err := e.Sudo.Capture(ctx, argv...)
	if err == nil {
		return string(res.Stdout), true, nil
	}
	if exit, exited := errors.AsType[*execx.ExitError](err); exited && exit != nil && !errors.Is(err, sudo.ErrNeedsPassword) {
		return "", false, nil
	}
	return "", false, e.fatal(argv[0]+" could not be run", res, err)
}

func (e *Engine) fatal(what string, res execx.Result, err error) error {
	return freeze.Die("%s: %s", what, freeze.FailureText(res, err))
}

// warnUnreadable names the containers this load could not read. They are in no group, no count
// and no selection, and a container of unknown state is never sent a verb.
func (e *Engine) warnUnreadable() error {
	if len(e.unreadable) == 0 {
		return nil
	}
	if err := freeze.Say(e.Err, "%s: NOTE — %d container(s) could not be read,\n"+
		"  so they are in NO group, NO count above and NO selection:\n", tool, len(e.unreadable)); err != nil {
		return err
	}
	for _, name := range e.unreadable {
		if err := freeze.Say(e.Err, "    %s\n", name); err != nil {
			return err
		}
	}
	return freeze.Say(e.Err, "  Either they went away between 'lxc-ls' and 'lxc-info' — a real race, and\n"+
		"  harmless — or 'sudo lxc-info -n NAME -s' fails for them. Run that to see\n"+
		"  which. A container of unknown state is never sent a verb.\n")
}
