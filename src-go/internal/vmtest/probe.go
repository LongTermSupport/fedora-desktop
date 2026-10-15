package vmtest

import (
	"context"
	"errors"
	"os"
	"strings"
)

// probe is one run of probe_upstream: its marker lines, kept in a private file the helpers
// read by path (E12).
type probe struct {
	path  string
	lines []string
}

// artefact is one `VMTEST-FRESHNESS-ARTEFACT` line: a published file a base is built from.
type artefact struct {
	Base, File, SHA256, Label, Variant, URL string
}

// probeUpstream runs the probe once. The caller removes the file with remove, on every path.
func (c *cli) probeUpstream(ctx context.Context, l *lab) (*probe, error) {
	f, err := os.CreateTemp(c.env.TempDir, "vmtest-probe.*")
	if err != nil {
		return nil, failf("cannot create the probe file: %v", err)
	}
	p := &probe{path: f.Name()}
	_, runErr := c.helper(ctx, helperCall{
		module: "probe_upstream", stdout: f,
		args: []string{flagFedoraVersion, l.fedora, "--manifest", l.at(manifestFile)},
	})
	if cerr := f.Close(); cerr != nil && runErr == nil {
		runErr = cerr
	}
	body, rerr := os.ReadFile(p.path)
	if rerr != nil {
		return nil, errors.Join(failf("cannot read the probe file: %v", rerr), p.remove())
	}
	p.lines = strings.Split(strings.TrimRight(string(body), "\n"), "\n")
	if runErr != nil {
		if ctx.Err() != nil {
			return nil, errors.Join(ctxErr(ctx, runErr), p.remove())
		}
		for _, line := range p.lines {
			if strings.HasPrefix(line, "VMTEST-FRESHNESS-UNREADABLE") {
				c.p.Print(c.env.Stderr, line+"\n")
			}
		}
		return nil, errors.Join(failf("upstream freshness signal unavailable — cannot prove what the published media is"), p.remove())
	}
	return p, nil
}

// remove deletes the probe file; nil when there is none to delete.
func (p *probe) remove() error {
	if p == nil || p.path == "" {
		return nil
	}
	if err := os.Remove(p.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return failf("cannot remove the probe file %s: %v", p.path, err)
	}
	return nil
}

// with is the probe's lines that start with prefix followed by a space, the prefix cut.
func (p *probe) with(prefix string) []string {
	var out []string
	for _, line := range p.lines {
		if rest, ok := strings.CutPrefix(line, prefix+" "); ok {
			out = append(out, rest)
		}
	}
	return out
}

// single is the value of a marker that must occur exactly once (E13, B10).
func (p *probe) single(marker string) (string, error) {
	values := p.with(marker)
	switch len(values) {
	case 0:
		return "", failf("probe printed no %s line", marker)
	case 1:
		return values[0], nil
	}
	return "", failf("probe printed %d %s lines; it must print one", len(values), marker)
}

// artefacts is every artefact the probe listed for the base, in probe order.
func (p *probe) artefacts(base string) ([]artefact, error) {
	var out []artefact
	for _, rest := range p.with("VMTEST-FRESHNESS-ARTEFACT") {
		f := strings.Fields(rest)
		if len(f) == 0 || f[0] != base {
			continue
		}
		a := artefact{Base: f[0]}
		ok := len(f) == 6
		if ok {
			a.File, a.SHA256 = f[1], f[2]
			var l, v, u bool
			a.Label, l = strings.CutPrefix(f[3], "label=")
			a.Variant, v = strings.CutPrefix(f[4], "variant=")
			a.URL, u = strings.CutPrefix(f[5], "url=")
			ok = l && v && u && strings.HasPrefix(a.URL, "https://") && a.File != "" && !strings.Contains(a.File, "/") && sha256Hex.MatchString(a.SHA256)
		}
		if !ok {
			return nil, failf("malformed probe line: VMTEST-FRESHNESS-ARTEFACT %s", rest)
		}
		out = append(out, a)
	}
	return out, nil
}

// artefactWithPrefix is the one artefact of the base whose file name starts with prefix.
func (p *probe) artefactWithPrefix(base, prefix string) (string, error) {
	arts, err := p.artefacts(base)
	if err != nil {
		return "", err
	}
	var found []string
	for _, a := range arts {
		if strings.HasPrefix(a.File, prefix) {
			found = append(found, a.File)
		}
	}
	if len(found) != 1 {
		return "", failf("the probe listed %d %s* artefacts for %s; it must list one", len(found), prefix, base)
	}
	return found[0], nil
}

// treeURL is the install tree's URL the probe read for tree.
func (p *probe) treeURL(tree string) (string, error) {
	var found []string
	for _, rest := range p.with("VMTEST-FRESHNESS-TREE-URL") {
		if name, url, ok := strings.Cut(rest, " "); ok && name == tree {
			found = append(found, url)
		}
	}
	if len(found) != 1 || !strings.HasPrefix(found[0], "https://") {
		return "", failf("the probe printed no URL for tree %s", tree)
	}
	return found[0], nil
}
