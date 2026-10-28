package sshkey

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// The argument-taking letters of each program's options (E13): a letter after one of these in
// a cluster is that option's value, not a flag.
const (
	sshValueLetters = "BbcDEeFIiJLlmOoPpQRSWw"
	scpValueLetters = "cDFiJlMoPSX"
)

// The values of a yes-or-no ssh option.
const (
	yes = "yes"
	no  = "no"
)

// sshNoCommandLetters are ssh's modes that run no remote command (E18).
const sshNoCommandLetters = "NWOGVQs"

var (
	errSecondKey  = errors.New("is a second key; give the one key as the first argument")
	errOtherAgent = errors.New("would forward an agent this tool did not start; use -A for a new one")
	errOwnAgent   = errors.New("is this tool's to set: it uses no agent, or with -A only the one it starts")
	// errOtherProgram is scp's -S: the probe asks ssh, not the program scp would run (E13).
	errOtherProgram = errors.New("would run another program as ssh, which the check of ssh's configuration cannot ask")
	errNoInstallKey = errors.New("-i FILE, the public key to install, is required")
)

// invocation is a command line read (E3, E4, E11, E13).
type invocation struct {
	// key is the key flag's value, "" when the picker is to choose (E6).
	key string
	// passed are the arguments after the key flag, given to the program as they are.
	passed []string
	// forward is agent forwarding asked (E13).
	forward bool
	// command is an ssh remote command given; noCommand a mode that runs none (E18).
	command, noCommand bool
	// dests are what the identity probe asks ssh about (E11).
	dests []destination
	// install is ssh-copy-id's -i, the key to install; force its -f (E20).
	install string
	force   bool
}

// destination is one host the probe asks `ssh -G` about: its name in messages, and the
// arguments after the tool's options that make ssh resolve its configuration.
type destination struct {
	name string
	args []string
}

// parse reads a command line whose first argument is not --help.
func (t Tool) parse(args []string) (invocation, error) {
	var inv invocation
	rest := args
	if len(rest) > 0 && rest[0] == t.KeyFlag {
		if len(rest) < 2 || rest[1] == "" {
			return inv, fmt.Errorf("%s needs a key path", t.KeyFlag)
		}
		inv.key, rest = rest[1], rest[2:]
	}
	inv.passed = rest
	var err error
	switch t.kind {
	case kindSSH:
		err = inv.readSSH()
	case kindSCP:
		err = inv.readSCP()
	case kindCopyID:
		err = inv.readCopyID()
	}
	return inv, err
}

// agentAsk collects what asks for forwarding: the last -A or -a flag, else the first
// ForwardAgent option (E13).
type agentAsk struct {
	flag     byte
	value    string
	optioned bool
}

func (a *agentAsk) forward() bool {
	if a.flag != 0 {
		return a.flag == 'A'
	}
	return a.value == yes
}

// option judges one -o value: a second key and an agent named are refused, ForwardAgent is
// recorded (E4, E13).
func (a *agentAsk) option(value string) error {
	key, val := splitOption(value)
	switch {
	case strings.EqualFold(key, "IdentityFile"):
		return fmt.Errorf("-o %s %w", value, errSecondKey)
	case strings.EqualFold(key, "IdentityAgent"):
		return fmt.Errorf("-o %s %w", value, errOwnAgent)
	case strings.EqualFold(key, "ForwardAgent"):
		val = strings.ToLower(val)
		if val != yes && val != no {
			return fmt.Errorf("-o %s %w", value, errOtherAgent)
		}
		if !a.optioned {
			a.value, a.optioned = val, true
		}
	}
	return nil
}

// splitOption is an ssh -o value's keyword and argument: "Key=value", "Key value" or
// "Key = value".
func splitOption(s string) (string, string) {
	s = strings.TrimLeft(s, " \t")
	i := strings.IndexAny(s, "= \t")
	if i < 0 {
		return s, ""
	}
	return s[:i], strings.TrimLeft(s[i:], "= \t")
}

// options is what one program's option letters mean: flag takes a flag letter, value an
// argument-taking letter and its value.
type options interface {
	flag(letter byte)
	value(letter byte, v string) error
}

