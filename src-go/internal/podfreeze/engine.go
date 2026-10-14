// Package podfreeze is the Podman engine of the freeze tools: the two ps queries and their
// parsers, the CCY session and identity groups, the networks, and pause/unpause. Everything the
// person touches (menu, drill-down, derived verb, dry run, act loop) is in internal/freeze.
package podfreeze

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/freeze"
)

const (
	tool      = "podfreeze"
	podmanBin = "podman"

	// ccyLabel is set at `podman run` time by CCY 3.40.0 and later, so it cannot be inherited: a
	// container carrying it IS a live Claude session.
	ccyLabel = "ccy=true"

	// fieldSep separates the fields of `podman ps --format`. Neither a container name nor a
	// network name may contain it. The identity labels are the weaker case (an ssh key basename
	// may legally contain it), which is why the label record is split into exactly four fields
	// and any other count refused.
	fieldSep = "|"

	// labelFields is the label record's field count: name, github, token, ssh keys.
	labelFields = 4

	// noValue is what CCY writes to a label for an axis that does not apply to a session.
	noValue = "none"

	// noNetwork is the networks column of a container attached to none.
	noNetwork = "none"
)

// ccyNamePattern covers sessions started before CCY 3.40.0, which carry no label: CCY names every
// session <project>_yolo[_N] / <project>_browser[_N]. It needs a project prefix.
var ccyNamePattern = regexp.MustCompile(`^.+_(yolo|browser)(_[0-9]+)?$`)

// Config is what podfreeze declares to the library about this engine.
func Config() freeze.Config {
	return freeze.Config{
		Tool:         tool,
		StateRunning: "running",
		StateFrozen:  "paused",
		HostOnlyNote: "  The host's podman is not reachable from in here, so every container would\n" +
			"  look absent. Freezing your own session would also be unrecoverable.",
		TargetHint: "NAME..., --ccy,\n  --network NET, or --all",
	}
}

// Engine is the Podman engine. Rootless: it never escalates.
type Engine struct {
	Runner   execx.Runner
	LookPath func(string) (string, error)
	// Err is where the engine's own notes go (the identity blind spot, a vanished group).
	Err io.Writer
}

// record is the engine's per-container data, carried in freeze.Entry.Data.
type record struct {
	ccy      bool   // a CCY session, by label or by name
	labelled bool   // came back from the label=ccy=true query (CCY 3.40.0 or later)
	nets     string // the networks column, verbatim ("none" when empty)
	github   string // identity labels, empty when absent or "none"
	token    string
	sshKeys  string
}

// recordOf is the engine's record of an entry; an entry it never loaded has none.
func recordOf(e freeze.Entry) record {
	rec, ok := e.Data.(record)
	if !ok {
		return record{}
	}
	return rec
}

// Preflight checks podman is installed.
func (e *Engine) Preflight(context.Context) error {
	if _, err := e.LookPath(podmanBin); err != nil {
		return freeze.Die("podman is not installed.\n" +
			"  It is declared in playbooks/imports/play-podman.yml. Deploy it with:\n" +
			"    ansible-playbook playbooks/imports/play-podman.yml\n" +
			"  Do NOT install it by hand.")
	}
	return nil
}

// Columns is the header of the two columns after NAME and STATE. NETWORKS is padded to its width
// even though it is the last field here, because it is not the last in the drill-down.
func (e *Engine) Columns() string { return fmt.Sprintf(columnFormat, "CCY", "NETWORKS") }

// Row is the CCY marker and the networks of one container.
func (e *Engine) Row(entry freeze.Entry) string {
	rec := recordOf(entry)
	marker := "-"
	if rec.ccy {
		marker = "CCY"
	}
	return fmt.Sprintf(columnFormat, marker, rec.nets)
}

const columnFormat = "%-4s %-22s"

// Act pauses or unpauses one container and returns podman's own words, success or not.
func (e *Engine) Act(ctx context.Context, verb freeze.Verb, name string) (string, error) {
	var sub string
	switch verb {
	case freeze.VerbFreeze:
		sub = "pause"
	case freeze.VerbThaw:
		sub = "unpause"
	case freeze.VerbDerived:
		return "", freeze.Die("internal error: act was handed no verb for '%s'.", name)
	}
	return execx.Combined(ctx, e.Runner, execx.Cmd{Name: podmanBin, Args: []string{sub, name}})
}

