// Package firewalld opens the homeserver port to a team's allow_from networks with rich
// rules, and remembers which rules it added so that removal takes away exactly those.
// Rules go to both the permanent and the runtime configuration; nothing is reloaded.
//
// The record is the installer's own: one "zone<TAB>rule" line per rule it added, kept where
// the homeserver's user cannot steer it. A rule is written to the record before firewalld is
// touched, so a failure part-way still leaves every rule that may have been added listed.
package firewalld

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"regexp"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/netprobe"
)

const (
	cmdName     = "firewall-cmd"
	noZoneText  = "no zone"
	maxPort     = 65535
	permanentOn = "--permanent"
)

// Rule is one rich rule in one zone.
type Rule struct {
	Zone string
	Text string
}

// Line is the rule as the record keeps it.
func (r Rule) Line() string { return r.Zone + "\t" + r.Text }

// Record is where the rules the installer added are kept.
type Record interface {
	// Read is the record's content and whether the record exists.
	Read() (data []byte, exists bool, err error)
	// Write replaces the record.
	Write(data []byte) error
	// Delete removes the record.
	Delete() error
}

// Firewall runs firewall-cmd through a Runner. Chatter receives what firewall-cmd prints
// when it changes a rule; Out receives the CHANGED lines.
type Firewall struct {
	Runner  execx.Runner
	Chatter io.Writer
	Out     io.Writer
}

// RichRule is the rule that accepts TCP to port from the network cidr. The text is checked
// as a CIDR in canonical form, so nothing else can reach the rule.
func RichRule(cidr string, port int) (string, error) {
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil || prefix.String() != cidr || prefix.Addr().Is4In6() {
		return "", fmt.Errorf("%q is not a network in CIDR form", cidr)
	}
	if port < 1 || port > maxPort {
		return "", fmt.Errorf("%d is not a TCP port", port)
	}
	family := "ipv4"
	if prefix.Addr().Is6() {
		family = "ipv6"
	}
	return fmt.Sprintf(`rule family="%s" source address="%s" port port="%d" protocol="tcp" accept`, family, cidr, port), nil
}

var recordLine = regexp.MustCompile(`^([^\t]+)\t(.+)$`)

// ParseRecord reads the rules out of a record; blank lines are skipped.
func ParseRecord(data []byte) ([]Rule, error) {
	var rules []Rule
	for line := range strings.SplitSeq(string(data), "\n") {
		if line == "" {
			continue
		}
		m := recordLine.FindStringSubmatch(line)
		if m == nil {
			return nil, fmt.Errorf("firewalld record: %q is not zone<TAB>rule", line)
		}
		rules = append(rules, Rule{Zone: m[1], Text: m[2]})
	}
	return rules, nil
}

