package sshkey

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// probe is E11: ssh is asked, for each destination, which identity files it would offer; any
// besides the key stops the run before anything connects.
func (r *run) probe(ctx context.Context, inv invocation) bool {
	for _, d := range inv.dests {
		args := append(append([]string{"-G"}, r.ownOptions(inv.key, true)...), d.args...)
		res, err := r.env.Runner.Run(ctx, execx.Cmd{Name: progSSH, Args: args})
		if err != nil {
			r.probeFailed(d, err)
			return false
		}
		extra := r.extraKeys(string(res.Stdout), inv.key)
		if len(extra) > 0 {
			r.failf("ssh's configuration also gives %s the key(s) %s, which ssh would offer too; "+
				"this tool offers only %s. Remove those IdentityFile lines for this host, or pass -F "+
				"with a configuration that has none. Nothing was run.",
				d.name, strings.Join(extra, ", "), r.show(inv.key))
			return false
		}
	}
	return true
}

func (r *run) probeFailed(d destination, err error) {
	if execx.IsNotInstalled(err) {
		r.failf("ssh is not installed.")
		return
	}
	r.failf("ssh could not read its configuration for %s: %v. Nothing was run.", d.name, err)
}

// extraKeys are the identity files of `ssh -G`'s output other than key that ssh may offer, as
// the person reads them.
func (r *run) extraKeys(config, key string) []string {
	own := r.absolute(key)
	var extra []string
	for line := range strings.Lines(config) {
		path, ok := strings.CutPrefix(strings.TrimRight(line, "\r\n"), "identityfile ")
		if !ok || path == key {
			continue
		}
		expanded, plain := r.expand(path)
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

// expand is a configured identity path with "~" and %d made the home directory and %% a
// percent sign; false when another token is left that only ssh can expand.
func (r *run) expand(path string) (string, bool) {
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
		switch path[i] {
		case 'd':
			b.WriteString(r.home)
		case '%':
			b.WriteByte('%')
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