// Load reads the machine: the running and paused containers with their networks, then the CCY
// label pass. Both queries run on every call, so the menu describes the machine now. stdout is
// parsed; stderr only ever feeds an error's text.
func (e *Engine) Load(ctx context.Context) (freeze.Inventory, error) {
	psOut, err := e.query(ctx, "podman ps",
		"ps", "--all", "--format", "{{.Names}}"+fieldSep+"{{.State}}"+fieldSep+"{{.Networks}}")
	if err != nil {
		return freeze.Inventory{}, err
	}
	entries := parseContainers(psOut)
	labelOut, err := e.query(ctx, "podman ps --filter label="+ccyLabel,
		"ps", "--all", "--filter", "label="+ccyLabel, "--format",
		"{{.Names}}"+fieldSep+`{{index .Labels "ccy-github"}}`+fieldSep+
			`{{index .Labels "ccy-token"}}`+fieldSep+`{{index .Labels "ccy-ssh-keys"}}`)
	if err != nil {
		return freeze.Inventory{}, err
	}
	if err := applyLabels(entries, labelOut); err != nil {
		return freeze.Inventory{}, err
	}
	return freeze.NewInventory(Config(), entries), nil
}

// query runs podman with stdout and stderr captured separately. A failure is fatal and reports
// what podman said; an empty answer to a query that could not be asked must never read as an
// empty machine.
func (e *Engine) query(ctx context.Context, what string, args ...string) (string, error) {
	res, err := e.Runner.Run(ctx, execx.Cmd{Name: podmanBin, Args: args})
	if err != nil {
		return "", freeze.Die("%s failed: %s", what, failureText(res, err))
	}
	return string(res.Stdout), nil
}

// failureText is what a failed command said: stderr first, then any stdout, else the error.
func failureText(res execx.Result, err error) string {
	var parts []string
	for _, s := range []string{string(res.Stderr), string(res.Stdout)} {
		if s = strings.TrimSpace(s); s != "" {
			parts = append(parts, s)
		}
	}
	if len(parts) == 0 {
		return err.Error()
	}
	return strings.Join(parts, "\n")
}

// parseContainers keeps the running and paused containers in podman's order. An exited or
// created container can be neither frozen nor thawed, and a line that is not a record (no
// separator, no name) is dropped.
func parseContainers(out string) []freeze.Entry {
	cfg := Config()
	var entries []freeze.Entry
	for line := range strings.SplitSeq(out, "\n") {
		fields := strings.SplitN(line, fieldSep, 3)
		if len(fields) < 3 || fields[0] == "" {
			continue
		}
		name, state, nets := fields[0], fields[1], fields[2]
		if state != cfg.StateRunning && state != cfg.StateFrozen {
			continue
		}
		if nets == "" {
			nets = noNetwork
		}
		entries = append(entries, freeze.Entry{
			Name: name, State: state,
			Data: record{ccy: ccyNamePattern.MatchString(name), nets: nets},
		})
	}
	return entries
}

// applyLabels reads the label pass over entries. The label is authoritative over the name
// pattern. A record that is not exactly four fields is refused: a mis-split is silent, and the
// session would be grouped under an identity it does not have. Labelled containers outside the
// inventory (an exited session) are validated but belong to no group.
func applyLabels(entries []freeze.Entry, out string) error {
	at := make(map[string]int, len(entries))
	for i, entry := range entries {
		if _, seen := at[entry.Name]; !seen {
			at[entry.Name] = i
		}
	}
	for line := range strings.SplitSeq(out, "\n") {
		fields := strings.Split(line, fieldSep)
		if fields[0] == "" {
			continue
		}
		name := fields[0]
		if len(fields) < labelFields {
			return freeze.Die("the CCY identity record for '%s' has %d of %d fields, so its labels\n"+
				"  cannot be read reliably. Relaunch that session to restamp its labels.",
				name, len(fields), labelFields)
		}
		if len(fields) > labelFields {
			return freeze.Die("a CCY identity label on '%s' contains '%s', which\n"+
				"  makes its labels ambiguous to parse. Rename the offending SSH key or token\n"+
				"  config so it has no '%s', then relaunch that session.", name, fieldSep, fieldSep)
		}
		i, present := at[name]
		if !present {
			continue
		}
		rec := recordOf(entries[i])
		rec.ccy, rec.labelled = true, true
		rec.github, rec.token, rec.sshKeys = identityLabel(fields[1]), identityLabel(fields[2]), identityLabel(fields[3])
		entries[i].Data = rec
	}
	return nil
}

// identityLabel is a label value, or empty for the "none" CCY writes for an axis that does not
// apply: nobody wants to freeze "everything with no GitHub account".
func identityLabel(v string) string {
	if v == noValue {
		return ""
	}
	return v
}

// say writes a note to the engine's stderr; a failed write is returned, not lost.
func (e *Engine) say(format string, args ...any) error {
	if _, err := fmt.Fprintf(e.Err, format, args...); err != nil {
		return errors.Join(errors.New("write note"), err)
	}
	return nil
}
