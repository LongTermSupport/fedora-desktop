// Package vaultlist is the vault-list command: it adds items to a host_vars list whose every
// item is vault-encrypted (such as private_name_terms, the private-name guard's owner names).
//
//   - V1 Each value is asked with the terminal's echo off, one at a time, until an empty line; a
//     bad value (a control character, a leading #) is refused and asked again, at most three
//     times. A value is never printed, logged or put in an argv: it reaches vault.bash and
//     ansible-vault on stdin only. Only counts are printed.
//   - V2 --import FILE adds the values of a file first, one per line (blank and # lines
//     skipped); a bad line is refused by its number, never its value.
//   - V3 The list as read must be absent, null, or a sequence of !vault items; each item is
//     decrypted (ansible-vault decrypt) so a value already listed, in any case, is not added
//     twice. Nothing new is a success that leaves the file alone: a second run is safe.
//   - V4 Each new value is encrypted by vault.bash encrypt (the repository's own route, under
//     the vault id ansible.cfg names). The list's block alone is rewritten in the text, with
//     the items read kept as they were and the new ones after them; the edit is read back
//     (every other key the same) before it is written, and refused if the file changed while
//     the run asked its questions.
//   - V5 The written file is read again and every item decrypted: the run succeeds only when
//     the list holds exactly the values it should.
//   - V6 It runs on the host only, from the checkout scripts/vault-list.bash names
//     (VAULT_LIST_ROOT).
package vaultlist

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"go.yaml.in/yaml/v3"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ccyenv"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/hostvars"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ttyecho"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

const (
	tool         = "vault-list"
	rootEnv      = "VAULT_LIST_ROOT"
	hostVarsPath = "environment/localhost/host_vars/localhost.yml"
	itemKey      = "item"
	// maxAsked bounds the values one run asks for.
	maxAsked     = 100
	headingWidth = 80
)

const usageText = `Usage: vault-list add VARIABLE [--import FILE]

Adds values to VARIABLE in environment/localhost/host_vars/localhost.yml, a list whose every
item is vault-encrypted (for example private_name_terms). Each value is asked with the echo
off, one at a time; an empty line ends. --import FILE adds the values of FILE first, one per
line (blank lines and lines starting with # are skipped). A value already in the list is not
added twice. Each value is encrypted with ./vault.bash encrypt; nothing else in the file
changes, and the list is decrypted again after the write to check it. Only counts are printed:
stdout is "VARIABLE: N items, M added".

Run it on the host as scripts/vault-list.bash, which builds it from this checkout.
`

var variableGrammar = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// Env is everything a run touches outside its own memory.
type Env struct {
	Runner      execx.Runner
	Getenv      func(string) string
	InContainer func() bool
	ReadFile    func(string) ([]byte, error)
	// WriteFile replaces host_vars whole, its mode kept.
	WriteFile      func(string, []byte) error
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	// EchoOff turns the terminal's echo off for one answer (ttyecho.Off on stdin).
	EchoOff func() (restore func() error, err error)
	// IsTerminal tells a terminal stdin from a pipe (term.FileIsTerminal when nil).
	IsTerminal func(any) bool
}

// NewEnv is the production environment.
func NewEnv() Env {
	return Env{
		Runner: execx.OS{}, Getenv: os.Getenv, InContainer: ccyenv.InContainer,
		ReadFile: os.ReadFile, WriteFile: hostvars.WriteKeepingMode,
		Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
		EchoOff: ttyecho.Off(int(os.Stdin.Fd())),
	}
}

// App is one vault-list invocation.
type App struct{ Env Env }

// errReported is a failure already explained on stderr.
var errReported = errors.New("failure reported")

type run struct {
	env        Env
	p          term.Printer
	lines      *ui.Lines
	root, vars string
	variable   string
	importFile string
}