// cluster reads the option token args[i] (a dash and its letters): each flag letter is given to
// o.flag, and the first argument-taking letter to o.value with the rest of the token or the next
// one. It returns how many tokens it used.
func cluster(args []string, i int, letters string, o options) (int, error) {
	tok := args[i][1:]
	for j := range len(tok) {
		ch := tok[j]
		if strings.IndexByte(letters, ch) < 0 {
			o.flag(ch)
			continue
		}
		switch {
		case j+1 < len(tok):
			return 1, o.value(ch, tok[j+1:])
		case i+1 < len(args):
			return 2, o.value(ch, args[i+1])
		default:
			return 1, o.value(ch, "")
		}
	}
	return 1, nil
}

func isOption(tok string) bool { return len(tok) > 1 && tok[0] == '-' }

// sshOptions are ssh's option letters (E4, E13, E18).
type sshOptions struct {
	inv *invocation
	ask agentAsk
}

func (o *sshOptions) flag(ch byte) {
	if ch == 'A' || ch == 'a' {
		o.ask.flag = ch
	}
	if strings.IndexByte(sshNoCommandLetters, ch) >= 0 {
		o.inv.noCommand = true
	}
}

func (o *sshOptions) value(ch byte, v string) error {
	if strings.IndexByte(sshNoCommandLetters, ch) >= 0 {
		o.inv.noCommand = true
	}
	switch ch {
	case 'i':
		return fmt.Errorf("-i %s %w", v, errSecondKey)
	case 'o':
		return o.ask.option(v)
	}
	return nil
}

// readSSH is ssh's grammar: options, the destination, more options, then the remote command;
// `--` ends the options (E13).
func (inv *invocation) readSSH() error {
	o := &sshOptions{inv: inv}
	p := inv.passed
	dest := ""
	for i := 0; i < len(p); {
		switch tok := p[i]; {
		case tok == "--":
			i++
			if dest == "" && i < len(p) {
				dest = p[i]
				i++
			}
			inv.command = i < len(p)
			i = len(p)
		case isOption(tok):
			n, err := cluster(p, i, sshValueLetters, o)
			if err != nil {
				return err
			}
			i += n
		case dest == "":
			dest = tok
			i++
		default:
			inv.command = true
			i = len(p)
		}
	}
	inv.forward = o.ask.forward()
	if dest != "" {
		inv.dests = []destination{{name: dest, args: p}}
	}
	return nil
}

// scpOptions are scp's option letters; probe gathers the ones that decide ssh's configuration
// (E11).
type scpOptions struct {
	ask   agentAsk
	probe []string
}

func (o *scpOptions) flag(ch byte) {
	switch ch {
	case 'A':
		o.ask.flag = ch
	case '4', '6':
		o.probe = append(o.probe, "-"+string(ch))
	}
}

func (o *scpOptions) value(ch byte, v string) error {
	switch ch {
	case 'i':
		return fmt.Errorf("-i %s %w", v, errSecondKey)
	case 'o':
		if err := o.ask.option(v); err != nil {
			return err
		}
		o.probe = append(o.probe, "-o", v)
	case 'S':
		return fmt.Errorf("-S %s %w", v, errOtherProgram)
	case 'F', 'J':
		o.probe = append(o.probe, "-"+string(ch), v)
	case 'P':
		o.probe = append(o.probe, "-p", v)
	}
	return nil
}

// readSCP is scp's grammar: options, then the operands (E11, E13).
func (inv *invocation) readSCP() error {
	o := &scpOptions{}
	p := inv.passed
	i := 0
	for i < len(p) && isOption(p[i]) {
		if p[i] == "--" {
			i++
			break
		}
		n, err := cluster(p, i, scpValueLetters, o)
		if err != nil {
			return err
		}
		i += n
	}
	inv.forward = o.ask.forward()
	for _, op := range p[i:] {
		if r, ok := remoteOperand(op); ok {
			inv.addDestination(r.destination(o.probe))
		}
	}
	return nil
}