func join(lines []string) []byte {
	if len(lines) == 0 {
		return nil
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

func scope(permanent bool, zone string) []string {
	args := make([]string, 0, 3)
	if permanent {
		args = append(args, permanentOn)
	}
	return append(args, "--zone="+zone)
}

// stores are visited permanent first, then runtime.
var stores = []struct {
	name      string
	permanent bool
}{{"permanent", true}, {"runtime", false}}

// ZoneOfInterface is the zone of the interface, or the default zone when it has none. The
// text "no zone" means none whatever the exit status was; any other failure is an error.
func (f *Firewall) ZoneOfInterface(ctx context.Context, dev string) (string, error) {
	arg := "--get-zone-of-interface=" + dev
	out, err := execx.Combined(ctx, f.Runner, execx.Cmd{Name: cmdName, Args: []string{arg}})
	text := strings.TrimRight(out, "\n")
	if err != nil {
		if _, refused := execx.ExitOf(err); !refused {
			return "", fmt.Errorf("%s %s: %w", cmdName, arg, err)
		}
		if text != noZoneText {
			return "", fmt.Errorf("%s %s failed: %s: %w", cmdName, arg, text, err)
		}
	}
	zone, none, parseErr := netprobe.ParseZone(text)
	if parseErr != nil {
		return "", parseErr
	}
	if !none {
		return zone, nil
	}
	return f.defaultZone(ctx)
}

// Query is whether the rule is in the zone, in the permanent or the runtime configuration.
// Any exit status of firewall-cmd is an answer (absent); only a firewall-cmd that cannot run
// is an error.
func (f *Firewall) Query(ctx context.Context, zone, rule string, permanent bool) (bool, error) {
	_, err := f.Runner.Run(ctx, execx.Cmd{Name: cmdName, Args: append(scope(permanent, zone), "--query-rich-rule="+rule)})
	if err == nil {
		return true, nil
	}
	if _, refused := execx.ExitOf(err); refused {
		return false, nil
	}
	return false, fmt.Errorf("%s --query-rich-rule: %w", cmdName, err)
}

// Sync leaves the zones holding the wanted rules: every rule in the record that is not
// wanted is removed, every wanted rule is added, and the record ends as the wanted rules.
func (f *Firewall) Sync(ctx context.Context, rec Record, wanted []Rule) error {
	wantedLines := make([]string, len(wanted))
	for i, r := range wanted {
		wantedLines[i] = r.Line()
	}
	current, exists, err := recordAhead(rec, wantedLines)
	if err != nil {
		return err
	}
	recorded, err := ParseRecord(current)
	if err != nil {
		return err
	}
	for _, r := range recorded {
		if !slices.Contains(wantedLines, r.Line()) {
			if err = f.remove(ctx, r); err != nil {
				return err
			}
		}
	}
	for _, r := range wanted {
		if err = f.add(ctx, r); err != nil {
			return err
		}
	}
	if final := join(wantedLines); !exists || !bytes.Equal(final, current) {
		if err = rec.Write(final); err != nil {
			return fmt.Errorf("writing the firewalld record: %w", err)
		}
	}
	return nil
}

// RemoveAll removes every rule in the record, then the record. Nothing outside the record is
// touched; no record means nothing to do.
func (f *Firewall) RemoveAll(ctx context.Context, rec Record) error {
	data, exists, err := rec.Read()
	if err != nil {
		return fmt.Errorf("reading the firewalld record: %w", err)
	}
	if !exists {
		return nil
	}
	rules, err := ParseRecord(data)
	if err != nil {
		return err
	}
	for _, r := range rules {
		if err = f.remove(ctx, r); err != nil {
			return err
		}
	}
	if err = rec.Delete(); err != nil {
		return fmt.Errorf("deleting the firewalld record: %w", err)
	}
	return nil
}

// recordAhead writes the record as the old rules plus the wanted ones (sorted, unique)
// before any firewalld change, so a failure part-way leaves every rule this run may have
// added in the record that removal reads. It returns the record as it then is.
func recordAhead(rec Record, wantedLines []string) ([]byte, bool, error) {
	old, exists, err := rec.Read()
	if err != nil {
		return nil, false, fmt.Errorf("reading the firewalld record: %w", err)
	}
	oldRules, err := ParseRecord(old)
	if err != nil {
		return nil, false, err
	}
	pending := slices.Clone(wantedLines)
	for _, r := range oldRules {
		pending = append(pending, r.Line())
	}
	slices.Sort(pending)
	pending = slices.Compact(pending)
	if len(pending) == 0 {
		return old, exists, nil
	}
	data := join(pending)
	if exists && bytes.Equal(data, old) {
		return old, true, nil
	}
	if err = rec.Write(data); err != nil {
		return nil, false, fmt.Errorf("writing the firewalld record: %w", err)
	}
	return data, true, nil
}

func (f *Firewall) defaultZone(ctx context.Context) (string, error) {
	res, err := f.Runner.Run(ctx, execx.Cmd{Name: cmdName, Args: []string{"--get-default-zone"}})
	if err != nil {
		return "", fmt.Errorf("%s --get-default-zone: %w", cmdName, err)
	}
	zone, none, parseErr := netprobe.ParseZone(string(res.Stdout))
	if parseErr != nil {
		return "", parseErr
	}
	if none {
		return "", errors.New("firewall-cmd --get-default-zone answered \"no zone\"")
	}
	return zone, nil
}

func (f *Firewall) change(ctx context.Context, permanent bool, zone, flag, rule string) error {
	args := append(scope(permanent, zone), flag+"="+rule)
	_, err := f.Runner.Run(ctx, execx.Cmd{Name: cmdName, Args: args, Stdout: f.Chatter, Stderr: f.Chatter})
	if err != nil {
		return fmt.Errorf("%s %s: %w", cmdName, strings.Join(args, " "), err)
	}
	return nil
}

func (f *Firewall) changed(text string) error {
	if _, err := fmt.Fprintf(f.Out, "CHANGED\t%s\n", text); err != nil {
		return fmt.Errorf("writing the change report: %w", err)
	}
	return nil
}

func (f *Firewall) add(ctx context.Context, r Rule) error {
	return f.apply(ctx, r, "--add-rich-rule", false)
}

func (f *Firewall) remove(ctx context.Context, r Rule) error {
	return f.apply(ctx, r, "--remove-rich-rule", true)
}

// apply adds the rule to each store that lacks it, or removes it from each that has it.
func (f *Firewall) apply(ctx context.Context, r Rule, flag string, removal bool) error {
	verb := ""
	if removal {
		verb = "removed "
	}
	for _, store := range stores {
		present, err := f.Query(ctx, r.Zone, r.Text, store.permanent)
		if err != nil {
			return err
		}
		if present != removal {
			continue
		}
		if err = f.change(ctx, store.permanent, r.Zone, flag, r.Text); err != nil {
			return err
		}
		if err = f.changed("firewalld " + store.name + " " + r.Zone + ": " + verb + r.Text); err != nil {
			return err
		}
	}
	return nil
}
