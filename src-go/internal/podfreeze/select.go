package podfreeze

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/freeze"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/freeze/cli"
)

const (
	keyCCY     = "ccy"
	keyAll     = "all"
	keyNetwork = "net:"
	keyID      = "id:"
)

// Groups are the menu rows in order: the CCY sessions, everything, each network in use, then each
// value of each identity axis that actually tells two sessions apart.
func (e *Engine) Groups(inv freeze.Inventory[record]) []freeze.Group {
	groups := []freeze.Group{
		{Key: keyCCY, Description: "all CCY (Claude) containers", Members: ccyNames(inv)},
		{Key: keyAll, Description: "all containers", Members: inv.Names()},
	}
	nets := networkMap(inv)
	for _, net := range networkNames(nets) {
		groups = append(groups, freeze.Group{Key: keyNetwork + net, Description: "network: " + net, Members: nets[net]})
	}
	for _, ax := range identityAxes {
		if !discriminates(ax, inv) {
			continue
		}
		for _, value := range identityValues(ax, inv) {
			groups = append(groups, freeze.Group{
				Key: keyID + ax.name + ":" + value, Description: ax.name + ": " + value,
				Members: identityNames(ax, value, inv),
			})
		}
	}
	return groups
}

// Select resolves a menu key to its members. A network or identity value that vanished is said
// on stderr and returned as freeze.ErrGroupGone, so the menu re-prompts instead of ending.
func (e *Engine) Select(ctx context.Context, inv freeze.Inventory[record], key string) ([]string, error) {
	names, err := e.selectKey(ctx, inv, key)
	return names, freeze.SelectGone(e.Err, tool, err)
}

func (e *Engine) selectKey(ctx context.Context, inv freeze.Inventory[record], key string) ([]string, error) {
	switch {
	case key == keyCCY:
		return ccyNames(inv), nil
	case key == keyAll:
		return inv.SelectAll(), nil
	case strings.HasPrefix(key, keyNetwork):
		return e.selectNetwork(ctx, inv, strings.TrimPrefix(key, keyNetwork))
	case strings.HasPrefix(key, keyID):
		// id:<axis>:<value>; the value may itself contain a colon, so only the axis is split off.
		if axis, value, found := strings.Cut(strings.TrimPrefix(key, keyID), ":"); found {
			return e.selectIdentity(inv, axis, value)
		}
	}
	return nil, freeze.Die("internal error: unknown target key '%s'.", key)
}

// SelectTarget resolves the one-shot command line to a selection: a mode (the target flag, or
// cli.ModeNames), its value, and the names given. A vanished network or identity value is fatal
// here, with the same words the menu path says.
func (e *Engine) SelectTarget(ctx context.Context, inv freeze.Inventory[record], mode, value string, names []string) ([]string, error) {
	var selected []string
	var err error
	switch mode {
	case "--all":
		selected = inv.SelectAll()
	case "--ccy":
		selected = ccyNames(inv)
	case "--network":
		selected, err = e.selectNetwork(ctx, inv, value)
	case "--github", "--token", "--ssh-key":
		selected, err = e.selectIdentity(inv, strings.TrimPrefix(mode, "--"), value)
	case cli.ModeNames:
		selected, err = inv.SelectNames(names)
	default:
		err = freeze.Die("internal error: unknown target mode '%s'.", mode)
	}
	return selected, freeze.DieGone(err)
}

// selectNetwork checks the network exists (a filter naming one that does not would answer
// "nothing is on it", a confident answer to a question never asked) and returns its members from
// the inventory just loaded, so the menu's count and the action cannot disagree. The name is a
// literal, not a pattern.
func (e *Engine) selectNetwork(ctx context.Context, inv freeze.Inventory[record], net string) ([]string, error) {
	res, err := e.Runner.Run(ctx, execx.Cmd{Name: podmanBin, Args: []string{"network", "ls", "--format", "{{.Name}}"}})
	if err != nil {
		return nil, freeze.Die("podman network ls failed: %s", freeze.FailureText(res, err))
	}
	known := strings.Split(strings.TrimRight(string(res.Stdout), "\n"), "\n")
	if !slices.Contains(known, net) {
		var b strings.Builder
		fmt.Fprintf(&b, "no such network: '%s'\n  Known networks:", net)
		for _, name := range known {
			if name = strings.TrimSpace(name); name != "" {
				fmt.Fprintf(&b, "\n    %s", name)
			}
		}
		return nil, freeze.Gone("%s", b.String())
	}
	return networkMap(inv)[net], nil
}

// selectIdentity resolves one value of one axis. An unknown value must fail loudly: resolving it
// to an empty set would report "nothing to do" for a question that was never asked. It is also the
// guard that keeps an empty value from matching every session without a label.
func (e *Engine) selectIdentity(inv freeze.Inventory[record], axis, want string) ([]string, error) {
	ax, err := axisByName(axis)
	if err != nil {
		return nil, err
	}
	values := identityValues(ax, inv)
	if !slices.Contains(values, want) {
		var b strings.Builder
		fmt.Fprintf(&b, "no running or frozen CCY session has --%s '%s'", axis, want)
		if len(values) == 0 {
			fmt.Fprintf(&b, "\n  No session carries a %s label at all. Sessions started by\n"+
				"  a CCY older than 3.40.0 are unlabelled — relaunch to label them.", axis)
		} else {
			b.WriteString("\n  Known values:")
			for _, v := range values {
				fmt.Fprintf(&b, "\n    %s", v)
			}
		}
		return nil, freeze.Gone("%s", b.String())
	}
	names := identityNames(ax, want, inv)
	return names, e.warnBlindSpot(inv)
}