// addDestination adds d unless the same probe is already there.
func (inv *invocation) addDestination(d destination) {
	if !slices.ContainsFunc(inv.dests, func(e destination) bool { return slices.Equal(e.args, d.args) }) {
		inv.dests = append(inv.dests, d)
	}
}

// remote is the host part of an scp operand.
type remote struct{ user, host, port string }

// destination is the probe of r after the options that decide ssh's configuration.
func (r remote) destination(probe []string) destination {
	d := destination{name: r.host, args: slices.Clone(probe)}
	if r.user != "" {
		d.args = append(d.args, "-l", r.user)
	}
	if r.port != "" {
		d.args = append(d.args, "-p", r.port)
	}
	d.args = append(d.args, r.host)
	return d
}

// remoteOperand reads an scp operand: scp://[user@]host[:port][/path], [user@]host:path or
// [user@][v6address]:path. A colon after a slash is a local path's.
func remoteOperand(op string) (remote, bool) {
	var r remote
	if strings.HasPrefix(op, "scp://") {
		u, err := url.Parse(op)
		if err != nil {
			return remote{}, false
		}
		r.host, r.port = u.Hostname(), u.Port()
		if u.User != nil {
			r.user = u.User.Username()
		}
		return r, r.host != ""
	}
	if open := strings.IndexByte(op, '['); open >= 0 && (open == 0 || op[open-1] == '@') {
		end := strings.IndexByte(op[open:], ']') + open
		if end <= open || end+1 >= len(op) || op[end+1] != ':' {
			return remote{}, false
		}
		if open > 0 {
			r.user = op[:open-1]
		}
		r.host = op[open+1 : end]
		return r, r.host != ""
	}
	colon := strings.IndexByte(op, ':')
	if colon <= 0 || strings.ContainsRune(op[:colon], '/') {
		return remote{}, false
	}
	hostPart := op[:colon]
	if at := strings.LastIndexByte(hostPart, '@'); at >= 0 {
		r.user, hostPart = hostPart[:at], hostPart[at+1:]
	}
	r.host = hostPart
	return r, r.host != ""
}

// readCopyID is ssh-copy-id's grammar: options, then the destination last (E4, E11, E20).
func (inv *invocation) readCopyID() error {
	p := inv.passed
	if len(p) == 0 {
		return errNoInstallKey
	}
	var ask agentAsk
	var probe []string
	opts, dest := p[:len(p)-1], p[len(p)-1]
	for i := 0; i < len(opts); i++ {
		n, args, err := inv.copyIDOption(opts, i, &ask)
		if err != nil {
			return err
		}
		probe = append(probe, args...)
		i += n
	}
	if inv.install == "" {
		return errNoInstallKey
	}
	if !strings.HasPrefix(dest, "-") {
		inv.dests = []destination{{name: dest, args: append(probe, dest)}}
	}
	return nil
}

// copyIDOptionLetters are ssh-copy-id's argument-taking letters.
const copyIDOptionLetters = "iopFt"

// copyIDOption reads the ssh-copy-id option cluster at opts[i]: how many more tokens its value
// used, and what the probe takes from it (-o, -p, -F). -f is recorded, and -i's value is the key
// to install, which is optional to ssh-copy-id: a word that is not an option.
func (inv *invocation) copyIDOption(opts []string, i int, ask *agentAsk) (int, []string, error) {
	tok := opts[i]
	if !isOption(tok) {
		return 0, nil, nil
	}
	for j := 1; j < len(tok); j++ {
		letter := tok[j]
		if strings.IndexByte(copyIDOptionLetters, letter) < 0 {
			inv.force = inv.force || letter == 'f'
			continue
		}
		v, used := tok[j+1:], 0
		if v == "" && i+1 < len(opts) && (letter != 'i' || !strings.HasPrefix(opts[i+1], "-")) {
			v, used = opts[i+1], 1
		}
		switch letter {
		case 'i':
			if inv.install == "" {
				inv.install = v
			}
			return used, nil, nil
		case 't':
			return used, nil, nil
		case 'o':
			if err := ask.option(v); err != nil {
				return used, nil, err
			}
		}
		return used, []string{"-" + string(letter), v}, nil
	}
	return 0, nil, nil
}
