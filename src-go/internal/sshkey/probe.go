package sshkey

import (
	"context"
	"errors"
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// probe is E11: ssh is asked, for each destination, which identity files it would offer; any
// besides the key stops the run before anything connects. A forwarded run's probe leaves the
// agent to the configuration, which must not name one (E13).
func (r *run) probe(ctx context.Context, inv invocation) bool {
	for _, d := range inv.dests {
		config, ok := r.askConfig(ctx, d, append(r.ownOptions(inv.key, !inv.forward), d.args...))
		if !ok {
			return false
		}
		if extra := r.extraKeys(config, inv.key); len(extra) > 0 {
			r.failf("ssh's configuration also gives %s the key(s) %s, which ssh would offer too; "+
				"this tool offers only %s. Remove those IdentityFile lines for this host, or pass -F "+
				"with a configuration that has none. Nothing was run.",
				d.name, strings.Join(extra, ", "), r.show(inv.key))
			return false
		}
		if line, named := namedAgent(config); inv.forward && named {
			r.failf("ssh's configuration gives %s %s, so -A would forward that agent, not the one this run "+
				"starts. Remove that line for this host, or pass -F with a configuration that has none. Nothing was run.",
				d.name, line)
			return false
		}
	}
	return true
}

// askConfig is `ssh -G` for d with args: the configuration ssh would use, which connects to
// nothing. -T keeps ssh's note that stdin is not a terminal out of a failure's message.
func (r *run) askConfig(ctx context.Context, d destination, args []string) (string, bool) {
	res, err := r.env.Runner.Run(ctx, execx.Cmd{Name: progSSH, Args: append([]string{"-G", "-T"}, args...)})
	if err != nil {
		r.probeFailed(d, err)
		return "", false
	}
	return string(res.Stdout), true
}

func (r *run) probeFailed(d destination, err error) {
	if execx.IsNotInstalled(err) {
		r.failf("ssh is not installed.")
		return
	}
	r.failf("ssh could not read its configuration for %s: %v. Nothing was run.", d.name, err)
}

// configLines are `ssh -G` output's keyword and value pairs.
func configLines(config string) iter.Seq2[string, string] {
	return func(yield func(string, string) bool) {
		for line := range strings.Lines(config) {
			key, value, _ := strings.Cut(strings.TrimRight(line, "\r\n"), " ")
			if !yield(key, value) {
				return
			}
		}
	}
}

// namedAgent is a configured agent socket a forwarded login would use instead of the run's own:
// an identityagent other than the environment's, or a forwardagent that is a path (E13).
func namedAgent(config string) (string, bool) {
	for key, value := range configLines(config) {
		if key == "identityagent" && value != "SSH_AUTH_SOCK" ||
			key == "forwardagent" && value != yes && value != no {
			return key + " " + value, true
		}
	}
	return "", false
}

// extraKeys are the identity files of `ssh -G`'s output other than key that ssh may offer, as
// the person reads them.
func (r *run) extraKeys(config, key string) []string {
	own := r.absolute(key)
	tokens := connectionTokens(config)
	var extra []string
	for k, path := range configLines(config) {
		if k != "identityfile" || path == key || path == "none" {
			continue
		}
		expanded, plain := r.expand(path, tokens)
		if !plain {
			extra = append(extra, path)
			continue
		}
		if r.absolute(expanded) == own || !mayExist(expanded) {
			continue
		}
		extra = append(extra, r.show(r.absolute(expanded)))
	}
	return extra
}

// connectionTokens are the % tokens of an identity path that `ssh -G`'s own lines give: %h the
// host name, %n the host as given, %r the remote user, %p the port.
func connectionTokens(config string) map[byte]string {
	names := map[string]byte{"hostname": 'h', "host": 'n', "user": 'r', "port": 'p'}
	tokens := map[byte]string{}
	for key, value := range configLines(config) {
		if t, ok := names[key]; ok {
			if _, seen := tokens[t]; !seen {
				tokens[t] = value
			}
		}
	}
	return tokens
}

// expand is a configured identity path with "~" and %d made the home directory, %% a percent
// sign and the connection's tokens their values; false when another token is left that only
// ssh can expand.
func (r *run) expand(path string, tokens map[byte]string) (string, bool) {
	path = term.ExpandHome(path, r.home)
	var b strings.Builder
	for i := 0; i < len(path); i++ {
		if path[i] != '%' {
			b.WriteByte(path[i])
			continue
		}
		if i+1 >= len(path) {
			return "", false
		}
		i++
		value, known := tokens[path[i]]
		switch {
		case path[i] == 'd':
			b.WriteString(r.home)
		case path[i] == '%':
			b.WriteByte('%')
		case known:
			b.WriteString(value)
		default:
			return "", false
		}
	}
	return b.String(), true
}

// absolute is path made absolute and clean, so two spellings of one file compare equal; a path
// that cannot be made absolute stays as it is.
func (r *run) absolute(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

// mayExist reports whether ssh could offer path: the key or its public half is there, or
// whether it is cannot be told.
func mayExist(path string) bool {
	for _, p := range []string{path, path + ".pub"} {
		_, err := os.Stat(p)
		if !errors.Is(err, fs.ErrNotExist) {
			return true
		}
	}
	return false
}