// Run is the whole command; it returns the exit code.
func (a App) Run(ctx context.Context, args []string) term.ExitCode {
	r := &run{env: a.Env}
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
		r.p.Print(r.env.Stdout, usageText)
		return r.finish(term.ExitOK)
	}
	if err := r.parse(args); err != nil {
		r.p.Print(r.env.Stderr, tool+": "+err.Error()+" — see '"+tool+" --help'.\n")
		return term.ExitUsage
	}
	r.lines = &ui.Lines{
		In: r.env.Stdin, Out: r.env.Stderr, Width: headingWidth, EchoOff: r.env.EchoOff,
		Prog: tool, Err: r.env.Stderr, IsTerminal: r.env.IsTerminal,
	}
	return r.finish(r.exitFor(r.main(ctx)))
}

func (r *run) parse(args []string) error {
	if len(args) < 2 || args[0] != "add" {
		return errors.New("the one verb is: add VARIABLE [--import FILE]")
	}
	if !variableGrammar.MatchString(args[1]) {
		return fmt.Errorf("%q is not a variable name (lower case letters, digits and _)", args[1])
	}
	r.variable = args[1]
	switch rest := args[2:]; {
	case len(rest) == 0:
	case len(rest) == 2 && rest[0] == "--import" && rest[1] != "":
		r.importFile = rest[1]
	default:
		return errors.New("after VARIABLE only --import FILE is taken")
	}
	return nil
}

func (r *run) finish(code term.ExitCode) term.ExitCode {
	if r.p.Err() != nil && code == term.ExitOK {
		return term.ExitFailure
	}
	return code
}

func (r *run) exitFor(err error) term.ExitCode {
	if err == nil {
		return term.ExitOK
	}
	var gaveUp *ui.GaveUpError
	switch {
	case errors.Is(err, ui.ErrInputEnded):
		r.say("✗ The input ended before an empty line; nothing was changed")
	case errors.As(err, &gaveUp):
		r.say("✗ " + err.Error() + "; nothing was changed")
	case !errors.Is(err, errReported):
		r.say("✗ " + err.Error())
	}
	return term.ExitFailure
}

func (r *run) say(text string) { r.p.Print(r.env.Stderr, text+"\n") }

func (r *run) failf(format string, args ...any) error {
	r.say("✗ " + fmt.Sprintf(format, args...))
	return errReported
}

func (r *run) main(ctx context.Context) error {
	if r.env.InContainer() {
		return r.failf("vault-list runs on the host, not in a CCY container (the vault password is not here)")
	}
	r.root = r.env.Getenv(rootEnv)
	if fi, err := os.Stat(r.root); r.root == "" || !filepath.IsAbs(r.root) || err != nil || !fi.IsDir() {
		return r.failf("%s is not this checkout's directory; run scripts/vault-list.bash, which sets it", rootEnv)
	}
	r.vars = filepath.Join(r.root, hostVarsPath)
	text, err := r.env.ReadFile(r.vars)
	if err != nil {
		return r.failf("cannot read %s: %v", hostVarsPath, err)
	}
	before, err := hostvars.TopMapping(text)
	if err != nil {
		return r.failf("%s: %v", hostVarsPath, err)
	}
	items, err := r.listItems(before)
	if err != nil {
		return err
	}
	known, err := r.decryptAll(ctx, items)
	if err != nil {
		return err
	}
	added, err := r.collect(ctx, known)
	if err != nil {
		return err
	}
	if len(added) == 0 {
		r.say("i Nothing new to add; " + hostVarsPath + " is unchanged")
		r.p.Print(r.env.Stdout, r.summary(len(items), 0))
		return nil
	}
	nodes, err := r.encryptAll(ctx, added)
	if err != nil {
		return err
	}
	if err := r.write(text, before, items, nodes); err != nil {
		return err
	}
	if err := r.check(ctx, append(known, added...)); err != nil {
		return err
	}
	r.say("✓ " + strconv.Itoa(len(added)) + " added to " + r.variable + ", checked by decrypting the list again")
	r.p.Print(r.env.Stdout, r.summary(len(items)+len(added), len(added)))
	return nil
}

func (r *run) summary(items, added int) string {
	return r.variable + ": " + strconv.Itoa(items) + " items, " + strconv.Itoa(added) + " added\n"
}

// listItems is the variable's items: none when it is absent or null, else every item a !vault
// scalar (V3).
func (r *run) listItems(top *yaml.Node) ([]*yaml.Node, error) {
	v := hostvars.TopValue(top, r.variable)
	if v == nil || (v.Kind == yaml.ScalarNode && v.ShortTag() == "!!null") {
		return nil, nil
	}
	if v.Kind != yaml.SequenceNode {
		return nil, r.failf("%s in %s is not a list; it must be a list of vault-encrypted items", r.variable, hostVarsPath)
	}
	for i, item := range v.Content {
		if item.Kind != yaml.ScalarNode || item.Tag != hostvars.VaultTag {
			return nil, r.failf("%s item %d in %s is not vault-encrypted (no value is shown); encrypt it or remove it first", r.variable, i+1, hostVarsPath)
		}
	}
	return v.Content, nil
}

// decryptAll is each item's value, in order.
func (r *run) decryptAll(ctx context.Context, items []*yaml.Node) ([]string, error) {
	out := make([]string, 0, len(items))
	for i, item := range items {
		plain, err := r.decrypt(ctx, item.Value)
		if err != nil {
			return nil, r.failf("%s item %d cannot be decrypted: %v", r.variable, i+1, err)
		}
		out = append(out, plain)
	}
	return out, nil
}

func (r *run) decrypt(ctx context.Context, ciphertext string) (string, error) {
	res, err := r.env.Runner.Run(ctx, execx.Cmd{
		Name:  "ansible-vault",
		Args:  []string{"decrypt", "--output", "-"},
		Dir:   r.root,
		Env:   []string{"ANSIBLE_CONFIG=" + filepath.Join(r.root, "ansible.cfg")},
		Stdin: strings.NewReader(ciphertext),
	})
	if err != nil {
		return "", fmt.Errorf("ansible-vault decrypt failed: %w", err)
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}

// collect is the new values: the import file's, then those asked for (V1, V2), none already
// known or given twice.
func (r *run) collect(ctx context.Context, known []string) ([]string, error) {
	seen := map[string]bool{}
	for _, k := range known {
		seen[strings.ToLower(k)] = true
	}
	var added []string
	take := func(name string) {
		if key := strings.ToLower(name); seen[key] {
			r.say("i That value is already listed; it is not added twice")
		} else {
			seen[key] = true
			added = append(added, name)
		}
	}
	if r.importFile != "" {
		names, err := r.readImport()
		if err != nil {
			return nil, err
		}
		for _, n := range names {
			take(n)
		}
		r.say("i " + strconv.Itoa(len(names)) + " value(s) read from the import file")
	}
	r.say("Enter each value to add to " + r.variable + "; nothing you type is shown. An empty line ends.")
	h := ui.Here(tool, "add", r.variable)
	for n := 1; n <= maxAsked; n++ {
		name, err := r.lines.Secret(ctx, h, "Value "+strconv.Itoa(n)+" (empty line to finish)", func(s string) error {
			if s == "" {
				return nil
			}
			return checkName(s)
		})
		if err != nil {
			return nil, err
		}
		if name == "" {
			return added, nil
		}
		take(name)
	}
	r.say("i " + strconv.Itoa(maxAsked) + " values is the most one run asks for; run it again for more")
	return added, nil
}

// readImport is the import file's values (V2).
func (r *run) readImport() ([]string, error) {
	data, err := r.env.ReadFile(r.importFile)
	if err != nil {
		return nil, r.failf("cannot read the import file: %v", err)
	}
	var names []string
	sc := bufio.NewScanner(bytes.NewReader(data))
	for i := 1; sc.Scan(); i++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if err := checkName(line); err != nil {
			return nil, r.failf("line %d of the import file: %v; nothing was changed", i, err)
		}
		names = append(names, line)
	}
	if err := sc.Err(); err != nil {
		return nil, r.failf("cannot read the import file: %v", err)
	}
	return names, nil
}

// checkName refuses a value a list reader would not take as one value on one line.
func checkName(s string) error {
	if strings.HasPrefix(s, "#") {
		return errors.New("a value cannot start with # (a list reader takes that line for a comment)")
	}
	for _, c := range s {
		if unicode.IsControl(c) {
			return errors.New("a value is one line of printable text (no tab or control character)")
		}
	}
	return nil
}

// encryptAll is each value encrypted by vault.bash, as a !vault node (V4).
func (r *run) encryptAll(ctx context.Context, names []string) ([]*yaml.Node, error) {
	out := make([]*yaml.Node, 0, len(names))
	for i, name := range names {
		res, err := r.env.Runner.Run(ctx, execx.Cmd{
			Name:  filepath.Join(r.root, "vault.bash"),
			Args:  []string{"encrypt", itemKey},
			Dir:   r.root,
			Stdin: strings.NewReader(name),
		})
		if err != nil {
			return nil, r.failf("vault.bash encrypt failed on value %d: %v; nothing was changed", i+1, err)
		}
		v, err := hostvars.ParseVault(res.Stdout, itemKey)
		if err != nil {
			return nil, r.failf("value %d: %v; nothing was changed", i+1, err)
		}
		out = append(out, &yaml.Node{Kind: yaml.ScalarNode, Tag: hostvars.VaultTag, Value: v.Value, Style: yaml.LiteralStyle})
	}
	return out, nil
}

// write replaces the variable's block with the items read and the new ones, read back before
// it is written, unless the file changed during the run (V4).
func (r *run) write(text []byte, before *yaml.Node, items, added []*yaml.Node) error {
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: append(append([]*yaml.Node(nil), items...), added...)}
	key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: r.variable}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := errors.Join(enc.Encode(&yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{key, seq}}), enc.Close()); err != nil {
		return r.failf("cannot write %s: %v; nothing was changed", r.variable, err)
	}
	lines := hostvars.ReplaceBlock(hostvars.SplitLines(string(text)), r.variable, hostvars.SplitLines(buf.String()))
	out := []byte(strings.Join(lines, ""))
	after, err := hostvars.TopMapping(out)
	if err == nil {
		err = hostvars.SameOthers(before, after, r.variable)
	}
	if err == nil && !hostvars.SameNode(hostvars.TopValue(after, r.variable), seq) {
		err = errors.New(r.variable + " does not read back as intended")
	}
	if err != nil {
		return r.failf("the edit would not leave %s as intended (%v); nothing was changed", hostVarsPath, err)
	}
	now, err := r.env.ReadFile(r.vars)
	if err != nil {
		return r.failf("cannot read %s again before writing it: %v", hostVarsPath, err)
	}
	if !bytes.Equal(now, text) {
		return r.failf("%s changed while vault-list ran; nothing was written. Run it again", hostVarsPath)
	}
	if err := r.env.WriteFile(r.vars, out); err != nil {
		return r.failf("%v", err)
	}
	return nil
}

// check reads the written file and decrypts the list: it must hold exactly want (V5).
func (r *run) check(ctx context.Context, want []string) error {
	text, err := r.env.ReadFile(r.vars)
	if err != nil {
		return r.failf("cannot read %s back to check it: %v", hostVarsPath, err)
	}
	top, err := hostvars.TopMapping(text)
	if err != nil {
		return r.failf("%s as written: %v", hostVarsPath, err)
	}
	items, err := r.listItems(top)
	if err != nil {
		return err
	}
	got, err := r.decryptAll(ctx, items)
	if err != nil {
		return err
	}
	if len(got) != len(want) {
		return r.failf("%s as written holds %d items, not %d; check it with ./vault.bash", r.variable, len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			return r.failf("%s item %d as written does not decrypt back to the value given; check it with ./vault.bash", r.variable, i+1)
		}
	}
	return nil
}
